package effects_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type overlaySandboxFake struct {
	runs     []ports.SandboxJob
	overlays [][]ports.File
	res      ports.SandboxResult
	err      error
}

func (f *overlaySandboxFake) Run(_ context.Context, j ports.SandboxJob) (ports.SandboxResult, error) {
	f.runs = append(f.runs, j)
	return f.res, f.err
}

func (f *overlaySandboxFake) RunOverlay(_ context.Context, j ports.SandboxJob, o []ports.File) (ports.SandboxResult, error) {
	f.runs = append(f.runs, j)
	f.overlays = append(f.overlays, o)
	return f.res, f.err
}

const (
	veTicket   = id.ChangeID("PAY-142")
	veRepo     = change.Repo("acme/pay-demo")
	veHead     = id.SHA("0123456789abcdef0123456789abcdef01234567")
	veFndID    = id.FindingID("fnd_01HZX3K9QW7M2P5R8T0V4YB6CD")
	veTestPath = "tests/repro_test.go"
	veTestName = "TestRepro"
)

func sampleReproEffect() change.Effect {
	src := base64.StdEncoding.EncodeToString([]byte("package test\nfunc TestRepro(t *testing.T){}\n"))
	ref, _ := id.ParseFindingRef("F1")
	p := change.VerifyEvidencePayload{
		Repo:       veRepo,
		Ticket:     veTicket,
		FindingID:  veFndID,
		FindingRef: ref,
		Severity:   finding.SeverityHigh,
		HeadSHA:    veHead,
		TestPath:   veTestPath,
		TestSource: src,
		TestName:   veTestName,
	}
	raw, _ := json.Marshal(p)
	key, _ := change.IdempotencyKey(veTicket, 1, change.EffectVerifyEvidence, string(veFndID)+":"+string(veHead))
	return change.Effect{
		Key:     key,
		Kind:    change.EffectVerifyEvidence,
		Target:  string(veTicket),
		Payload: raw,
	}
}

func sampleChangeEvents() []change.Event {
	return []change.Event{
		{
			ID:       "ev-1",
			ChangeID: veTicket,
			Version:  1,
			Type:     change.EventChangeTriggered,
			Payload:  json.RawMessage(`{"ticket":"PAY-142","repo":"` + string(veRepo) + `","assignee":"person:alice","source":"jira"}`),
		},
		{
			ID:       "ev-2",
			ChangeID: veTicket,
			Version:  2,
			Type:     change.EventPROpened,
			Payload:  json.RawMessage(`{"repo":"` + string(veRepo) + `","number":42,"head_sha":"` + string(veHead) + `","author":"ana","ticket":"PAY-142"}`),
		},
		{
			ID:       "ev-3",
			ChangeID: veTicket,
			Version:  3,
			Type:     change.EventFindingFiled,
			Payload:  json.RawMessage(`{"finding_id":"` + string(veFndID) + `","ref":"F1","tier":"E1","severity":"high","state":"open","by":"reviewer-agent"}`),
		},
	}
}

func TestVerifyEvidenceAssertionFailureVerifiesE1(t *testing.T) {
	ctx := context.Background()
	sandbox := &overlaySandboxFake{
		res: ports.SandboxResult{
			Failed: 1,
			Tests: []ports.TestResult{
				{Name: veTestName, Outcome: ports.OutcomeFailure},
			},
		},
	}
	store := &fakeStore{events: sampleChangeEvents()}
	ingest := &fakeIngest{}
	source := &fakeSource{}

	exec, err := effects.NewVerifyEvidence(effects.VerifyEvidenceDeps{
		Source:  source,
		Sandbox: sandbox,
		Store:   store,
		Ingest:  ingest,
		IDs:     &fakeIDs{},
		Clock:   fixedClock{},
	})
	require.NoError(t, err)

	err = exec.Execute(ctx, sampleReproEffect())
	require.NoError(t, err)

	require.Len(t, ingest.written, 1)
	require.Equal(t, change.EventFindingVerified, ingest.written[0].Type)

	var verified change.FindingVerified
	require.NoError(t, json.Unmarshal(ingest.written[0].Payload, &verified))
	require.Equal(t, veFndID, verified.FindingID)
	require.Equal(t, finding.TierE1, verified.Tier)
	require.Equal(t, finding.StateOpen, verified.State)

	require.Len(t, sandbox.runs, 1)
	require.Equal(t, ports.SandboxRepro, sandbox.runs[0].Kind)
	require.Equal(t, veHead, sandbox.runs[0].SHA)
	require.Len(t, sandbox.overlays, 1)
	require.Equal(t, veTestPath, sandbox.overlays[0][0].Path)
}

func TestVerifyEvidencePassDoesNotVerifyE1(t *testing.T) {
	ctx := context.Background()
	// Repro passes on head -> does not verify -> state_changed to E4 unverified
	sandbox := &overlaySandboxFake{
		res: ports.SandboxResult{
			Passed: 1,
			Tests: []ports.TestResult{
				{Name: veTestName, Outcome: ports.OutcomePassed},
			},
		},
	}
	store := &fakeStore{events: sampleChangeEvents()}
	ingest := &fakeIngest{}
	source := &fakeSource{}

	exec, err := effects.NewVerifyEvidence(effects.VerifyEvidenceDeps{
		Source:  source,
		Sandbox: sandbox,
		Store:   store,
		Ingest:  ingest,
		IDs:     &fakeIDs{},
		Clock:   fixedClock{},
	})
	require.NoError(t, err)

	err = exec.Execute(ctx, sampleReproEffect())
	require.NoError(t, err)

	require.Len(t, ingest.written, 1)
	require.Equal(t, change.EventFindingStateChanged, ingest.written[0].Type)

	var sc change.FindingStateChanged
	require.NoError(t, json.Unmarshal(ingest.written[0].Payload, &sc))
	require.Equal(t, veFndID, sc.FindingID)
	require.Equal(t, finding.TierE4, sc.Tier)
	require.Equal(t, finding.StateUnverified, sc.State)
}

