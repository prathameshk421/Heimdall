package postgres

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// maxListedChanges bounds the changes the dashboard lists. The newest-first order is not needed: the page sorts them.
const maxListedChanges = 500

// ListChanges returns the ids of the changes that have events, for the read-only dashboard (A7).
func (s *Store) ListChanges(ctx context.Context) ([]id.ChangeID, error) {
	rows, err := s.pool.Query(ctx, "SELECT DISTINCT change_id FROM event ORDER BY change_id LIMIT $1", maxListedChanges)
	if err != nil {
		return nil, fmt.Errorf("list changes: %w", err)
	}
	defer rows.Close()
	var out []id.ChangeID
	for rows.Next() {
		var cid string
		if err := rows.Scan(&cid); err != nil {
			return nil, fmt.Errorf("scan change id: %w", err)
		}
		out = append(out, id.ChangeID(cid))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read change ids: %w", err)
	}
	return out, nil
}
