package action_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// The gate's read of a proof artifact (ACTION_MODE 2.5, 2.7, work package AM14b step 4). A file the gate cannot trust
// gives unknown: no result, never a pass. These cases are the ones a forged or stale upload would try.

// artifactKey is the run the gate serves.
func artifactKey() action.ArtifactKey {
	return action.ArtifactKey{Head: proofHead, Base: proofBase, PolicyHash: proofHash, WorkflowRun: proofRunID}
}

// goodArtifact is a bounded, versioned, skipped result for artifactKey.
func goodArtifact() action.ProofArtifact {
	return action.ProofArtifact{Version: action.ArtifactVersion, Head: proofHead, Base: proofBase, PolicyHash: proofHash,
		WorkflowRun: proofRunID, Model: "deepseek/deepseek-v4-flash", Kind: proof.KindSkippedTrivial}
}

// mustEncode writes one artifact, failing the test when the format refuses it.
func mustEncode(t *testing.T, a action.ProofArtifact) []byte {
	t.Helper()
	raw, err := json.Marshal(a)
	require.NoError(t, err)
	return raw
}

func TestTheGateRefusesEveryArtifactItCannotTrust(t *testing.T) {
	good := mustEncode(t, goodArtifact())
	otherHead := idSHA("6666666666666666666666666666666666666666")
	otherBase := idSHA("7777777777777777777777777777777777777777")

	oversized := make([]byte, action.MaxArtifactBytes+1)
	copy(oversized, good)

	staleHead := goodArtifact()
	staleHead.Head = otherHead
	staleBase := goodArtifact()
	staleBase.Base = otherBase
	staleHash := goodArtifact()
	staleHash.PolicyHash = strings.Repeat("b", 64)
	staleRun := goodArtifact()
	staleRun.WorkflowRun = proofRunID + 1
	newerVersion := goodArtifact()
	newerVersion.Version = action.ArtifactVersion + 1
	zeroVersion := goodArtifact()
	zeroVersion.Version = 0
	unknownField := goodArtifact()
	forged := goodArtifact()
	forged.Kind = proof.KindProven
	forged.Counts = change.ProofCounts{In: 1, Proven: 1, NeedsPerson: 1}
	forged.Candidates = []action.ArtifactCandidate{{Class: proof.ClassProven, Blocking: true}}
	blockedNoCounts := goodArtifact()
	blockedNoCounts.Kind = proof.KindBlocked
	unknownNoReason := goodArtifact()
	unknownNoReason.Kind = proof.KindUnknown
	skipWithCost := goodArtifact()
	skipWithCost.Cost = change.Cost{USDMicros: 1000}
	badCandidate := goodArtifact()
	badCandidate.Kind = proof.KindProven
	badCandidate.Counts = change.ProofCounts{In: 1, Proven: 1, NeedsPerson: 1}
	badCandidate.Candidates = []action.ArtifactCandidate{{Class: proof.ClassProven, Blocking: true, Dropped: proof.DropNoProof}}

	cases := []struct {
		name string
		raw  []byte
	}{
		{"empty", nil},
		{"oversized", oversized},
		{"malformed json", []byte(`{"version":1,"head":`)},
		{"not an object", []byte(`[1,2,3]`)},
		{"two json values", append(append([]byte{}, good...), []byte(`{"version":1}`)...)},
		{"another head", mustEncode(t, staleHead)},
		{"another base", mustEncode(t, staleBase)},
		{"another policy hash", mustEncode(t, staleHash)},
		{"another workflow run", mustEncode(t, staleRun)},
		{"a newer version", mustEncode(t, newerVersion)},
		{"version zero", mustEncode(t, zeroVersion)},
		{"a field the build does not know", mustEncode(t, unknownField)},
		{"proven with no count that supports it", mustEncode(t, forged)},
		{"blocked with no blocking finding", mustEncode(t, blockedNoCounts)},
		{"unknown with no reason", mustEncode(t, unknownNoReason)},
		{"a skipped run that spent money", mustEncode(t, skipWithCost)},
		{"a candidate whose flags contradict its class", mustEncode(t, badCandidate)},
	}
	for _, c := range cases {
		if c.name == "a field the build does not know" {
			// An unknown field is refused by the strict decoder, which is the point: a newer run's extra key must not
			// be guessed at.
			var m map[string]any
			require.NoError(t, json.Unmarshal(good, &m))
			m["extra"] = true
			c.raw, _ = json.Marshal(m)
		}
		_, err := action.ParseProofArtifact(c.raw, artifactKey())
		require.ErrorIs(t, err, action.ErrProofArtifact, c.name)
		out := action.ArtifactOutcome(c.raw, artifactKey())
		require.Equal(t, proof.KindUnknown, out.Kind, c.name+": a refused artifact is unknown")
		require.Equal(t, proof.ReasonNoResult, out.Reason, c.name)
		require.Equal(t, unknownVerdict(), out.Verdict(), c.name+": unknown never passes")
	}
}

