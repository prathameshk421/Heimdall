package change_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// catalog is TECH_SPEC 3.2, spelled exactly. A typo in the enum fails here, not in production.
var catalog = []string{
	"change.triggered", "room.opened", "branch.ensured",
	"task.assigned", "task.acked", "task.timed_out", "task.failed", "task.progress", "step.retried", "step.escalated", "step.resumed",
	"question.asked", "question.answered",
	"tests.submitted", "tests.verified", "tests.rejected", "tests.committed", "tests.approved",
	"ticket.changed",
	"pr.opened", "pr.pushed", "pr.linked", "pr.closed",
	"slop.evaluated",
	"reviewers.picked",
	"review.submitted", "review.unavailable", "review.waived",
	"finding.filed", "finding.verified", "finding.state_changed",
	"challenge.filed",
	"case.opened", "verdict.submitted", "verdict.applied", "verdict.escalated",
	"decision.recorded",
	"approval.recorded", "approval.invalidated",
	"ci.reported", "gate.evaluated",
	"change.rejected", "change.merged",
}

func TestAllEventTypesMatchTheSpecCatalogInOrder(t *testing.T) {
	require.Len(t, catalog, 43)
	var got []string
	for _, et := range change.All() {
		require.True(t, et.Valid(), et)
		got = append(got, string(et))
	}
	require.Equal(t, catalog, got)
}

func TestEventTypeValidRefusesUnknownAndMisspelledTypes(t *testing.T) {
	for _, bad := range []change.EventType{"", "change.Triggered", "pr.open", "tests.submit", "change.merge", "gate.evaluate"} {
		require.False(t, bad.Valid(), bad)
	}
}

func TestAllReturnsAFreshSliceSoCallersCannotMutateTheCatalog(t *testing.T) {
	a := change.All()
	a[0] = "tampered"
	require.Equal(t, change.EventType("change.triggered"), change.All()[0])
}

func TestActorKindsAndValidation(t *testing.T) {
	for _, k := range []change.ActorKind{change.ActorHuman, change.ActorAgent, change.ActorSystem, change.ActorGitHub, change.ActorJira, change.ActorSwitch} {
		require.True(t, k.Valid(), k)
		require.NoError(t, change.Actor{Kind: k, ID: "bob"}.Validate())
	}
	require.False(t, change.ActorKind("robot").Valid())
	err := change.Actor{Kind: "robot", ID: ""}.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "actor.kind")
	require.Contains(t, err.Error(), "actor.id")
}

// envelope is a valid event whose payload is the smallest one the catalog has; tests change one field at a time.
func envelope(t *testing.T) change.Event {
	t.Helper()
	return change.Event{
		ID:            "3f1c9a52-8d2b-4e7a-9c11-0b6f5d4e3a21",
		ChangeID:      "PAY-142",
		Version:       1,
		Type:          change.EventChangeRejected,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    time.Date(2026, 11, 4, 9, 30, 0, 0, time.UTC),
		CorrelationID: "trace-1",
		Payload:       json.RawMessage(`{"reason":"ticket closed"}`),
		BlobRefs:      []workflow.BlobRef{},
	}
}

func TestEventValidateAcceptsAWellFormedEnvelope(t *testing.T) {
	require.NoError(t, envelope(t).Validate())
}

func TestEventValidateNamesEveryBrokenField(t *testing.T) {
	cases := map[string]struct {
		mutate func(*change.Event)
		field  string
	}{
		"change id":      {func(e *change.Event) { e.ChangeID = "not a change" }, "change_id"},
		"version zero":   {func(e *change.Event) { e.Version = 0 }, "version"},
		"unknown type":   {func(e *change.Event) { e.Type = "change.exploded" }, "type"},
		"unknown kind":   {func(e *change.Event) { e.Actor.Kind = "robot" }, "actor.kind"},
		"no occurred at": {func(e *change.Event) { e.OccurredAt = time.Time{} }, "occurred_at"},
		"no correlation": {func(e *change.Event) { e.CorrelationID = "" }, "correlation_id"},
		"payload not object": {func(e *change.Event) {
			e.Payload = json.RawMessage(`[1]`)
		}, "payload"},
		"payload with an unknown field": {func(e *change.Event) {
			e.Payload = json.RawMessage(`{"reason":"x","extra":1}`)
		}, "payload"},
		"empty payload": {func(e *change.Event) { e.Payload = nil }, "payload"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := envelope(t)
			c.mutate(&e)
			err := e.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), c.field)
		})
	}
}

func TestEventValidateJoinsEveryProblemNotJustTheFirst(t *testing.T) {
	e := envelope(t)
	e.Version = 0
	e.Actor.Kind = "robot"
	e.OccurredAt = time.Time{}
	err := e.Validate()
	require.Error(t, err)
	msg := err.Error()
	require.Contains(t, msg, "version")
	require.Contains(t, msg, "actor.kind")
	require.Contains(t, msg, "occurred_at")
}

func TestInvariantOneAgentsCannotRecordDecisionsOrApprovals(t *testing.T) {
	for _, typ := range []change.EventType{change.EventDecisionRecorded, change.EventApprovalRecorded} {
		e := envelope(t)
		e.Type = typ
		e.Actor = change.Actor{Kind: change.ActorAgent, ID: "bob-reviewer"}
		err := e.Validate()
		require.Error(t, err, typ)
		require.Contains(t, err.Error(), "invariant 1", typ)
	}
	// Other events may come from agents: an agent files findings and submits tests.
	e := envelope(t)
	e.Type = change.EventFindingFiled
	e.Actor = change.Actor{Kind: change.ActorAgent, ID: "bob-reviewer"}
	require.NotContains(t, errString(e.Validate()), "invariant 1")
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestEventJSONRoundTripKeepsTheEnvelopeFieldsAndNames(t *testing.T) {
	e := envelope(t)
	cause := change.EventID("9d2e6f10-1b3c-4a5d-8e7f-2a4b6c8d0e1f")
	e.CausationID = &cause
	b, err := json.Marshal(e)
	require.NoError(t, err)
	var keys map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &keys))
	for _, k := range []string{"id", "change_id", "version", "type", "actor", "occurred_at", "causation_id", "correlation_id", "payload", "blob_refs", "prev_hash", "hash"} {
		require.Contains(t, keys, k, "envelope field %s is part of the frozen wire format", k)
	}
	var back change.Event
	require.NoError(t, json.Unmarshal(b, &back))
	require.Equal(t, e.ChangeID, back.ChangeID)
	require.Equal(t, cause, *back.CausationID)
	require.Contains(t, string(b), `"type":"change.rejected"`)
}

func TestEventDecodingRefusesABadIDBeforeValidate(t *testing.T) {
	var e change.Event
	err := json.Unmarshal([]byte(`{"id":"nope","change_id":"PAY-142","version":1}`), &e)
	require.Error(t, err)
}
