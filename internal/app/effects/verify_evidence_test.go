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

func runRepro(t *testing.T, sandbox *overlaySandboxFake) (*fakeIngest, error) {
	t.Helper()
	ingest := &fakeIngest{}
	exec, err := effects.NewVerifyEvidence(effects.VerifyEvidenceDeps{
		Source: &fakeSource{}, Sandbox: sandbox, Profiles: profileFake{}, Ingest: ingest, IDs: &fakeIDs{}, Clock: fixedClock{},
	})
	require.NoError(t, err)
	return ingest, exec.Execute(context.Background(), sampleReproEffect())
}

// Only an assertion failure of the named repro on the head verifies E1. A pass, a compile/setup error, or a test that
// errors instead of failing (panic, timeout) is not a failed assertion: the finding drops to E4 unverified.
func TestVerifyEvidenceReproOutcomes(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		res       ports.SandboxResult
		wantEvent change.EventType
		wantTier  finding.Tier
		wantState finding.State
	}{
		"assertion failure verifies E1": {
			ports.SandboxResult{Failed: 1, Tests: []ports.TestResult{{Name: veTestName, Outcome: ports.OutcomeFailure}}},
			change.EventFindingVerified, finding.TierE1, finding.StateOpen},
		"pass does not verify": {
			ports.SandboxResult{Passed: 1, Tests: []ports.TestResult{{Name: veTestName, Outcome: ports.OutcomePassed}}},
			change.EventFindingStateChanged, finding.TierE4, finding.StateUnverified},
		"compile error never verifies": {
			ports.SandboxResult{Errored: 1, Tests: []ports.TestResult{{Name: veTestName, Outcome: ports.OutcomeError}}},
			change.EventFindingStateChanged, finding.TierE4, finding.StateUnverified},
		"a test that errors instead of failing never verifies": {
			ports.SandboxResult{Errored: 1, Tests: []ports.TestResult{{Name: "TestReproErrors", Outcome: ports.OutcomeError}}},
			change.EventFindingStateChanged, finding.TierE4, finding.StateUnverified},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			sandbox := &overlaySandboxFake{res: c.res}
			ingest, err := runRepro(t, sandbox)
			require.NoError(t, err)

			require.Len(t, ingest.written, 1)
			require.Equal(t, c.wantEvent, ingest.written[0].Type)
			var rec change.FindingRecord
			require.NoError(t, json.Unmarshal(ingest.written[0].Payload, &rec))
			require.Equal(t, veFndID, rec.FindingID)
			require.Equal(t, c.wantTier, rec.Tier)
			require.Equal(t, c.wantState, rec.State)

			require.Len(t, sandbox.runs, 1)
			require.Equal(t, ports.SandboxRepro, sandbox.runs[0].Kind)
			require.Equal(t, veHead, sandbox.runs[0].SHA)
			require.Len(t, sandbox.overlays, 1)
			require.Equal(t, veTestPath, sandbox.overlays[0][0].Path)
		})
	}
}

// No result at all (runner busy, socket down, effect out of time) says nothing about the claim: the effect fails, so
// the outbox retries it, and the finding keeps its tier.
func TestVerifyEvidenceRetriesWhenTheSandboxGivesNoResult(t *testing.T) {
	t.Parallel()
	ingest, err := runRepro(t, &overlaySandboxFake{err: errors.New("runner rejected job: runner is busy")})
	require.Error(t, err)
	require.Empty(t, ingest.written, "no state change without a result")
}
