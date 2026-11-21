package action_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testintegrity"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Component test of the gate and the ingest step (ACTION_MODE 5, 6; work package AM14a). Every port is an in-memory
// fake in this file: the event store, GitHub, the reviews, the check writer, the summary commenter and the fact reader.
// The test drives app.Action through its public API only.

const (
	testRepo   = change.Repo("acme/shop")
	testNumber = 7
	// testHead and testBase are the commits the fake GitHub reports. They are real 40-hex ids, as GitHub gives them.
	testHead  = id.SHA("1111111111111111111111111111111111111111")
	testBase  = id.SHA("2222222222222222222222222222222222222222")
	testHead2 = id.SHA("3333333333333333333333333333333333333333")
)

// --- the event store ---

// store is an in-memory ports.EventStore that chains events the way the real store does, so the fold and the chain
// checks behave as they do in production.
type store struct {
	mu sync.Mutex
	// eventsByChange is the log of each change, chained.
	eventsByChange map[id.ChangeID][]change.Event
	// appends counts every successful Append, per change.
	appends  map[id.ChangeID]int
	failLoad error
}

var _ ports.EventStore = (*store)(nil)

func newStore() *store {
	return &store{eventsByChange: map[id.ChangeID][]change.Event{}, appends: map[id.ChangeID]int{}}
}

func (s *store) Load(_ context.Context, c id.ChangeID) ([]change.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failLoad != nil {
		return nil, s.failLoad
	}
	return append([]change.Event(nil), s.eventsByChange[c]...), nil
}

func (s *store) Append(_ context.Context, c id.ChangeID, expected int64, evs []change.Event, _ []change.Effect) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.eventsByChange[c]
	if int64(len(stored)) != expected {
		return 0, fmt.Errorf("append: want version %d, log at %d: %w", expected, len(stored), ports.ErrVersionConflict)
	}
	chained, err := chain(int64(len(stored)), stored, evs)
	if err != nil {
		return 0, err
	}
	s.eventsByChange[c] = append(stored, chained...)
	s.appends[c]++
	return int64(len(s.eventsByChange[c])), nil
}

// chain gives every event its version and hash, and refuses an event that does not validate.
func chain(at int64, stored, evs []change.Event) ([]change.Event, error) {
	var prev change.Hash
	if len(stored) > 0 {
		prev = stored[len(stored)-1].Hash
	}
	out := make([]change.Event, 0, len(evs))
	for i, e := range evs {
		e.Version = at + int64(i) + 1
		if err := e.Validate(); err != nil {
			return nil, fmt.Errorf("event %d: %w", i+1, err)
		}
		if at > 0 || i > 0 {
			p := prev
			e.PrevHash = &p
		}
		h, err := change.Chain(prev, e)
		if err != nil {
			return nil, err
		}
		e.Hash = h
		prev = h
		out = append(out, e)
	}
	return out, nil
}

// seed plants one chained event in a change's log.
func (s *store) seed(t *testing.T, c id.ChangeID, e change.Event) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	chained, err := chain(int64(len(s.eventsByChange[c])), s.eventsByChange[c], []change.Event{e})
	require.NoError(t, err)
	s.eventsByChange[c] = append(s.eventsByChange[c], chained...)
}

// appendCount is how many Appends succeeded for a change.
func (s *store) appendCount(c id.ChangeID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appends[c]
}

// --- resetter ---

// resetter replaces a change's log, as the gate does after a failed verification.
type resetter struct{ s *store }

var _ action.Resetter = resetter{}

func (r resetter) Reset(_ context.Context, c id.ChangeID, evs []change.Event) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	// A new log verifies again: whatever made the old one fail is gone with it.
	r.s.failLoad = nil
	chained, err := chain(0, nil, evs)
	if err != nil {
		return err
	}
	r.s.eventsByChange[c] = chained
	r.s.appends[c]++
	return nil
}

// --- GitHub ---

