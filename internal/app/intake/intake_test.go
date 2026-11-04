package intake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	testRepo    change.Repo = "acme/pay"
	shaBase     id.SHA      = "1111111111111111111111111111111111111111"
	shaHead1    id.SHA      = "2222222222222222222222222222222222222222"
	shaHead2    id.SHA      = "3333333333333333333333333333333333333333"
	shaTree     id.SHA      = "4444444444444444444444444444444444444444"
	ticketPay42 id.ChangeID = "PAY-142"
	fixturePath             = "../../../testdata/repos/pay-demo/base/heimdall.yaml"
	testTaskID              = "tsk_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	testRunID               = "run_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	testBlob                = "blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// seededToBuilding is how many events rig.seed writes to take a ticket change from TRIGGERED to BUILDING.
const seededToBuilding = 6

// fakeEvents is an in-memory EventStore with the same version rule as the real one: Append refuses unless expected
// is the stream's last version. conflicts makes the next n Append calls refuse, to exercise the retry loop.
type fakeEvents struct {
	streams   map[id.ChangeID][]change.Event
	effects   []change.Effect
	calls     int
	conflicts int
	loadErr   error
	appendErr error
}

func (f *fakeEvents) Load(_ context.Context, c id.ChangeID) ([]change.Event, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return append([]change.Event(nil), f.streams[c]...), nil
}

func (f *fakeEvents) Append(_ context.Context, c id.ChangeID, expected int64, evs []change.Event, fx []change.Effect) (int64, error) {
	f.calls++
	if f.appendErr != nil {
		return 0, f.appendErr
	}
	if f.conflicts > 0 {
		f.conflicts--
		return 0, ports.ErrVersionConflict
	}
	var cur int64
	if n := len(f.streams[c]); n > 0 {
		cur = f.streams[c][n-1].Version
	}
	if cur != expected {
		return 0, ports.ErrVersionConflict
	}
	f.streams[c] = append(f.streams[c], evs...)
	f.effects = append(f.effects, fx...)
	return cur + int64(len(evs)), nil
}

// fakePRs returns one pull request and its files.
type fakePRs struct {
	pr       ports.PR
	files    []ports.ChangedFile
	prErr    error
	filesErr error
}

func (f *fakePRs) PR(context.Context, change.Repo, int) (ports.PR, error) {
	if f.prErr != nil {
		return ports.PR{}, f.prErr
	}
	return f.pr, nil
}

func (f *fakePRs) OpenPRs(context.Context, change.Repo) ([]ports.PR, error) {
	return nil, errors.New("OpenPRs is not used by intake")
}

func (f *fakePRs) Files(context.Context, change.Repo, int) ([]ports.ChangedFile, error) {
	if f.filesErr != nil {
		return nil, f.filesErr
	}
	return f.files, nil
}

// fakeCode serves files and tree hashes keyed by sha and path, and records every sha it was asked about.
type fakeCode struct {
	files   map[string][]byte
	trees   map[string]bool
	readAt  []id.SHA
	treeAt  []string
	readErr error
	treeErr error
}

func pathKey(sha id.SHA, path string) string { return string(sha) + " " + path }

func (f *fakeCode) ReadFile(_ context.Context, _ change.Repo, sha id.SHA, path string) ([]byte, error) {
	f.readAt = append(f.readAt, sha)
	if f.readErr != nil {
		return nil, f.readErr
	}
	b, ok := f.files[pathKey(sha, path)]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return b, nil
}

func (f *fakeCode) TreeHash(_ context.Context, _ change.Repo, sha id.SHA, dir string) (id.SHA, error) {
	f.treeAt = append(f.treeAt, pathKey(sha, dir))
	if f.treeErr != nil {
		return "", f.treeErr
	}
	if !f.trees[pathKey(sha, dir)] {
		return "", ports.ErrNotFound
	}
	return shaTree, nil
}

