package ports

import (
	"context"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// Clock is the only source of the current time for application code (CODING_STANDARDS 2 and 4). Tests use a fake
// clock; the domain never reads time itself. Adapter: the system clock in internal/platform (task T17).
type Clock interface {
	Now() time.Time
}

// Waiter pauses for d or until ctx is done, and returns ctx.Err() in the second case. It is the only way application
// code waits (R7: no time.Sleep). Adapter: clock.Wait in internal/platform/clock. Tests pass a fake that moves a fake
// clock and records d.
type Waiter func(ctx context.Context, d time.Duration) error

// Random gives the jitter of a retry delay: Int63n returns a number in [0, n) for n > 0. It is a port so a test can
// fix the delays. Adapter: a crypto/rand source in the caller's wiring.
type Random interface {
	Int63n(n int64) int64
}

// IDs makes new identifiers (TECH_SPEC 3.1). Each method returns a well-formed id of its type, so no caller
// receives a malformed one. Adapter: a ULID generator and a UUID generator in internal/platform (task T17).
type IDs interface {
	NewFinding() id.FindingID
	NewRun() id.RunID
	NewCase() id.CaseID
	NewTask() id.TaskID
	NewEvent() change.EventID
}
