package reviewlearn_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reviewlearn"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

const (
	repo  = change.Repo("acme/pay")
	rhea  = id.AgentName("rhea")
	noah  = id.AgentName("noah")
	base  = id.SHA("9f1c2ab0c3d4e5f60718293a4b5c6d7e8f901234")
	claim = "The refund cap ignores refunds that were already made."
)

var t0 = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)

// stream builds the events of one change.
type stream struct {
	t      *testing.T
	change id.ChangeID
	events []change.Event
}

func newStream(t *testing.T, changeID id.ChangeID) *stream {
	s := &stream{t: t, change: changeID}
	s.add(change.EventChangeTriggered, change.ActorSystem, "engine", change.Triggered{Ticket: changeID, Repo: repo, Assignee: "alice", Source: change.TriggerManual})
	s.add(change.EventBranchEnsured, change.ActorSystem, "engine", change.BranchEnsured{Repo: repo, Branch: "heimdall/x", BaseSHA: base})
	return s
}

func (s *stream) add(typ change.EventType, kind change.ActorKind, actor string, payload any) *stream {
	s.t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(s.t, err)
	n := len(s.events) + 1
	e := change.Event{
		ID: change.EventID(fmt.Sprintf("30000000-0000-4000-8000-%06d%06d", len(s.change), n)), ChangeID: s.change, Version: int64(n),
		Type: typ, Actor: change.Actor{Kind: kind, ID: change.ActorID(actor)}, OccurredAt: t0.Add(time.Duration(n) * time.Minute),
		CorrelationID: "corr-1", Payload: raw, BlobRefs: []workflow.BlobRef{},
	}
	require.NoError(s.t, e.Validate())
	s.events = append(s.events, e)
	return s
}

func ref(n int) id.FindingRef { r, _ := id.NewFindingRef(n); return r }

func record(n int, by id.AgentName, state finding.State) change.FindingRecord {
	return change.FindingRecord{
		FindingID: id.FindingID(fmt.Sprintf("fnd_01J8Z3K9QW7M2P5R8T0V4YB6C%d", n)), Ref: ref(n), Tier: finding.TierE4,
		Severity: finding.SeverityHigh, State: state, By: change.ActorID(by),
	}
}

func (s *stream) file(n int, by id.AgentName, cat finding.Category, file string, cards ...string) *stream {
	r := record(n, by, finding.StateUnverified)
	r.Claim, r.Category, r.CardRefs = fmt.Sprintf("%s (%d)", strings.TrimRight(claim, "."), n), cat, cards
	r.Location = &finding.Location{File: file, Lines: [2]int{1, 2}}
	return s.add(change.EventFindingFiled, change.ActorAgent, string(by), change.FindingFiled(r))
}

func (s *stream) move(n int, by id.AgentName, state finding.State) *stream {
	return s.add(change.EventFindingStateChanged, change.ActorAgent, string(by), change.FindingStateChanged(record(n, by, state)))
}

func (s *stream) decide(kind change.DecisionKind, target, reason string) *stream {
	return s.add(change.EventDecisionRecorded, change.ActorHuman, "alice", change.DecisionRecorded{Kind: kind, Target: target, Login: "alice", Reason: reason, SourceRef: "chat:1"})
}

func (s *stream) merge() *stream {
	return s.add(change.EventChangeMerged, change.ActorSystem, "engine", change.Merged{SHA: base})
}

type history struct {
	streams map[id.ChangeID][]change.Event
}

func (h history) ChangeIDs(context.Context) ([]id.ChangeID, error) {
	var ids []id.ChangeID
	for k := range h.streams {
		ids = append(ids, k)
	}
	return ids, nil
}