func TestVerifyEvidenceCompileErrorNeverVerifies(t *testing.T) {
	ctx := context.Background()
	// Compile or setup error (<error>) -> state_changed to E4 unverified
	sandbox := &overlaySandboxFake{
		res: ports.SandboxResult{
			Errored: 1,
			Tests: []ports.TestResult{
				{Name: veTestName, Outcome: ports.OutcomeError},
			},
		},
	}
	store := &fakeStore{events: sampleChangeEvents()}
	ingest := &fakeIngest{}
	source := &fakeSource{}

	exec, err := effects.NewVerifyEvidence(effects.VerifyEvidenceDeps{
		Source:  source,
		Sandbox: sandbox,
		Store:   store,
		Ingest:  ingest,
		IDs:     &fakeIDs{},
		Clock:   fixedClock{},
	})
	require.NoError(t, err)

	err = exec.Execute(ctx, sampleReproEffect())
	require.NoError(t, err)

	require.Len(t, ingest.written, 1)
	require.Equal(t, change.EventFindingStateChanged, ingest.written[0].Type)

	var sc change.FindingStateChanged
	require.NoError(t, json.Unmarshal(ingest.written[0].Payload, &sc))
	require.Equal(t, veFndID, sc.FindingID)
	require.Equal(t, finding.TierE4, sc.Tier)
	require.Equal(t, finding.StateUnverified, sc.State)
}

func TestVerifyEvidenceRetriesWhenTheSandboxGivesNoResult(t *testing.T) {
	// No result at all (runner busy, socket down, effect out of time) says nothing about the claim: the effect fails,
	// so the outbox retries it, and the finding keeps its tier.
	sandbox := &overlaySandboxFake{err: errors.New("runner rejected job: runner is busy")}
	ingest := &fakeIngest{}
	exec, err := effects.NewVerifyEvidence(effects.VerifyEvidenceDeps{
		Source: &fakeSource{}, Sandbox: sandbox, Store: &fakeStore{events: sampleChangeEvents()}, Ingest: ingest, IDs: &fakeIDs{}, Clock: fixedClock{},
	})
	require.NoError(t, err)

	require.Error(t, exec.Execute(context.Background(), sampleReproEffect()))
	require.Empty(t, ingest.written, "no state change without a result")
}

func TestVerifyEvidenceTestThatErrorsInsteadOfFailingNeverVerifies(t *testing.T) {
	// A repro that panics, does not compile or times out inside the job is a result: it did not fail on an assertion,
	// so the finding drops to E4 unverified.
	sandbox := &overlaySandboxFake{res: ports.SandboxResult{Errored: 1, Tests: []ports.TestResult{{Name: "TestReproErrors", Outcome: ports.OutcomeError}}}}
	ingest := &fakeIngest{}
	exec, err := effects.NewVerifyEvidence(effects.VerifyEvidenceDeps{
		Source: &fakeSource{}, Sandbox: sandbox, Store: &fakeStore{events: sampleChangeEvents()}, Ingest: ingest, IDs: &fakeIDs{}, Clock: fixedClock{},
	})
	require.NoError(t, err)

	require.NoError(t, exec.Execute(context.Background(), sampleReproEffect()))
	require.Len(t, ingest.written, 1)
	require.Equal(t, change.EventFindingStateChanged, ingest.written[0].Type)
	var sc change.FindingStateChanged
	require.NoError(t, json.Unmarshal(ingest.written[0].Payload, &sc))
	require.Equal(t, finding.TierE4, sc.Tier)
	require.Equal(t, finding.StateUnverified, sc.State)
}

func TestVerifyEvidenceSkipsIfAlreadyRecorded(t *testing.T) {
	ctx := context.Background()
	events := append(sampleChangeEvents(), change.Event{
		ID:       "ev-ver-1",
		ChangeID: veTicket,
		Version:  4,
		Type:     change.EventFindingVerified,
		Payload:  json.RawMessage(`{"finding_id":"` + string(veFndID) + `","ref":"F1","tier":"E1","severity":"high","state":"open","by":"heimdall"}`),
	})
	store := &fakeStore{events: events}
	sandbox := &overlaySandboxFake{}
	ingest := &fakeIngest{}
	source := &fakeSource{}

	exec, err := effects.NewVerifyEvidence(effects.VerifyEvidenceDeps{
		Source:  source,
		Sandbox: sandbox,
		Store:   store,
		Ingest:  ingest,
		IDs:     &fakeIDs{},
		Clock:   fixedClock{},
	})
	require.NoError(t, err)

	err = exec.Execute(ctx, sampleReproEffect())
	require.NoError(t, err)

	require.Empty(t, ingest.written)
	require.Empty(t, sandbox.runs)
}

func TestVerifyEvidenceMissingDepsRefused(t *testing.T) {
	_, err := effects.NewVerifyEvidence(effects.VerifyEvidenceDeps{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing")
}
