package postgres

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.OpenChanges = (*Store)(nil)

// openChangesSQL selects every change with no event that ends it. It is change.View.Over in SQL: a pr.closed that no
// later pr.reopened of the same unmerged pull request undoes, a change.rejected, or a decision.recorded of kind reject.
const openChangesSQL = `
SELECT DISTINCT e.change_id
FROM event e
WHERE NOT EXISTS (
  SELECT 1 FROM event x
  WHERE x.change_id = e.change_id
    AND (x.type = 'change.rejected'
         OR (x.type = 'decision.recorded' AND x.payload->>'kind' = 'reject')
         OR (x.type = 'pr.closed' AND NOT EXISTS (` + reopenedAfterSQL + `))))
ORDER BY e.change_id`

// reopenedAfterSQL finds a pr.reopened of the pull request that the pr.closed x closed without a merge, after x.
const reopenedAfterSQL = `
  SELECT 1 FROM event r
  WHERE r.change_id = x.change_id AND r.type = 'pr.reopened' AND r.version > x.version
    AND COALESCE((x.payload->>'merged')::boolean, false) = false
    AND r.payload->>'repo' = x.payload->>'repo' AND r.payload->'number' = x.payload->'number'`

// OpenChanges implements ports.OpenChanges.
func (s *Store) OpenChanges(ctx context.Context) ([]id.ChangeID, error) {
	rows, err := s.pool.Query(ctx, openChangesSQL)
	if err != nil {
		return nil, fmt.Errorf("list open changes: %w", err)
	}
	defer rows.Close()
	var out []id.ChangeID
	for rows.Next() {
		var cid string
		if err := rows.Scan(&cid); err != nil {
			return nil, fmt.Errorf("list open changes: %w", err)
		}
		out = append(out, id.ChangeID(cid))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list open changes: %w", err)
	}
	return out, nil
}