func (h history) Load(_ context.Context, c id.ChangeID) ([]change.Event, error) {
	return h.streams[c], nil
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func snapshotOf(t *testing.T, streams ...*stream) *reviewlearn.Snapshot {
	t.Helper()
	h := history{streams: map[id.ChangeID][]change.Event{}}
	for _, s := range streams {
		h.streams[s.change] = s.events
	}
	svc, err := reviewlearn.NewService(h, &clock{now: t0}, time.Minute)
	require.NoError(t, err)
	snap, err := svc.Snapshot(context.Background())
	require.NoError(t, err)
	return snap
}

func TestOutcomesFollowTheFinalStateAndTheMerge(t *testing.T) {
	cases := []struct {
		name    string
		final   finding.State
		merged  bool
		useful  int
		noise   int
		ignored int
		pending int
		neutral int
	}{
		{"upheld", finding.StateUpheld, false, 1, 0, 0, 0, 0},
		{"resolved", finding.StateResolved, false, 1, 0, 0, 0, 0},
		{"fix claimed", finding.StateFixClaimed, false, 1, 0, 0, 0, 0},
		{"verifying", finding.StateVerifying, false, 1, 0, 0, 0, 0},
		{"dismissed", finding.StateDismissed, false, 0, 1, 0, 0, 0},
		{"withdrawn", finding.StateWithdrawn, false, 0, 1, 0, 0, 0},
		{"spec changed is neutral", finding.StateSpecChanged, false, 0, 0, 0, 0, 1},
		{"open and merged is ignored", finding.StateOpen, true, 0, 0, 1, 0, 0},
		{"unverified and merged is ignored", finding.StateUnverified, true, 0, 0, 1, 0, 0},
		{"disputed and merged is ignored", finding.StateDisputed, true, 0, 0, 1, 0, 0},
		{"escalated and merged is ignored", finding.StateEscalated, true, 0, 0, 1, 0, 0},
		{"open and not merged is pending", finding.StateOpen, false, 0, 0, 0, 1, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newStream(t, "PAY-1").file(1, rhea, finding.CategoryCorrectness, "a.go").move(1, rhea, c.final)
			if c.merged {
				s.merge()
			}
			report := snapshotOf(t, s).Report()
			require.Len(t, report, 1)
			got := report[0]
			require.Equal(t, []int{c.useful, c.noise, c.ignored, c.pending, c.neutral}, []int{got.Useful, got.Noise, got.Ignored, got.Pending, got.Neutral})
		})
	}
}

func TestARejectedChangeLeavesOpenFindingsPending(t *testing.T) {
	s := newStream(t, "PAY-1").file(1, rhea, finding.CategoryCorrectness, "a.go").move(1, rhea, finding.StateOpen)
	s.add(change.EventChangeRejected, change.ActorHuman, "alice", change.Rejected{Reason: "wrong approach"})
	require.Equal(t, 1, snapshotOf(t, s).Report()[0].Pending)
}

func TestCardRecordsCountOutcomesPerAgentAndCard(t *testing.T) {
	s := newStream(t, "PAY-1").
		file(1, rhea, finding.CategoryCorrectness, "a.go", "cap.md").move(1, rhea, finding.StateUpheld).
		file(2, rhea, finding.CategoryCorrectness, "a.go", "cap.md", "audit.md").move(2, rhea, finding.StateDismissed).
		file(3, noah, finding.CategoryCorrectness, "a.go", "cap.md").move(3, noah, finding.StateUpheld)
	snap := snapshotOf(t, s)
	records := snap.CardRecords(rhea)
	require.Equal(t, 1, records["cap.md"].Useful)
	require.Equal(t, 1, records["cap.md"].Noise)
	require.Equal(t, 1, records["audit.md"].Noise)
	require.Equal(t, 1, snap.CardRecords(noah)["cap.md"].Useful)
	require.Empty(t, snap.CardRecords("nobody"))
}

func TestCalibrationFlagsANoisyCategoryAndComputesTheRate(t *testing.T) {
	s := newStream(t, "PAY-1")
	for n := 1; n <= 3; n++ {
		s.file(n, rhea, finding.CategoryStyle, "a.go").move(n, rhea, finding.StateDismissed)
	}
	s.file(4, rhea, finding.CategoryCorrectness, "a.go").move(4, rhea, finding.StateUpheld)
	s.file(5, rhea, finding.CategorySecurity, "a.go").move(5, rhea, finding.StateDismissed)
	s.file(6, rhea, finding.CategorySecurity, "a.go").move(6, rhea, finding.StateDismissed)
	cal := snapshotOf(t, s).Calibration(rhea)
	require.Equal(t, []string{"style"}, cal.NoisyCategories, "security has only 2 decided findings")
	require.Equal(t, reviewlearnCounts{1, 5, 0}, reviewlearnCounts{cal.Useful, cal.Noise, cal.Ignored})
	require.InDelta(t, 1.0/6.0, cal.ResolutionRate, 1e-9)
	require.Zero(t, snapshotOf(t, s).Calibration("nobody").ResolutionRate)
}