// github is the in-memory stand-in for the GitHub ports: one pull request, its files and its reviews.
type github struct {
	pr      ports.PR
	files   []ports.ChangedFile
	reviews []ports.PRReview
	// failFiles makes the file read fail, which must leave every clause that needs the list unknown.
	failFiles bool
}

var (
	_ ports.PRReader     = (*github)(nil)
	_ ports.ReviewReader = (*github)(nil)
)

func newGitHub() *github {
	return &github{pr: ports.PR{Repo: testRepo, Number: testNumber, Title: "Add refunds", Branch: "add-refunds",
		Author: "dev1", BaseSHA: testBase, HeadSHA: testHead, State: ports.PRStateOpen}}
}

func (g *github) PR(_ context.Context, _ change.Repo, _ int) (ports.PR, error) { return g.pr, nil }

func (g *github) Files(_ context.Context, _ change.Repo, _ int) ([]ports.ChangedFile, error) {
	if g.failFiles {
		return nil, errors.New("github: files unavailable")
	}
	return g.files, nil
}

func (g *github) OpenPRs(_ context.Context, _ change.Repo) ([]ports.PR, error) {
	return []ports.PR{g.pr}, nil
}

func (g *github) PRReviews(_ context.Context, _ change.Repo, _ int) ([]ports.PRReview, error) {
	return g.reviews, nil
}

// code is the file reader: it serves the policy file at the merge base and nothing else.
type code struct {
	policy map[id.SHA][]byte
}

var _ ports.CodeReader = (*code)(nil)

func (c *code) ReadFile(_ context.Context, _ change.Repo, sha id.SHA, path string) ([]byte, error) {
	if path != "heimdall.yaml" {
		return nil, ports.ErrNotFound
	}
	raw, ok := c.policy[sha]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return raw, nil
}

func (c *code) TreeHash(_ context.Context, _ change.Repo, _ id.SHA, _ string) (id.SHA, error) {
	return "", ports.ErrNotFound
}

// Tarball is unused by the gate; the port needs it.
func (c *code) Tarball(_ context.Context, _ change.Repo, _ id.SHA) (io.ReadCloser, error) {
	return nil, ports.ErrNotFound
}

// --- checks and the summary ---

// checkWriter records every check run the gate posts, keyed by external id, so a repeat updates and adds none.
type checkWriter struct {
	mu   sync.Mutex
	runs map[string]ports.Check
	shas map[string]id.SHA
	all  []string
}

var _ ports.CheckWriter = (*checkWriter)(nil)

func newCheckWriter() *checkWriter {
	return &checkWriter{runs: map[string]ports.Check{}, shas: map[string]id.SHA{}}
}

func (w *checkWriter) SetCheck(_ context.Context, _ change.Repo, sha id.SHA, c ports.Check) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, seen := w.runs[c.ExternalID]; !seen {
		w.all = append(w.all, c.ExternalID)
	}
	w.runs[c.ExternalID] = c
	w.shas[c.ExternalID] = sha
	return nil
}

// fast is the heimdall/fast run the gate posted, if any.
func (w *checkWriter) fast() (ports.Check, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, c := range w.runs {
		if c.Name == "heimdall/fast" {
			return c, true
		}
	}
	return ports.Check{}, false
}

func (w *checkWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.all)
}

// summaryWriter stands for the GitHub comment API. It behaves as the adapter does: the comment is found by its marker
// and the author login, so a second write updates that comment and adds none.
type summaryWriter struct {
	mu     sync.Mutex
	author string
	bodies map[int64]string
	order  []int64
	next   int64
	writes int
}

var _ action.SummaryCommenter = (*summaryWriter)(nil)

func newSummaryWriter() *summaryWriter {
	return &summaryWriter{author: "github-actions[bot]", bodies: map[int64]string{}}
}

