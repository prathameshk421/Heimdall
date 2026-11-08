package statements

import (
	"errors"
	"fmt"

	"settle/internal/merchants"
)

// Header writes the first line of a merchant's monthly statement.
func Header(store *merchants.Store, id string) (string, error) {
	m, err := store.Lookup(id)
	if errors.Is(err, merchants.ErrNotFound) {
		return "", fmt.Errorf("statement for closed merchant %s: %w", id, err)
	}
	if err != nil {
		return "", err
	}
	if m.Frozen {
		return "ACCOUNT FROZEN", nil
	}
	return fmt.Sprintf("Statement for %s, balance %d", m.ID, m.Balance), nil
}