func (f *fakeCode) Tarball(context.Context, change.Repo, id.SHA) (io.ReadCloser, error) {
	return nil, errors.New("tarball is not used by intake")
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

// seqIDs makes sequential event ids. The other id kinds are not used by intake.
type seqIDs struct{ n int }

func (s *seqIDs) NewEvent() change.EventID {
	s.n++
	return change.EventID(fmt.Sprintf("00000000-0000-4000-8000-%012d", s.n))
}

func (s *seqIDs) NewFinding() id.FindingID { return "" }
func (s *seqIDs) NewRun() id.RunID         { return "" }
func (s *seqIDs) NewCase() id.CaseID       { return "" }
func (s *seqIDs) NewTask() id.TaskID       { return "" }

// rig is one intake wired to fakes. The policy at the base sha is the project's own fixture.
type rig struct {
	ev    *fakeEvents
	prs   *fakePRs
	code  *fakeCode
	clock fixedClock
	ids   *seqIDs
	in    *Intake
}

func newRig(t *testing.T) *rig {
	t.Helper()
	pol, err := os.ReadFile(fixturePath)
	require.NoError(t, err)
	r := &rig{
		ev:    &fakeEvents{streams: map[id.ChangeID][]change.Event{}},
		prs:   &fakePRs{},
		code:  &fakeCode{files: map[string][]byte{}, trees: map[string]bool{}},
		clock: fixedClock{at: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)},
		ids:   &seqIDs{},
	}
	r.code.files[pathKey(shaBase, policyPath)] = pol
	in, err := New(Config{Events: r.ev, PRs: r.prs, Code: r.code, Clock: r.clock, IDs: r.ids, Policy: policyyaml.Decode})
	require.NoError(t, err)
	r.in = in
	return r
}

// pullRequest is an open PR by ana against the base sha.
func pullRequest(n int, title, branch string, head id.SHA) ports.PR {
	return ports.PR{Repo: testRepo, Number: n, Title: title, Branch: branch, Author: "ana",
		BaseSHA: shaBase, HeadSHA: head, State: ports.PRStateOpen}
}

// testsOnlyFiles is a diff that touches only the ticket's test directory, so it needs no justification.
func testsOnlyFiles() []ports.ChangedFile {
	return []ports.ChangedFile{{Path: "tests/heimdall/PAY-142/refund_test.go", Status: ports.FileAdded, Additions: 40}}
}

// sourceFiles is a diff that touches product code, which needs a justification when it has no expected paths.
func sourceFiles() []ports.ChangedFile {
	return []ports.ChangedFile{{Path: "internal/refund/round.go", Status: ports.FileModified, Additions: 12, Deletions: 3}}
}

type step struct {
	et change.EventType
	p  change.Payload
}

// event builds one envelope for a seeded stream.
func (r *rig) event(t *testing.T, stream id.ChangeID, version int64, et change.EventType, p change.Payload) change.Event {
	t.Helper()
	require.NoError(t, p.Validate())
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	eid := r.ids.NewEvent()
	return change.Event{ID: eid, ChangeID: stream, Version: version, Type: et,
		Actor:      change.Actor{Kind: change.ActorSystem, ID: "heimdall"},
		OccurredAt: r.clock.at, CorrelationID: change.CorrelationID(eid.String()), Payload: raw}
}

// seed writes a ticket change through the real reducer's path. With toBuilding false the stream stops at TRIGGERED;
// with true it runs to BUILDING, the state in which a PR opens the PR state.
func (r *rig) seed(t *testing.T, toBuilding bool) {
	t.Helper()
	ticket := ticketPay42
	steps := []step{{change.EventChangeTriggered,
		change.Triggered{Ticket: ticket, Repo: testRepo, Assignee: "ana", Source: change.TriggerJira}}}
	if toBuilding {
		blob := workflow.BlobRef(testBlob)
		steps = append(steps,
			step{change.EventRoomOpened, change.RoomOpened{RoomID: "room_01"}},
			step{change.EventTaskAssigned, change.TaskAssigned{TaskID: testTaskID, Agent: "tester",
				Role: workflow.RoleTestAuthor, Deadline: r.clock.at.Add(time.Hour), BundleRef: blob}},
			step{change.EventTestsSubmitted, change.TestsSubmitted{Refs: []string{"AC1"},
				ACMap: map[string][]string{"AC1": {"tests/heimdall/PAY-142/refund_test.go"}}, SHA: shaHead1, TreeHash: shaTree}},
			step{change.EventTestsVerified, change.TestsVerified{RunIDs: []id.RunID{testRunID}, SHA: shaHead1,
				TreeHash: shaTree, NeedsSignoff: false}},
			step{change.EventTestsCommitted, change.TestsCommitted{SHA: shaHead1, TreeHash: shaTree}},
		)
	}
	for i, s := range steps {
		r.ev.streams[ticket] = append(r.ev.streams[ticket], r.event(t, ticket, int64(i+1), s.et, s.p))
	}
}

func types(evs []change.Event) []change.EventType {
	out := make([]change.EventType, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Type)
	}
	return out
}

