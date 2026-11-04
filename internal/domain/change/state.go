package change

import "slices"

// State is where a change is in its life (TECH_SPEC 3.3). The reducer that moves between states is task T21; this
// type only names them and says which are terminal.
type State string

// The states of TECH_SPEC 3.3, in the order the spec draws them.
const (
	StateTriggered           State = "TRIGGERED"
	StateRoomOpen            State = "ROOM_OPEN"
	StateWritingTests        State = "WRITING_TESTS"
	StateVerifyingTests      State = "VERIFYING_TESTS"
	StateAwaitingTestSignoff State = "AWAITING_TEST_SIGNOFF"
	StateTestsReady          State = "TESTS_READY"
	StateBuilding            State = "BUILDING"
	StatePROpen              State = "PR_OPEN"
	StateSlopRejected        State = "SLOP_REJECTED"
	StateUnlinkedPR          State = "UNLINKED_PR"
	StatePickingReviewers    State = "PICKING_REVIEWERS"
	StateInReview            State = "IN_REVIEW"
	StateDeliberating        State = "DELIBERATING"
	StateEscalated           State = "ESCALATED"
	StateFixing              State = "FIXING"
	StateVerifyingFix        State = "VERIFYING_FIX"
	StateReadyForApproval    State = "READY_FOR_APPROVAL"
	StateApproved            State = "APPROVED"
	StateMerged              State = "MERGED"
	StateRejected            State = "REJECTED"
)

// AllStates returns every state in the order of TECH_SPEC 3.3, as a fresh slice.
func AllStates() []State {
	return []State{
		StateTriggered, StateRoomOpen, StateWritingTests, StateVerifyingTests, StateAwaitingTestSignoff,
		StateTestsReady, StateBuilding, StatePROpen, StateSlopRejected, StateUnlinkedPR,
		StatePickingReviewers, StateInReview, StateDeliberating, StateEscalated, StateFixing,
		StateVerifyingFix, StateReadyForApproval, StateApproved, StateMerged, StateRejected,
	}
}

// Valid reports whether s is one of the states.
func (s State) Valid() bool { return slices.Contains(AllStates(), s) }

// IsTerminal reports whether a change in s takes no further events: merged or rejected (any state may go to
// REJECTED, TECH_SPEC 3.3). UNLINKED_PR is not terminal: pr.linked moves it on.
func (s State) IsTerminal() bool { return s == StateMerged || s == StateRejected }