func (w *summaryWriter) UpsertSummary(_ context.Context, _ change.Repo, _ int, body string) (int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes++
	marker := strings.SplitN(body, "\n", 2)[0]
	for _, id := range w.order {
		if strings.SplitN(w.bodies[id], "\n", 2)[0] == marker {
			w.bodies[id] = body
			return id, nil
		}
	}
	w.next++
	w.bodies[w.next] = body
	w.order = append(w.order, w.next)
	return w.next, nil
}

func (w *summaryWriter) comments() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.order)
}

func (w *summaryWriter) body() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.order) == 0 {
		return ""
	}
	return w.bodies[w.order[len(w.order)-1]]
}

// --- facts, clock, ids ---

// facts is the fast-check fact reader. It answers with the groups a test sets, and reports a clean scan by default.
type facts struct {
	secrets   fastcheck.SecretFacts
	analyzers fastcheck.AnalyzerFacts
	tests     fastcheck.TestFacts
	integrity action.IntegrityFacts
	coverage  fastcheck.CoverageFacts
	memory    fastcheck.MemoryFacts
}

var _ action.FactsReader = (*facts)(nil)

func cleanFacts() *facts {
	return &facts{
		secrets:   fastcheck.SecretFacts{Known: true},
		analyzers: fastcheck.AnalyzerFacts{Known: true},
		tests:     fastcheck.TestFacts{Known: true},
		integrity: action.IntegrityFacts{Known: true},
		coverage:  fastcheck.CoverageFacts{Known: true},
		memory:    fastcheck.MemoryFacts{Known: true},
	}
}

func (f *facts) FastFacts(_ context.Context, _ action.FactRequest) (action.FastFacts, error) {
	return action.FastFacts{Secrets: f.secrets, Analyzers: f.analyzers, Tests: f.tests, Integrity: f.integrity,
		Coverage: f.coverage, Memory: f.memory}, nil
}

// clock is a fixed clock: the gate reads the time once per event and once per decision.
type clock struct {
	now time.Time
}

var _ ports.Clock = clock{}

func (c clock) Now() time.Time { return c.now }

// ids makes deterministic event ids: a counter formatted as a UUID.
type ids struct {
	mu sync.Mutex
	n  int
}

var _ ports.IDs = (*ids)(nil)

func (i *ids) NewEvent() change.EventID {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.n++
	return change.EventID(fmt.Sprintf("00000000-0000-4000-8000-%012d", i.n))
}

func (i *ids) NewFinding() id.FindingID { return "01HZZZZZZZZZZZZZZZZZZZZZZZ" }
func (i *ids) NewRun() id.RunID         { return "" }
func (i *ids) NewCase() id.CaseID       { return "" }
func (i *ids) NewTask() id.TaskID       { return "" }

// --- the policy ---

// policyFixture is a real policy the repository ships: the pay-demo base tree. The gate must see a policy that
// validates, because an invalid one blocks every row and the test would not exercise the run. The action mode is
// overwritten per test, because the fixture does not set it.
const policyFixture = "../../../testdata/repos/pay-demo/base/heimdall.yaml"

// testPolicy reads the fixture and sets the action mode.
func testPolicy(t *testing.T, mode string) policy.Policy {
	t.Helper()
	raw, err := os.ReadFile(policyFixture)
	require.NoError(t, err)
	p, err := policyyaml.Decode(raw)
	require.NoError(t, err)
	p.Action.Mode = mode
	require.NoError(t, p.Validate())
	return p
}

// policyRaw encodes a policy the way the gate's decoder reads it: the JSON form the yaml decoder also fills. The
// fixture itself is yaml, so the test re-encodes it as JSON for the fake file reader, and the gate's decoder is the
// same policyyaml.Decode the yaml form uses.
func policyRaw(t *testing.T, p policy.Policy) []byte {
	t.Helper()
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	return raw
}

// --- the harness ---

// harness holds one wired gate and its fakes.
type harness struct {
	store    *store
	github   *github
	code     *code
	checks   *checkWriter
	summary  *summaryWriter
	facts    *facts
	clock    clock
	ids      *ids
	gate     *action.Gate
	changeID id.ChangeID
}

