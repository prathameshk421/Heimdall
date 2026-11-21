package action_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/actionflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The proof records a gate run makes itself (AM13 rows proof and sweep: SkipProof and ReportProofUnknown). The gate
// writes them through the same recorder as RecordProof, so the log holds the same events either way. A real proof run's
// result is never made here: RecordProof reads its artifact.

const gateRunID = int64(7002)

// runWithID is one gate run that names its GitHub Actions run id.
func (h *harness) runWithID(t *testing.T, runID int64) (action.Result, error) {
	t.Helper()
	return h.gate.Run(t.Context(), action.RunIn{ChangeID: h.changeID, Repo: testRepo, Number: testNumber, WorkflowRun: runID})
}

// proofOnLog is the proof result the log holds after a fold.
func (h *harness) proofOnLog(t *testing.T) *change.ProofInfo {
	t.Helper()
	events, err := h.store.Load(t.Context(), h.changeID)
	require.NoError(t, err)
	view, err := change.Fold(events)
	require.NoError(t, err)
	if view.Action == nil {
		return nil
	}
	return view.Action.Proof
}

// laterGate is the harness's gate with a clock moved forward, over the same fakes and the same log.
func (h *harness) laterGate(t *testing.T, after time.Duration) *action.Gate {
	t.Helper()
	g, err := action.NewGate(action.Config{Store: h.store, PRs: h.github, Reviews: h.github, Checks: h.checks,
		Summary: h.summary, Facts: h.facts, Code: h.code, Decode: policyyaml.Decode, Clock: clock{now: h.clock.now.Add(after)},
		IDs: h.ids, IsUnverified: func(err error) bool { return errors.As(err, &errBadMAC{}) }})
	require.NoError(t, err)
	return g
}

// TestATrivialChangeRecordsItsOwnSkipAtZeroDollars changes one markdown file. No proof run starts for it, so the gate
// records the skip itself: one proof.reported of the kind skipped_trivial, no cost.recorded, and then the proof check.
func TestATrivialChangeRecordsItsOwnSkipAtZeroDollars(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)
	h.github.files = []ports.ChangedFile{{Path: "docs/refunds.md", Status: ports.FileAdded, Additions: 30}}

	res, err := h.runWithID(t, gateRunID)
	require.NoError(t, err)

	require.Contains(t, res.Appended, change.EventProofReported)
	require.NotContains(t, res.Appended, change.EventCostRecorded, "a skip costs nothing")
	p := h.proofOnLog(t)
	require.NotNil(t, p)
	require.True(t, p.Done)
	require.Equal(t, proof.KindSkippedTrivial, p.Kind)
	require.Equal(t, gateRunID, p.WorkflowRun)
	require.Equal(t, testHead, p.Head)
	require.Equal(t, testBase, p.Base)
	require.Equal(t, h.policyHash(), p.PolicyHash)
	require.Equal(t, change.Cost{}, p.Cost)
	require.Equal(t, 2, h.checks.count(), "the proof check is posted once the log holds a proof result")
}

// TestTheSkipIsRecordedOnce runs the gate twice on the same head: the second run appends nothing and posts no new check.
func TestTheSkipIsRecordedOnce(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)
	h.github.files = []ports.ChangedFile{{Path: "docs/refunds.md", Status: ports.FileAdded, Additions: 30}}
	_, err := h.runWithID(t, gateRunID)
	require.NoError(t, err)
	logged, posted := len(h.log(t)), h.checks.count()

	second, err := h.runWithID(t, gateRunID+1)
	require.NoError(t, err)

	require.Empty(t, second.Appended)
	require.Len(t, h.log(t), logged)
	require.Equal(t, posted, h.checks.count())
	require.Equal(t, gateRunID, h.proofOnLog(t).WorkflowRun, "the first run's record stands")
}

// TestARecordNeedsAWorkflowRunId refuses to record a result with no run id, which proof.reported requires, and says so.
// A silent wait would hide a wiring fault.
func TestARecordNeedsAWorkflowRunId(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)
	h.github.files = []ports.ChangedFile{{Path: "docs/refunds.md", Status: ports.FileAdded, Additions: 30}}

	_, err := h.runWithID(t, 0)

	require.ErrorContains(t, err, "workflow run id")
	require.Nil(t, h.proofOnLog(t), "nothing about proof was written")
}

// TestAProofThatNeverReportedIsClosedAsUnknownAfterTheSweep leaves a head with no proof result past the sweep window.
// The gate records unknown: no result, and the proof check then says so. A lost run can never leave a pass.
func TestAProofThatNeverReportedIsClosedAsUnknownAfterTheSweep(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)
	_, err := h.runWithID(t, gateRunID)
	require.NoError(t, err)
	require.Nil(t, h.proofOnLog(t), "within the window the row only waits")

	later := h.laterGate(t, actionflow.SweepAfter+time.Minute)
	res, err := later.Run(t.Context(), action.RunIn{ChangeID: h.changeID, Repo: testRepo, Number: testNumber, WorkflowRun: gateRunID + 5})
	require.NoError(t, err)

	require.Contains(t, res.Appended, change.EventProofReported)
	p := h.proofOnLog(t)
	require.NotNil(t, p)
	require.True(t, p.Done)
	require.Equal(t, proof.KindUnknown, p.Kind)
	require.Equal(t, proof.ReasonNoResult, p.Reason)
	require.Equal(t, gateRunID+5, p.WorkflowRun, "with no run to close, the gate names its own")
	require.Equal(t, 2, h.checks.count())
}

// TestAProofRunThatIsOnTheLogIsClosedByItsOwnId closes a run that started and never finished: the unknown result names
// that run, not the gate's.
func TestAProofRunThatIsOnTheLogIsClosedByItsOwnId(t *testing.T) {
	h := newHarness(t, policy.ActionModeEnforce)
	_, err := h.runWithID(t, gateRunID)
	require.NoError(t, err)
	started := change.ProofStarted{WorkflowRun: proofRunID, Head: testHead, Base: testBase, PolicyHash: h.policyHash(),
		Model: "deepseek/deepseek-v4-flash"}
	ev, err := h.gateEvent(change.EventProofStarted, started)
	require.NoError(t, err)
	h.store.seed(t, h.changeID, ev)

	later := h.laterGate(t, actionflow.SweepAfter+time.Minute)
	_, err = later.Run(t.Context(), action.RunIn{ChangeID: h.changeID, Repo: testRepo, Number: testNumber, WorkflowRun: gateRunID + 5})
	require.NoError(t, err)

	p := h.proofOnLog(t)
	require.NotNil(t, p)
	require.Equal(t, proof.KindUnknown, p.Kind)
	require.Equal(t, proofRunID, p.WorkflowRun)
	require.Equal(t, "deepseek/deepseek-v4-flash", p.Model, "the model id of the started run is kept for the summary")
}
