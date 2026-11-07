package change

import (
	"errors"
	"fmt"
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// ErrIllegalTransition is the refusal of the reducer: the event may not move the change out of its state, either
// because the table has no move for it or because its content refuses the move. Branch with errors.Is.
var ErrIllegalTransition = errors.New("illegal transition")

// noState is the state before the first event of a stream. It is the zero State and is not in AllStates.
const noState State = ""

// rule is one cell of the transition table: the next state for an event of the cell's type. It returns an error that
// wraps ErrIllegalTransition when the move is refused, or the decode error of a payload that does not decode.
type rule func(e Event) (State, error)

// table maps a state to the event types it accepts and the rule for each. A missing cell is an illegal transition.
type table map[State]map[EventType]rule

// Reduce returns the state after e, following TECH_SPEC 3.3, or it returns s unchanged and an error. The error wraps
// ErrIllegalTransition when e may not happen in s; a payload that does not decode is a different error.
//
// The table is built on each call, because package-level mutable state is not allowed in the domain (R7). Only the
// events named in doc.go have their payloads decoded; the others move on their type alone.
func Reduce(s State, e Event) (State, error) {
	cell, ok := transitions()[s][e.Type]
	if !ok {
		cell = refuse("the spec has no move for this event in this state")
	}
	next, err := cell(e)
	if err != nil {
		return s, fmt.Errorf("state %s, event %s: %w", stateName(s), e.Type, err)
	}
	return next, nil
}

// Replay applies events in order from the empty state and returns the state they reach. An empty stream reaches the
// empty state, which is not an error. On a refused event it returns the state before that event and an error that
// names the event's position; the error wraps ErrIllegalTransition when the refusal is illegal, and it does not
// check versions or the hash chain (VerifyChain does that).
func Replay(events []Event) (State, error) {
	var s State
	for i, e := range events {
		next, err := Reduce(s, e)
		if err != nil {
			return s, fmt.Errorf("event %d of %d: %w", i+1, len(events), err)
		}
		s = next
	}
	return s, nil
}

func stateName(s State) string {
	if s == noState {
		return "(no state)"
	}
	return string(s)
}

// transitions builds the whole table from the rules of TECH_SPEC 3.3, D146 and the review of the spec in doc.go.
func transitions() table {
	t := table{}
	addStart(t)
	addBeforeBuilding(t)
	addPRStates(t)
	addReview(t)
	addMerge(t)
	addUnlinked(t)
	addEverywhere(t)
	return t
}

// addStart is the first event of a stream: a change is triggered, or a PR with no ticket opens its own stream (D146).
func addStart(t table) {
	t.set(noState, EventChangeTriggered, to(StateTriggered))
	t.set(noState, EventPROpened, when(ticketed, refuse("a PR that names a ticket belongs to that ticket's change"), to(StateUnlinkedPR)))
}

// addBeforeBuilding covers TRIGGERED through BUILDING. A PR may attach before BUILDING and keep the state it is in
// (D146); the first push after tests are committed opens the PR state.
func addBeforeBuilding(t table) {
	attach := []State{StateTriggered, StateRoomOpen, StateWritingTests, StateVerifyingTests, StateAwaitingTestSignoff, StateTestsReady}
	t.each(attach, EventPROpened, func(s State) rule {
		return when(ticketed, to(s), refuse("a PR without a ticket has no change before BUILDING"))
	})
	t.each(attach, EventPRPushed, to)
	t.each(attach, EventSlopEvaluated, to) // slop judges an attached PR at once; the gate still says tests are not ready (D146)
	t.set(StateBuilding, EventPROpened, when(ticketed, to(StatePROpen), refuse("a PR without a ticket has no change")))
	t.set(StateBuilding, EventPRPushed, to(StatePROpen))
	t.each(append(attach, StateBuilding), EventPRClosed, func(s State) rule {
		return when(mergedPR, refuse("a PR cannot merge before its change is approved"), to(s))
	})
	t.set(StateTriggered, EventRoomOpened, to(StateRoomOpen))
	t.set(StateRoomOpen, EventBranchEnsured, to(StateRoomOpen))
	t.set(StateRoomOpen, EventTaskAssigned, when(testAuthorTask, to(StateWritingTests), refuse("only the test-author opens WRITING_TESTS")))
	stays(t, StateWritingTests, EventTaskAssigned, EventTaskAcked, EventTaskTimedOut, EventTaskFailed, EventTaskProgress)
	t.set(StateWritingTests, EventTestsSubmitted, to(StateVerifyingTests))
	t.set(StateVerifyingTests, EventTestsVerified, when(needsSignoff, to(StateAwaitingTestSignoff), to(StateTestsReady)))
	t.set(StateVerifyingTests, EventTestsRejected, to(StateWritingTests))
	t.set(StateAwaitingTestSignoff, EventTestsApproved, to(StateTestsReady))
	t.set(StateTestsReady, EventTestsCommitted, to(StateBuilding))
	t.set(StateBuilding, EventCIReported, to(StateBuilding))
	t.each([]State{StateRoomOpen, StateWritingTests, StateVerifyingTests, StateAwaitingTestSignoff, StateTestsReady, StateBuilding},
		EventQuestionAsked, to)
	t.each([]State{StateRoomOpen, StateWritingTests, StateVerifyingTests, StateAwaitingTestSignoff, StateTestsReady, StateBuilding},
		EventQuestionAnswered, to)
	t.move([]State{StateWritingTests, StateVerifyingTests, StateAwaitingTestSignoff, StateTestsReady, StateBuilding},
		EventTicketChanged, StateWritingTests)
}

// addPRStates gives every PR state the moves that apply to all of them. The states that differ are overridden in
// addReview and addMerge.
func addPRStates(t table) {
	prs := prStates()
	t.move(prs, EventPRPushed, StatePROpen)
	t.each(prs, EventPRClosed, func(State) rule {
		return when(mergedPR, refuse("a PR cannot merge before its change is approved"), to(StateRejected))
	})
	t.each(prs, EventCIReported, to)
	t.each(prs, EventGateEvaluated, to)
	t.each(prs, EventApprovalRecorded, to)
	t.each(prs, EventApprovalInvalidated, to)
}

// addReview covers slop, reviewers, the review loop, deliberation and the fix loop.
func addReview(t table) {
	t.set(StatePROpen, EventSlopEvaluated, when(passedSlop, to(StatePickingReviewers), to(StateSlopRejected)))
	t.set(StatePickingReviewers, EventReviewersPicked, to(StateInReview))
	// A push to a pull request already in review asks the same reviewers again, with no new pick.
	t.set(StatePickingReviewers, EventTaskAssigned, to(StateInReview))

	stays(t, StateInReview, EventReviewSubmitted, EventReviewUnavailable, EventReviewWaived, EventFindingFiled,
		EventFindingVerified, EventChallengeFiled, EventTaskAssigned, EventTaskAcked, EventTaskTimedOut, EventTaskFailed, EventTaskProgress)
	t.set(StateInReview, EventFindingStateChanged, when(fixClaimed, to(StateFixing), to(StateInReview)))
	t.set(StateInReview, EventCaseOpened, to(StateDeliberating))
	t.set(StateInReview, EventGateEvaluated, when(gatePassed, to(StateReadyForApproval), to(StateInReview)))

	stays(t, StateDeliberating, EventCaseOpened, EventVerdictSubmitted, EventChallengeFiled, EventFindingFiled,
		EventFindingVerified, EventFindingStateChanged, EventTaskAssigned, EventTaskAcked, EventTaskTimedOut, EventTaskFailed, EventTaskProgress)
	t.set(StateDeliberating, EventVerdictApplied, to(StateInReview))
	t.set(StateDeliberating, EventVerdictEscalated, to(StateEscalated))

	stays(t, StateEscalated, EventChallengeFiled, EventFindingFiled, EventFindingVerified, EventFindingStateChanged)

	stays(t, StateFixing, EventFindingFiled, EventFindingVerified)
	t.set(StateFixing, EventFindingStateChanged, when(verifyingFinding, to(StateVerifyingFix),
		when(openFinding, to(StateInReview), refuse("a fix in FIXING can only be verified or retracted"))))

	stays(t, StateVerifyingFix, EventFindingFiled, EventFindingVerified)
	t.set(StateVerifyingFix, EventFindingStateChanged, when(resolvedOrOpen, to(StateInReview),
		refuse("a fix being verified ends as resolved or open")))
}

// addMerge covers approval, the gate and the merge (invariant 2 is for the reader: the reducer does not track heads).
func addMerge(t table) {
	t.set(StateReadyForApproval, EventApprovalRecorded, to(StateApproved))
	t.set(StateReadyForApproval, EventApprovalInvalidated, to(StateInReview))
	t.set(StateReadyForApproval, EventGateEvaluated, when(gatePassed, to(StateReadyForApproval), to(StateInReview)))
	// READY_FOR_APPROVAL is reached only by a green gate on the current head, and the gate is green only with a valid
	// approval on that head (gate.checkApproval). An approval given before the gate turned green arrives in IN_REVIEW
	// and moves nothing, so a merge from here is a merge of an approved head.
	t.set(StateReadyForApproval, EventPRClosed, when(mergedPR, to(StateMerged), to(StateRejected)))

	t.set(StateApproved, EventChangeMerged, to(StateMerged))
	t.set(StateApproved, EventPRClosed, when(mergedPR, to(StateMerged), to(StateRejected)))
	t.set(StateApproved, EventApprovalInvalidated, to(StateInReview))
	t.set(StateApproved, EventGateEvaluated, when(gatePassed, to(StateApproved), to(StateInReview)))
}

// addUnlinked covers a PR with no ticket (D146): it fails slop and stays red; pr.linked closes this stream.
func addUnlinked(t table) {
	stays(t, StateUnlinkedPR, EventPRPushed, EventCIReported)
	t.set(StateUnlinkedPR, EventSlopEvaluated, when(passedSlop,
		refuse("a PR with no ticket fails the slop rule (D146)"), to(StateUnlinkedPR)))
	t.set(StateUnlinkedPR, EventGateEvaluated, when(gatePassed,
		refuse("a PR with no ticket cannot pass the gate"), to(StateUnlinkedPR)))
	t.set(StateUnlinkedPR, EventPRLinked, to(StateRejected))
	t.set(StateUnlinkedPR, EventPRClosed, when(mergedPR, refuse("a PR with no ticket cannot merge"), to(StateRejected)))
}

// addEverywhere gives every live state the moves that apply to all of them: a change may be rejected, and a human
// rejection ends it (TECH_SPEC 3.3). A decision in ESCALATED resolves the escalation (uphold, dismiss, spec change or
// override) and returns the change to review.
func addEverywhere(t table) {
	for _, s := range AllStates() {
		if s.IsTerminal() {
			continue
		}
		t.set(s, EventChangeRejected, to(StateRejected))
		t.set(s, EventStepRetried, to(s))
		t.set(s, EventStepEscalated, to(s))
		t.set(s, EventStepResumed, to(s))
		if s == StateEscalated {
			t.set(s, EventDecisionRecorded, when(rejectDecision, to(StateRejected),
				when(resolvesEscalation, to(StateInReview), to(s))))
			continue
		}
		t.set(s, EventDecisionRecorded, when(rejectDecision, to(StateRejected), to(s)))
	}
}

// prStates is every state in which the change has an open PR.
func prStates() []State {
	return []State{StatePROpen, StateSlopRejected, StatePickingReviewers, StateInReview, StateDeliberating,
		StateEscalated, StateFixing, StateVerifyingFix, StateReadyForApproval, StateApproved}
}

// set records the rule for one state and event type.
func (t table) set(from State, et EventType, r rule) {
	if t[from] == nil {
		t[from] = map[EventType]rule{}
	}
	t[from][et] = r
}

// each records mk(s) for every state in states.
func (t table) each(states []State, et EventType, mk func(State) rule) {
	for _, s := range states {
		t.set(s, et, mk(s))
	}
}

// move records a move to next for every state in states.
func (t table) move(states []State, et EventType, next State) {
	t.each(states, et, func(State) rule { return to(next) })
}

// stays records that each event type leaves s where it is.
func stays(t table, s State, ets ...EventType) {
	for _, et := range ets {
		t.set(s, et, to(s))
	}
}

// to is the rule that moves to next.
func to(next State) rule {
	return func(Event) (State, error) { return next, nil }
}

// refuse is the rule that refuses the event, with the reason.
func refuse(reason string) rule {
	return func(Event) (State, error) {
		return "", fmt.Errorf("%w: %s", ErrIllegalTransition, reason)
	}
}

// when decodes the payload as T and chooses between two rules by its content. It is the only place a transition
// reads a payload.
func when[T any](test func(T) bool, yes, no rule) rule {
	return func(e Event) (State, error) {
		v, err := decodeTo[T](e)
		if err != nil {
			return "", err
		}
		if test(v) {
			return yes(e)
		}
		return no(e)
	}
}

// decodeTo returns the typed payload of e, through Decode, so invariant 1 and strict decoding apply.
func decodeTo[T any](e Event) (T, error) {
	var zero T
	p, err := Decode(e)
	if err != nil {
		return zero, err
	}
	v, ok := p.(T)
	if !ok {
		return zero, fmt.Errorf("payload of %s is %T, not the payload the reducer reads", e.Type, p)
	}
	return v, nil
}

func ticketed(p PROpened) bool           { return p.Ticket != "" }
func mergedPR(p PRClosed) bool           { return p.Merged }
func passedSlop(p SlopEvaluated) bool    { return p.Pass }
func gatePassed(p GateEvaluated) bool    { return p.OK }
func testAuthorTask(p TaskAssigned) bool { return p.Role == workflow.RoleTestAuthor }
func rejectDecision(p DecisionRecorded) bool {
	return p.Kind == DecisionReject
}

func resolvesEscalation(p DecisionRecorded) bool {
	return slices.Contains([]DecisionKind{DecisionUphold, DecisionDismiss, DecisionSpecChange, DecisionOverride}, p.Kind)
}

func fixClaimed(p FindingStateChanged) bool       { return p.State == finding.StateFixClaimed }
func verifyingFinding(p FindingStateChanged) bool { return p.State == finding.StateVerifying }
func openFinding(p FindingStateChanged) bool      { return p.State == finding.StateOpen }
func needsSignoff(p TestsVerified) bool           { return p.NeedsSignoff }
func resolvedOrOpen(p FindingStateChanged) bool {
	return p.State == finding.StateResolved || p.State == finding.StateOpen
}
