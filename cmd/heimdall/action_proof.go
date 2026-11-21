package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/github"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/openrouter"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/uidsandbox"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agentloop"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// proofRegistries are the hosts the fetch phase may reach: the Go module proxy and checksum database, because run_test
// writes and runs Go tests only.
func proofRegistries() []string { return []string{"proxy.golang.org", "sum.golang.org"} }

// sandboxLimits are the limits of one sandbox job. They leave the runner room for the loop.
func sandboxLimits() uidsandbox.Limits {
	return uidsandbox.Limits{CPUPercent: 200, MemoryMax: 4 << 30, TasksMax: 1024}
}

// actionPolicyFile is the policy at the merge base, which the gate also reads (TECH_SPEC invariant 5).
const actionPolicyFile = "heimdall.yaml"

// heldKey is the model key in this process's memory, for the chat client. It is never written to a file, a log or
// the environment of a sandbox job: every job starts from env -i with a fixed set of variables.
type heldKey struct{ key string }

func (h heldKey) Get(string) (string, error) {
	if h.key == "" {
		return "", errors.New("the model key is not set")
	}
	return h.key, nil
}

// proofStep is one proof run's inputs, read from GitHub with the read-only token.
type proofStep struct {
	env    config.ActionEnv
	repo   change.Repo
	api    *github.ActionAPI
	gh     *github.Adapter
	ev     proofEvent
	pr     ports.PR
	files  []ports.ChangedFile
	pol    policy.Policy
	key    action.ArtifactKey
	stdout io.Writer
	stderr io.Writer
}

// runActionProof is the unprivileged step. It runs PR code only through uidsandbox, holds the model key in this
// process alone, and writes the bounded proof artifact. Every end writes an artifact that names its reason, except a
// run whose head is already old: the gate ignores a result for an old head, so this run writes none.
func runActionProof(ctx context.Context, args []string, stdout, stderr io.Writer, deps actionDeps) int {
	out, ok := outFlag("proof", args, proofFile, stderr)
	if !ok {
		return exitActionUsage
	}
	env, err := config.LoadAction(deps.getenv, config.ActionProof)
	if err != nil {
		return actionFail(stderr, "proof", err)
	}
	raw, err := readEvent(env.EventPath)
	if err != nil {
		return actionFail(stderr, "proof", err)
	}
	ev, err := parseProofEvent(raw, env.Repo)
	if err != nil {
		return actionFail(stderr, "proof", err)
	}
	api, repo, err := actionGitHub(env, deps)
	if err != nil {
		return actionFail(stderr, "proof", err)
	}
	s := &proofStep{env: env, repo: repo, api: api, gh: api.Adapter(), ev: ev, stdout: stdout, stderr: stderr}
	artifact, runErr := s.run(ctx, deps)
	if artifact != nil {
		if err := os.WriteFile(out, artifact, 0o600); err != nil {
			return actionFail(stderr, "proof", errors.Join(runErr, fmt.Errorf("write %s: %w", out, err)))
		}
	}
	if runErr != nil {
		return actionFail(stderr, "proof", runErr)
	}
	return exitActionOK
}

// run reads the inputs and runs the proof. It returns the artifact to write, or nil when none must be written.
func (s *proofStep) run(ctx context.Context, deps actionDeps) ([]byte, error) {
	pr, err := s.gh.PR(ctx, s.repo, s.ev.Number)
	if err != nil {
		return nil, fmt.Errorf("read the pull request: %w", err)
	}
	if pr.HeadSHA != s.ev.Head {
		writeDiagnostic(s.stdout, fmt.Sprintf("heimdall action proof: #%d moved to %s; this run is for an old head and records nothing\n",
			s.ev.Number, pr.HeadSHA))
		return nil, nil
	}
	s.pr = pr
	hash, rawPolicy, err := s.readPolicy(ctx)
	if err != nil {
		// Without the policy hash no artifact can name its input. The gate's sweep closes the missing result as
		// unknown: no result.
		return nil, err
	}
	s.key = action.ArtifactKey{Head: pr.HeadSHA, Base: pr.BaseSHA, PolicyHash: hash, WorkflowRun: s.env.RunID}
	if rawPolicy != nil {
		if s.pol, err = policyyaml.Decode(rawPolicy); err == nil {
			err = s.pol.Validate()
		}
		if err != nil {
			return s.unknown(proof.ReasonInputMissing, fmt.Errorf("the policy at the merge base: %w", err))
		}
	}
	if s.files, err = s.gh.Files(ctx, s.repo, s.ev.Number); err != nil {
		return s.unknown(proof.ReasonInputMissing, fmt.Errorf("read the changed files: %w", err))
	}
	if !s.env.HasModelKey() {
		if s.ev.NoKeyByRule {
			return s.unknown(proof.ReasonForkNoKey, nil)
		}
		return s.unknown(proof.ReasonInputMissing,
			errors.New("HEIMDALL_MODEL_KEY is not set on a same-repository pull request: set it on the proof step"))
	}
	return s.prove(ctx, deps)
}

// readPolicy reads the policy at the merge base and its hash. No file is the empty policy, as the gate reads it.
func (s *proofStep) readPolicy(ctx context.Context) (string, []byte, error) {
	raw, err := s.gh.ReadFile(ctx, s.repo, s.pr.BaseSHA, actionPolicyFile)
	if errors.Is(err, ports.ErrNotFound) {
		return hashOfPolicy(nil), nil, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("read %s at the merge base: %w", actionPolicyFile, err)
	}
	return hashOfPolicy(raw), raw, nil
}