// lastGate returns the gate check payload of the most recent effect.
func (r *rig) lastGate(t *testing.T) ports.Check {
	t.Helper()
	require.NotEmpty(t, r.ev.effects)
	fx := r.ev.effects[len(r.ev.effects)-1]
	require.Equal(t, change.EffectGitHubCheck, fx.Kind)
	var c ports.Check
	require.NoError(t, json.Unmarshal(fx.Payload, &c))
	return c
}

func slopOf(t *testing.T, e change.Event) change.SlopEvaluated {
	t.Helper()
	p, err := change.Decode(e)
	require.NoError(t, err)
	s, ok := p.(change.SlopEvaluated)
	require.True(t, ok, "event %s is not slop.evaluated", e.Type)
	return s
}

func TestBadPRFailsTheSlopRuleAndNamesTheRule(t *testing.T) {
	r := newRig(t)
	r.prs.pr = pullRequest(7, "refund rounding", "feature/refund", shaHead1)
	r.prs.files = sourceFiles()

	res, err := r.in.HandlePR(context.Background(), testRepo, 7)
	require.NoError(t, err)
	require.True(t, res.Appended)
	require.False(t, res.Pass)

	evs := r.ev.streams[res.Stream]
	require.Equal(t, []change.EventType{change.EventPROpened, change.EventSlopEvaluated}, types(evs))
	rules := slopOf(t, evs[1]).FailingRules
	require.Contains(t, rules, "ticket_linked")
	require.Contains(t, rules, "tests_present")
	require.Contains(t, rules, "scope")

	c := r.lastGate(t)
	require.Equal(t, ports.CheckCompleted, c.Status)
	require.Equal(t, change.CIFailure, c.Conclusion)
	require.Contains(t, c.Summary, "ticket_linked")
	require.Equal(t, gateCheck, c.Name)
}

func TestGoodPRPassesAndTheGateStaysPending(t *testing.T) {
	r := newRig(t)
	r.seed(t, true)
	r.prs.pr = pullRequest(3, "PAY-142 refund rounding", "feature/PAY-142-refund", shaHead1)
	r.prs.files = testsOnlyFiles()

	res, err := r.in.HandlePR(context.Background(), testRepo, 3)
	require.NoError(t, err)
	require.True(t, res.Appended)
	require.True(t, res.Pass)
	require.Equal(t, ticketPay42, res.Stream)

	evs := r.ev.streams[ticketPay42][seededToBuilding:]
	require.Equal(t, []change.EventType{change.EventPROpened, change.EventSlopEvaluated}, types(evs))
	require.True(t, slopOf(t, evs[1]).Pass)

	c := r.lastGate(t)
	require.Equal(t, ports.CheckInProgress, c.Status)
	require.Empty(t, c.Conclusion)
	require.Contains(t, c.Summary, "stays pending")
}

func TestPolicyIsReadAtTheMergeBaseNeverTheHead(t *testing.T) {
	r := newRig(t)
	r.seed(t, true)
	r.prs.pr = pullRequest(3, "PAY-142 refund", "feature/PAY-142-refund", shaHead1)
	r.prs.files = testsOnlyFiles()

	_, err := r.in.HandlePR(context.Background(), testRepo, 3)
	require.NoError(t, err)
	require.NotEmpty(t, r.code.readAt)
	for _, sha := range r.code.readAt {
		require.Equal(t, shaBase, sha, "heimdall.yaml must be read at the base sha, never the head")
	}
}

func TestTestTreeIsCheckedOnlyWhenThePRTouchesNoTests(t *testing.T) {
	cases := []struct {
		name     string
		files    []ports.ChangedFile
		tree     bool
		wantPass bool
		wantTree bool
	}{
		{"tests changed, tree not consulted", testsOnlyFiles(), false, true, false},
		{"no tests changed, tree present", nil, true, true, true},
		{"no tests changed, tree absent", nil, false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			r.seed(t, true)
			r.code.trees[pathKey(shaHead1, "tests/heimdall/PAY-142")] = c.tree
			r.prs.pr = pullRequest(3, "PAY-142 refund", "feature/PAY-142-refund", shaHead1)
			r.prs.files = c.files

			res, err := r.in.HandlePR(context.Background(), testRepo, 3)
			require.NoError(t, err)
			require.Equal(t, c.wantPass, res.Pass)
			if c.wantTree {
				require.Equal(t, []string{pathKey(shaHead1, "tests/heimdall/PAY-142")}, r.code.treeAt)
			} else {
				require.Empty(t, r.code.treeAt)
			}
		})
	}
}

