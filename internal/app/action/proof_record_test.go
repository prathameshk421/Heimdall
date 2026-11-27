package action_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// The gate side of the proof result (ACTION_MODE 2.5, 2.7; work package AM14b step 4). The gate records what an
// unprivileged run uploaded, after it checks the run's input. A refused artifact must never leave the proof passing.

// recordProof writes one proof artifact through the gate and returns what it recorded.
func (h *harness) recordProof(t *testing.T, artifact []byte) action.ProofRecord {
	t.Helper()
	rec, err := h.gate.RecordProof(t.Context(), action.ProofRecordIn{ChangeID: h.changeID, Repo: testRepo,
		Number: testNumber, WorkflowRun: proofRunID, Artifact: artifact})
	require.NoError(t, err)
	return rec
}

// proofArtifactFor builds an artifact about the harness's head, base and policy hash. The hash is the sha256 of the
// policy bytes at the merge base, as the gate computes it; the test recomputes it from the same fixture.
func (h *harness) proofArtifact(kind proof.Kind) action.ProofArtifact {
	return action.ProofArtifact{Version: action.ArtifactVersion, Head: testHead, Base: testBase,
		PolicyHash: h.policyHash(), WorkflowRun: proofRunID, Model: "deepseek/deepseek-v4-flash", Kind: kind}
}

// policyHash is the sha256 of the policy the fake code reader serves at the merge base.
func (h *harness) policyHash() string {
	sum := sha256.Sum256(h.code.policy[testBase])
	return hex.EncodeToString(sum[:])
}

// mustJSON encodes one artifact.
func mustJSON(t *testing.T, a action.ProofArtifact) []byte {
	t.Helper()
	raw, err := json.Marshal(a)
	require.NoError(t, err)
	return raw
}

// TestTheGateRecordsAProvenProofAndItsCost feeds the gate a proven, blocking artifact and reads the log back: one
// proof.reported and one cost.recorded, for this head.
func TestTheGateRecordsAProvenProofAndItsCost(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)
	h.run(t) // pr.opened, fast.reported and the summary, so the log holds a pull request.

	a := h.proofArtifact(proof.KindBlocked)
	a.Counts = change.ProofCounts{In: 1, Proven: 1, Blocking: 1}
	a.Candidates = []action.ArtifactCandidate{{Ref: "F1", Class: proof.ClassProven, Severity: "high", Blocking: true}}
	a.Cost = change.Cost{USDMicros: 4200, PromptTokens: 900, CompletionTokens: 120}

	rec := h.recordProof(t, mustJSON(t, a))

	require.True(t, rec.Accepted, rec.Refused)
	require.Equal(t, proof.KindBlocked, rec.Outcome.Kind)
	require.Equal(t, []change.EventType{change.EventProofReported, change.EventCostRecorded}, rec.Appended)
	types := h.log(t)
	require.Contains(t, types, change.EventProofReported)
	require.Contains(t, types, change.EventCostRecorded)
}

// TestTheGateRecordsARefusedArtifactAsUnknown feeds artifacts the gate cannot trust. Each one is recorded as unknown:
// no result, and never a pass. Each case is its own proof run, as a real upload is.
func TestTheGateRecordsARefusedArtifactAsUnknown(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)
	h.run(t)

	stale := h.proofArtifact(proof.KindSkippedTrivial)
	stale.Head = testHead2
	cases := map[string][]byte{
		"another head":   mustJSON(t, stale),
		"malformed json": []byte(`{"version":1,`),
		"missing":        nil,
	}
	run := proofRunID
	for name, raw := range cases {
		run++
		rec, err := h.gate.RecordProof(t.Context(), action.ProofRecordIn{ChangeID: h.changeID, Repo: testRepo,
			Number: testNumber, WorkflowRun: run, Artifact: raw})
		require.NoError(t, err)
		require.False(t, rec.Accepted, name)
		require.Equal(t, proof.KindUnknown, rec.Outcome.Kind, name)
		require.Equal(t, proof.ReasonNoResult, rec.Outcome.Reason, name)
		require.NotEmpty(t, rec.Refused, name)
		require.Equal(t, []change.EventType{change.EventProofReported}, rec.Appended, name+": unknown costs nothing")
	}
}

// TestTheGateRefusesAFalseBlockingFlag makes the artifact say a proven high finding does not block, while the policy at
// the merge base says it does. The gate refuses the artifact rather than trust its flag.
func TestTheGateRefusesAFalseBlockingFlag(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)
	h.run(t)

	a := h.proofArtifact(proof.KindProven)
	a.Counts = change.ProofCounts{In: 1, Proven: 1, NeedsPerson: 1}
	a.Candidates = []action.ArtifactCandidate{{Ref: "F1", Class: proof.ClassProven, Severity: "high", Blocking: false}}

	rec := h.recordProof(t, mustJSON(t, a))

	require.False(t, rec.Accepted, "a false blocking flag is a forged result, not a finding a person decides")
	require.Equal(t, proof.KindUnknown, rec.Outcome.Kind)
	require.Contains(t, rec.Refused, "blocking")
}

// TestARecordedProofIsIdempotent records the same artifact twice: the second call appends nothing, as every gate write
// is safe to repeat (ACTION_MODE 2.5).
func TestARecordedProofIsIdempotent(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)
	h.run(t)

	raw := mustJSON(t, h.proofArtifact(proof.KindSkippedTrivial))
	first := h.recordProof(t, raw)
	require.True(t, first.Accepted, first.Refused)
	require.NotEmpty(t, first.Appended)

	second := h.recordProof(t, raw)

	require.Empty(t, second.Appended, "the log already holds this run's result")
	require.Equal(t, 1, countOf(h.log(t), change.EventProofReported))
}

// TestTheSummaryListsTheProofRunsOwnFindings is the live run of Nov 27 (heimdall-fixture-pay #1): Heimdall's own
// reviewer proved 3 findings that no comment had named. The headline said a finding is proven and the list said
// "Nothing is proven", because a candidate without a log ref was dropped when the gate recorded the run.
func TestTheSummaryListsTheProofRunsOwnFindings(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)
	h.run(t)

	a := h.proofArtifact(proof.KindBlocked)
	a.Counts = change.ProofCounts{In: 1, Proven: 1, Blocking: 1}
	a.Candidates = []action.ArtifactCandidate{{File: "internal/pay/refund.go", First: 56, Last: 58, Severity: "high",
		Claim: "The cap check compares one refund against the payment and never subtracts earlier refunds.",
		Class: proof.ClassProven, Blocking: true, Why: "fails_twice_passes_fixed"}}
	rec := h.recordProof(t, mustJSON(t, a))
	require.True(t, rec.Accepted, rec.Refused)

	h.run(t)
	body := h.summary.body()
	require.NotContains(t, body, "Nothing is proven.")
	require.Contains(t, body, "never subtracts earlier refunds")
	require.Contains(t, body, "internal/pay/refund.go:56-58")
}
