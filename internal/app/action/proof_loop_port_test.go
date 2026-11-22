package action_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The proof use case through a fake ports.AgentLoop, with no model and no real loop: what the use case asks of the loop,
// and how it reads every way the loop can end. The loop's own rules (caps, turns, refusal) are tested in
// internal/app/agentloop.

// fakeLoop records the request and answers with a fixed result. Before it answers it runs the calls through the
// request's dispatcher, as a real loop would.
type fakeLoop struct {
	req    ports.AgentRun
	calls  []ports.ModelToolCall
	result ports.AgentResult
	err    error
	got    []string
}

var _ ports.AgentLoop = (*fakeLoop)(nil)

func (f *fakeLoop) Run(ctx context.Context, req ports.AgentRun) (ports.AgentResult, error) {
	f.req = req
	for _, c := range f.calls {
		out, err := req.Dispatcher.Dispatch(ctx, c.Name, json.RawMessage(c.Arguments))
		if err != nil {
			out = "error: " + err.Error()
		}
		f.got = append(f.got, out)
	}
	return f.result, f.err
}

func (h *proofHarness) runWith(t *testing.T, loop ports.AgentLoop) (action.ProofResult, error) {
	t.Helper()
	p, err := action.NewProof(h.config(loop, h.sandbox))
	require.NoError(t, err)
	return p.Run(t.Context(), h.in)
}

func TestTheProofRunAsksTheLoopWithThePolicyCapsAndItsOwnTools(t *testing.T) {
	h := newProofHarness(t, nil)
	loop := &fakeLoop{result: ports.AgentResult{Stop: ports.AgentStopDone}}

	_, err := h.runWith(t, loop)
	require.NoError(t, err)

	act := h.in.Action.Effective()
	req := loop.req
	require.Equal(t, "deepseek/deepseek-v4-flash", req.Model)
	require.NotEmpty(t, req.System)
	require.Contains(t, req.User, "acme/shop")
	require.InDelta(t, act.Budget.PerPRUSD, req.Budget.USD, 1e-12)
	require.Equal(t, int64(act.Budget.PerPRTokens.Input), req.Budget.InputTokens)
	require.Equal(t, int64(act.Budget.PerPRTokens.Output), req.Budget.OutputTokens)
	require.Equal(t, act.Budget.CallOutputTokens, req.MaxOutputTokens)
	require.Positive(t, req.MaxTurns)
	require.True(t, req.Deadline.After(h.clock.now), "the deadline is the clock plus the proof timeout")
	require.NotNil(t, req.Dispatcher)

	var names []string
	for _, spec := range req.Tools {
		names = append(names, spec.Name)
	}
	slices.Sort(names)
	require.Equal(t, []string{"finish_review", "list_changes", "propose_finding", "read_file", "run_test", "search"}, names)
	require.Equal(t, []string{"finish_review"}, req.FinalTools)
}

// TestTheProofRunSetsTheContextBudgetAndTheStallRule holds the product run to the limits the bench proved (AM14e): a
// 48 KB request target with the 3 newest results whole, and a stall rule of 10 turns, one nudge and 4 grace turns.
// The nudge names the product's own tools, and the progress tools are tools of the run.
func TestTheProofRunSetsTheContextBudgetAndTheStallRule(t *testing.T) {
	h := newProofHarness(t, nil)
	loop := &fakeLoop{result: ports.AgentResult{Stop: ports.AgentStopDone}}
	_, err := h.runWith(t, loop)
	require.NoError(t, err)

	req := loop.req
	require.Equal(t, 48<<10, req.ContextBytes)
	require.Equal(t, 3, req.KeepResults)
	require.Equal(t, 10, req.Stall.Turns)
	require.Equal(t, 4, req.Stall.Grace)
	require.ElementsMatch(t, []string{"run_test", "propose_finding"}, req.Stall.Progress)
	require.Contains(t, req.Stall.Nudge, "propose_finding")
	require.Contains(t, req.Stall.Nudge, "finish_review")
	var names []string
	for _, spec := range req.Tools {
		names = append(names, spec.Name)
	}
	for _, p := range req.Stall.Progress {
		require.Contains(t, names, p, "a progress tool the model cannot call would stall every run")
	}
	require.Less(t, req.Stall.Turns+req.Stall.Grace, req.MaxTurns, "the stall rule must end a reading run before the turn cap")
}

func TestTheDispatcherRunsTheReviewerToolsAndRefusesOtherNames(t *testing.T) {
	h := newProofHarness(t, nil)
	loop := &fakeLoop{
		calls: []ports.ModelToolCall{
			{Name: "read_file", Arguments: `{"path":"` + sourcePath + `"}`},
			{Name: "read_file", Arguments: `{"path":"../../etc/passwd"}`},
			{Name: "shell", Arguments: `{}`},
		},
		result: ports.AgentResult{Stop: ports.AgentStopDone},
	}
	_, err := h.runWith(t, loop)
	require.NoError(t, err)

	require.Len(t, loop.got, 3)
	require.Contains(t, loop.got[0], "func RefundTotal")
	require.Contains(t, loop.got[1], "error:", "a path outside the checkout is refused")
	require.Contains(t, loop.got[2], "error:", "a tool the run does not have is refused")
}