func TestInvalidPolicyFailsClosedAndNamesThePolicy(t *testing.T) {
	fixture, err := os.ReadFile(fixturePath)
	require.NoError(t, err)
	cases := map[string]func(*rig){
		"missing": func(r *rig) { delete(r.code.files, pathKey(shaBase, policyPath)) },
		"empty":   func(r *rig) { r.code.files[pathKey(shaBase, policyPath)] = []byte("") },
		"unknown key": func(r *rig) {
			r.code.files[pathKey(shaBase, policyPath)] = append([]byte("bogus_key: 1\n"), fixture...)
		},
		"does not parse": func(r *rig) { r.code.files[pathKey(shaBase, policyPath)] = []byte("slop: [unclosed") },
		"fails validation": func(r *rig) {
			r.code.files[pathKey(shaBase, policyPath)] = []byte(strings.Replace(string(fixture), "dir: tests/heimdall", "dir: .", 1))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.seed(t, true)
			mutate(r)
			r.prs.pr = pullRequest(3, "PAY-142 refund", "feature/PAY-142-refund", shaHead1)
			r.prs.files = testsOnlyFiles()

			res, err := r.in.HandlePR(context.Background(), testRepo, 3)
			require.NoError(t, err)
			require.True(t, res.Appended, "a fail-closed verdict is still recorded")
			require.False(t, res.Pass)
			rules := slopOf(t, r.ev.streams[ticketPay42][len(r.ev.streams[ticketPay42])-1]).FailingRules
			require.Len(t, rules, 1)
			require.True(t, strings.HasPrefix(rules[0], ruleInvalidPolicy+": "), "rule %q", rules[0])
			c := r.lastGate(t)
			require.Equal(t, ports.CheckCompleted, c.Status)
			require.Equal(t, change.CIFailure, c.Conclusion)
		})
	}
}

func TestTransportErrorsAppendNothingSoThePollRetries(t *testing.T) {
	boom := errors.New("github unreachable")
	cases := map[string]func(*rig){
		"pr read":     func(r *rig) { r.prs.prErr = boom },
		"files read":  func(r *rig) { r.prs.filesErr = boom },
		"policy read": func(r *rig) { r.code.readErr = boom },
		"tests tree":  func(r *rig) { r.code.treeErr = boom },
		"load":        func(r *rig) { r.ev.loadErr = boom },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.seed(t, true)
			before := len(r.ev.streams[ticketPay42])
			mutate(r)
			r.prs.pr = pullRequest(3, "PAY-142 refund", "feature/PAY-142-refund", shaHead1)
			r.prs.files = nil // no tests changed, so the tests tree is consulted

			_, err := r.in.HandlePR(context.Background(), testRepo, 3)
			require.ErrorIs(t, err, boom)
			require.Len(t, r.ev.streams[ticketPay42], before)
			require.Empty(t, r.ev.effects)
		})
	}
}

func TestSameHeadTwiceAppendsNothing(t *testing.T) {
	r := newRig(t)
	r.seed(t, true)
	r.prs.pr = pullRequest(3, "PAY-142 refund", "feature/PAY-142-refund", shaHead1)
	r.prs.files = testsOnlyFiles()

	first, err := r.in.HandlePR(context.Background(), testRepo, 3)
	require.NoError(t, err)
	require.True(t, first.Appended)
	calls := r.ev.calls
	n := len(r.ev.streams[ticketPay42])

	second, err := r.in.HandlePR(context.Background(), testRepo, 3)
	require.NoError(t, err)
	require.False(t, second.Appended)
	require.Equal(t, calls, r.ev.calls, "a handled head must not call Append again")
	require.Len(t, r.ev.streams[ticketPay42], n)
}

func TestMovedHeadAppendsPushedThenEvaluatesAgain(t *testing.T) {
	r := newRig(t)
	r.seed(t, true)
	r.prs.files = testsOnlyFiles()
	ctx := context.Background()

	r.prs.pr = pullRequest(3, "PAY-142 refund", "feature/PAY-142-refund", shaHead1)
	_, err := r.in.HandlePR(ctx, testRepo, 3)
	require.NoError(t, err)
	firstKey := r.ev.effects[len(r.ev.effects)-1].Key

	r.prs.pr = pullRequest(3, "PAY-142 refund", "feature/PAY-142-refund", shaHead2)
	res, err := r.in.HandlePR(ctx, testRepo, 3)
	require.NoError(t, err)
	require.True(t, res.Appended)
	require.Equal(t, []change.EventType{change.EventPROpened, change.EventSlopEvaluated,
		change.EventPRPushed, change.EventSlopEvaluated}, types(r.ev.streams[ticketPay42][seededToBuilding:]))
	require.NotEqual(t, firstKey, r.ev.effects[len(r.ev.effects)-1].Key,
		"a new head and a new version must give a new effect key")
}

