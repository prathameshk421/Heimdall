package change

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Payload is the typed content of one event. Every payload type has a Validate that reports every problem.
type Payload interface {
	Validate() error
}

// payloadEntry is what the registry knows about one event type: its zero value and how to decode and check it.
type payloadEntry struct {
	zero   Payload
	decode func(json.RawMessage) (Payload, error)
}

// entry builds the registry entry of payload type T.
func entry[T Payload]() payloadEntry {
	var zero T
	return payloadEntry{zero: zero, decode: decodeAs[T]}
}

// registry maps every catalog type to its payload type. It is built fresh on each call because package-level
// mutable state is not allowed in the domain; the map is small.
func registry() map[EventType]payloadEntry {
	return map[EventType]payloadEntry{
		EventChangeTriggered:     entry[Triggered](),
		EventRoomOpened:          entry[RoomOpened](),
		EventBranchEnsured:       entry[BranchEnsured](),
		EventTaskAssigned:        entry[TaskAssigned](),
		EventTaskAcked:           entry[TaskAcked](),
		EventTaskTimedOut:        entry[TaskTimedOut](),
		EventTaskFailed:          entry[TaskFailed](),
		EventTaskProgress:        entry[TaskProgress](),
		EventStepRetried:         entry[StepRetried](),
		EventStepEscalated:       entry[StepEscalated](),
		EventQuestionAsked:       entry[QuestionAsked](),
		EventQuestionAnswered:    entry[QuestionAnswered](),
		EventTestsSubmitted:      entry[TestsSubmitted](),
		EventTestsVerified:       entry[TestsVerified](),
		EventTestsRejected:       entry[TestsRejected](),
		EventTestsCommitted:      entry[TestsCommitted](),
		EventTestsApproved:       entry[TestsApproved](),
		EventTicketChanged:       entry[TicketChanged](),
		EventPROpened:            entry[PROpened](),
		EventPRPushed:            entry[PRPushed](),
		EventPRLinked:            entry[PRLinked](),
		EventPRClosed:            entry[PRClosed](),
		EventSlopEvaluated:       entry[SlopEvaluated](),
		EventReviewersPicked:     entry[ReviewersPicked](),
		EventReviewSubmitted:     entry[ReviewSubmitted](),
		EventReviewUnavailable:   entry[ReviewUnavailable](),
		EventReviewWaived:        entry[ReviewWaived](),
		EventFindingFiled:        entry[FindingFiled](),
		EventFindingVerified:     entry[FindingVerified](),
		EventFindingStateChanged: entry[FindingStateChanged](),
		EventChallengeFiled:      entry[ChallengeFiled](),
		EventCaseOpened:          entry[CaseOpened](),
		EventVerdictSubmitted:    entry[VerdictSubmitted](),
		EventVerdictApplied:      entry[VerdictApplied](),
		EventVerdictEscalated:    entry[VerdictEscalated](),
		EventDecisionRecorded:    entry[DecisionRecorded](),
		EventApprovalRecorded:    entry[ApprovalRecorded](),
		EventApprovalInvalidated: entry[ApprovalInvalidated](),
		EventCIReported:          entry[CIReported](),
		EventGateEvaluated:       entry[GateEvaluated](),
		EventChangeRejected:      entry[Rejected](),
		EventChangeMerged:        entry[Merged](),
	}
}

// PayloadFor returns a zero value of the payload type of t, and false when t is not in the catalog. Consumers that
// need the typed content of a stored event use Decode.
func PayloadFor(t EventType) (Payload, bool) {
	e, ok := registry()[t]
	return e.zero, ok
}

// Decode returns the typed payload of e. It refuses an event whose type is unknown, whose payload is not a strict
// JSON object of the right shape, or whose actor breaks invariant 1 (TECH_SPEC 3.4): no agent may record a decision
// or an approval. The result is the payload type of e.Type, as a value.
func Decode(e Event) (Payload, error) {
	entry, ok := registry()[e.Type]
	if !ok {
		return nil, fmt.Errorf("type %q is not in the event catalog", e.Type)
	}
	if err := checkActorRule(e); err != nil {
		return nil, err
	}
	p, err := entry.decode(e.Payload)
	if err != nil {
		return nil, fmt.Errorf("payload of %s: %w", e.Type, err)
	}
	return p, nil
}

// checkActorRule enforces invariant 1 (TECH_SPEC 3.4). It is checked before the payload is read, so the refusal
// does not depend on the payload's shape.
func checkActorRule(e Event) error {
	if (e.Type == EventDecisionRecorded || e.Type == EventApprovalRecorded) && e.Actor.Kind == ActorAgent {
		return fmt.Errorf("invariant 1: %s cannot come from an agent (actor %q)", e.Type, e.Actor.ID)
	}
	return nil
}

// decodeAs strictly decodes raw into a T and validates it.
func decodeAs[T Payload](raw json.RawMessage) (Payload, error) {
	if err := checkStrictJSON(raw); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var v T
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("has data after the object")
	}
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return v, nil
}
