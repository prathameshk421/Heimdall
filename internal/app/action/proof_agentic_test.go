package action_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agentloop"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type cmdFakeSandbox struct {
	*fakeSandbox
	cmds []struct {
		dir      string
		argv     []string
		overlays []ports.File
	}
}

func (c *cmdFakeSandbox) RunCommand(_ context.Context, _ id.SHA, dir string, argv []string, overlays []ports.File, _ time.Duration) (action.CommandResult, error) {
	c.cmds = append(c.cmds, struct {
		dir      string
		argv     []string
		overlays []ports.File
	}{dir: dir, argv: argv, overlays: overlays})
	return action.CommandResult{Exit: 0, Output: "build ok"}, nil
}

// A bug in a newly added file (404 at merge base) is proven when propose_finding supplies fix_replacement for the
// blamed lines: head fails twice, fixed passes once.
func TestProofProvesBugInNewFileWithFixReplacement(t *testing.T) {
	testSrc := "package pay\nimport \"testing\"\nfunc " + testName + "(t *testing.T) { t.Fail() }\n"
	h := newProofHarness(t, []modelStep{
		toolCall("c1", "propose_finding", map[string]any{
			"comment_id":      0,
			"file":            sourcePath,
			"start_line":      3,
			"end_line":        3,
			"severity":        "high",
			"claim":           "Refund cap compares against total payment instead of remaining amount.",
			"test_path":       proofTest,
			"test_name":       testName,
			"test_source":     testSrc,
			"fix_replacement": "\treturn amount > pay.Remaining",
		}, 0.01),
		toolCall("c2", "finish_review", map[string]any{"summary": "found over-refund bug"}, 0.005),
	})
	// Simulate newly added file: base has no entry for sourcePath.
	delete(h.code.base, sourcePath)
	h.sandbox.set(string(proofHead), 0, 1)
	h.sandbox.set(string(proofBase), 1, 0)

	res, err := h.run(t, ports.ModelPrice{InputPerM: 0.09, OutputPerM: 0.18})
	require.NoError(t, err)
	require.Equal(t, proof.KindBlocked, res.Outcome.Kind)
	require.Len(t, res.Candidates, 1)
	require.Equal(t, proof.ClassProven, res.Candidates[0].Class)
	// Fixed overlay should contain the replacement on line 3.
	require.Len(t, h.sandbox.overlays, 3)
	require.Contains(t, string(h.sandbox.overlays[2][0].Content), "return amount > pay.Remaining")
}

// A newly added file without fix_replacement drops the candidate cleanly without marking the whole run input_missing.
func TestProofNewFileWithoutFixReplacementDropsCandidateNotRun(t *testing.T) {
	testSrc := "package pay\nimport \"testing\"\nfunc " + testName + "(t *testing.T) { t.Fail() }\n"
	h := newProofHarness(t, []modelStep{
		toolCall("c1", "propose_finding", map[string]any{
			"comment_id":  0,
			"file":        sourcePath,
			"start_line":  3,
			"end_line":    3,
			"severity":    "high",
			"claim":       "Refund cap compares against total payment instead of remaining amount.",
			"test_path":   proofTest,
			"test_name":   testName,
			"test_source": testSrc,
		}, 0.01),
		toolCall("c2", "finish_review", map[string]any{"summary": "done"}, 0.005),
	})
	delete(h.code.base, sourcePath)

	res, err := h.run(t, ports.ModelPrice{InputPerM: 0.09, OutputPerM: 0.18})
	require.NoError(t, err)
	require.NotEqual(t, proof.ReasonInputMissing, res.Outcome.Reason)
	require.Len(t, res.Candidates, 1)
	require.Equal(t, proof.ClassDropped, res.Candidates[0].Class)
	require.Equal(t, proof.DropNoProof, res.Candidates[0].Dropped)
}

// list_files and run_command are available to the reviewer when the sandbox implements CommandSandbox, and polyglot
// test files (e.g., pytest) can be proposed and proven via framework-qualified test names.
func TestProofAgenticToolsAndPolyglotTest(t *testing.T) {
	h := newProofHarness(t, []modelStep{
		toolCall("c1", "list_files", map[string]any{}, 0.002),
		toolCall("c2", "run_command", map[string]any{"argv": []string{"pytest", "--version"}, "dir": "."}, 0.002),
		toolCall("c3", "propose_finding", map[string]any{
			"comment_id":      0,
			"file":            "app/calc.py",
			"start_line":      1,
			"end_line":        2,
			"severity":        "high",
			"claim":           "add() subtracts instead of adding when b is positive.",
			"test_path":       "tests/test_calc_repro.py",
			"test_name":       "test_add_positive",
			"test_source":     "def test_add_positive():\n    assert 1 + 2 == 3\n",
			"fix_replacement": "def add(a, b):\n    return a + b",
		}, 0.01),
		toolCall("c4", "finish_review", map[string]any{"summary": "proven"}, 0.002),
	})
	require.NoError(t, os.MkdirAll(filepath.Join(h.dir, "app"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(h.dir, "app", "calc.py"), []byte("def add(a, b):\n    return a - b\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(h.dir, "node_modules", "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(h.dir, "node_modules", "pkg", "ignored.js"), []byte("x"), 0o644))

	cs := &cmdFakeSandbox{fakeSandbox: h.sandbox}
	// Framework-qualified pytest name (tests/test_calc_repro.py::test_add_positive) matches test_name.
	cs.outcomes[string(proofHead)+"|"+testName] = ports.SandboxResult{
		Failed: 1, Tests: []ports.TestResult{{Name: "tests/test_calc_repro.py::test_add_positive", Outcome: ports.OutcomeFailure}},
	}
	cs.outcomes[string(proofBase)+"|"+testName] = ports.SandboxResult{
		Passed: 1, Tests: []ports.TestResult{{Name: "tests/test_calc_repro.py::test_add_positive", Outcome: ports.OutcomePassed}},
	}

	loop, err := agentloop.NewRunner(h.model, h.clock, ports.ModelPrice{InputPerM: 0.09, OutputPerM: 0.18})
	require.NoError(t, err)
	p, err := action.NewProof(action.ProofConfig{Loop: loop, Sandbox: cs, Code: h.code, Clock: h.clock, ModelName: "test-model"})
	require.NoError(t, err)
	res, err := p.Run(context.Background(), h.in)
	require.NoError(t, err)
	require.Len(t, cs.cmds, 1)
	require.Equal(t, []string{"pytest", "--version"}, cs.cmds[0].argv)
	require.Equal(t, proof.KindBlocked, res.Outcome.Kind)
	require.Len(t, res.Candidates, 1)
	require.Equal(t, proof.ClassProven, res.Candidates[0].Class)
}