// hashOfPolicy is the sha256 of the policy file, the hash the gate compares with the artifact's.
func hashOfPolicy(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// unknown writes the artifact of a run that stopped before the proof, with its reason. cause, when set, is the
// failure the step also reports.
func (s *proofStep) unknown(reason proof.Reason, cause error) ([]byte, error) {
	raw, err := unknownProofArtifact(s.key, s.env.Model, reason)
	if err != nil {
		return nil, errors.Join(cause, err)
	}
	writeDiagnostic(s.stdout, fmt.Sprintf("heimdall action proof: #%d: unknown (%s)\n", s.ev.Number, reason))
	return raw, cause
}

// prove sets the sandbox up, wires the agent loop with its retry rule, and runs the proof use case.
func (s *proofStep) prove(ctx context.Context, deps actionDeps) ([]byte, error) {
	sbx, cleanup, err := deps.sandbox(ctx, uidsandbox.Config{RunnerHome: s.env.Home, RunnerTemp: s.env.RunnerTemp,
		Registries: proofRegistries(), Ports: []int{443}, ToolPath: s.env.SandboxPath, Limits: sandboxLimits(),
		Logger: actionLogger(s.stderr)})
	if errors.Is(err, uidsandbox.ErrCannotIsolate) {
		writeDiagnostic(s.stderr, fmt.Sprintf("heimdall action proof: %v\n", err))
		return s.unknown(proof.ReasonCannotIsolate, nil)
	}
	if err != nil {
		return s.unknown(proof.ReasonSandbox, err)
	}
	defer func() {
		if cerr := cleanup(context.WithoutCancel(ctx)); cerr != nil {
			writeDiagnostic(s.stderr, fmt.Sprintf("heimdall action proof: sandbox cleanup: %v\n", cerr))
		}
	}()
	loop, err := s.agentLoop(deps)
	if err != nil {
		return nil, err
	}
	p, err := action.NewProof(action.ProofConfig{Loop: loop,
		Sandbox: &sandboxBridge{sbx: sbx, checkout: s.env.Workspace, head: s.key.Head, base: s.key.Base},
		Code:    s.gh, Clock: clock.System{}, ModelName: s.env.Model})
	if err != nil {
		return nil, fmt.Errorf("proof: %w", err)
	}
	changeID, err := id.PRChangeID(s.repo.String(), s.ev.Number)
	if err != nil {
		return nil, fmt.Errorf("proof: %w", err)
	}
	res, runErr := p.Run(ctx, action.ProofIn{ChangeID: changeID, Repo: s.repo, Number: s.ev.Number, Head: s.key.Head,
		Base: s.key.Base, PolicyHash: s.key.PolicyHash, WorkflowRun: s.key.WorkflowRun, Action: s.pol.Action,
		Policy: s.pol, Files: s.files, Checkout: s.env.Workspace, Claims: s.claims(ctx)})
	if res.Artifact == nil {
		return s.unknown(proof.ReasonNoResult, fmt.Errorf("proof: %w", runErr))
	}
	o := res.Outcome
	line := fmt.Sprintf("heimdall action proof: #%d: %s", s.ev.Number, o.Kind)
	if o.Reason != "" {
		line += " (" + string(o.Reason) + ")"
	}
	writeDiagnostic(s.stdout, fmt.Sprintf("%s; %d candidates, %d proven, %d blocking; $%.4f, %d turns\n", line,
		o.Counts.In, o.Counts.Proven, o.Counts.Blocking, res.Spend.USD, res.Turns))
	if runErr != nil {
		return res.Artifact, fmt.Errorf("proof: %w", runErr)
	}
	return res.Artifact, nil
}

// agentLoop is the one agent loop of the product on the configured endpoint, with the retry rule of AM12c: a rate
// limit or a server error is sent again, up to 3 attempts, waiting on the real clock with crypto/rand jitter.
func (s *proofStep) agentLoop(deps actionDeps) (*agentloop.Runner, error) {
	price := ports.ModelPrice{InputPerM: s.env.Price.InputPerM, CachedInputPerM: s.env.Price.CachedInputPerM,
		OutputPerM: s.env.Price.OutputPerM}
	chat := openrouter.ChatClient{BaseURL: s.env.ModelBaseURL, Secrets: heldKey{key: s.env.ModelKey}, KeyName: "model",
		HTTP: deps.http, Prices: map[string]openrouter.Price{s.env.Model: {Input: price.InputPerM,
			CachedInput: price.CachedInputPerM, Output: price.OutputPerM}}}
	runner, err := agentloop.NewRunner(chat, clock.System{}, price)
	if err != nil {
		return nil, fmt.Errorf("agent loop: %w", err)
	}
	runner, err = runner.WithRetry(deps.wait, cryptoJitter{})
	if err != nil {
		return nil, fmt.Errorf("agent loop: %w", err)
	}
	return runner, nil
}

// claims are the third-party comments the proof tries to prove. They are claims, never evidence, so a failed read is
// reported and the run goes on without them.
func (s *proofStep) claims(ctx context.Context) []action.Claim {
	ing, err := action.NewIngester(s.api)
	if err != nil {
		writeDiagnostic(s.stderr, fmt.Sprintf("heimdall action proof: third-party comments: %v\n", err))
		return nil
	}
	got, err := ing.Ingest(ctx, action.IngestIn{Repo: s.repo, Number: s.ev.Number, Head: s.key.Head,
		Sources: s.pol.Action.Effective().Sources})
	if err != nil {
		writeDiagnostic(s.stderr, fmt.Sprintf("heimdall action proof: third-party comments not read: %v\n", err))
		return nil
	}
	out := make([]action.Claim, 0, len(got.Candidates))
	for _, c := range got.Candidates {
		out = append(out, action.Claim{Source: c.Source, CommentID: c.CommentID, Location: c.Location, Claim: c.Claim,
			Severity: c.Severity})
	}
	return out
}
