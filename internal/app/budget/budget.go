// Package budget checks proposed LLM runs against the committed ledger, purpose limits and provider balance.
package budget

import (
	"context"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// LedgerEntry is the shared representation persisted by the ledger adapter.
type LedgerEntry = ports.LedgerEntry

// Ledger is the persisted cost history consumed by the guard.
type Ledger interface {
	Total(context.Context) (float64, map[string]float64, error)
}

// Balance reads the provider's currently available credit.
type Balance interface {
	Balance(context.Context) (float64, error)
}

// Guard refuses a run that would breach any configured budget.
type Guard struct {
	CapUSD     float64
	PerPurpose map[string]float64
	Ledger     Ledger
	Balance    Balance
}

// Check reserves twice the estimate to enforce the D161 stop rule before a run starts.
func (g Guard) Check(ctx context.Context, purpose string, estimate float64) error {
	if estimate < 0 {
		return fmt.Errorf("estimate $%.4f must not be negative", estimate)
	}
	if g.Ledger == nil || g.Balance == nil {
		return errors.New("budget guard requires ledger and live balance")
	}
	total, purposes, err := g.Ledger.Total(ctx)
	if err != nil {
		return fmt.Errorf("read cost ledger: %w", err)
	}
	reserve := 2 * estimate
	if total+reserve >= g.CapUSD {
		return fmt.Errorf("run estimate $%.4f at 2x would reach $%.4f, over $%.2f testing cap", estimate, total+reserve, g.CapUSD)
	}
	if limit, ok := g.PerPurpose[purpose]; ok && purposes[purpose]+reserve >= limit {
		return fmt.Errorf("purpose %q estimate $%.4f at 2x would reach $%.4f, over $%.2f purpose budget", purpose, estimate, purposes[purpose]+reserve, limit)
	}
	available, err := g.Balance.Balance(ctx)
	if err != nil {
		return fmt.Errorf("read live provider balance: %w", err)
	}
	if available < reserve {
		return fmt.Errorf("live balance $%.4f is below 2x estimate $%.4f", available, reserve)
	}
	return nil
}