// newHarness wires the gate with a clean fast check, a valid enforce-mode policy and one changed source file.
func newHarness(t *testing.T, mode string) *harness {
	t.Helper()
	return newHarnessWith(t, mode, nil)
}

// newHarnessWith is newHarness with a third-party comment ingester wired in (nil for none).
func newHarnessWith(t *testing.T, mode string, ing *action.Ingester) *harness {
	t.Helper()
	cid, err := id.PRChangeID(string(testRepo), testNumber)
	require.NoError(t, err)
	h := &harness{
		store: newStore(), github: newGitHub(), checks: newCheckWriter(), summary: newSummaryWriter(), facts: cleanFacts(),
		clock: clock{now: time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)}, ids: &ids{}, changeID: cid,
	}
	h.github.files = []ports.ChangedFile{{Path: "internal/pay/refund.go", Status: ports.FileModified, Additions: 12, Deletions: 3}}
	// An approval from an approver of the changed path's default domain on head, not the author: the approval clause
	// is that rule, and without it a clean fast check fails on purpose.
	h.github.reviews = []ports.PRReview{{ID: "1001", Login: "rekdfr", State: ports.ReviewStateApproved,
		CommitSHA: testHead, SubmittedAt: h.clock.now}}
	raw := policyRaw(t, testPolicy(t, mode))
	h.code = &code{policy: map[id.SHA][]byte{testBase: raw}}
	g, err := action.NewGate(action.Config{Store: h.store, PRs: h.github, Reviews: h.github, Checks: h.checks,
		Summary: h.summary, Facts: h.facts, Code: h.code, Decode: policyyaml.Decode, Clock: h.clock, IDs: h.ids,
		Ingester: ing, IsUnverified: func(err error) bool { return errors.As(err, &errBadMAC{}) }})
	require.NoError(t, err)
	h.gate = g
	return h
}

// run executes one gate run for the harness's change.
func (h *harness) run(t *testing.T) action.Result {
	t.Helper()
	res, err := h.gate.Run(t.Context(), action.RunIn{ChangeID: h.changeID, Repo: testRepo, Number: testNumber})
	require.NoError(t, err)
	return res
}

// log is the stored event types of the change, in order.
func (h *harness) log(t *testing.T) []change.EventType {
	t.Helper()
	events, err := h.store.Load(t.Context(), h.changeID)
	require.NoError(t, err)
	out := make([]change.EventType, 0, len(events))
	for _, e := range events {
		out = append(out, e.Type)
	}
	return out
}

// --- the cases ---

// TestEnforceCleanFastCheckPostsSuccess is the whole run in enforce mode with a clean fast check: the log gains the
// pull request, the fast result and the summary, and the check says success, which is the only place a pass may appear.
func TestEnforceCleanFastCheckPostsSuccess(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)

	res := h.run(t)

	require.Equal(t, testHead, res.Head)
	require.Equal(t, gate.VerdictPass, res.Verdict)
	require.Contains(t, h.log(t), change.EventPROpened)
	require.Contains(t, h.log(t), change.EventFastReported)
	require.Contains(t, h.log(t), change.EventSummaryPosted)
	c, ok := h.checks.fast()
	require.True(t, ok, "the fast check must be posted")
	require.Equal(t, change.CISuccess, c.Conclusion)
	require.Equal(t, testHead, h.checks.shas[c.ExternalID])
	require.Contains(t, h.summary.body(), "heimdall:proof-summary")
}

// TestShadowModePostsNeutral is the same run in shadow mode: the check reports only, so it is neutral and never success.
func TestShadowModePostsNeutral(t *testing.T) {
	h := newHarness(t, policy.ActionModeShadow)

	res := h.run(t)

	require.Equal(t, gate.VerdictPass, res.Verdict)
	c, ok := h.checks.fast()
	require.True(t, ok)
	require.Equal(t, change.CINeutral, c.Conclusion)
	require.Contains(t, h.summary.body(), "Shadow mode")
}

