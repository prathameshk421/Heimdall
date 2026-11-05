package ledger

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestAppendTotalAndCorruptLine(t *testing.T) {
	path := t.TempDir() + "/cost.jsonl"
	l := Ledger{Path: path}
	entry := ports.LedgerEntry{RunID: "r1", Purpose: "eval", Model: "m", CostUSD: .2, At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	if err := l.Append(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	total, byPurpose, err := l.Total(context.Background())
	if err != nil || total != .2 || byPurpose["eval"] != .2 {
		t.Fatalf("Total() = %v %v %v", total, byPurpose, err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("{bad}\n")
	_ = f.Close()
	_, _, err = l.Total(context.Background())
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("corrupt line error = %v", err)
	}
}

func TestAppendOnceDoesNotDuplicateOrMisattributeRun(t *testing.T) {
	path := t.TempDir() + "/cost.jsonl"
	l := Ledger{Path: path}
	entry := ports.LedgerEntry{RunID: "run-1", Purpose: "live", Model: "model-a", CostUSD: .2, At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	if err := l.AppendOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	replayed := entry
	replayed.At = replayed.At.Add(time.Hour)
	if err := l.AppendOnce(context.Background(), replayed); err != nil {
		t.Fatalf("replaying the same run: %v", err)
	}
	if err := l.AppendOnce(context.Background(), ports.LedgerEntry{RunID: entry.RunID, Purpose: "eval", Model: entry.Model, CostUSD: entry.CostUSD, At: entry.At}); err == nil {
		t.Fatal("AppendOnce accepted a different attribution for an existing run")
	}
	total, byPurpose, err := l.Total(context.Background())
	if err != nil || total != .2 || byPurpose["live"] != .2 || byPurpose["eval"] != 0 {
		t.Fatalf("Total() after replay = %v %v %v", total, byPurpose, err)
	}
}
