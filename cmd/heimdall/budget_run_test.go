package main

import (
	"bytes"
	"context"
	"io"
	"math"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestBudgetRunRecordsMeasuredSpendWhenCommandFails(t *testing.T) {
	var out, errOut bytes.Buffer
	var recorded ports.LedgerEntry
	balances := []float64{2, 1.8}
	deps := budgetRunDeps{
		getenv:     func(string) string { return "" },
		loadConfig: func(string) (config.Config, error) { return config.Config{}, nil },
		balance: func(config.Config) (float64, error) {
			v := balances[0]
			balances = balances[1:]
			return v, nil
		},
		append: func(_ string, entry ports.LedgerEntry) error { recorded = entry; return nil },
		run: func(_ context.Context, _ []string, stdout, stderr io.Writer) int {
			_, _ = io.WriteString(stdout, "child output\n")
			_, _ = io.WriteString(stderr, "child diagnostic\n")
			return 7
		},
		now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	}
	code := runBudgetRunWithDeps([]string{"--purpose", "live", "--estimate", ".2", "--task", "T19", "--run-id", "run-1", "--model", "model-a", "--config", "test.toml", "--", "tagged-test"}, &out, &errOut, deps)
	if code != 7 {
		t.Fatalf("runBudgetRun() = %d, want child exit 7; stderr=%q", code, errOut.String())
	}
	if recorded.RunID != "run-1" || recorded.Purpose != "live" || recorded.Model != "model-a" || math.Abs(recorded.CostUSD-.2) > 1e-9 || recorded.Note == "" {
		t.Fatalf("recorded entry = %#v", recorded)
	}
	if !bytes.Contains(out.Bytes(), []byte("recorded")) {
		t.Fatalf("stdout = %q, want recording confirmation", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("child output")) || !bytes.Contains(errOut.Bytes(), []byte("child diagnostic")) {
		t.Fatalf("child output not forwarded: stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

func TestBudgetRunRefusesBeforeRunWhenBalanceBelowReservation(t *testing.T) {
	var out, errOut bytes.Buffer
	ran, appended := false, false
	deps := budgetRunDeps{
		getenv:     func(string) string { return "" },
		loadConfig: func(string) (config.Config, error) { return config.Config{}, nil },
		balance:    func(config.Config) (float64, error) { return .3, nil },
		append:     func(string, ports.LedgerEntry) error { appended = true; return nil },
		run:        func(context.Context, []string, io.Writer, io.Writer) int { ran = true; return 0 },
		now:        func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	}
	code := runBudgetRunWithDeps([]string{"--purpose", "live", "--estimate", ".2", "--task", "T19", "--run-id", "run-3", "--model", "model-a", "--config", "test.toml", "--", "tagged-test"}, &out, &errOut, deps)
	if code == 0 || ran || appended {
		t.Fatalf("code=%d ran=%v appended=%v stderr=%q, want refusal with no run and no ledger line", code, ran, appended, errOut.String())
	}
	if !bytes.Contains(errOut.Bytes(), []byte("below 2x estimate")) {
		t.Fatalf("stderr = %q, want a balance-below-reservation message", errOut.String())
	}
}

func TestBudgetRunStopsAfterRecordingTwiceEstimate(t *testing.T) {
	var out, errOut bytes.Buffer
	var recorded ports.LedgerEntry
	balances := []float64{1, .5}
	deps := budgetRunDeps{
		getenv:     func(string) string { return "" },
		loadConfig: func(string) (config.Config, error) { return config.Config{}, nil },
		balance: func(config.Config) (float64, error) {
			v := balances[0]
			balances = balances[1:]
			return v, nil
		},
		append: func(_ string, entry ports.LedgerEntry) error { recorded = entry; return nil },
		run:    func(context.Context, []string, io.Writer, io.Writer) int { return 0 },
		now:    func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	}
	code := runBudgetRunWithDeps([]string{"--purpose", "live", "--estimate", ".2", "--task", "T19", "--run-id", "run-2", "--model", "model-a", "--config", "test.toml", "--", "tagged-test"}, &out, &errOut, deps)
	if code == 0 || recorded.RunID != "run-2" || recorded.CostUSD < .49 {
		t.Fatalf("code=%d recorded=%#v stderr=%q", code, recorded, errOut.String())
	}
}