// TestForgedArtifactClaimingPassGivesUnknown feeds the fact reader a violation it invented without the block level the
// rules set. The clause stays met only for a blocking violation, so a forged claim cannot reach the check: the reader's
// Known flag is what the gate believes, and only Blocking() reaches the clause.
func TestForgedArtifactClaimingPassGivesUnknown(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)
	// The artifact claims the integrity rules ran and found nothing, but it did not say it read the diff. That is the
	// shape of a forged artifact: a pass-shaped answer with no known fact behind it.
	h.facts.integrity = action.IntegrityFacts{Known: false}

	res := h.run(t)

	require.Equal(t, gate.VerdictUnknown, res.Verdict)
	c, ok := h.checks.fast()
	require.True(t, ok)
	require.Equal(t, change.CIActionRequired, c.Conclusion)
}

// TestWarningOnlyIntegrityDoesNotBlock pins the rule that only testintegrity's blocking violations reach the clause:
// a warning is shown elsewhere and never makes the fast check fail (A6, ACTION_MODE 5.1).
func TestWarningOnlyIntegrityDoesNotBlock(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)
	h.facts.integrity = action.IntegrityFacts{Known: true, Result: testintegrity.Result{Violations: []testintegrity.Violation{{
		ID: "v1", Rule: testintegrity.RuleMocksChangedSource, Level: testintegrity.LevelWarn,
		Path: "internal/pay/refund_test.go", Message: "the test mocks a file this change edits"}}}}

	res := h.run(t)

	require.Equal(t, gate.VerdictPass, res.Verdict, "a warning is not a blocking violation")
	c, ok := h.checks.fast()
	require.True(t, ok)
	require.Equal(t, change.CISuccess, c.Conclusion)
}

// TestNoProofResultPostsOneCheck keeps the proof check off the roll-up when the log holds no proof result for the
// head: a run must never post a pass it does not have (ACTION_MODE 5.4).
func TestNoProofResultPostsOneCheck(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)

	h.run(t)

	require.Equal(t, 1, h.checks.count(), "only the fast check is posted")
	_, ok := h.checks.fast()
	require.True(t, ok)
}

// TestBadMACResetsState makes the store's Load fail as a bad MAC does, over a log that holds a forged fast result
// claiming a pass. The gate must reset the log, so the forged result is gone, and the run must then report what it can
// actually prove: with a fact it cannot read, that is unknown, and the check is action_required and never success.
func TestBadMACResetsState(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)
	h.seedForgedPass(t)
	h.facts.integrity = action.IntegrityFacts{Known: false}
	h.store.failLoad = fmt.Errorf("load: %w", errBadMAC{})
	g, err := action.NewGate(action.Config{Store: h.store, PRs: h.github, Reviews: h.github, Checks: h.checks,
		Summary: h.summary, Facts: h.facts, Code: h.code, Decode: policyyaml.Decode, Clock: h.clock, IDs: h.ids,
		Resetter: resetter{s: h.store}, IsUnverified: func(err error) bool { return errors.As(err, &errBadMAC{}) }})
	require.NoError(t, err)
	h.gate = g

	res := h.run(t)

	require.Equal(t, gate.VerdictUnknown, res.Verdict)
	types := h.log(t)
	require.Equal(t, change.EventStateReset, types[0], "state.reset is the first event of the new log")
	// The forged fast.reported was written before the reset, so it is gone. The observed facts come back from GitHub
	// (ACTION_MODE 2.1), which is why pr.opened is recorded again after the reset.
	require.Equal(t, 1, countOf(types, change.EventStateReset))

	c, ok := h.checks.fast()
	require.True(t, ok)
	require.Equal(t, change.CIActionRequired, c.Conclusion)
}

