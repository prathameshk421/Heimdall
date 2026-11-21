package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/github"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/gitlog"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// triggerLimits and proofLimits bound the two artifacts the gate downloads. Both are written by an unprivileged run,
// so both are untrusted.
func triggerLimits() ports.ArtifactLimits {
	return ports.ArtifactLimits{MaxZipBytes: 64 << 10, MaxEntries: 4, MaxFileBytes: maxTriggerBytes,
		MaxTotalBytes: 4 * maxTriggerBytes}
}

func proofLimits() ports.ArtifactLimits {
	return ports.ArtifactLimits{MaxZipBytes: 1 << 20, MaxEntries: 4, MaxFileBytes: action.MaxArtifactBytes,
		MaxTotalBytes: 2 * action.MaxArtifactBytes}
}

// summaryPoster is the gate's action.SummaryCommenter on the Action API. It finds its comment by the marker of the
// pull request's summary key and by the author login of the token, and never edits a comment of anyone else.
type summaryPoster struct{ api *github.ActionAPI }

func (s summaryPoster) UpsertSummary(ctx context.Context, repo change.Repo, n int, body string) (int64, error) {
	got, err := s.api.UpsertComment(ctx, repo, n, change.EffectKey("action-summary:"+strconv.Itoa(n)), body)
	if err != nil {
		return 0, fmt.Errorf("summary comment: %w", err)
	}
	return got, nil
}

// gateWiring is one gate step's ports.
type gateWiring struct {
	env  config.ActionEnv
	repo change.Repo
	api  *github.ActionAPI
	gh   *github.Adapter
	gate *action.Gate
}

func newGateWiring(env config.ActionEnv, deps actionDeps) (*gateWiring, error) {
	api, repo, err := actionGitHub(env, deps)
	if err != nil {
		return nil, err
	}
	store, err := gitlog.New(gitlog.Config{Repo: repo, Git: api, Key: env.LogKey, Wait: deps.wait})
	if err != nil {
		return nil, fmt.Errorf("state log: %w", err)
	}
	ingester, err := action.NewIngester(api)
	if err != nil {
		return nil, fmt.Errorf("ingester: %w", err)
	}
	facts, err := newDiffFacts()
	if err != nil {
		return nil, err
	}
	gh := api.Adapter()
	// Resetter stays nil: the git-ref store has no way to replace a change's log yet, so a log that fails its MAC or
	// its chain stops the gate with ErrStateUnverified and nothing is posted (fail closed).
	gate, err := action.NewGate(action.Config{Store: store, PRs: gh, Reviews: gh, Checks: gh, Summary: summaryPoster{api: api},
		Ingester: ingester, Facts: facts, Code: gh, Decode: policyyaml.Decode, Clock: clock.System{}, IDs: ids.New(),
		IsUnverified: func(err error) bool { return errors.Is(err, gitlog.ErrIntegrity) }})
	if err != nil {
		return nil, fmt.Errorf("gate: %w", err)
	}
	return &gateWiring{env: env, repo: repo, api: api, gh: gh, gate: gate}, nil
}

// runActionGate is the privileged step. It never runs PR code and never checks the PR out: it reads the PR as data,
// reads the trigger and proof artifacts as untrusted input, and writes the state ref, the check runs and the summary.
func runActionGate(ctx context.Context, args []string, stdout, stderr io.Writer, deps actionDeps) int {
	if len(args) > 0 {
		writeDiagnostic(stderr, "usage: heimdall action gate\n")
		return exitActionUsage
	}
	env, err := config.LoadAction(deps.getenv, config.ActionGate)
	if err != nil {
		return actionFail(stderr, "gate", err)
	}
	raw, err := readEvent(env.EventPath)
	if err != nil {
		return actionFail(stderr, "gate", err)
	}
	ev, err := parseGateEvent(env.EventName, raw)
	if err != nil {
		return actionFail(stderr, "gate", err)
	}
	w, err := newGateWiring(env, deps)
	if err != nil {
		return actionFail(stderr, "gate", err)
	}
	if err := w.serve(ctx, ev, stdout); err != nil {
		return actionFail(stderr, "gate", err)
	}
	return exitActionOK
}

