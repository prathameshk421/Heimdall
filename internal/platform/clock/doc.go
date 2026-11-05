// Package clock is the system clock and the real waiter for the server (CODING_STANDARDS 2 and 4, TECH_SPEC 2.2).
//
// # Purpose
//
// It gives the application the real time through ports.Clock, and gives the adapters that retry (Jira, GitHub,
// Switch) a real Waiter. It deliberately does not schedule work and does not hold state.
//
// # API
//
//   - System: the zero value implements ports.Clock. Now() returns time.Now().
//   - Wait(ctx, d) error: the function shape the adapters call as their Waiter. It returns nil after d, or
//     ctx.Err() at once when ctx is done. A d of zero or less returns nil at once when ctx is live.
//
// # Invariants
//
//   - Now is never zero for a running process.
//   - Wait never leaves its timer running after it returns.
//
// # Failure modes
//
//   - A done context returns ctx.Err() and wraps nothing, so callers compare with errors.Is(err, context.Canceled)
//     or context.DeadlineExceeded.
//
// # Tests
//
// clock_test.go checks Now against the real time on both sides of the call, Wait with zero and negative durations,
// a short real wait, and a cancelled wait that must return promptly.
package clock

import (
	"context"
	"time"
)

// System is the wall clock. Its zero value is ready to use.
type System struct{}

// Now returns the current time.
func (System) Now() time.Time {
	return time.Now()
}

// Wait pauses for d or until ctx is done. A done context returns its error first, even for a zero wait. A d of zero
// or less returns nil at once. The timer is stopped before Wait returns.
func Wait(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