// seedForgedPass plants a fast.reported that claims a clean pass, as a writer with the ref but not the key would.
func (h *harness) seedForgedPass(t *testing.T) {
	t.Helper()
	clean := fastcheck.Result{Verdict: gate.VerdictPass, Clauses: []fastcheck.Clause{
		{ID: fastcheck.ClauseSecrets, Status: fastcheck.StatusMet, Reason: "none"},
		{ID: fastcheck.ClauseAnalyzers, Status: fastcheck.StatusMet, Reason: "none"},
		{ID: fastcheck.ClausePinnedTests, Status: fastcheck.StatusMet, Reason: "none"},
		{ID: fastcheck.ClauseTestIntegrity, Status: fastcheck.StatusMet, Reason: "none"},
		{ID: fastcheck.ClauseDiffCoverage, Status: fastcheck.StatusMet, Reason: "none"},
		{ID: fastcheck.ClauseApproval, Status: fastcheck.StatusMet, Reason: "approved"},
		{ID: fastcheck.ClauseMemoryRules, Status: fastcheck.StatusMet, Reason: "none"},
		{ID: fastcheck.ClauseHeimdallFiles, Status: fastcheck.StatusMet, Reason: "none"},
	}}
	ev, err := h.gateEvent(change.EventFastReported, change.NewFastReported(testHead, clean))
	require.NoError(t, err)
	h.store.seed(t, h.changeID, ev)
}

// gateEvent builds one chained-by-the-store event for a seed.
func (h *harness) gateEvent(typ change.EventType, payload change.Payload) (change.Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return change.Event{}, err
	}
	return change.Event{ID: h.ids.NewEvent(), ChangeID: h.changeID, Type: typ,
		Actor:      change.Actor{Kind: change.ActorSystem, ID: "action-gate"},
		OccurredAt: h.clock.now, CorrelationID: "seed", Payload: raw}, nil
}

// TestBadMACWithoutResetterFailsClosed refuses the run when nothing can start a new log: no check may claim anything.
func TestBadMACWithoutResetterFailsClosed(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)
	h.store.failLoad = fmt.Errorf("load: %w", errBadMAC{})

	_, err := h.gate.Run(t.Context(), action.RunIn{ChangeID: h.changeID, Repo: testRepo, Number: testNumber})

	require.ErrorIs(t, err, action.ErrStateUnverified)
	require.Equal(t, 0, h.checks.count(), "nothing may be posted when the log is untrusted")
}

// TestSecondRunPostsOneComment runs the gate twice on the same head. The log gains nothing the second time, the check
// is updated in place and one comment exists.
func TestSecondRunPostsOneComment(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)

	first := h.run(t)
	require.True(t, first.SummaryWritten)
	appends := h.store.appendCount(h.changeID)
	events := len(h.log(t))

	second := h.run(t)

	require.Equal(t, appends, h.store.appendCount(h.changeID), "a repeat on the same head appends nothing")
	require.Len(t, h.log(t), events)
	require.False(t, second.SummaryWritten)
	require.Equal(t, 1, h.summary.comments(), "one comment, updated in place")
	require.Equal(t, int64(0), second.SummaryID, "a run that writes no comment reports none")
	require.Equal(t, 1, h.checks.count(), "one fast check run; a repeat updates it or leaves it alone, never adds one")
}

// countOf counts the events of one type.
func countOf(types []change.EventType, want change.EventType) int {
	n := 0
	for _, t := range types {
		if t == want {
			n++
		}
	}
	return n
}

// errBadMAC stands in for the store's integrity error. The gate never names an adapter (R3), so the harness supplies
// the predicate that decides what a failed load means.
type errBadMAC struct{}

func (errBadMAC) Error() string { return "state log of the change failed verification: mac" }

// --- ingest ---

// commentReader serves a fixed list of review comments.
type commentReader struct {
	comments []ports.ReviewComment
	err      error
}

var _ ports.PRReviewCommentReader = (*commentReader)(nil)

func (c *commentReader) PRReviewComments(_ context.Context, _ change.Repo, _ int) ([]ports.ReviewComment, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.comments, nil
}

