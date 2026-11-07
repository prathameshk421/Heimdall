package change_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// The state "no state yet" is the zero State, which is not in AllStates. Tests write it as an empty string.

// reducerEvent is a valid envelope of type et with the given payload. Each version gets its own id.
func reducerEvent(version int64, et change.EventType, raw json.RawMessage) change.Event {
	e := envelopeOf(et)
	e.ID = change.EventID(fmt.Sprintf("3f1c9a52-8d2b-4e7a-9c11-%012x", version))
	e.Version = version
	e.Payload = raw
	if et == change.EventDecisionRecorded {
		e.Actor = change.Actor{Kind: change.ActorHuman, ID: "rekdfr"}
	}
	return e
}

const (
	ticketedPROpened          = `{"repo":"acme/pay-demo","number":7,"head_sha":"9f1c2ab","author":"aarush323","ticket":"PAY-142"}`
	noTicketPROpened          = `{"repo":"acme/pay-demo","number":7,"head_sha":"9f1c2ab","author":"aarush323"}`
	prClosedUnmerged          = `{"repo":"acme/pay-demo","number":7,"head_sha":"9f1c2ab","merged":false}`
	prClosedMerged            = `{"repo":"acme/pay-demo","number":7,"head_sha":"9f1c2ab","merged":true}`
	slopPassed                = `{"pass":true,"failing_rules":[]}`
	slopFailed                = `{"pass":false,"failing_rules":["ac_coverage"]}`
	gateGreen                 = `{"ok":true,"reasons":[],"sha":"9f1c2ab","tree_hash":"7d3e9a1","policy_version":3}`
	gateRed                   = `{"ok":false,"reasons":["ci_red"],"sha":"9f1c2ab","tree_hash":"7d3e9a1","policy_version":3}`
	testsVerifiedNeedsSignoff = `{"run_ids":["run_01HZX3K9QW7M2P5R8T0V4YB6CD"],"sha":"9f1c2ab","tree_hash":"7d3e9a1","needs_signoff":true}`
	testsVerifiedNoSignoff    = `{"run_ids":["run_01HZX3K9QW7M2P5R8T0V4YB6CD"],"sha":"9f1c2ab","tree_hash":"7d3e9a1","needs_signoff":false}`
	testAuthorTask            = `{"task_id":"tsk_01HZX3K9QW7M2P5R8T0V4YB6CD","agent":"bob-reviewer","role":"test-author","deadline":"2026-11-05T14:00:00Z","bundle_ref":"blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
)

// defaultPayload is the golden example of et, except where the golden file is not the common case: a closed PR is
// unmerged, a finding has just claimed a fix, and a task is for the test-author.
func defaultPayload(t *testing.T, et change.EventType) json.RawMessage {
	t.Helper()
	if et == change.EventPRClosed {
		return json.RawMessage(prClosedUnmerged)
	}
	if et == change.EventTaskAssigned {
		return json.RawMessage(testAuthorTask)
	}
	if et == change.EventTaskProgress {
		return json.RawMessage(`{"task_id":"tsk_01HZX3K9QW7M2P5R8T0V4YB6CD","agent":"bob-reviewer","role":"test-author","text":"working"}`)
	}
	if et == change.EventFindingStateChanged {
		return findingPayload("fix_claimed")
	}
	return goldenPayload(t, et)
}

// tablePayload is the payload the table test uses. A PR opened from no state has no ticket (D146).
func tablePayload(t *testing.T, s change.State, et change.EventType) json.RawMessage {
	t.Helper()
	if s == "" && et == change.EventPROpened {
		return json.RawMessage(noTicketPROpened)
	}
	return defaultPayload(t, et)
}

func findingPayload(state string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"finding_id":"fnd_01HZX3K9QW7M2P5R8T0V4YB6CD","ref":"F1","tier":"E1","severity":"high","state":%q,"by":"rekdfr"}`, state))
}

