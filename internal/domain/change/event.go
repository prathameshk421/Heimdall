package change

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// EventType names what happened. Each constant is one row of the catalog in TECH_SPEC 3.2; the constants live in
// the area files events_<area>.go beside their payloads.
type EventType string

// All returns every event type in catalog order. It returns a fresh slice, because package-level mutable state is
// not allowed in the domain.
func All() []EventType {
	return []EventType{
		EventChangeTriggered, EventRoomOpened, EventBranchEnsured,
		EventTaskAssigned, EventTaskAcked, EventTaskTimedOut, EventTaskFailed, EventTaskProgress,
		EventTaskSuperseded, EventTaskRewoken, EventProviderProbed,
		EventStepRetried, EventStepEscalated, EventStepResumed,
		EventProfileProposed,
		EventQuestionAsked, EventQuestionAnswered, EventCommandProposed, EventCommandConfirmed,
		EventTestsSubmitted, EventTestsVerified, EventTestsRejected, EventTestsCommitted, EventTestsApproved,
		EventTicketChanged,
		EventPROpened, EventPRPushed, EventPRLinked, EventPRClosed, EventPRReopened,
		EventSlopEvaluated,
		EventReviewersPicked,
		EventReviewSubmitted, EventReviewUnavailable, EventReviewWaived,
		EventFindingFiled, EventFindingVerified, EventFindingStateChanged, EventFindingRecheckRequested,
		EventChallengeFiled,
		EventCaseOpened, EventVerdictSubmitted, EventVerdictApplied, EventVerdictEscalated,
		EventDecisionRecorded,
		EventApprovalRecorded, EventApprovalInvalidated,
		EventCIReported, EventChecksReported, EventGateEvaluated,
		EventChangeRejected, EventChangeMerged,
		// The Action area (events_action.go) follows the server's events, so no earlier position moves.
		EventFastReported, EventProofStarted, EventProofReported, EventCostRecorded, EventExternalIngested,
		EventSummaryPosted, EventBreakGlassRecorded, EventBreakGlassReviewed, EventStateReset,
	}
}

// Valid reports whether t is in the catalog.
func (t EventType) Valid() bool { return slices.Contains(All(), t) }

// Event is one fact about one change, in the envelope of TECH_SPEC 3.2. Events are immutable once appended; the
// hash chain (hash.go) makes any edit visible.
type Event struct {
	ID            EventID            `json:"id"`
	ChangeID      id.ChangeID        `json:"change_id"`
	Version       int64              `json:"version"`
	Type          EventType          `json:"type"`
	Actor         Actor              `json:"actor"`
	OccurredAt    time.Time          `json:"occurred_at"`
	CausationID   *EventID           `json:"causation_id"`
	CorrelationID CorrelationID      `json:"correlation_id"`
	Payload       json.RawMessage    `json:"payload"`
	BlobRefs      []workflow.BlobRef `json:"blob_refs"`
	PrevHash      *Hash              `json:"prev_hash"`
	Hash          Hash               `json:"hash"`
}

// Validate returns every problem with the envelope and its payload. It does not check the hash chain; VerifyChain
// does that across events. The payload is decoded with the typed registry, so a payload of the wrong shape is
// refused here as well.
func (e Event) Validate() error {
	var v problems
	_, err := id.ParseChangeID(string(e.ChangeID))
	v.add("change_id", err)
	_, err = ParseEventID(string(e.ID))
	v.add("id", err)
	v.require("version", e.Version >= 1, "must be 1 or more")
	v.require("type", e.Type.Valid(), "is not in the event catalog")
	v = append(v, e.Actor.Validate())
	v.require("occurred_at", !e.OccurredAt.IsZero(), "is required")
	if e.CausationID != nil {
		_, err = ParseEventID(string(*e.CausationID))
		v.add("causation_id", err)
	}
	_, err = ParseCorrelationID(string(e.CorrelationID))
	v.add("correlation_id", err)
	v.each("blob_refs", e.BlobRefs, checkBlob)
	v = append(v, e.validatePayload())
	return v.join()
}

// validatePayload decodes the payload of the event's type; an unknown type has nothing to decode here, and the
// type problem is already reported.
func (e Event) validatePayload() error {
	if !e.Type.Valid() {
		return nil
	}
	_, err := Decode(e)
	return err
}