// TestIngestMapsACommentToAPayload is the ingest step in isolation: one CodeRabbit comment becomes one payload at head.
func TestIngestMapsACommentToAPayload(t *testing.T) {
	body := "**Issue**: the refund is not idempotent\n\n_Logic_ | _Major_\n\nA retry charges twice."
	ing, err := action.NewIngester(&commentReader{comments: []ports.ReviewComment{{ID: 501, Login: "coderabbitai[bot]",
		UserType: "Bot", Body: body, Path: "internal/pay/refund.go", Line: 14, Side: "RIGHT", CommitID: string(testHead)}}})
	require.NoError(t, err)

	res, err := ing.Ingest(t.Context(), action.IngestIn{Repo: testRepo, Number: testNumber, Head: testHead,
		Sources: []string{"coderabbitai[bot]"}})

	require.NoError(t, err)
	require.Len(t, res.Candidates, 1)
	c := res.Candidates[0]
	require.Equal(t, external.ToolCodeRabbit, c.Source)
	require.Equal(t, int64(501), c.CommentID)
	require.Equal(t, testHead, c.Head)
	require.Equal(t, "internal/pay/refund.go", c.Location.File)
	require.Equal(t, [2]int{14, 14}, c.Location.Lines)
	require.NoError(t, c.Validate())
}

// TestIngestMergesACandidateWithOurFinding is the duplicate rule: same file, overlapping lines and a similar claim
// merge into our finding instead of becoming a second candidate (ACTION_MODE 6.2 step 5).
func TestIngestMapsNothingWithoutADiffForAnOldComment(t *testing.T) {
	body := "**Issue**: the refund is not idempotent\n\n_Logic_ | _Major_\n\nA retry charges twice."
	ing, err := action.NewIngester(&commentReader{comments: []ports.ReviewComment{{ID: 502, Login: "coderabbitai[bot]",
		UserType: "Bot", Body: body, Path: "internal/pay/refund.go", Line: 14, Side: "RIGHT",
		CommitID: string(testHead2)}}})
	require.NoError(t, err)

	res, err := ing.Ingest(t.Context(), action.IngestIn{Repo: testRepo, Number: testNumber, Head: testHead,
		Sources: []string{"coderabbitai[bot]"}})

	require.NoError(t, err)
	require.Empty(t, res.Candidates)
	require.Equal(t, 1, res.Counts.Outdated, "a comment on an older commit with no diff is outdated, not guessed")
}

// TestIngestMergesACandidate is the merge with one of our findings.
func TestIngestMergesACandidate(t *testing.T) {
	body := "**Issue**: the refund is not idempotent\n\n_Logic_ | _Major_\n\nA retry charges twice."
	ing, err := action.NewIngester(&commentReader{comments: []ports.ReviewComment{{ID: 503, Login: "coderabbitai[bot]",
		UserType: "Bot", Body: body, Path: "internal/pay/refund.go", Line: 14, Side: "RIGHT", CommitID: string(testHead)}}})
	require.NoError(t, err)

	res, err := ing.Ingest(t.Context(), action.IngestIn{Repo: testRepo, Number: testNumber, Head: testHead,
		Sources: []string{"coderabbitai[bot]"}, Ours: []action.OurFinding{{
			Ref: "F1", Claim: "The refund is not idempotent: a retry charges twice.",
			Location: findingLocation("internal/pay/refund.go", 10, 20)}}})

	require.NoError(t, err)
	require.Empty(t, res.Candidates)
	require.Len(t, res.Merged, 1)
	require.Equal(t, "F1", res.Merged[0].Finding)
	require.Equal(t, int64(503), res.Merged[0].Candidate.CommentID)
}

