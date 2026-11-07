package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// workflowTimers fires the deadlines the engine computes (reviewer pick, task deadlines). The engine only returns
// them; before this loop nothing called Engine.Timeout, so the reviewer pick never fell back to the defaults (found
// live on KAN-19). Deadlines are recomputed from the stored events on every pass, so a restart loses none.
type workflowTimers struct {
	eng   *engine.Engine
	rooms ports.RoomIndex
	clock ports.Clock
}

// RunOnce fires every due timer of every open change and returns how many fired.
func (w workflowTimers) RunOnce(ctx context.Context) (int, error) {
	bindings, err := w.rooms.OpenRooms(ctx)
	if err != nil {
		return 0, fmt.Errorf("timers: list open changes: %w", err)
	}
	seen := map[id.ChangeID]bool{}
	fired := 0
	var errs []error
	for _, b := range bindings {
		if seen[b.ChangeID] {
			continue
		}
		seen[b.ChangeID] = true
		n, err := w.fire(ctx, b.ChangeID)
		fired += n
		if err != nil {
			errs = append(errs, err)
		}
	}
	return fired, errors.Join(errs...)
}

// fire runs the change once to learn its deadlines, then the OnTimeout of each one that is due.
func (w workflowTimers) fire(ctx context.Context, cid id.ChangeID) (int, error) {
	out, err := w.eng.Advance(ctx, cid, nil)
	if err != nil {
		return 0, fmt.Errorf("timers: run %s: %w", cid, err)
	}
	now := w.clock.Now()
	fired := 0
	for _, t := range out.Timers {
		if t.Due.After(now) {
			continue
		}
		if _, err := w.eng.Timeout(ctx, cid, t.Step); err != nil {
			return fired, fmt.Errorf("timers: fire %s of %s: %w", t.Step, cid, err)
		}
		fired++
	}
	return fired, nil
}

// timerLoop fires due workflow deadlines every 15 seconds.
func (a *app) timerLoop(ctx context.Context) {
	for {
		n, err := a.timers.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			a.logger.ErrorContext(ctx, "workflow timers failed; they will be retried", "error", err)
		} else if n > 0 {
			a.logger.InfoContext(ctx, "workflow timers fired", "count", n)
		}
		if err := clock.Wait(ctx, 15*time.Second); err != nil {
			return
		}
	}
}
