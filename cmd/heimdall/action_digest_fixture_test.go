package main

// The change-view fixture of the digest tests. It builds a real chained event log with change.Chain, so the extraction
// runs on the same shape a state ref holds: a trigger, a pull request, a fast check, a proof run that started and
// reported, one cost record and one open break-glass.

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// digestChangeID is the pull request the fixture is about.
const digestChangeID = id.ChangeID("pr-acme-pay-7")

// digestPolicyHash is a policy hash of the right shape.
const digestPolicyHash = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

// fixtureStep is one fact of the fixture: its type, its payload and how long after the start it happened.
type fixtureStep struct {
	typ     change.EventType
	payload change.Payload
	at      time.Duration
}

// digestChangeEvents builds the chained log of one change. It fails the test on a payload the catalog refuses, so a
// fixture can never drift away from the events the gate writes.
func digestChangeEvents(t *testing.T) []change.Event {
	t.Helper()
	base := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	steps := []fixtureStep{
		{change.EventPROpened, change.PROpened{Repo: "acme/pay", Number: 7, HeadSHA: testHead, Author: "ana",
			BaseSHA: testBase}, 0},
		{change.EventFastReported, change.FastReported{Head: testHead, Verdict: gate.VerdictPass,
			Clauses: []change.FastClause{{ID: fastcheck.ClauseSecrets, Status: fastcheck.StatusMet, Reason: "none"}}},
			time.Minute},
		{change.EventProofStarted, change.ProofStarted{WorkflowRun: 5002, Head: testHead, Base: testBase,
			PolicyHash: digestPolicyHash, Model: "test-model"}, 2 * time.Minute},
		{change.EventProofReported, change.ProofReported{WorkflowRun: 5002, Head: testHead, Base: testBase,
			PolicyHash: digestPolicyHash, Model: "test-model", Kind: proof.KindBlocked,
			Counts: change.ProofCounts{In: 1, Proven: 1, Blocking: 1},
			Cost:   change.Cost{USDMicros: 250_000}}, 7 * time.Minute},
		{change.EventCostRecorded, change.CostRecorded{WorkflowRun: 5002, Model: "test-model",
			Cost: change.Cost{USDMicros: 250_000}}, 7 * time.Minute},
		{change.EventBreakGlassRecorded, change.BreakGlassRecorded{Head: testHead, Login: "ana",
			Reason: "the fix is in the next push", SourceRef: "comment-1"}, 8 * time.Minute},
	}
	return chainedEvents(t, base, steps)
}

// chainedEvents turns the steps into a valid log: versions from 1, one actor, one correlation id, and a hash chain.
func chainedEvents(t *testing.T, base time.Time, steps []fixtureStep) []change.Event {
	t.Helper()
	out := make([]change.Event, 0, len(steps))
	var prev change.Hash
	for i, s := range steps {
		raw, err := json.Marshal(s.payload)
		require.NoError(t, err)
		e := change.Event{ID: change.EventID(fixtureEventID(i)), ChangeID: digestChangeID, Version: int64(i + 1), Type: s.typ,
			Actor: change.Actor{Kind: change.ActorSystem, ID: "action-gate"}, OccurredAt: base.Add(s.at),
			CorrelationID: "digest-fixture", Payload: raw, PrevHash: prevAt(i, prev)}
		require.NoError(t, e.Validate(), "the fixture event %d must be valid", i+1)
		h, err := change.Chain(prev, e)
		require.NoError(t, err)
		e.Hash, prev = h, h
		out = append(out, e)
	}
	return out
}

// prevAt is the previous hash of an event: nil for the first, the chain head after it.
func prevAt(i int, prev change.Hash) *change.Hash {
	if i == 0 {
		return nil
	}
	return &prev
}

// fixtureEventID is an event id of the shape the engine writes: a lowercase UUID whose last group counts the event.
func fixtureEventID(i int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1) }

// changeViewOf folds the fixture, so a test can read the view the extraction reads.
func changeViewOf(t *testing.T, events []change.Event) change.View {
	t.Helper()
	v, err := change.Fold(events)
	require.NoError(t, err)
	return v
}
