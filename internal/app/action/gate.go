package action

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/actionflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testintegrity"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// ErrStateUnverified means the state log failed its MAC or its hash chain and no Resetter was given. Nothing is
// written to a log that failed verification (ACTION_MODE 2.7).
var ErrStateUnverified = errors.New("action: the state log failed verification")

// The names, paths and ids the gate writes. The check names are the ones a ruleset requires (ACTION_MODE 3.3).
const (
	checkFast  = "heimdall/fast"
	checkProof = "heimdall/proof"
	// gateActor is the system actor of the events this run records.
	gateActor change.ActorID = "action-gate"
	// policyPath is the policy file, read at the merge base, never at the head (TECH_SPEC invariant 5).
	policyPath = "heimdall.yaml"
	// proofWorkflow is the workflow whose own change makes a proof result untrusted (ACTION_MODE 2.7).
	proofWorkflow = ".github/workflows/heimdall-proof.yml"
	// maxRunTries bounds the retries after a version conflict, as the reconciler does (TECH_SPEC 2.4).
	maxRunTries = 5
	// maxRounds bounds the read-decide-write rounds of one run. Each round that writes moves the log forward, and the
	// rows stop asking after a handful: fast, ingest, then publish.
	maxRounds = 4
)

// Resetter starts a new log for a change: it replaces the stored events with evs, whose first is state.reset. It is
// the one way a bad log is repaired, and only the gate calls it (ACTION_MODE 2.7).
type Resetter interface {
	Reset(ctx context.Context, changeID id.ChangeID, evs []change.Event) error
}

// SummaryCommenter writes the proof summary comment. The implementation finds its own comment by the marker in the
// body and by the author login of its token, so a repeat updates that comment and adds none (ACTION_MODE 2.5).
type SummaryCommenter interface {
	// UpsertSummary posts body on pull request n, or updates the comment that carries its marker, and returns its id.
	UpsertSummary(ctx context.Context, repo change.Repo, n int, body string) (int64, error)
}

// FactRequest names what the fact reader may read. Every commit is the head of the change the facts are about.
type FactRequest struct {
	Repo   change.Repo
	Number int
	Head   id.SHA
	Base   id.SHA
	// Policy is the action area at the merge base, and Tests its tests area, which names the test globs.
	Policy policy.Action
	Tests  policy.Tests
	// Files are the changed files, as the pull request reports them.
	Files []ports.ChangedFile
}

// IntegrityFacts are the static test-integrity rules before the gate filters them.
type IntegrityFacts struct {
	Known  bool
	Result testintegrity.Result
}

// FastFacts are the fast-check groups that the ports alone cannot build: the secret scan, the analyzer results, the
// pinned-test facts, the integrity rules, the coverage report and the rule cards at the merge base. The reader sets
// each Known flag. The gate fills nothing on its own, so a group a reader leaves unknown stays unknown.
type FastFacts struct {
	Secrets   fastcheck.SecretFacts
	Analyzers fastcheck.AnalyzerFacts
	Tests     fastcheck.TestFacts
	Integrity IntegrityFacts
	Coverage  fastcheck.CoverageFacts
	Memory    fastcheck.MemoryFacts
}

// FactsReader reads the fast-check groups that need an adapter or a scanner outside this package. Consumer: the gate.
// cmd wires it to internal/secretscan, the coverage adapter and the domain rules, which R3 keeps out of app.
type FactsReader interface {
	FastFacts(ctx context.Context, req FactRequest) (FastFacts, error)
}

// PolicyDecoder parses raw policy bytes.
type PolicyDecoder func([]byte) (policy.Policy, error)

