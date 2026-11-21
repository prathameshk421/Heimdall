package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/github"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/uidsandbox"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

// The exit statuses of the action subcommands. A step that fails makes its workflow run fail, which is what the
// gate's sweep and the required check read: a failure is never a pass.
const (
	exitActionOK    = 0
	exitActionFail  = 1
	exitActionUsage = 2
	// githubTimeout bounds one GitHub call of an action step.
	githubTimeout = 2 * time.Minute
)

// actionDeps are the parts of the world an action step reaches. Tests replace each one; production uses
// realActionDeps.
type actionDeps struct {
	getenv func(string) string
	// http reaches GitHub and the model endpoint.
	http *http.Client
	wait func(ctx context.Context, d time.Duration) error
	// sandbox builds and sets up the sandbox of a proof run. The returned cleanup removes the user's processes, its
	// firewall table and the proxy.
	sandbox func(ctx context.Context, cfg uidsandbox.Config) (sandboxJobs, func(context.Context) error, error)
	// gh runs the maintainer's own gh CLI, for action init only.
	gh ghRunner
	// newKey makes the state log's MAC key, for action init only.
	newKey func() ([]byte, error)
}

func realActionDeps() actionDeps {
	return actionDeps{getenv: config.Getenv, http: &http.Client{Timeout: githubTimeout}, wait: clock.Wait,
		sandbox: realSandbox, gh: execGH{}, newKey: randomLogKey}
}

// realSandbox checks the runner and sets the sandbox up. A runner that cannot isolate returns an error that
// errors.Is(err, uidsandbox.ErrCannotIsolate) recognises.
func realSandbox(ctx context.Context, cfg uidsandbox.Config) (sandboxJobs, func(context.Context) error, error) {
	s, err := uidsandbox.New(ctx, cfg, uidsandbox.OSExec{})
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox: %w", err)
	}
	if err := s.Setup(ctx); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("sandbox setup: %w", err), s.Cleanup(context.WithoutCancel(ctx)))
	}
	return s, s.Cleanup, nil
}

// runAction is `heimdall action <step>`. The workflows of ACTION_MODE 3.2 run trigger, proof and gate; a maintainer
// runs init on their own machine.
func runAction(args []string, stdin io.Reader, stdout, stderr io.Writer, deps actionDeps) int {
	if len(args) == 0 {
		writeDiagnostic(stderr, "usage: heimdall action trigger|proof|gate|init [flags]\n")
		return exitActionUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	step, rest := args[0], args[1:]
	switch step {
	case "trigger":
		return runActionTrigger(ctx, rest, stdout, stderr, deps)
	case "proof":
		return runActionProof(ctx, rest, stdout, stderr, deps)
	case "gate":
		return runActionGate(ctx, rest, stdout, stderr, deps)
	case "init":
		return runActionInit(ctx, rest, stdin, stdout, stderr, deps)
	}
	writeDiagnostic(stderr, fmt.Sprintf("heimdall action: unknown step %q\n", clipText(step, 40)))
	return exitActionUsage
}

// actionFail reports a failed step on stderr and returns the failure status. The error never holds a secret: config
// redacts its values and the adapters name a variable, never its value.
func actionFail(stderr io.Writer, step string, err error) int {
	writeDiagnostic(stderr, fmt.Sprintf("heimdall action %s: %v\n", step, err))
	return exitActionFail
}

// readEvent reads the event file GitHub wrote for this run, bounded.
func readEvent(p string) ([]byte, error) {
	f, err := os.Open(p) // #nosec G304 -- GITHUB_EVENT_PATH, the event file the runner wrote for this run
	if err != nil {
		return nil, fmt.Errorf("open the event file: %w", err)
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxEventBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read the event file: %w", err)
	}
	return raw, nil
}

// outFlag is the one flag of the trigger and proof steps: where the record goes. The workflow uploads that file.
func outFlag(step string, args []string, def string, stderr io.Writer) (string, bool) {
	fs := flag.NewFlagSet("heimdall action "+step, flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", def, "the file the record is written to")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 || *out == "" {
		return "", false
	}
	return *out, true
}

// runActionTrigger records which pull request and head the event is about. It reads no token and no secret.
func runActionTrigger(_ context.Context, args []string, stdout, stderr io.Writer, deps actionDeps) int {
	out, ok := outFlag("trigger", args, triggerFile, stderr)
	if !ok {
		return exitActionUsage
	}
	env, err := config.LoadAction(deps.getenv, config.ActionTrigger)
	if err != nil {
		return actionFail(stderr, "trigger", err)
	}
	raw, err := readEvent(env.EventPath)
	if err != nil {
		return actionFail(stderr, "trigger", err)
	}
	rec, err := triggerFromEvent(env.EventName, raw)
	if err != nil {
		return actionFail(stderr, "trigger", err)
	}
	body, err := encodeTriggerRecord(rec)
	if err != nil {
		return actionFail(stderr, "trigger", err)
	}
	if err := os.WriteFile(out, body, 0o600); err != nil {
		return actionFail(stderr, "trigger", fmt.Errorf("write %s: %w", out, err))
	}
	writeDiagnostic(stdout, fmt.Sprintf("heimdall action trigger: %s #%d at %s\n", rec.Event, rec.Number, rec.Head))
	return exitActionOK
}

// actionGitHub builds the Action operations on the workflow's token. The gate's token writes; the proof's reads.
func actionGitHub(env config.ActionEnv, deps actionDeps) (*github.ActionAPI, change.Repo, error) {
	repo, err := change.ParseRepo(env.Repo)
	if err != nil {
		return nil, "", fmt.Errorf("GITHUB_REPOSITORY: %w", err)
	}
	tokens, err := github.NewStaticToken(env.Token)
	if err != nil {
		return nil, "", fmt.Errorf("GITHUB_TOKEN: %w", err)
	}
	api, err := github.NewAction(github.ActionConfig{BaseURL: env.APIURL, Tokens: tokens, HTTPClient: deps.http,
		Clock: clock.System{}, Wait: deps.wait, Author: env.CommentAuthor})
	if err != nil {
		return nil, "", fmt.Errorf("github: %w", err)
	}
	return api, repo, nil
}

// unknownProofArtifact is the artifact of a proof run that could not start: unknown, with the reason that stopped it,
// no candidate and no cost. It is checked with the gate's own parser, so the gate can never refuse what this writes.
func unknownProofArtifact(key action.ArtifactKey, model string, reason proof.Reason) ([]byte, error) {
	a := action.ProofArtifact{Version: action.ArtifactVersion, Head: key.Head, Base: key.Base, PolicyHash: key.PolicyHash,
		WorkflowRun: key.WorkflowRun, Model: model, Kind: proof.KindUnknown, Reason: reason}
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, fmt.Errorf("encode the proof artifact: %w", err)
	}
	if _, err := action.ParseProofArtifact(raw, key); err != nil {
		return nil, err
	}
	return raw, nil
}

// actionLogger writes the sandbox's own log lines to stderr, which the workflow log keeps.
func actionLogger(stderr io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
