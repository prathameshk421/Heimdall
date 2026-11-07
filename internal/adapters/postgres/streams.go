package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.PRStreams = (*Store)(nil)

// openPRSQL selects the latest pr.opened of each change, and only when no pr.closed follows it (D56). A change with no
// pr.opened is not selected. The version check keeps a change whose pull request closed from being listed again.
const openPRSQL = `
SELECT o.change_id, o.payload
FROM event o
WHERE o.type = 'pr.opened'
  AND NOT EXISTS (SELECT 1 FROM event n
                  WHERE n.change_id = o.change_id AND n.type = 'pr.opened' AND n.version > o.version)
  AND NOT EXISTS (SELECT 1 FROM event c
                  WHERE c.change_id = o.change_id AND c.type = 'pr.closed' AND c.version > o.version)
ORDER BY o.change_id`

// OpenPRStreams implements ports.PRStreams. It reads the event log only.
func (s *Store) OpenPRStreams(ctx context.Context) ([]ports.OpenPRStream, error) {
	rows, err := s.pool.Query(ctx, openPRSQL)
	if err != nil {
		return nil, fmt.Errorf("list open pull request streams: %w", err)
	}
	defer rows.Close()
	var out []ports.OpenPRStream
	for rows.Next() {
		var cid string
		var payload []byte
		if err := rows.Scan(&cid, &payload); err != nil {
			return nil, fmt.Errorf("list open pull request streams: %w", err)
		}
		var pl change.PROpened
		if err := json.Unmarshal(payload, &pl); err != nil {
			return nil, fmt.Errorf("pr.opened of %s: %w", cid, err)
		}
		out = append(out, ports.OpenPRStream{Change: id.ChangeID(cid), Repo: pl.Repo, Number: pl.Number})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list open pull request streams: %w", err)
	}
	return out, nil
}
