package postgres

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// VerifyAll checks the hash chain of every change (TECH_SPEC 4.2). It reads the whole event table in one ordered
// query and checks each change as its last event arrives, so only one change is held in memory at a time. A broken
// change is reported in Broken and does not stop the others. The error return is for the database only.
//
// VerifyAll does not publish heads: the nightly job does that, from the Verified list (TECH_SPEC 4.2). A deleted
// tail verifies as a shorter chain, so comparing heads with the published ones is the job's check, not this one.
func (s *Store) VerifyAll(ctx context.Context) (VerifyReport, error) {
	var report VerifyReport
	rows, err := s.pool.Query(ctx, "SELECT "+eventColumns+" FROM event ORDER BY change_id, version")
	if err != nil {
		return report, fmt.Errorf("verify: %w", err)
	}
	defer rows.Close()

	var current []change.Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return report, fmt.Errorf("verify: %w", err)
		}
		if len(current) > 0 && e.ChangeID != current[0].ChangeID {
			report.add(current)
			current = nil
		}
		current = append(current, e)
	}
	if err := rows.Err(); err != nil {
		return report, fmt.Errorf("verify: %w", err)
	}
	if len(current) > 0 {
		report.add(current)
	}
	return report, nil
}

// add records one change, whose events are in version order, as verified or broken.
func (r *VerifyReport) add(events []change.Event) {
	cid := events[0].ChangeID
	if err := change.VerifyChain(events); err != nil {
		r.Broken = append(r.Broken, BrokenChange{ChangeID: cid, Err: err})
		return
	}
	last := events[len(events)-1]
	r.Verified = append(r.Verified, ChangeHead{ChangeID: cid, LastVersion: last.Version, HeadHash: last.Hash.String()})
}