// serve runs the gate for every pull request the event names. Each failure is reported, and any failure fails the
// step, so a check that could not be posted is never read as a pass.
func (w *gateWiring) serve(ctx context.Context, ev gateEvent, stdout io.Writer) error {
	switch {
	case ev.Run != nil:
		return w.serveRun(ctx, *ev.Run, stdout)
	case ev.Name == "issue_comment":
		if ev.Number == 0 {
			writeDiagnostic(stdout, "heimdall action gate: the comment is on an issue, not a pull request\n")
			return nil
		}
		return w.runAll(ctx, []int{ev.Number}, stdout)
	}
	// The sweep and a manual run serve every open pull request.
	prs, err := w.gh.OpenPRs(ctx, w.repo)
	if err != nil {
		return fmt.Errorf("list the open pull requests: %w", err)
	}
	numbers := make([]int, 0, len(prs))
	for _, pr := range prs {
		numbers = append(numbers, pr.Number)
	}
	return w.runAll(ctx, numbers, stdout)
}

// serveRun serves a finished workflow run. A run of Heimdall's own workflows names its pull request in its trigger
// record, because GitHub lists no pull request for a fork run; any other run is a declared input, found by its head.
func (w *gateWiring) serveRun(ctx context.Context, run workflowRun, stdout io.Writer) error {
	if run.Path != triggerWorkflowPath && run.Path != proofWorkflowPath {
		numbers, err := w.prsAtHead(ctx, run)
		if err != nil {
			return err
		}
		return w.runAll(ctx, numbers, stdout)
	}
	rec, recErr := w.triggerRecord(ctx, run)
	numbers := slices.Clone(run.PRs)
	if recErr == nil && !slices.Contains(numbers, rec.Number) {
		numbers = append(numbers, rec.Number)
	}
	if len(numbers) == 0 {
		return fmt.Errorf("run %d names no pull request: %w", run.ID, recErr)
	}
	if recErr != nil {
		writeDiagnostic(stdout, fmt.Sprintf("heimdall action gate: run %d: %v\n", run.ID, recErr))
	}
	if run.Path == triggerWorkflowPath {
		return w.runAll(ctx, numbers, stdout)
	}
	var errs []error
	for _, n := range numbers {
		errs = append(errs, w.serveProof(ctx, run, rec, n, stdout))
	}
	return errors.Join(errs...)
}

// prsAtHead is every open pull request whose head is the run's head, with the ones GitHub lists for the run.
func (w *gateWiring) prsAtHead(ctx context.Context, run workflowRun) ([]int, error) {
	numbers := slices.Clone(run.PRs)
	prs, err := w.gh.OpenPRs(ctx, w.repo)
	if err != nil {
		return nil, fmt.Errorf("list the open pull requests: %w", err)
	}
	for _, pr := range prs {
		if pr.HeadSHA == run.HeadSHA && !slices.Contains(numbers, pr.Number) {
			numbers = append(numbers, pr.Number)
		}
	}
	return numbers, nil
}

// triggerRecord reads the run's trigger record. A record for another head than GitHub's own head of the run is
// refused: the run that wrote it is unprivileged.
func (w *gateWiring) triggerRecord(ctx context.Context, run workflowRun) (triggerRecord, error) {
	raw, err := w.artifactFile(ctx, run.ID, triggerArtifact, triggerFile, triggerLimits())
	if err != nil {
		return triggerRecord{}, err
	}
	if raw == nil {
		return triggerRecord{}, fmt.Errorf("the run uploaded no %s artifact", triggerArtifact)
	}
	rec, err := parseTriggerRecord(raw)
	if err != nil {
		return triggerRecord{}, err
	}
	if rec.Head != run.HeadSHA {
		return triggerRecord{}, errors.New("the trigger record names another head than the run")
	}
	return rec, nil
}