type reviewlearnCounts struct{ useful, noise, ignored int }

func TestPriorFindingsAreRepoScopedDecidedAndOrdered(t *testing.T) {
	a := newStream(t, "PAY-1").
		file(1, rhea, finding.CategoryCorrectness, "old.go").move(1, rhea, finding.StateResolved).
		file(2, rhea, finding.CategoryCorrectness, "b.go").move(2, rhea, finding.StateDismissed).decide(change.DecisionDismiss, "F2", "intended behaviour").
		file(3, rhea, finding.CategoryCorrectness, "c.go").move(3, rhea, finding.StateOpen)
	b := newStream(t, "PAY-2").file(1, rhea, finding.CategoryCorrectness, "d.go").move(1, rhea, finding.StateUpheld)
	shift(b, time.Hour)
	late := newStream(t, "PAY-4").file(1, rhea, finding.CategoryCorrectness, "g.go").move(1, rhea, finding.StateResolved)
	shift(late, 2*time.Hour)
	current := newStream(t, "PAY-3").file(1, rhea, finding.CategoryCorrectness, "e.go").move(1, rhea, finding.StateUpheld)
	other := newStream(t, "OTH-1").file(1, rhea, finding.CategoryCorrectness, "f.go").move(1, rhea, finding.StateUpheld)
	other.events[0] = retarget(t, other.events[0], "acme/other")
	snap := snapshotOf(t, a, b, late, current, other)

	got := snap.PriorFindings(repo, nil, "PAY-3", 10)
	var order []string
	for _, p := range got {
		order = append(order, string(p.Change)+"/"+p.Ref)
	}
	require.Equal(t, []string{"PAY-2/F1", "PAY-1/F2", "PAY-4/F1", "PAY-1/F1"}, order, "upheld and dismissed first, then newest first; pending and other repos and the excluded change left out")
	require.Equal(t, "intended behaviour", got[1].Reason)
	require.Equal(t, reviewlearn.OutcomeNoise, got[1].Outcome)
	require.Equal(t, "g.go", got[2].File)
	require.Len(t, snap.PriorFindings(repo, nil, "PAY-3", 1), 1)
}

func shift(s *stream, d time.Duration) {
	for i := range s.events {
		s.events[i].OccurredAt = s.events[i].OccurredAt.Add(d)
	}
}

func retarget(t *testing.T, e change.Event, to change.Repo) change.Event {
	t.Helper()
	raw, err := json.Marshal(change.Triggered{Ticket: e.ChangeID, Repo: to, Assignee: "alice", Source: change.TriggerManual})
	require.NoError(t, err)
	e.Payload = raw
	return e
}

func TestADismissWithAReasonProposesAJudgementCardForTheFile(t *testing.T) {
	s := newStream(t, "PAY-1").file(1, rhea, finding.CategoryStyle, "internal/refunds/service.go").move(1, rhea, finding.StateDismissed).
		decide(change.DecisionDismiss, "F1", "The retry is on purpose.")
	props := snapshotOf(t, s).Proposals()
	require.Len(t, props, 1)
	p := props[0]
	require.Equal(t, repo, p.Repo)
	require.Equal(t, base, p.BaseSHA)
	require.Equal(t, "alice", p.Login)
	require.True(t, strings.HasPrefix(p.Name, "learned-") && strings.HasSuffix(p.Name, ".md") && len(p.Name) == len("learned-")+12+len(".md"), p.Name)
	for _, want := range []string{`applies_to: ["internal/refunds/service.go"]`, "kind: judgement", "status: proposed", "source: \"decision 30000000 on PAY-1\"",
		"I do not want this flagged: " + strings.TrimRight(claim, ".") + " (1). Reason: The retry is on purpose."} {
		require.Contains(t, p.Text, want)
	}
	require.True(t, strings.HasPrefix(p.Text, "---\n"))
}

