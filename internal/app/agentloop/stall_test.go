package agentloop

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const stallNudge = "Propose your strongest finding with a test now, or call finish_review"

func stallHarness(t *testing.T, steps ...modelStep) (*harness, *fakeTool) {
	h := newHarness(t, steps...)
	h.cfg.MaxTurns = 40
	h.cfg.StallTurns, h.cfg.StallGrace, h.cfg.StallNudge = 3, 2, stallNudge
	h.cfg.ProgressTools = []string{"run_test"}
	runTest := &fakeTool{name: "run_test", run: func(json.RawMessage) (string, error) { return "passed 1", nil }}
	return h, runTest
}

func runWith(t *testing.T, h *harness, extra ...Tool) (Result, error) {
	t.Helper()
	l, err := New(h.model, h.clock, h.cfg, append([]Tool{h.read}, extra...), []Submitter{h.sub})
	require.NoError(t, err)
	return l.Run(context.Background(), []ports.ModelMessage{{Role: ports.ModelRoleUser, Content: "go"}})
}

func userTexts(res Result) []string {
	var out []string
	for _, m := range res.Transcript[1:] {
		if m.Role == ports.ModelRoleUser {
			out = append(out, m.Content)
		}
	}
	return out
}

func TestStallNudgesOnceThenStopsWithNoProposal(t *testing.T) {
	// Three turns with no progress call: one nudge. Two more: the run stops before a sixth call.
	h, runTest := stallHarness(t, reads(5)...)
	res, err := runWith(t, h, runTest)
	require.NoError(t, err)
	require.Equal(t, StopNoProgress, res.Reason)
	require.Equal(t, 5, res.Turns, "no model call after the grace turns")
	require.Equal(t, []string{stallNudge}, userTexts(res), "the nudge is sent once")
	require.True(t, res.Turn[2].StallNudged, "the nudge follows the third answer")
	require.Equal(t, ports.AgentStopNoProposal, portStop(res.Reason))
}

func TestAProgressCallResetsTheStall(t *testing.T) {
	steps := append(reads(2), answer(0.001, call("t1", "run_test", `{}`)))
	steps = append(steps, reads(2)...)
	steps = append(steps, answer(0.001, call("t2", "run_test", `{}`)))
	steps = append(steps, reads(2)...)
	steps = append(steps, answer(0.001, call("s", "submit_plan", `{}`)))
	h, runTest := stallHarness(t, steps...)
	res, err := runWith(t, h, runTest)
	require.NoError(t, err)
	require.Equal(t, StopSubmitted, res.Reason)
	require.Empty(t, userTexts(res), "no stretch reached three turns with no progress")
}

func TestStallAfterTheNudgeWasUsedStopsWithNoSecondNudge(t *testing.T) {
	// Three reads (nudge), a test run, then five reads: the second stall gets no nudge and stops at its grace end.
	steps := append(reads(3), answer(0.001, call("t1", "run_test", `{}`)))
	steps = append(steps, reads(5)...)
	h, runTest := stallHarness(t, steps...)
	res, err := runWith(t, h, runTest)
	require.NoError(t, err)
	require.Equal(t, StopNoProgress, res.Reason)
	require.Equal(t, 9, res.Turns)
	require.Equal(t, []string{stallNudge}, userTexts(res))
}

func TestNoStallRuleWhenItIsOff(t *testing.T) {
	h := newHarness(t, append(reads(8), answer(0.001, call("s", "submit_plan", `{}`)))...)
	h.cfg.MaxTurns = 40
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	require.Equal(t, StopSubmitted, res.Reason)
}

func TestStallConfigNeedsItsParts(t *testing.T) {
	for name, edit := range map[string]func(*Config){
		"no grace":          func(c *Config) { c.StallGrace = 0 },
		"no nudge":          func(c *Config) { c.StallNudge = "" },
		"no progress tools": func(c *Config) { c.ProgressTools = nil },
		"negative turns":    func(c *Config) { c.StallTurns = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			h, runTest := stallHarness(t)
			edit(&h.cfg)
			_, err := New(h.model, h.clock, h.cfg, []Tool{h.read, runTest}, []Submitter{h.sub})
			require.Error(t, err)
		})
	}
}