// Config wires one gate run. The required ports are checked by NewGate; the optional ones are named on their fields.
type Config struct {
	Store ports.EventStore
	PRs   ports.PRReader
	// Reviews read the approvals of the approval clause. Nil makes the clause unknown.
	Reviews ports.ReviewReader
	// Checks posts the two check runs. Nil means the run decides and appends, but posts no check.
	Checks ports.CheckWriter
	// Summary posts the summary comment. Nil means no comment is written.
	Summary SummaryCommenter
	// Ingester reads the third-party comments. Nil means no comment is ingested.
	Ingester *Ingester
	// Facts reads the fast-check groups. Nil makes every group unknown.
	Facts FactsReader
	// Code reads the policy at the merge base.
	Code   ports.CodeReader
	Decode PolicyDecoder
	Clock  ports.Clock
	IDs    ports.IDs
	// Resetter starts a new log after a failed verification. Nil makes that failure ErrStateUnverified.
	Resetter Resetter
	// IsUnverified reports whether an error from Store.Load means the log failed its MAC or its chain. cmd wires it
	// to errors.Is(err, gitlog.ErrIntegrity), because this package may not import an adapter (R3).
	IsUnverified func(error) bool
}

// Gate runs the fast half of Action mode: it reads the log and GitHub, decides with package actionflow, and writes the
// events, the checks and the summary.
type Gate struct {
	cfg Config
}

