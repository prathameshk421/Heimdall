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
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
)

type fakeRecheck struct {
	state verify.MemoryState
	err   error
	agent id.AgentName
	sha   id.SHA
	ref   string
	calls int
}

func (f *fakeRecheck) Recheck(_ context.Context, agent id.AgentName, _ change.Repo, sha id.SHA, ref string) (verify.MemoryState, error) {
	f.calls++
	f.agent, f.sha, f.ref = agent, sha, ref
	return f.state, f.err
}

func memoryEffect(t *testing.T) change.Effect {
	t.Helper()
	ref, _ := id.ParseFindingRef("F1")
	p := change.VerifyEvidencePayload{
		Repo: veRepo, Ticket: veTicket, FindingID: veFndID, FindingRef: ref, Severity: finding.SeverityHigh, HeadSHA: veHead, BaseSHA: memoryBase,
		Memory: &change.VerifyMemory{Ref: "decision:refund-limit", Agent: "bob-reviewer"},
	}
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	key, err := change.IdempotencyKey(veTicket, 1, change.EffectVerifyEvidence, string(veFndID)+":"+string(veHead))
	require.NoError(t, err)
	return change.Effect{Key: key, Kind: change.EffectVerifyEvidence, Target: string(veTicket), Payload: raw}
}

func runMemory(t *testing.T, recheck effects.MemoryRecheck) (*fakeIngest, error) {
	t.Helper()
	ingest := &fakeIngest{}
	exec, err := effects.NewVerifyEvidence(effects.VerifyEvidenceDeps{
		Source: &fakeSource{}, Sandbox: &overlaySandboxFake{}, Profiles: profileFake{}, Memory: recheck, Ingest: ingest, IDs: &fakeIDs{}, Clock: fixedClock{},
	})
	require.NoError(t, err)
	return ingest, exec.Execute(context.Background(), memoryEffect(t))
}

// memoryBase is the merge base of the change in these tests. It differs from veHead on purpose.
const memoryBase = id.SHA("1234567")

// A confirmed, current memory reference verifies the E3 finding, looked up with the filing reviewer's scopes. The
// policy behind the re-query comes from the merge base, so a change cannot edit its way to more evidence or less.
func TestVerifyMemoryConfirmsAFoundConfirmedReferenceAtTheMergeBase(t *testing.T) {
	t.Parallel()
	rc := &fakeRecheck{state: verify.MemoryState{Found: true, Confirmed: true}}
	ingest, err := runMemory(t, rc)
	require.NoError(t, err)
	typ, rec := decodeVerifyWritten(t, ingest)
	require.Equal(t, change.EventFindingVerified, typ)
	require.Equal(t, finding.TierE3, rec.Tier)
	require.Equal(t, id.AgentName("bob-reviewer"), rc.agent)
	require.Equal(t, "decision:refund-limit", rc.ref)
	require.Equal(t, memoryBase, rc.sha)
	require.NotEqual(t, veHead, rc.sha)
}

// A reference that is missing, only observed or superseded drops the finding to E4, on the first run and on a re-run.
func TestVerifyMemoryDropsToE4WhenTheReferenceNoLongerHolds(t *testing.T) {
	t.Parallel()
	for name, state := range map[string]verify.MemoryState{
		"not found":  {},
		"observed":   {Found: true},
		"superseded": {Found: true, Confirmed: true, Superseded: true},
	} {
		t.Run(name, func(t *testing.T) {
			ingest, err := runMemory(t, &fakeRecheck{state: state})
			require.NoError(t, err)
			typ, rec := decodeVerifyWritten(t, ingest)
			require.Equal(t, change.EventFindingStateChanged, typ)
			require.Equal(t, finding.TierE4, rec.Tier)
			require.Equal(t, finding.StateUnverified, rec.State)
		})
	}
}

// A lookup that fails, or no memory service at all, writes nothing and fails so the outbox retries.
func TestVerifyMemoryThatCannotBeLookedUpWritesNothing(t *testing.T) {
	t.Parallel()
	ingest, err := runMemory(t, &fakeRecheck{err: context.DeadlineExceeded})
	require.Error(t, err)
	require.Empty(t, ingest.written)

	ingest, err = runMemory(t, nil)
	require.Error(t, err)
	require.Empty(t, ingest.written)
}