// serveProof records a proof run's result and runs the gate again. The gate runs first, so the log holds the pull
// request before the result is recorded. A run for an older head says nothing about the current head and is not
// recorded. A run the trust rule refuses is recorded with no artifact, which is unknown: no result.
func (w *gateWiring) serveProof(ctx context.Context, run workflowRun, rec triggerRecord, n int, stdout io.Writer) error {
	if err := w.runOne(ctx, n, stdout); err != nil {
		return err
	}
	pr, err := w.gh.PR(ctx, w.repo, n)
	if err != nil {
		return fmt.Errorf("#%d: read the pull request: %w", n, err)
	}
	if pr.HeadSHA != run.HeadSHA {
		writeDiagnostic(stdout, fmt.Sprintf("heimdall action gate: #%d: proof run %d is for an older head; not recorded\n", n, run.ID))
		return nil
	}
	var artifact []byte
	if why := proofUntrusted(run, rec, pr.HeadSHA); why != "" {
		writeDiagnostic(stdout, fmt.Sprintf("heimdall action gate: #%d: proof artifact not read: %s\n", n, why))
	} else if artifact, err = w.artifactFile(ctx, run.ID, proofArtifactName, proofFile, proofLimits()); err != nil {
		writeDiagnostic(stdout, fmt.Sprintf("heimdall action gate: #%d: proof artifact not read: %v\n", n, err))
		artifact = nil
	}
	changeID, err := id.PRChangeID(w.repo.String(), n)
	if err != nil {
		return fmt.Errorf("#%d: %w", n, err)
	}
	got, err := w.gate.RecordProof(ctx, action.ProofRecordIn{ChangeID: changeID, Repo: w.repo, Number: n,
		WorkflowRun: run.ID, Artifact: artifact})
	if err != nil {
		return fmt.Errorf("#%d: record the proof run: %w", n, err)
	}
	line := fmt.Sprintf("heimdall action gate: #%d: proof run %d recorded as %s", n, run.ID, got.Outcome.Kind)
	if got.Outcome.Reason != "" {
		line += " (" + string(got.Outcome.Reason) + ")"
	}
	if got.Refused != "" {
		line += "; artifact refused: " + clipText(got.Refused, 300)
	}
	writeDiagnostic(stdout, line+"\n")
	return w.runOne(ctx, n, stdout)
}

// artifactFile downloads one file of one artifact of a run. A run with no such artifact, or an artifact without the
// file, gives nil and no error: the caller reads that as a missing fact.
func (w *gateWiring) artifactFile(ctx context.Context, runID int64, name, file string, limits ports.ArtifactLimits) ([]byte, error) {
	list, err := w.api.RunArtifacts(ctx, w.repo, runID)
	if err != nil {
		return nil, fmt.Errorf("list the artifacts of run %d: %w", runID, err)
	}
	var found []ports.WorkflowArtifact
	for _, a := range list {
		if a.Name == name && !a.Expired {
			found = append(found, a)
		}
	}
	switch len(found) {
	case 0:
		return nil, nil
	case 1:
	default:
		return nil, fmt.Errorf("run %d has %d artifacts named %s", runID, len(found), name)
	}
	files, err := w.api.DownloadArtifact(ctx, w.repo, found[0].ID, limits)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", name, err)
	}
	for _, f := range files {
		if f.Path == file {
			return f.Content, nil
		}
	}
	return nil, nil
}

func (w *gateWiring) runAll(ctx context.Context, numbers []int, stdout io.Writer) error {
	var errs []error
	for _, n := range numbers {
		errs = append(errs, w.runOne(ctx, n, stdout))
	}
	return errors.Join(errs...)
}

// runOne is one gate run on one pull request.
func (w *gateWiring) runOne(ctx context.Context, n int, stdout io.Writer) error {
	changeID, err := id.PRChangeID(w.repo.String(), n)
	if err != nil {
		return fmt.Errorf("#%d: %w", n, err)
	}
	res, err := w.gate.Run(ctx, action.RunIn{ChangeID: changeID, Repo: w.repo, Number: n, WorkflowRun: w.env.RunID})
	if err != nil {
		return fmt.Errorf("#%d: %w", n, err)
	}
	parts := []string{fmt.Sprintf("#%d at %s: fast %s", n, res.Head, res.Verdict)}
	if len(res.Appended) > 0 {
		types := make([]string, 0, len(res.Appended))
		for _, t := range res.Appended {
			types = append(types, string(t))
		}
		parts = append(parts, "appended "+strings.Join(types, ", "))
	}
	parts = append(parts, fmt.Sprintf("%d checks posted", res.Checks))
	if res.SummaryWritten {
		parts = append(parts, fmt.Sprintf("summary comment %d", res.SummaryID))
	}
	if res.Waits != "" {
		parts = append(parts, "waits: "+clipText(res.Waits, 400))
	}
	if res.IngestFailed != "" {
		parts = append(parts, "third-party comments not read: "+clipText(res.IngestFailed, 300))
	}
	writeDiagnostic(stdout, "heimdall action gate: "+strings.Join(parts, "; ")+"\n")
	return nil
}