func TestTheGateAcceptsTheArtifactOfItsOwnRun(t *testing.T) {
	raw := mustEncode(t, goodArtifact())

	a, err := action.ParseProofArtifact(raw, artifactKey())
	require.NoError(t, err)
	require.Equal(t, proof.KindSkippedTrivial, a.Kind)
	require.Equal(t, proof.KindSkippedTrivial, action.ArtifactOutcome(raw, artifactKey()).Kind)
	require.Equal(t, gatePass(), action.ArtifactOutcome(raw, artifactKey()).Verdict())
}

// TestAnArtifactCannotClaimProvenWithoutAProvenCandidate plants a result whose kind is proven and whose counts support
// it while no candidate bucket agrees. change.ProofReported refuses it, and so does the gate.
func TestAnArtifactCannotClaimProvenWithoutAProvenCandidate(t *testing.T) {
	a := goodArtifact()
	a.Kind = proof.KindProven
	a.Counts = change.ProofCounts{In: 1, Proven: 1, NeedsPerson: 1}
	a.Candidates = []action.ArtifactCandidate{{Class: proof.ClassDropped, Dropped: proof.DropRefuted}}

	_, err := action.ParseProofArtifact(mustEncode(t, a), artifactKey())
	require.ErrorIs(t, err, action.ErrProofArtifact)
	require.Equal(t, proof.KindUnknown, action.ArtifactOutcome(mustEncode(t, a), artifactKey()).Kind)
}

// TestTheArtifactPayloadsNameTheRun pins the two log records the gate makes of an accepted artifact: one
// proof.reported and one cost.recorded, both for this run.
func TestTheArtifactPayloadsNameTheRun(t *testing.T) {
	a := goodArtifact()
	a.Kind = proof.KindProven
	a.Counts = change.ProofCounts{In: 1, Proven: 1, NeedsPerson: 1}
	a.Candidates = []action.ArtifactCandidate{{Ref: "F1", Class: proof.ClassProven}}
	a.Cost = change.Cost{USDMicros: 12_345, PromptTokens: 100, CompletionTokens: 20}

	p, err := action.ParseProofArtifact(mustEncode(t, a), artifactKey())
	require.NoError(t, err)

	reported := p.Payload()
	require.NoError(t, reported.Validate())
	require.Equal(t, proofRunID, reported.WorkflowRun)
	require.Equal(t, proofHead, reported.Head)
	require.Equal(t, proofBase, reported.Base)
	require.Equal(t, proofHash, reported.PolicyHash)

	cost := p.CostRecorded()
	require.NoError(t, cost.Validate())
	require.Equal(t, proofRunID, cost.WorkflowRun)
	require.Equal(t, a.Cost, cost.Cost)
}

// idSHA is a real 40-hex commit id, for a case that names another run's input.
func idSHA(s string) id.SHA { return id.SHA(s) }

// unknownVerdict and gatePass name the two verdicts these cases assert on.
func unknownVerdict() gate.Verdict { return gate.VerdictUnknown }
func gatePass() gate.Verdict       { return gate.VerdictPass }
