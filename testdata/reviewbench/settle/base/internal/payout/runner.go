package payout

import (
	"fmt"

	"settle/internal/bank"
	"settle/internal/merchants"
)

// Runner pays due merchants.
type Runner struct {
	store *merchants.Store
	bank  *bank.Client
}

// NewRunner returns a Runner.
func NewRunner(store *merchants.Store, client *bank.Client) *Runner {
	return &Runner{store: store, bank: client}
}

// Nightly pays every due merchant in bank batches.
func (r *Runner) Nightly(dryRun bool) error {
	var lines []bank.Line
	for _, id := range r.store.Due() {
		line, err := r.lineFor(id)
		if err != nil {
			return fmt.Errorf("plan payout %s: %w", id, err)
		}
		lines = append(lines, line)
	}
	if dryRun {
		return nil
	}
	return r.bank.Send(lines)
}

func (r *Runner) lineFor(id string) (bank.Line, error) {
	m, err := r.store.Lookup(id)
	if err != nil {
		return bank.Line{}, err
	}
	return bank.Line{IBAN: m.IBAN, Amount: m.Balance}, nil
}