// NewGate checks the required ports. A miswired run fails at boot, not at the first pull request.
func NewGate(cfg Config) (*Gate, error) {
	var missing []string
	for _, dep := range []struct {
		name string
		nil  bool
	}{{"Store", cfg.Store == nil}, {"PRs", cfg.PRs == nil}, {"Code", cfg.Code == nil},
		{"Decode", cfg.Decode == nil}, {"Clock", cfg.Clock == nil}, {"IDs", cfg.IDs == nil}} {
		if dep.nil {
			missing = append(missing, dep.name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("action: gate needs %s", strings.Join(missing, ", "))
	}
	return &Gate{cfg: cfg}, nil
}

// RunIn names one pull request the gate runs for.
type RunIn struct {
	ChangeID id.ChangeID
	Repo     change.Repo
	Number   int
	// WorkflowRun is the GitHub Actions run id of this gate run. A result the gate records itself (a trivial skip, an
	// unknown) names it, as proof.reported requires a run id. A run that has none fails when it must record one.
	WorkflowRun int64
}

// Result is what one gate run did. It is a fact for the CLI and the tests: the log is the record of what happened.
type Result struct {
	Head    id.SHA
	Verdict gate.Verdict
	// Appended are the types of the events this run wrote, in order.
	Appended []change.EventType
	// Checks is how many check runs the run posted.
	Checks int
	// SummaryID and SummaryWritten report the summary comment.
	SummaryID      int64
	SummaryWritten bool
	// Waits says why the run did nothing, when it did nothing. Empty when it ran.
	Waits   string
	Version int64
	// IngestFailed names why the third-party comments could not be read, when they could not. The run goes on with
	// no candidates (a comment is a claim, never evidence), but the failure is reported, not swallowed (R6).
	IngestFailed string
}

// Run executes one gate run. A version conflict reloads the log and decides again, up to maxRunTries times.
func (g *Gate) Run(ctx context.Context, in RunIn) (Result, error) {
	if _, err := id.ParseChangeID(string(in.ChangeID)); err != nil {
		return Result{}, fmt.Errorf("action gate: %w", err)
	}
	var last error
	for range maxRunTries {
		res, err := g.runOnce(ctx, in)
		if !errors.Is(err, ports.ErrVersionConflict) {
			return res, err
		}
		last = err
	}
	return Result{}, fmt.Errorf("action gate on %s: still conflicting after %d tries: %w", in.ChangeID, maxRunTries, last)
}

// runOnce is one attempt: load, observe, decide, write.
func (g *Gate) runOnce(ctx context.Context, in RunIn) (Result, error) {
	events, err := g.load(ctx, in.ChangeID)
	if err != nil {
		return Result{}, err
	}
	view, err := change.Fold(events)
	if err != nil {
		return Result{}, fmt.Errorf("action gate on %s: fold: %w", in.ChangeID, err)
	}
	pr, err := g.cfg.PRs.PR(ctx, in.Repo, in.Number)
	if err != nil {
		return Result{}, fmt.Errorf("action gate on %s: read pull request: %w", in.ChangeID, err)
	}
	if err := pr.Validate(); err != nil {
		return Result{}, fmt.Errorf("action gate on %s: pull request from GitHub: %w", in.ChangeID, err)
	}
	r := &run{g: g, in: in, pr: pr, events: events, view: view}
	if err := r.observe(ctx); err != nil {
		return Result{}, err
	}
	if r.view.PR == nil {
		return Result{Waits: "the log has no pull request yet"}, nil
	}
	r.head, r.base = r.view.PR.HeadSHA, r.view.PR.Base
	return r.execute(ctx)
}

// load reads the log. A log that failed verification is reset first, and the new log is read (ACTION_MODE 2.7).
func (g *Gate) load(ctx context.Context, c id.ChangeID) ([]change.Event, error) {
	events, err := g.cfg.Store.Load(ctx, c)
	if err == nil {
		return events, nil
	}
	if g.cfg.IsUnverified == nil || !g.cfg.IsUnverified(err) {
		return nil, fmt.Errorf("action gate on %s: load: %w", c, err)
	}
	if g.cfg.Resetter == nil {
		return nil, fmt.Errorf("action gate on %s: %w: %w", c, ErrStateUnverified, err)
	}
	reason := "state failed verification: " + verificationReason(err)
	ev, err := g.event(c, change.EventStateReset, change.StateReset{Reason: reason}, 1)
	if err != nil {
		return nil, fmt.Errorf("action gate on %s: state.reset: %w", c, err)
	}
	if err := g.cfg.Resetter.Reset(ctx, c, []change.Event{ev}); err != nil {
		return nil, fmt.Errorf("action gate on %s: reset: %w", c, err)
	}
	events, err = g.cfg.Store.Load(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("action gate on %s: load after reset: %w", c, err)
	}
	return events, nil
}

// verificationReason keeps the adapter's words but never a key or a file content. The message names the failed check
// and nothing else.
func verificationReason(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, "failed verification: "); i >= 0 {
		msg = msg[i+len("failed verification: "):]
	}
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

// event builds one checked event at version.
func (g *Gate) event(c id.ChangeID, typ change.EventType, payload change.Payload, version int64) (change.Event, error) {
	if err := payload.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("payload: %w", err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return change.Event{}, fmt.Errorf("encode: %w", err)
	}
	eid := g.cfg.IDs.NewEvent()
	ev := change.Event{ID: eid, ChangeID: c, Version: version, Type: typ,
		Actor: change.Actor{Kind: change.ActorSystem, ID: gateActor}, OccurredAt: g.cfg.Clock.Now().UTC(),
		CorrelationID: change.CorrelationID(eid.String()), Payload: raw}
	if err := ev.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("validate: %w", err)
	}
	return ev, nil
}

// hashOf is the sha256 of the policy file, as the decision input needs it.
func hashOf(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// appendEvents writes evs at the current version and folds them into the view. A version conflict is returned as is,
// so Run loads again and decides again.
func (r *run) appendEvents(ctx context.Context, evs ...change.Event) error {
	if len(evs) == 0 {
		return nil
	}
	expected := int64(len(r.events))
	if _, err := r.g.cfg.Store.Append(ctx, r.in.ChangeID, expected, evs, nil); err != nil {
		return fmt.Errorf("action gate on %s: append: %w", r.in.ChangeID, err)
	}
	for _, e := range evs {
		r.res.Appended = append(r.res.Appended, e.Type)
	}
	r.events = append(r.events, evs...)
	folded, err := change.Fold(r.events)
	if err != nil {
		return fmt.Errorf("action gate on %s: fold after append: %w", r.in.ChangeID, err)
	}
	r.view = folded
	return nil
}

// nextVersion is the version the next event of this run takes.
func (r *run) nextVersion(i int) int64 { return int64(len(r.events)) + int64(i) + 1 }

// build makes one checked event of this run at the version after the log, offset by i.
func (r *run) build(typ change.EventType, payload change.Payload, i int) (change.Event, error) {
	return r.g.event(r.in.ChangeID, typ, payload, r.nextVersion(i))
}

// actionflow's Effect values are the decision; the gate is the only place that turns one into a write.

// observe records what GitHub says about the pull request: pr.opened the first time, pr.pushed when the head or the
// base moved. The event time is when the head was seen, which is the age the sweep row reads.
func (r *run) observe(ctx context.Context) error {
	typ, changed := observedType(r.view, r.pr)
	if !changed {
		return nil
	}
	var payload change.Payload = change.PRPushed{Repo: r.pr.Repo, Number: r.pr.Number, HeadSHA: r.pr.HeadSHA,
		BaseSHA: r.pr.BaseSHA}
	if r.view.PR == nil {
		payload = change.PROpened{Repo: r.pr.Repo, Number: r.pr.Number, HeadSHA: r.pr.HeadSHA,
			Author: r.pr.Author, BaseSHA: r.pr.BaseSHA}
	}
	ev, err := r.build(typ, payload, 0)
	if err != nil {
		return fmt.Errorf("action gate on %s: %s: %w", r.in.ChangeID, typ, err)
	}
	return r.appendEvents(ctx, ev)
}

// observedType is the event that records the pull request, or false when the log already holds this head and base.
func observedType(view change.View, pr ports.PR) (change.EventType, bool) {
	switch {
	case view.PR == nil:
		return change.EventPROpened, true
	case view.PR.HeadSHA != pr.HeadSHA || view.PR.Base != pr.BaseSHA:
		return change.EventPRPushed, true
	}
	return "", false
}

// run is one gate run over one loaded snapshot. Every round re-reads the decision from the folded log.
type run struct {
	g      *Gate
	in     RunIn
	pr     ports.PR
	events []change.Event
	view   change.View
	head   id.SHA
	base   id.SHA

	pol           policy.Policy
	policyKnown   bool
	policyProblem string
	act           policy.Action
	hash          string

	files      []ports.ChangedFile
	filesKnown bool
	approvals  []gate.Approval
	revKnown   bool
	fast       fastcheck.Result
	ingested   IngestResult

	res Result
}

// execute reads the facts, decides, and runs the effects of the rows until nothing is left to do.
func (r *run) execute(ctx context.Context) (Result, error) {
	r.readPolicy(ctx)
	r.readFiles(ctx)
	r.readApprovals(ctx)
	r.fast = r.evaluate(ctx)
	r.ingested, r.res.IngestFailed = r.ingest(ctx)
	r.res.Head, r.res.Verdict = r.head, r.fast.Verdict
	waits, version, err := r.rounds(ctx)
	r.res.Waits, r.res.Version = waits, version
	return r.res, err
}

// round is one decide-and-write round.
func (r *run) rounds(ctx context.Context) (string, int64, error) {
	waits := ""
	for range maxRounds {
		rows := actionflow.Decide(r.in2())
		wrote, err := r.apply(ctx, rows)
		if err != nil {
			return "", 0, err
		}
		if !wrote {
			waits = joinWaits(actionflow.Missing(rows))
			break
		}
	}
	return waits, int64(len(r.events)), nil
}

// joinWaits is the sentence that says what the change still waits for, in row order.
func joinWaits(needs []actionflow.Need) string {
	parts := make([]string, 0, len(needs))
	for _, n := range needs {
		parts = append(parts, string(n.Row)+": "+n.Text)
	}
	return strings.Join(parts, "; ")
}
