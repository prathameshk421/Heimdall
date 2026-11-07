package effects_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const checkFile = "internal/pay/refund.go"

func verifyCheckEffect(t *testing.T) change.Effect {
	t.Helper()
	ref, _ := id.ParseFindingRef("F1")
	p := change.VerifyEvidencePayload{
		Repo: veRepo, Ticket: veTicket, FindingID: veFndID, FindingRef: ref, Severity: finding.SeverityHigh, HeadSHA: veHead,
		Check: &change.VerifyCheck{Name: change.CheckGoVet, File: checkFile, Lines: [2]int{10, 12}},
	}
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	key, err := change.IdempotencyKey(veTicket, 1, change.EffectVerifyEvidence, string(veFndID)+":"+string(veHead))
	require.NoError(t, err)
	return change.Effect{Key: key, Kind: change.EffectVerifyEvidence, Target: string(veTicket), Payload: raw}
}

func verifyCheckEvents(extra ...change.Event) []change.Event {
	evs := sampleChangeEvents()
	evs[2].Payload = json.RawMessage(`{"finding_id":"` + string(veFndID) + `","ref":"F1","tier":"E2","severity":"high","state":"open","by":"reviewer-agent"}`)
	return append(evs, extra...)
}

func verifyCheckVerified() change.Event {
	return change.Event{
		ID: "ev-4", ChangeID: veTicket, Version: 4, Type: change.EventFindingVerified,
		Payload: json.RawMessage(`{"finding_id":"` + string(veFndID) + `","ref":"F1","tier":"E2","severity":"high","state":"open","by":"heimdall"}`),
	}
}

func verifyCheckPushed() change.Event {
	return change.Event{
		ID: "ev-5", ChangeID: veTicket, Version: 5, Type: change.EventPRPushed,
		Payload: json.RawMessage(`{"repo":"` + string(veRepo) + `","number":42,"head_sha":"` + string(veHead) + `"}`),
	}
}

func runVerifyCheck(t *testing.T, res ports.SandboxResult, runErr error, events []change.Event) (*overlaySandboxFake, *fakeIngest, error) {
	t.Helper()
	sandbox := &overlaySandboxFake{res: res, err: runErr}
	ingest := &fakeIngest{}
	exec, err := effects.NewVerifyEvidence(effects.VerifyEvidenceDeps{
		Source: &fakeSource{}, Sandbox: sandbox, Store: &fakeStore{events: events}, Ingest: ingest, IDs: &fakeIDs{}, Clock: fixedClock{},
	})
	require.NoError(t, err)
	return sandbox, ingest, exec.Execute(context.Background(), verifyCheckEffect(t))
}

func decodeVerifyWritten(t *testing.T, ingest *fakeIngest) (change.EventType, change.FindingRecord) {
	t.Helper()
	require.Len(t, ingest.written, 1)
	var rec change.FindingRecord
	require.NoError(t, json.Unmarshal(ingest.written[0].Payload, &rec))
	return ingest.written[0].Type, rec
}

// The first run confirms an E2 finding only when the check fails at the location the finding names, and it runs the
// fixed static job through the runner.
func TestVerifyCheckConfirmsAFailureAtTheNamedLocation(t *testing.T) {
	sandbox, ingest, err := runVerifyCheck(t, ports.SandboxResult{Failed: 1, Output: "# pay\n./internal/pay/refund.go:11:3: bad format\n"}, nil, verifyCheckEvents())
	require.NoError(t, err)

	typ, rec := decodeVerifyWritten(t, ingest)
	require.Equal(t, change.EventFindingVerified, typ)
	require.Equal(t, finding.TierE2, rec.Tier)
	require.Equal(t, finding.StateOpen, rec.State)
	require.Len(t, sandbox.runs, 1)
	require.Equal(t, ports.SandboxStatic, sandbox.runs[0].Kind)
	require.Equal(t, ports.CheckGoVet, sandbox.runs[0].Check)
	require.Equal(t, "internal/pay", sandbox.runs[0].Dir)
}

// A check that fails somewhere else, or passes, does not confirm the claim: the finding drops to E4 and never blocks.
func TestVerifyCheckWithoutAMatchingFailureStaysE4(t *testing.T) {
	for name, res := range map[string]ports.SandboxResult{
		"failure elsewhere": {Failed: 1, Output: "./other.go:3:1: bad\n"},
		"pass":              {Passed: 1},
	} {
		t.Run(name, func(t *testing.T) {
			_, ingest, err := runVerifyCheck(t, res, nil, verifyCheckEvents())
			require.NoError(t, err)
			typ, rec := decodeVerifyWritten(t, ingest)
			require.Equal(t, change.EventFindingStateChanged, typ)
			require.Equal(t, finding.TierE4, rec.Tier)
			require.Equal(t, finding.StateUnverified, rec.State)
		})
	}
}

// After a push, a pass resolves the finding with no claim from the author.
func TestVerifyCheckResolvesAfterAFixPush(t *testing.T) {
	_, ingest, err := runVerifyCheck(t, ports.SandboxResult{Passed: 1}, nil, verifyCheckEvents(verifyCheckVerified(), verifyCheckPushed()))
	require.NoError(t, err)
	typ, rec := decodeVerifyWritten(t, ingest)
	require.Equal(t, change.EventFindingStateChanged, typ)
	require.Equal(t, finding.StateResolved, rec.State)
	require.Equal(t, finding.TierE2, rec.Tier)
}

// A failure elsewhere resolves the located problem too, but a diagnostic still at the location keeps it verified.
func TestVerifyCheckAfterAPushKeepsOrResolvesByLocation(t *testing.T) {
	events := verifyCheckEvents(verifyCheckVerified(), verifyCheckPushed())

	_, ingest, err := runVerifyCheck(t, ports.SandboxResult{Failed: 1, Output: "./other.go:3:1: bad\n"}, nil, events)
	require.NoError(t, err)
	_, rec := decodeVerifyWritten(t, ingest)
	require.Equal(t, finding.StateResolved, rec.State, "the located problem is fixed, another one is not this finding")

	_, ingest, err = runVerifyCheck(t, ports.SandboxResult{Failed: 1, Output: "./internal/pay/refund.go:12:1: still bad\n"}, nil, events)
	require.NoError(t, err)
	typ, rec := decodeVerifyWritten(t, ingest)
	require.Equal(t, change.EventFindingVerified, typ, "verified again, so the push does not leave the step waiting")
	require.Equal(t, finding.StateOpen, rec.State)
}

// A check that could not run proves nothing either way: the effect fails so the outbox retries, and nothing is written.
func TestVerifyCheckThatCouldNotRunWritesNothing(t *testing.T) {
	_, ingest, err := runVerifyCheck(t, ports.SandboxResult{}, nil, verifyCheckEvents())
	require.Error(t, err)
	require.Empty(t, ingest.written)

	_, ingest, err = runVerifyCheck(t, ports.SandboxResult{}, context.DeadlineExceeded, verifyCheckEvents())
	require.Error(t, err)
	require.Empty(t, ingest.written)
}