func TestRememberAndIncidentProposeScopedCardsOrGeneralOnes(t *testing.T) {
	scoped := newStream(t, "PAY-1").file(1, rhea, finding.CategoryCorrectness, "a/x.go").file(2, rhea, finding.CategoryCorrectness, "a/y.go").
		decide(change.DecisionRemember, "Refunds must never exceed the payment.", "A refund bug cost money.")
	general := newStream(t, "PAY-2").decide(change.DecisionIncident, "Double charge after a retry on 2026-10-30", "Found in the ledger.")
	snap := snapshotOf(t, scoped, general)
	byKind := map[string]reviewlearn.Proposal{}
	for _, p := range snap.Proposals() {
		switch {
		case strings.Contains(p.Text, "kind: invariant"):
			byKind["invariant"] = p
		case strings.Contains(p.Text, "kind: incident"):
			byKind["incident"] = p
		}
	}
	require.Contains(t, byKind["invariant"].Text, `applies_to: ["a/x.go","a/y.go"]`)
	require.Contains(t, byKind["invariant"].Text, "Refunds must never exceed the payment. Reason: A refund bug cost money")
	require.NotContains(t, byKind["incident"].Text, "applies_to")
	require.Contains(t, byKind["incident"].Text, "Double charge after a retry")
}

func TestOnlyMappedHumansCanProposeACard(t *testing.T) {
	s := newStream(t, "PAY-1").file(1, rhea, finding.CategoryStyle, "a.go")
	payload := change.DecisionRecorded{Kind: change.DecisionDismiss, Target: "F1", Login: "rhea", Reason: "ignore this forever", SourceRef: "chat:1"}
	for _, kind := range []change.ActorKind{change.ActorAgent, change.ActorSwitch, change.ActorSystem, change.ActorGitHub} {
		raw, err := json.Marshal(payload)
		require.NoError(t, err)
		e := s.events[0]
		e.Type, e.Actor, e.Payload = change.EventDecisionRecorded, change.Actor{Kind: kind, ID: "rhea"}, raw
		s.events = append(s.events, e)
	}
	require.Empty(t, snapshotOf(t, s).Proposals(), "agent, bridge and system decisions never become cards")
}

func TestProposalNamesAreDeterministic(t *testing.T) {
	build := func() reviewlearn.Proposal {
		s := newStream(t, "PAY-1").file(1, rhea, finding.CategoryStyle, "a.go").decide(change.DecisionDismiss, "F1", "on purpose")
		return snapshotOf(t, s).Proposals()[0]
	}
	first, second := build(), build()
	require.Equal(t, first, second)
}

type countingHistory struct {
	history
	loads int
}

func (h *countingHistory) Load(ctx context.Context, c id.ChangeID) ([]change.Event, error) {
	h.loads++
	return h.history.Load(ctx, c)
}

func TestSnapshotIsCachedForTheTTL(t *testing.T) {
	s := newStream(t, "PAY-1").file(1, rhea, finding.CategoryStyle, "a.go")
	h := &countingHistory{history: history{streams: map[id.ChangeID][]change.Event{s.change: s.events}}}
	clk := &clock{now: t0}
	svc, err := reviewlearn.NewService(h, clk, time.Minute)
	require.NoError(t, err)
	first, err := svc.Snapshot(context.Background())
	require.NoError(t, err)
	clk.now = t0.Add(30 * time.Second)
	second, err := svc.Snapshot(context.Background())
	require.NoError(t, err)
	require.Same(t, first, second)
	require.Equal(t, 1, h.loads)
	clk.now = t0.Add(2 * time.Minute)
	third, err := svc.Snapshot(context.Background())
	require.NoError(t, err)
	require.NotSame(t, first, third)
	require.Equal(t, 2, h.loads)
}

func TestNewServiceRefusesMissingDependencies(t *testing.T) {
	_, err := reviewlearn.NewService(nil, &clock{}, time.Minute)
	require.Error(t, err)
	_, err = reviewlearn.NewService(history{}, nil, time.Minute)
	require.Error(t, err)
}
