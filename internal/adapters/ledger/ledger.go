// Package ledger stores measured model costs as append-only JSONL.
package ledger

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Ledger reads and appends a JSONL cost ledger.
type Ledger struct{ Path string }

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
	f, err := os.Open(l.Path)
	if os.IsNotExist(err) {
		return 0, byPurpose, nil
	}
	if err != nil {
		return 0, nil, fmt.Errorf("open cost ledger: %w", err)
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
			return 0, nil, fmt.Errorf("scan cost ledger: %w", err)
		}
		var entry ports.LedgerEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return 0, nil, fmt.Errorf("decode cost ledger line %d: %w", line, err)
		}
		total += entry.CostUSD
		byPurpose[entry.Purpose] += entry.CostUSD
	}
	if err := scanner.Err(); err != nil {
		return 0, nil, fmt.Errorf("scan cost ledger: %w", err)
	}
	return total, byPurpose, nil
}