// TestEveryWayTheLoopEndsHasAnOutcome maps each stop of the port to what the gate reads. A run that finished, or that
// the stall rule ended with no proposal, is not reproduced and never a pass; every other stop is unknown for its own
// reason. The turn cap is not a model failure (AM16 live run).
func TestEveryWayTheLoopEndsHasAnOutcome(t *testing.T) {
	cases := []struct {
		stop   ports.AgentStop
		err    error
		kind   proof.Kind
		reason proof.Reason
		note   proof.Reason
	}{
		{stop: ports.AgentStopDone, kind: proof.KindNotReproduced},
		{stop: ports.AgentStopBudget, kind: proof.KindUnknown, reason: proof.ReasonBudget},
		{stop: ports.AgentStopBudget, err: ports.ErrAgentUsage, kind: proof.KindUnknown, reason: proof.ReasonBudget},
		{stop: ports.AgentStopDeadline, kind: proof.KindUnknown, reason: proof.ReasonDeadline},
		{stop: ports.AgentStopCancelled, err: context.Canceled, kind: proof.KindUnknown, reason: proof.ReasonSuperseded},
		{stop: ports.AgentStopTurns, kind: proof.KindUnknown, reason: proof.ReasonTurnCap},
		{stop: ports.AgentStopNoProposal, kind: proof.KindNotReproduced, note: proof.ReasonNoProposal},
		{stop: ports.AgentStopRefused, kind: proof.KindUnknown, reason: proof.ReasonModelFailure},
		{stop: ports.AgentStopError, err: ports.ErrUnavailable, kind: proof.KindUnknown, reason: proof.ReasonModelFailure},
	}
	for _, c := range cases {
		h := newProofHarness(t, nil)
		res, err := h.runWith(t, &fakeLoop{result: ports.AgentResult{Stop: c.stop}, err: c.err})
		require.NoError(t, err, c.stop)
		require.Equal(t, c.kind, res.Outcome.Kind, c.stop)
		require.Equal(t, c.reason, res.Outcome.Reason, c.stop)
		require.Equal(t, c.note, res.Outcome.Note, c.stop)
		a, err := action.ParseProofArtifact(res.Artifact, action.ArtifactKey{Head: h.in.Head, Base: h.in.Base,
			PolicyHash: h.in.PolicyHash, WorkflowRun: h.in.WorkflowRun})
		require.NoError(t, err, c.stop)
		require.Equal(t, res.Outcome.Kind, a.Kind, c.stop)
		require.Equal(t, c.reason, a.Reason, c.stop)
		require.Equal(t, c.note, a.Note, c.stop)
		require.NotEqual(t, proof.KindProven, res.Outcome.Kind, c.stop)
		require.Equal(t, c.stop, res.Stop)
		if c.err != nil {
			require.Contains(t, res.StopDetail, c.err.Error())
		}
	}
}

func TestTheSpendIsWhatTheLoopReported(t *testing.T) {
	h := newProofHarness(t, nil)
	res, err := h.runWith(t, &fakeLoop{result: ports.AgentResult{Stop: ports.AgentStopDone, CostUSD: 0.0421,
		Usage: ports.ModelUsage{InputTokens: 9000, CachedInputTokens: 4000, OutputTokens: 700},
		Turns: []ports.AgentTurn{{}, {}, {}}}})
	require.NoError(t, err)

	require.InDelta(t, 0.0421, res.Spend.USD, 1e-12)
	require.Equal(t, 9000, res.Spend.InputTokens)
	require.Equal(t, 700, res.Spend.OutputTokens)
	require.Equal(t, 3, res.Turns)
}

// TestARequestTheLoopRefusesIsAnErrorNotARun is a miswired loop: it returned no stop, so nothing ran, and a missing run
// must not read as a clean one.
func TestARequestTheLoopRefusesIsAnErrorNotARun(t *testing.T) {
	h := newProofHarness(t, nil)
	_, err := h.runWith(t, &fakeLoop{err: errors.New("agent loop final tool is not one of the tools")})
	require.ErrorContains(t, err, "agent loop")
}

// TestAClaimNotReachedBeforeTheTurnCapIsNotChecked: the turn cap is a limit of the run, so a claim the model never
// reached is "not checked: budget", not a drop. A stall stop gave every claim its chance, so there it is a drop.
func TestAClaimNotReachedBeforeTheTurnCapIsNotChecked(t *testing.T) {
	cases := []struct {
		stop    ports.AgentStop
		class   proof.Class
		reason  proof.Reason
		dropped proof.DropReason
	}{
		{stop: ports.AgentStopTurns, class: proof.ClassNotChecked, reason: proof.ReasonBudget},
		{stop: ports.AgentStopNoProposal, class: proof.ClassDropped, dropped: proof.DropNoProof},
	}
	for _, c := range cases {
		h := newProofHarness(t, nil)
		h.in.Claims = []action.Claim{{Source: external.ToolCodeRabbit, CommentID: 90125,
			Location: finding.Location{File: sourcePath, Lines: [2]int{4, 4}},
			Claim:    "the refund adds the fee a second time", Severity: finding.SeverityHigh}}
		res, err := h.runWith(t, &fakeLoop{result: ports.AgentResult{Stop: c.stop}})
		require.NoError(t, err, c.stop)
		require.Len(t, res.Candidates, 1, c.stop)
		require.Equal(t, c.class, res.Candidates[0].Class, c.stop)
		require.Equal(t, c.reason, res.Candidates[0].Reason, c.stop)
		require.Equal(t, c.dropped, res.Candidates[0].Dropped, c.stop)
	}
}