func TestVersionConflictIsRetriedAndSucceeds(t *testing.T) {
	r := newRig(t)
	r.seed(t, true)
	r.ev.conflicts = 2
	r.prs.pr = pullRequest(3, "PAY-142 refund", "feature/PAY-142-refund", shaHead1)
	r.prs.files = testsOnlyFiles()

	res, err := r.in.HandlePR(context.Background(), testRepo, 3)
	require.NoError(t, err)
	require.True(t, res.Appended)
	require.Equal(t, 3, r.ev.calls, "two refused appends, then one that lands")
	require.Len(t, r.ev.streams[ticketPay42], seededToBuilding+2, "the refused attempts appended nothing")
}

func TestVersionConflictRetriesAreBounded(t *testing.T) {
	r := newRig(t)
	r.seed(t, true)
	r.ev.conflicts = 1000
	r.prs.pr = pullRequest(3, "PAY-142 refund", "feature/PAY-142-refund", shaHead1)
	r.prs.files = testsOnlyFiles()

	_, err := r.in.HandlePR(context.Background(), testRepo, 3)
	require.ErrorIs(t, err, ports.ErrVersionConflict)
	require.Equal(t, 6, r.ev.calls, "one attempt plus the five retries TECH_SPEC 2.4 allows, then stop")
	require.Len(t, r.ev.streams[ticketPay42], seededToBuilding)
}

func TestUnknownTicketIsErrNoChangeAndAppendsNothing(t *testing.T) {
	r := newRig(t)
	r.prs.pr = pullRequest(9, "PAY-999 refund", "feature/PAY-999-refund", shaHead1)
	r.prs.files = testsOnlyFiles()

	_, err := r.in.HandlePR(context.Background(), testRepo, 9)
	require.ErrorIs(t, err, ErrNoChange)
	require.Zero(t, r.ev.calls)
	require.Empty(t, r.ev.streams)
}

func TestSecondPRForATicketIsRefusedWithoutAppend(t *testing.T) {
	r := newRig(t)
	r.seed(t, true)
	r.prs.files = testsOnlyFiles()
	r.prs.pr = pullRequest(3, "PAY-142 refund", "feature/PAY-142-refund", shaHead1)
	_, err := r.in.HandlePR(context.Background(), testRepo, 3)
	require.NoError(t, err)
	before := len(r.ev.streams[ticketPay42])

	r.prs.pr = pullRequest(4, "PAY-142 refund again", "feature/PAY-142-again", shaHead2)
	_, err = r.in.HandlePR(context.Background(), testRepo, 4)
	require.ErrorIs(t, err, ErrSecondPR)
	require.Len(t, r.ev.streams[ticketPay42], before)
}

func TestPRAttachedBeforeBuildingKeepsItsState(t *testing.T) {
	r := newRig(t)
	r.seed(t, false)
	r.prs.pr = pullRequest(3, "PAY-142 refund", "feature/PAY-142-refund", shaHead1)
	r.prs.files = testsOnlyFiles()

	res, err := r.in.HandlePR(context.Background(), testRepo, 3)
	require.NoError(t, err)
	require.True(t, res.Appended)
	require.True(t, res.Pass)

	state, err := change.Replay(r.ev.streams[ticketPay42])
	require.NoError(t, err)
	require.Equal(t, change.StateTriggered, state, "a PR before BUILDING does not move the change")
}

func TestPRWithNoTicketGetsItsOwnUnlinkedStream(t *testing.T) {
	r := newRig(t)
	r.prs.pr = pullRequest(7, "refund rounding", "feature/refund", shaHead1)
	r.prs.files = sourceFiles()

	res, err := r.in.HandlePR(context.Background(), testRepo, 7)
	require.NoError(t, err)
	require.Equal(t, id.ChangeID("pr-acme-pay-7"), res.Stream)
	require.False(t, res.Pass)

	state, err := change.Replay(r.ev.streams[res.Stream])
	require.NoError(t, err)
	require.Equal(t, change.StateUnlinkedPR, state)
}

func TestNewRefusesEveryNilPortByName(t *testing.T) {
	_, err := New(Config{})
	require.Error(t, err)
	for _, name := range []string{"Events", "PRs", "Code", "Clock", "IDs"} {
		require.Contains(t, err.Error(), name)
	}
}
