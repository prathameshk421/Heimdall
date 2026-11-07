// Package ledger stores measured model costs as append-only JSONL.
package ledger

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Ledger reads and appends a JSONL cost ledger.
type Ledger struct{ Path string }

// AppendOnce appends entry unless the same run id is already present. A replay of the
// same entry is idempotent; a different attribution for that id is refused.
func (l Ledger) AppendOnce(ctx context.Context, entry ports.LedgerEntry) error {
	entries, err := l.entries(ctx)
	if err != nil {
		return err
	}
	for _, existing := range entries {
		if existing.RunID != entry.RunID {
			continue
		}
		if sameRunMeasurement(existing, entry) {
			return nil
		}
		return fmt.Errorf("cost ledger run %q already has a different entry", entry.RunID)
	}
	return l.Append(ctx, entry)
}

func sameRunMeasurement(a, b ports.LedgerEntry) bool {
	a.At, b.At = time.Time{}, time.Time{}
	return a == b
}

// Append writes one measured run to the ledger.
func (l Ledger) Append(ctx context.Context, entry ports.LedgerEntry) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("append cost ledger: %w", err)
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode cost ledger entry: %w", err)
	}
	f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open cost ledger: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("append cost ledger: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close cost ledger: %w", err)
	}
	return nil
}

// Total returns cumulative spend overall and grouped by purpose.
func (l Ledger) Total(ctx context.Context) (total float64, byPurpose map[string]float64, retErr error) {
	byPurpose = make(map[string]float64)
	entries, err := l.entries(ctx)
	if err != nil {
		return 0, nil, err
	}
	for _, entry := range entries {
		total += entry.CostUSD
		byPurpose[entry.Purpose] += entry.CostUSD
	}
	return total, byPurpose, nil
}

func (l Ledger) entries(ctx context.Context) (entries []ports.LedgerEntry, retErr error) {
	f, err := os.Open(l.Path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open cost ledger: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close cost ledger: %w", err))
		}
	}()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for line := 1; scanner.Scan(); line++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("scan cost ledger: %w", err)
		}
		var entry ports.LedgerEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return nil, fmt.Errorf("decode cost ledger line %d: %w", line, err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan cost ledger: %w", err)
	}
	return entries, nil
}