// TestIngestCountsAStyleClaimApart keeps a style claim out of the log and in its own count (ACTION_MODE 6.2 step 4).
func TestIngestCountsAStyleClaimApart(t *testing.T) {
	body := "**Issue**: prefer an early return here\n\n_Stability_ | _Nitpick_"
	ing, err := action.NewIngester(&commentReader{comments: []ports.ReviewComment{{ID: 504, Login: "coderabbitai[bot]",
		UserType: "Bot", Body: body, Path: "internal/pay/refund.go", Line: 8, Side: "RIGHT", CommitID: string(testHead)}}})
	require.NoError(t, err)

	res, err := ing.Ingest(t.Context(), action.IngestIn{Repo: testRepo, Number: testNumber, Head: testHead,
		Sources: []string{"coderabbitai[bot]"}})

	require.NoError(t, err)
	require.Empty(t, res.Candidates)
	require.Equal(t, 1, res.Style)
}

// TestIngestRefusesNoHead keeps a malformed head from being recorded.
func TestIngestRefusesNoHead(t *testing.T) {
	ing, err := action.NewIngester(&commentReader{})
	require.NoError(t, err)

	_, err = ing.Ingest(t.Context(), action.IngestIn{Repo: testRepo, Number: testNumber, Head: "not-a-sha"})

	require.Error(t, err)
}

// TestNewIngesterNeedsTheReader refuses a miswired ingester.
func TestNewIngesterNeedsTheReader(t *testing.T) {
	_, err := action.NewIngester(nil)
	require.Error(t, err)
}

// TestNewGateNeedsEveryRequiredPort lists the ports a gate cannot run without.
func TestNewGateNeedsEveryRequiredPort(t *testing.T) {
	_, err := action.NewGate(action.Config{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Store")
}

// findingLocation is a small helper for a finding's place.
func findingLocation(file string, first, last int) finding.Location {
	return finding.Location{File: file, Lines: [2]int{first, last}}
}

// TestMovedHeadRecordsAPushAndReDecides is the break attempt at the log boundary: a new head must record pr.pushed,
// drop the old fast result and post a check for the new head, never reuse the old conclusion.
func TestMovedHeadRecordsAPushAndReDecides(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)
	h.run(t)
	old, ok := h.checks.fast()
	require.True(t, ok)
	require.Equal(t, testHead, h.checks.shas[old.ExternalID])

	// GitHub reports a new head. The approval of the old head no longer counts, so the run is a fail, not a pass.
	h.github.pr.HeadSHA = testHead2
	h.facts.integrity = action.IntegrityFacts{Known: true}
	res := h.run(t)

	require.Equal(t, testHead2, res.Head)
	require.NotEqual(t, gate.VerdictPass, res.Verdict, "the old approval is for the old head and does not carry over")
	types := h.log(t)
	require.Equal(t, 1, countOf(types, change.EventPRPushed))
	require.Equal(t, 2, countOf(types, change.EventFastReported), "a new head gets its own fast result")
	require.Equal(t, 2, h.checks.count(), "each head has its own check run")
	for _, id := range h.checks.all {
		want := testHead
		if strings.Contains(id, string(testHead2)) {
			want = testHead2
		}
		require.Equal(t, want, h.checks.shas[id], "a check run names its own head in its external id")
	}
}

// TestFailedCommentReadIsReportedAndNeverPasses proves that a failed read of the third-party comments is reported in
// the result (not swallowed, R6) and changes nothing else: the clean fast check still decides the verdict, and no
// candidate is invented.
func TestFailedCommentReadIsReportedAndNeverPasses(t *testing.T) {
	ing, err := action.NewIngester(&commentReader{err: errors.New("github: 502 bad gateway")})
	require.NoError(t, err)
	h := newHarnessWith(t, policy.ActionModeEnforce, ing)

	res := h.run(t)

	require.Contains(t, res.IngestFailed, "502 bad gateway")
	require.NotContains(t, h.log(t), change.EventExternalIngested)
	base := newHarness(t, policy.ActionModeEnforce).run(t)
	require.Equal(t, base.Verdict, res.Verdict, "a failed comment read must not change the verdict")
}