func decisionPayload(kind string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"kind":%q,"target":"F1","login":"rekdfr","reason":"owner call","source_ref":"comment-9"}`, kind))
}

// wantNext is TECH_SPEC 3.3 written out as a literal: for each state, each event type that moves it or keeps it (a
// valid no-op) and where it lands. Any pair not listed is an illegal transition. It is written from the spec and the
// rules of D146 and the review notes in reducer.go, and it is not derived from the production table.
var wantNext = map[change.State]map[change.EventType]change.State{
	"": {
		change.EventChangeTriggered: change.StateTriggered,
		change.EventPROpened:        change.StateUnlinkedPR,
	},
	change.StateTriggered: {
		change.EventStepRetried:      change.StateTriggered,
		change.EventStepResumed:      change.StateTriggered,
		change.EventStepEscalated:    change.StateTriggered,
		change.EventRoomOpened:       change.StateRoomOpen,
		change.EventPROpened:         change.StateTriggered,
		change.EventPRPushed:         change.StateTriggered,
		change.EventSlopEvaluated:    change.StateTriggered,
		change.EventPRClosed:         change.StateTriggered,
		change.EventDecisionRecorded: change.StateTriggered,
		change.EventChangeRejected:   change.StateRejected,
	},
	change.StateRoomOpen: {
		change.EventStepRetried:      change.StateRoomOpen,
		change.EventStepResumed:      change.StateRoomOpen,
		change.EventStepEscalated:    change.StateRoomOpen,
		change.EventBranchEnsured:    change.StateRoomOpen,
		change.EventTaskAssigned:     change.StateWritingTests,
		change.EventQuestionAsked:    change.StateRoomOpen,
		change.EventQuestionAnswered: change.StateRoomOpen,
		change.EventPROpened:         change.StateRoomOpen,
		change.EventPRPushed:         change.StateRoomOpen,
		change.EventSlopEvaluated:    change.StateRoomOpen,
		change.EventPRClosed:         change.StateRoomOpen,
		change.EventDecisionRecorded: change.StateRoomOpen,
		change.EventChangeRejected:   change.StateRejected,
	},
	change.StateWritingTests: {
		change.EventStepRetried:      change.StateWritingTests,
		change.EventStepResumed:      change.StateWritingTests,
		change.EventStepEscalated:    change.StateWritingTests,
		change.EventTaskAssigned:     change.StateWritingTests,
		change.EventTaskAcked:        change.StateWritingTests,
		change.EventTaskTimedOut:     change.StateWritingTests,
		change.EventTaskFailed:       change.StateWritingTests,
		change.EventTaskProgress:     change.StateWritingTests,
		change.EventQuestionAsked:    change.StateWritingTests,
		change.EventQuestionAnswered: change.StateWritingTests,
		change.EventTestsSubmitted:   change.StateVerifyingTests,
		change.EventTicketChanged:    change.StateWritingTests,
		change.EventPROpened:         change.StateWritingTests,
		change.EventPRPushed:         change.StateWritingTests,
		change.EventSlopEvaluated:    change.StateWritingTests,
		change.EventPRClosed:         change.StateWritingTests,
		change.EventDecisionRecorded: change.StateWritingTests,
		change.EventChangeRejected:   change.StateRejected,
	},
	change.StateVerifyingTests: {
		change.EventStepRetried:      change.StateVerifyingTests,
		change.EventStepResumed:      change.StateVerifyingTests,
		change.EventStepEscalated:    change.StateVerifyingTests,
		change.EventQuestionAsked:    change.StateVerifyingTests,
		change.EventQuestionAnswered: change.StateVerifyingTests,
		change.EventTestsVerified:    change.StateTestsReady,
		change.EventTestsRejected:    change.StateWritingTests,
		change.EventTicketChanged:    change.StateWritingTests,
		change.EventPROpened:         change.StateVerifyingTests,
		change.EventPRPushed:         change.StateVerifyingTests,
		change.EventSlopEvaluated:    change.StateVerifyingTests,
		change.EventPRClosed:         change.StateVerifyingTests,
		change.EventDecisionRecorded: change.StateVerifyingTests,
		change.EventChangeRejected:   change.StateRejected,
	},
	change.StateAwaitingTestSignoff: {
		change.EventStepRetried:      change.StateAwaitingTestSignoff,
		change.EventStepResumed:      change.StateAwaitingTestSignoff,
		change.EventStepEscalated:    change.StateAwaitingTestSignoff,
		change.EventQuestionAsked:    change.StateAwaitingTestSignoff,
		change.EventQuestionAnswered: change.StateAwaitingTestSignoff,
		change.EventTestsApproved:    change.StateTestsReady,
		change.EventTicketChanged:    change.StateWritingTests,
		change.EventPROpened:         change.StateAwaitingTestSignoff,
		change.EventPRPushed:         change.StateAwaitingTestSignoff,
		change.EventSlopEvaluated:    change.StateAwaitingTestSignoff,
		change.EventPRClosed:         change.StateAwaitingTestSignoff,
		change.EventDecisionRecorded: change.StateAwaitingTestSignoff,
		change.EventChangeRejected:   change.StateRejected,
	},
	change.StateTestsReady: {
		change.EventStepRetried:      change.StateTestsReady,
		change.EventStepResumed:      change.StateTestsReady,
		change.EventStepEscalated:    change.StateTestsReady,
		change.EventQuestionAsked:    change.StateTestsReady,
		change.EventQuestionAnswered: change.StateTestsReady,
		change.EventTestsCommitted:   change.StateBuilding,
		change.EventTicketChanged:    change.StateWritingTests,
		change.EventPROpened:         change.StateTestsReady,
		change.EventPRPushed:         change.StateTestsReady,
		change.EventSlopEvaluated:    change.StateTestsReady,
		change.EventPRClosed:         change.StateTestsReady,
		change.EventDecisionRecorded: change.StateTestsReady,
		change.EventChangeRejected:   change.StateRejected,
	},
	change.StateBuilding: {
		change.EventStepRetried:      change.StateBuilding,
		change.EventStepResumed:      change.StateBuilding,
		change.EventStepEscalated:    change.StateBuilding,
		change.EventQuestionAsked:    change.StateBuilding,
		change.EventQuestionAnswered: change.StateBuilding,
		change.EventCIReported:       change.StateBuilding,
		change.EventPROpened:         change.StatePROpen,
		change.EventPRPushed:         change.StatePROpen,
		change.EventPRClosed:         change.StateBuilding,
		change.EventTicketChanged:    change.StateWritingTests,
		change.EventDecisionRecorded: change.StateBuilding,
		change.EventChangeRejected:   change.StateRejected,
	},
	change.StatePROpen: {
		change.EventStepRetried:         change.StatePROpen,
		change.EventStepResumed:         change.StatePROpen,
		change.EventStepEscalated:       change.StatePROpen,
		change.EventSlopEvaluated:       change.StatePickingReviewers,
		change.EventPRPushed:            change.StatePROpen,
		change.EventPRClosed:            change.StateRejected,
		change.EventCIReported:          change.StatePROpen,
		change.EventGateEvaluated:       change.StatePROpen,
		change.EventApprovalRecorded:    change.StatePROpen,
		change.EventApprovalInvalidated: change.StatePROpen,
		change.EventDecisionRecorded:    change.StatePROpen,
		change.EventChangeRejected:      change.StateRejected,
	},
	change.StateSlopRejected: {
		change.EventStepRetried:         change.StateSlopRejected,
		change.EventStepResumed:         change.StateSlopRejected,
		change.EventStepEscalated:       change.StateSlopRejected,
		change.EventPRPushed:            change.StatePROpen,
		change.EventPRClosed:            change.StateRejected,
		change.EventCIReported:          change.StateSlopRejected,
		change.EventGateEvaluated:       change.StateSlopRejected,
		change.EventApprovalRecorded:    change.StateSlopRejected,
		change.EventApprovalInvalidated: change.StateSlopRejected,
		change.EventDecisionRecorded:    change.StateSlopRejected,
		change.EventChangeRejected:      change.StateRejected,
	},
	change.StatePickingReviewers: {
		change.EventStepRetried:         change.StatePickingReviewers,
		change.EventStepResumed:         change.StatePickingReviewers,
		change.EventStepEscalated:       change.StatePickingReviewers,
		change.EventReviewersPicked:     change.StateInReview,
		change.EventTaskAssigned:        change.StateInReview,
		change.EventPRPushed:            change.StatePROpen,
		change.EventPRClosed:            change.StateRejected,
		change.EventCIReported:          change.StatePickingReviewers,
		change.EventGateEvaluated:       change.StatePickingReviewers,
		change.EventApprovalRecorded:    change.StatePickingReviewers,
		change.EventApprovalInvalidated: change.StatePickingReviewers,
		change.EventDecisionRecorded:    change.StatePickingReviewers,
		change.EventChangeRejected:      change.StateRejected,
	},
	change.StateInReview: {
		change.EventStepRetried:         change.StateInReview,
		change.EventStepResumed:         change.StateInReview,
		change.EventStepEscalated:       change.StateInReview,
		change.EventReviewSubmitted:     change.StateInReview,
		change.EventReviewUnavailable:   change.StateInReview,
		change.EventReviewWaived:        change.StateInReview,
		change.EventFindingFiled:        change.StateInReview,
		change.EventFindingVerified:     change.StateInReview,
		change.EventFindingStateChanged: change.StateFixing,
		change.EventChallengeFiled:      change.StateInReview,
		change.EventCaseOpened:          change.StateDeliberating,
		change.EventTaskAssigned:        change.StateInReview,
		change.EventTaskAcked:           change.StateInReview,
		change.EventTaskTimedOut:        change.StateInReview,
		change.EventTaskFailed:          change.StateInReview,
		change.EventTaskProgress:        change.StateInReview,
		change.EventPRPushed:            change.StatePROpen,
		change.EventPRClosed:            change.StateRejected,
		change.EventCIReported:          change.StateInReview,
		change.EventGateEvaluated:       change.StateInReview,
		change.EventApprovalRecorded:    change.StateInReview,
		change.EventApprovalInvalidated: change.StateInReview,
		change.EventDecisionRecorded:    change.StateInReview,
		change.EventChangeRejected:      change.StateRejected,
	},
	change.StateDeliberating: {
		change.EventStepRetried:         change.StateDeliberating,
		change.EventStepResumed:         change.StateDeliberating,
		change.EventStepEscalated:       change.StateDeliberating,
		change.EventCaseOpened:          change.StateDeliberating,
		change.EventVerdictSubmitted:    change.StateDeliberating,
		change.EventVerdictApplied:      change.StateInReview,
		change.EventVerdictEscalated:    change.StateEscalated,
		change.EventChallengeFiled:      change.StateDeliberating,
		change.EventFindingFiled:        change.StateDeliberating,
		change.EventFindingVerified:     change.StateDeliberating,
		change.EventFindingStateChanged: change.StateDeliberating,
		change.EventTaskAssigned:        change.StateDeliberating,
		change.EventTaskAcked:           change.StateDeliberating,
		change.EventTaskTimedOut:        change.StateDeliberating,
		change.EventTaskFailed:          change.StateDeliberating,
		change.EventTaskProgress:        change.StateDeliberating,
		change.EventPRPushed:            change.StatePROpen,
		change.EventPRClosed:            change.StateRejected,
		change.EventCIReported:          change.StateDeliberating,
		change.EventGateEvaluated:       change.StateDeliberating,
		change.EventApprovalRecorded:    change.StateDeliberating,
		change.EventApprovalInvalidated: change.StateDeliberating,
		change.EventDecisionRecorded:    change.StateDeliberating,
		change.EventChangeRejected:      change.StateRejected,
	},
	change.StateEscalated: {
		change.EventStepRetried:         change.StateEscalated,
		change.EventStepResumed:         change.StateEscalated,
		change.EventStepEscalated:       change.StateEscalated,
		change.EventChallengeFiled:      change.StateEscalated,
		change.EventFindingFiled:        change.StateEscalated,
		change.EventFindingVerified:     change.StateEscalated,
		change.EventFindingStateChanged: change.StateEscalated,
		change.EventPRPushed:            change.StatePROpen,
		change.EventPRClosed:            change.StateRejected,
		change.EventCIReported:          change.StateEscalated,
		change.EventGateEvaluated:       change.StateEscalated,
		change.EventApprovalRecorded:    change.StateEscalated,
		change.EventApprovalInvalidated: change.StateEscalated,
		change.EventDecisionRecorded:    change.StateInReview,
		change.EventChangeRejected:      change.StateRejected,
	},
	change.StateFixing: {
		change.EventStepRetried:         change.StateFixing,
		change.EventStepResumed:         change.StateFixing,
		change.EventStepEscalated:       change.StateFixing,
		change.EventFindingFiled:        change.StateFixing,
		change.EventFindingVerified:     change.StateFixing,
		change.EventPRPushed:            change.StatePROpen,
		change.EventPRClosed:            change.StateRejected,
		change.EventCIReported:          change.StateFixing,
		change.EventGateEvaluated:       change.StateFixing,
		change.EventApprovalRecorded:    change.StateFixing,
		change.EventApprovalInvalidated: change.StateFixing,
		change.EventDecisionRecorded:    change.StateFixing,
		change.EventChangeRejected:      change.StateRejected,
	},
	change.StateVerifyingFix: {
		change.EventStepRetried:         change.StateVerifyingFix,
		change.EventStepResumed:         change.StateVerifyingFix,
		change.EventStepEscalated:       change.StateVerifyingFix,
		change.EventFindingFiled:        change.StateVerifyingFix,
		change.EventFindingVerified:     change.StateVerifyingFix,
		change.EventPRPushed:            change.StatePROpen,
		change.EventPRClosed:            change.StateRejected,
		change.EventCIReported:          change.StateVerifyingFix,
		change.EventGateEvaluated:       change.StateVerifyingFix,
		change.EventApprovalRecorded:    change.StateVerifyingFix,
		change.EventApprovalInvalidated: change.StateVerifyingFix,
		change.EventDecisionRecorded:    change.StateVerifyingFix,
		change.EventChangeRejected:      change.StateRejected,
	},
	change.StateReadyForApproval: {
		change.EventStepRetried:         change.StateReadyForApproval,
		change.EventStepResumed:         change.StateReadyForApproval,
		change.EventStepEscalated:       change.StateReadyForApproval,
		change.EventApprovalRecorded:    change.StateApproved,
		change.EventApprovalInvalidated: change.StateInReview,
		change.EventGateEvaluated:       change.StateInReview,
		change.EventCIReported:          change.StateReadyForApproval,
		change.EventPRPushed:            change.StatePROpen,
		change.EventPRClosed:            change.StateRejected,
		change.EventDecisionRecorded:    change.StateReadyForApproval,
		change.EventChangeRejected:      change.StateRejected,
	},
	change.StateApproved: {
		change.EventStepRetried:         change.StateApproved,
		change.EventStepResumed:         change.StateApproved,
		change.EventStepEscalated:       change.StateApproved,
		change.EventChangeMerged:        change.StateMerged,
		change.EventApprovalRecorded:    change.StateApproved,
		change.EventApprovalInvalidated: change.StateInReview,
		change.EventGateEvaluated:       change.StateInReview,
		change.EventCIReported:          change.StateApproved,
		change.EventPRPushed:            change.StatePROpen,
		change.EventPRClosed:            change.StateRejected,
		change.EventDecisionRecorded:    change.StateApproved,
		change.EventChangeRejected:      change.StateRejected,
	},
	change.StateUnlinkedPR: {
		change.EventStepRetried:      change.StateUnlinkedPR,
		change.EventStepResumed:      change.StateUnlinkedPR,
		change.EventStepEscalated:    change.StateUnlinkedPR,
		change.EventPRPushed:         change.StateUnlinkedPR,
		change.EventCIReported:       change.StateUnlinkedPR,
		change.EventGateEvaluated:    change.StateUnlinkedPR,
		change.EventPRLinked:         change.StateRejected,
		change.EventPRClosed:         change.StateRejected,
		change.EventDecisionRecorded: change.StateUnlinkedPR,
		change.EventChangeRejected:   change.StateRejected,
	},
}

// TestEveryStateAndEventTypeMatchesTheSpecTable runs the full state x event-type product. The expected result of each
// pair comes from wantNext: a listed pair must land on its state, and every other pair must be ErrIllegalTransition
// with the state unchanged. Terminal states appear in no row, so they refuse everything.
func TestEveryStateAndEventTypeMatchesTheSpecTable(t *testing.T) {
	states := append([]change.State{""}, change.AllStates()...)
	for _, s := range states {
		for _, et := range change.All() {
			name := fmt.Sprintf("%q x %s", s, et)
			t.Run(name, func(t *testing.T) {
				e := reducerEvent(1, et, tablePayload(t, s, et))
				next, err := change.Reduce(s, e)
				if want, ok := wantNext[s][et]; ok {
					require.NoError(t, err)
					require.Equal(t, want, next)
					return
				}
				require.ErrorIs(t, err, change.ErrIllegalTransition)
				require.Equal(t, s, next, "a refused event must leave the state where it was")
			})
		}
	}
}

// TestTerminalStatesAcceptNothing names the terminal states explicitly, so the rule does not depend on the table's
// shape: every event type is refused from MERGED and from REJECTED.
func TestTerminalStatesAcceptNothing(t *testing.T) {
	for _, s := range []change.State{change.StateMerged, change.StateRejected} {
		require.True(t, s.IsTerminal(), s)
		for _, et := range change.All() {
			_, err := change.Reduce(s, reducerEvent(1, et, defaultPayload(t, et)))
			require.ErrorIs(t, err, change.ErrIllegalTransition, "%s + %s", s, et)
		}
	}
}

// TestInitialEventsStartAChangeOrAnUnlinkedPR covers the first event of a stream. An empty stream has no state.
func TestInitialEventsStartAChangeOrAnUnlinkedPR(t *testing.T) {
	s, err := change.Replay(nil)
	require.NoError(t, err)
	require.Equal(t, change.State(""), s)

	s, err = change.Reduce("", reducerEvent(1, change.EventChangeTriggered, goldenPayload(t, change.EventChangeTriggered)))
	require.NoError(t, err)
	require.Equal(t, change.StateTriggered, s)

	s, err = change.Reduce("", reducerEvent(1, change.EventPROpened, json.RawMessage(noTicketPROpened)))
	require.NoError(t, err)
	require.Equal(t, change.StateUnlinkedPR, s)

	_, err = change.Reduce("", reducerEvent(1, change.EventPROpened, json.RawMessage(ticketedPROpened)))
	require.ErrorIs(t, err, change.ErrIllegalTransition, "a PR that names a ticket cannot start a stream")
	_, err = change.Reduce("", reducerEvent(1, change.EventRoomOpened, goldenPayload(t, change.EventRoomOpened)))
	require.ErrorIs(t, err, change.ErrIllegalTransition)
}

// variant is one content-dependent case: the same event type can move a state or refuse, depending on its payload.
type variant struct {
	name    string
	from    change.State
	et      change.EventType
	payload json.RawMessage
	want    change.State
	refused bool
}

func TestContentDependentTransitions(t *testing.T) {
	decision := decisionPayload
	cases := []variant{
		{"ticketed PR opened from TRIGGERED attaches", change.StateTriggered, change.EventPROpened, json.RawMessage(ticketedPROpened), change.StateTriggered, false},
		{"unticketed PR opened from TRIGGERED is refused", change.StateTriggered, change.EventPROpened, json.RawMessage(noTicketPROpened), "", true},
		{"unticketed PR opened at BUILDING is refused", change.StateBuilding, change.EventPROpened, json.RawMessage(noTicketPROpened), "", true},
		{"slop fail in PR_OPEN goes to SLOP_REJECTED", change.StatePROpen, change.EventSlopEvaluated, json.RawMessage(slopFailed), change.StateSlopRejected, false},
		{"slop pass in PR_OPEN goes on to picking", change.StatePROpen, change.EventSlopEvaluated, json.RawMessage(slopPassed), change.StatePickingReviewers, false},
		{"slop fail on an unlinked PR keeps it red", change.StateUnlinkedPR, change.EventSlopEvaluated, json.RawMessage(slopFailed), change.StateUnlinkedPR, false},
		{"slop pass on an unlinked PR is refused", change.StateUnlinkedPR, change.EventSlopEvaluated, json.RawMessage(slopPassed), "", true},
		{"merged PR closed while in review is refused", change.StateInReview, change.EventPRClosed, json.RawMessage(prClosedMerged), "", true},
		{"merged PR closed before approval is refused", change.StateBuilding, change.EventPRClosed, json.RawMessage(prClosedMerged), "", true},
		{"merged PR closed when approved merges", change.StateApproved, change.EventPRClosed, json.RawMessage(prClosedMerged), change.StateMerged, false},
		{"merged PR closed when ready merges, as a green gate holds an approval on head", change.StateReadyForApproval, change.EventPRClosed, json.RawMessage(prClosedMerged), change.StateMerged, false},
		{"unmerged PR closed when ready rejects", change.StateReadyForApproval, change.EventPRClosed, json.RawMessage(prClosedUnmerged), change.StateRejected, false},
		{"unmerged PR closed when approved rejects", change.StateApproved, change.EventPRClosed, json.RawMessage(prClosedUnmerged), change.StateRejected, false},
		{"green gate in review is ready for approval", change.StateInReview, change.EventGateEvaluated, json.RawMessage(gateGreen), change.StateReadyForApproval, false},
		{"red gate in review stays", change.StateInReview, change.EventGateEvaluated, json.RawMessage(gateRed), change.StateInReview, false},
		{"green gate on an unlinked PR is refused", change.StateUnlinkedPR, change.EventGateEvaluated, json.RawMessage(gateGreen), "", true},
		{"red gate when ready goes back to review", change.StateReadyForApproval, change.EventGateEvaluated, json.RawMessage(gateRed), change.StateInReview, false},
		{"fix claimed starts FIXING", change.StateInReview, change.EventFindingStateChanged, findingPayload("fix_claimed"), change.StateFixing, false},
		{"a finding upheld in review changes nothing", change.StateInReview, change.EventFindingStateChanged, findingPayload("upheld"), change.StateInReview, false},
		{"verifying a fix moves FIXING to VERIFYING_FIX", change.StateFixing, change.EventFindingStateChanged, findingPayload("verifying"), change.StateVerifyingFix, false},
		{"a retracted fix returns FIXING to review", change.StateFixing, change.EventFindingStateChanged, findingPayload("open"), change.StateInReview, false},
		{"a second fix claim while fixing is refused", change.StateFixing, change.EventFindingStateChanged, findingPayload("fix_claimed"), "", true},
		{"a resolved fix returns VERIFYING_FIX to review", change.StateVerifyingFix, change.EventFindingStateChanged, findingPayload("resolved"), change.StateInReview, false},
		{"a finding verifying again while verifying is refused", change.StateVerifyingFix, change.EventFindingStateChanged, findingPayload("verifying"), "", true},
		{"a test-author task opens WRITING_TESTS", change.StateRoomOpen, change.EventTaskAssigned, json.RawMessage(testAuthorTask), change.StateWritingTests, false},
		{"a reviewer task cannot open WRITING_TESTS", change.StateRoomOpen, change.EventTaskAssigned, goldenPayload(t, change.EventTaskAssigned), "", true},
		{"a verdict escalation moves DELIBERATING to ESCALATED", change.StateDeliberating, change.EventVerdictEscalated, goldenPayload(t, change.EventVerdictEscalated), change.StateEscalated, false},
		{"an uphold resolves ESCALATED", change.StateEscalated, change.EventDecisionRecorded, decision("uphold"), change.StateInReview, false},
		{"a dismiss resolves ESCALATED", change.StateEscalated, change.EventDecisionRecorded, decision("dismiss"), change.StateInReview, false},
		{"a spec change resolves ESCALATED", change.StateEscalated, change.EventDecisionRecorded, decision("spec_change"), change.StateInReview, false},
		{"an override resolves ESCALATED", change.StateEscalated, change.EventDecisionRecorded, decision("override"), change.StateInReview, false},
		{"a review command does not resolve ESCALATED", change.StateEscalated, change.EventDecisionRecorded, decision("review"), change.StateEscalated, false},
		{"a reject from ESCALATED rejects", change.StateEscalated, change.EventDecisionRecorded, decision("reject"), change.StateRejected, false},
		{"a reject from TRIGGERED rejects", change.StateTriggered, change.EventDecisionRecorded, decision("reject"), change.StateRejected, false},
		{"a reject from IN_REVIEW rejects", change.StateInReview, change.EventDecisionRecorded, decision("reject"), change.StateRejected, false},
		{"a reject from ready rejects", change.StateReadyForApproval, change.EventDecisionRecorded, decision("reject"), change.StateRejected, false},
		{"an uphold outside ESCALATED only records", change.StateInReview, change.EventDecisionRecorded, decision("uphold"), change.StateInReview, false},
		{"approval in ready makes the change approved", change.StateReadyForApproval, change.EventApprovalRecorded, goldenPayload(t, change.EventApprovalRecorded), change.StateApproved, false},
		{"approval in review is only recorded", change.StateInReview, change.EventApprovalRecorded, goldenPayload(t, change.EventApprovalRecorded), change.StateInReview, false},
		{"verified tests that need sign-off wait for it", change.StateVerifyingTests, change.EventTestsVerified, json.RawMessage(testsVerifiedNeedsSignoff), change.StateAwaitingTestSignoff, false},
		{"verified tests with no sign-off need are ready", change.StateVerifyingTests, change.EventTestsVerified, json.RawMessage(testsVerifiedNoSignoff), change.StateTestsReady, false},
		{"commit is refused while sign-off is pending", change.StateAwaitingTestSignoff, change.EventTestsCommitted, goldenPayload(t, change.EventTestsCommitted), "", true},
		{"approval releases sign-off into ready", change.StateAwaitingTestSignoff, change.EventTestsApproved, goldenPayload(t, change.EventTestsApproved), change.StateTestsReady, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			next, err := change.Reduce(c.from, reducerEvent(1, c.et, c.payload))
			if c.refused {
				require.ErrorIs(t, err, change.ErrIllegalTransition)
				require.Equal(t, c.from, next)
				return
			}
			require.NoError(t, err)
			require.Equal(t, c.want, next)
		})
	}
}

// TestAgentDecisionIsARefusedPayloadNotAnIllegalTransition checks invariant 1 through the reducer: the event is
// rejected because of who sent it, and the error does not claim the transition was illegal.
func TestAgentDecisionIsARefusedPayloadNotAnIllegalTransition(t *testing.T) {
	e := reducerEvent(1, change.EventDecisionRecorded, decisionPayload("reject"))
	e.Actor = change.Actor{Kind: change.ActorAgent, ID: "bob-reviewer"}
	_, err := change.Reduce(change.StateInReview, e)
	require.Error(t, err)
	require.NotErrorIs(t, err, change.ErrIllegalTransition)
	require.Contains(t, err.Error(), "invariant 1")
}

// streamStep is one event of a scripted life, with the state it must leave the change in. An empty payload is the
// default payload for the type.
type streamStep struct {
	et      change.EventType
	payload string
	want    change.State
}

// runStream applies each step in order, checking the event is valid and the state after it. It returns the events
// and the final state.
func runStream(t *testing.T, steps []streamStep) ([]change.Event, change.State) {
	t.Helper()
	var s change.State
	var events []change.Event
	for i, st := range steps {
		raw := defaultPayload(t, st.et)
		if st.payload != "" {
			raw = json.RawMessage(st.payload)
		}
		e := reducerEvent(int64(i+1), st.et, raw)
		require.NoError(t, e.Validate(), "step %d (%s) is not a valid event", i+1, st.et)
		next, err := change.Reduce(s, e)
		require.NoError(t, err, "step %d (%s) from %q", i+1, st.et, s)
		require.Equal(t, st.want, next, "step %d (%s) from %q", i+1, st.et, s)
		s = next
		events = append(events, e)
	}
	return events, s
}

// requireReplayTwice replays the stream twice and checks both runs agree with the state reached step by step.
func requireReplayTwice(t *testing.T, events []change.Event, want change.State) {
	t.Helper()
	first, err := change.Replay(events)
	require.NoError(t, err)
	second, err := change.Replay(events)
	require.NoError(t, err)
	require.Equal(t, want, first)
	require.Equal(t, first, second)
}

// Shorthands used by the streams below, so each step reads as one line.
var (
	open = []streamStep{
		{change.EventChangeTriggered, "", change.StateTriggered},
		{change.EventRoomOpened, "", change.StateRoomOpen},
		{change.EventBranchEnsured, "", change.StateRoomOpen},
		{change.EventTaskAssigned, "", change.StateWritingTests},
		{change.EventTaskAcked, "", change.StateWritingTests},
		{change.EventQuestionAsked, "", change.StateWritingTests},
		{change.EventQuestionAnswered, "", change.StateWritingTests},
		{change.EventTestsSubmitted, "", change.StateVerifyingTests},
		{change.EventTestsVerified, "", change.StateTestsReady},
		{change.EventTestsCommitted, "", change.StateBuilding},
	}
)

func TestStreamNonSensitiveChangeMergesThroughReview(t *testing.T) {
	steps := append(append([]streamStep{}, open...),
		streamStep{change.EventPROpened, "", change.StatePROpen},
		streamStep{change.EventSlopEvaluated, "", change.StatePickingReviewers},
		streamStep{change.EventReviewersPicked, "", change.StateInReview},
		streamStep{change.EventReviewSubmitted, "", change.StateInReview},
		streamStep{change.EventFindingFiled, "", change.StateInReview},
		streamStep{change.EventGateEvaluated, gateGreen, change.StateReadyForApproval},
		streamStep{change.EventApprovalRecorded, "", change.StateApproved},
		streamStep{change.EventChangeMerged, "", change.StateMerged},
	)
	events, final := runStream(t, steps)
	require.Equal(t, change.StateMerged, final)
	requireReplayTwice(t, events, final)
}

func TestStreamSensitiveDomainWaitsForSignoffBeforeTestsAreReady(t *testing.T) {
	steps := []streamStep{
		{change.EventChangeTriggered, "", change.StateTriggered},
		{change.EventRoomOpened, "", change.StateRoomOpen},
		{change.EventTaskAssigned, "", change.StateWritingTests},
		{change.EventTestsSubmitted, "", change.StateVerifyingTests},
		{change.EventTestsVerified, testsVerifiedNeedsSignoff, change.StateAwaitingTestSignoff},
		{change.EventTestsApproved, "", change.StateTestsReady},
		{change.EventTestsCommitted, "", change.StateBuilding},
		{change.EventPROpened, "", change.StatePROpen},
		{change.EventSlopEvaluated, "", change.StatePickingReviewers},
		{change.EventReviewersPicked, "", change.StateInReview},
		{change.EventGateEvaluated, gateGreen, change.StateReadyForApproval},
		{change.EventApprovalRecorded, "", change.StateApproved},
		{change.EventChangeMerged, "", change.StateMerged},
	}
	events, final := runStream(t, steps)
	requireReplayTwice(t, events, final)

	// Before sign-off, the tests cannot be committed: only the approval moves AWAITING_TEST_SIGNOFF on (D33).
	s, err := change.Replay(events[:5])
	require.NoError(t, err)
	require.Equal(t, change.StateAwaitingTestSignoff, s)
	_, err = change.Reduce(s, reducerEvent(6, change.EventTestsCommitted, defaultPayload(t, change.EventTestsCommitted)))
	require.ErrorIs(t, err, change.ErrIllegalTransition)
}

func TestStreamSlopRejectionLoopsOnPushThenMerges(t *testing.T) {
	steps := append(append([]streamStep{}, open...),
		streamStep{change.EventPROpened, "", change.StatePROpen},
		streamStep{change.EventSlopEvaluated, slopFailed, change.StateSlopRejected},
		streamStep{change.EventPRPushed, "", change.StatePROpen},
		streamStep{change.EventSlopEvaluated, slopFailed, change.StateSlopRejected},
		streamStep{change.EventPRPushed, "", change.StatePROpen},
		streamStep{change.EventSlopEvaluated, slopPassed, change.StatePickingReviewers},
		streamStep{change.EventReviewersPicked, "", change.StateInReview},
		streamStep{change.EventGateEvaluated, gateGreen, change.StateReadyForApproval},
		streamStep{change.EventApprovalRecorded, "", change.StateApproved},
		streamStep{change.EventPRClosed, prClosedMerged, change.StateMerged},
	)
	events, final := runStream(t, steps)
	require.Equal(t, change.StateMerged, final)
	requireReplayTwice(t, events, final)
}

func TestStreamDeliberationEscalatesAndReturnsToReview(t *testing.T) {
	steps := append(append([]streamStep{}, open...),
		streamStep{change.EventPROpened, "", change.StatePROpen},
		streamStep{change.EventSlopEvaluated, "", change.StatePickingReviewers},
		streamStep{change.EventReviewersPicked, "", change.StateInReview},
		streamStep{change.EventCaseOpened, "", change.StateDeliberating},
		streamStep{change.EventVerdictSubmitted, "", change.StateDeliberating},
		streamStep{change.EventVerdictEscalated, "", change.StateEscalated},
		streamStep{change.EventDecisionRecorded, "", change.StateInReview},
		streamStep{change.EventCaseOpened, "", change.StateDeliberating},
		streamStep{change.EventVerdictApplied, "", change.StateInReview},
		streamStep{change.EventGateEvaluated, "", change.StateInReview},
		streamStep{change.EventChangeRejected, "", change.StateRejected},
	)
	events, final := runStream(t, steps)
	requireReplayTwice(t, events, final)
}

func TestStreamFixLoopGoesThroughFixingAndVerifyingFix(t *testing.T) {
	steps := append(append([]streamStep{}, open...),
		streamStep{change.EventPROpened, "", change.StatePROpen},
		streamStep{change.EventSlopEvaluated, "", change.StatePickingReviewers},
		streamStep{change.EventReviewersPicked, "", change.StateInReview},
		streamStep{change.EventFindingStateChanged, findingPayloadString("fix_claimed"), change.StateFixing},
		streamStep{change.EventFindingStateChanged, findingPayloadString("verifying"), change.StateVerifyingFix},
		streamStep{change.EventFindingStateChanged, findingPayloadString("resolved"), change.StateInReview},
		streamStep{change.EventFindingStateChanged, findingPayloadString("fix_claimed"), change.StateFixing},
		streamStep{change.EventFindingStateChanged, findingPayloadString("open"), change.StateInReview},
		streamStep{change.EventGateEvaluated, gateGreen, change.StateReadyForApproval},
		streamStep{change.EventApprovalInvalidated, "", change.StateInReview},
		streamStep{change.EventPRPushed, "", change.StatePROpen},
	)
	events, final := runStream(t, steps)
	requireReplayTwice(t, events, final)
}

func findingPayloadString(state string) string { return string(findingPayload(state)) }

// A PR opened before BUILDING attaches and keeps the change where it is; its first push after BUILDING begins moves
// the change to PR_OPEN (D146).
func TestStreamPROpenedBeforeBuildingAttachesAndKeepsItsPlace(t *testing.T) {
	steps := []streamStep{
		{change.EventChangeTriggered, "", change.StateTriggered},
		{change.EventPROpened, "", change.StateTriggered},
		{change.EventPRPushed, "", change.StateTriggered},
		{change.EventRoomOpened, "", change.StateRoomOpen},
		{change.EventTaskAssigned, "", change.StateWritingTests},
		{change.EventTestsSubmitted, "", change.StateVerifyingTests},
		{change.EventTestsVerified, "", change.StateTestsReady},
		{change.EventTestsCommitted, "", change.StateBuilding},
		{change.EventPRPushed, "", change.StatePROpen},
		{change.EventSlopEvaluated, "", change.StatePickingReviewers},
	}
	events, final := runStream(t, steps)
	requireReplayTwice(t, events, final)
}

// A PR with no ticket is its own stream. It stays red through slop, pushes and CI; pr.linked closes it.
func TestStreamUnlinkedPRStaysRedUntilLinkedThenCloses(t *testing.T) {
	steps := []streamStep{
		{change.EventPROpened, noTicketPROpened, change.StateUnlinkedPR},
		{change.EventSlopEvaluated, slopFailed, change.StateUnlinkedPR},
		{change.EventPRPushed, "", change.StateUnlinkedPR},
		{change.EventCIReported, "", change.StateUnlinkedPR},
		{change.EventGateEvaluated, gateRed, change.StateUnlinkedPR},
		{change.EventPRLinked, "", change.StateRejected},
	}
	events, final := runStream(t, steps)
	requireReplayTwice(t, events, final)

	steps = []streamStep{
		{change.EventPROpened, noTicketPROpened, change.StateUnlinkedPR},
		{change.EventPRClosed, prClosedUnmerged, change.StateRejected},
	}
	events, final = runStream(t, steps)
	requireReplayTwice(t, events, final)
}

func TestStreamTicketChangeRewindsToWritingTests(t *testing.T) {
	steps := append(append([]streamStep{}, open...),
		streamStep{change.EventTicketChanged, "", change.StateWritingTests},
		streamStep{change.EventTestsSubmitted, "", change.StateVerifyingTests},
		streamStep{change.EventTestsRejected, "", change.StateWritingTests},
		streamStep{change.EventTestsSubmitted, "", change.StateVerifyingTests},
		streamStep{change.EventTestsVerified, "", change.StateTestsReady},
		streamStep{change.EventTestsCommitted, "", change.StateBuilding},
		streamStep{change.EventTicketChanged, "", change.StateWritingTests},
	)
	events, final := runStream(t, steps)
	requireReplayTwice(t, events, final)
}

// Replay reports the position of the first refused event and keeps ErrIllegalTransition for errors.Is.
func TestReplayNamesTheFirstRefusedEvent(t *testing.T) {
	events := []change.Event{
		reducerEvent(1, change.EventChangeTriggered, goldenPayload(t, change.EventChangeTriggered)),
		reducerEvent(2, change.EventReviewersPicked, goldenPayload(t, change.EventReviewersPicked)),
	}
	s, err := change.Replay(events)
	require.ErrorIs(t, err, change.ErrIllegalTransition)
	require.Contains(t, err.Error(), "event 2")
	require.Equal(t, change.StateTriggered, s)
}

// alternatives are the other payloads a walk may send for a type whose content decides a move. A walk always offers
// the default from tablePayload first; only that default is checked against wantNext, and the variants are checked by
// TestContentDependentTransitions.
func alternatives(et change.EventType) []json.RawMessage {
	var out []json.RawMessage
	if et == change.EventPROpened {
		out = append(out, json.RawMessage(ticketedPROpened), json.RawMessage(noTicketPROpened))
	}
	if et == change.EventSlopEvaluated {
		out = append(out, json.RawMessage(slopPassed), json.RawMessage(slopFailed))
	}
	if et == change.EventGateEvaluated {
		out = append(out, json.RawMessage(gateGreen), json.RawMessage(gateRed))
	}
	if et == change.EventPRClosed {
		out = append(out, json.RawMessage(prClosedMerged), json.RawMessage(prClosedUnmerged))
	}
	if et == change.EventFindingStateChanged {
		for _, st := range []string{"fix_claimed", "verifying", "resolved", "open", "upheld"} {
			out = append(out, findingPayload(st))
		}
	}
	if et == change.EventDecisionRecorded {
		out = append(out, decisionPayload("reject"), decisionPayload("dismiss"), decisionPayload("review"))
	}
	if et == change.EventTestsVerified {
		out = append(out, json.RawMessage(testsVerifiedNeedsSignoff))
	}
	return out
}

// walkMove is one event a walk could send from a state, with the state it leads to and whether it is the default
// payload of its type.
type walkMove struct {
	ev        change.Event
	next      change.State
	isDefault bool
}

// legalMoves lists every event a walk could send from s: each type with its default payload and its alternatives.
func legalMoves(t *testing.T, s change.State, version int64) []walkMove {
	t.Helper()
	var moves []walkMove
	for _, et := range change.All() {
		payloads := append([]json.RawMessage{tablePayload(t, s, et)}, alternatives(et)...)
		for i, raw := range payloads {
			e := reducerEvent(version, et, raw)
			if n, err := change.Reduce(s, e); err == nil {
				moves = append(moves, walkMove{ev: e, next: n, isDefault: i == 0})
			}
		}
	}
	return moves
}

// pickMove chooses a move with rng. Rejections end a walk, so most picks avoid them; one in twenty is allowed.
func pickMove(rng *rand.Rand, moves []walkMove) walkMove {
	if rng.Intn(20) == 0 {
		return moves[rng.Intn(len(moves))]
	}
	var progress []walkMove
	for _, m := range moves {
		if m.next != change.StateRejected {
			progress = append(progress, m)
		}
	}
	if len(progress) == 0 {
		return moves[rng.Intn(len(moves))]
	}
	return progress[rng.Intn(len(progress))]
}

// The random walk takes legal moves chosen by a fixed seed, checks each default move against wantNext, and checks
// that replay reproduces the state. Fixed seeds keep it deterministic.
func TestRandomWalksOverLegalTransitionsAgreeWithTheTableAndReplayTwice(t *testing.T) {
	const walks = 25
	const maxSteps = 200
	for seed := int64(1); seed <= walks; seed++ {
		rng := rand.New(rand.NewSource(seed))
		var s change.State
		var events []change.Event
		for step := 0; step < maxSteps && !s.IsTerminal(); step++ {
			moves := legalMoves(t, s, int64(len(events)+1))
			if len(moves) == 0 {
				break
			}
			m := pickMove(rng, moves)
			if m.isDefault {
				want, ok := wantNext[s][m.ev.Type]
				require.True(t, ok, "walk seed %d: %q + %s is legal but not in the spec table", seed, s, m.ev.Type)
				require.Equal(t, want, m.next, "walk seed %d: %q + %s", seed, s, m.ev.Type)
			}
			require.NoError(t, m.ev.Validate())
			events = append(events, m.ev)
			s = m.next
		}
		requireReplayTwice(t, events, s)
	}
}
