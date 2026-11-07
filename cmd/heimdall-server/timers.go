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
	// fired holds every deadline already fired, by change, step and due time, so a step whose timeout changes
	// nothing is not fired again on every pass (seen live: three deadlines refired every 15 seconds).
	fired map[string]bool
}

// RunOnce fires every due timer of every open change and returns the deadlines it fired, as "change/step".
func (w *workflowTimers) RunOnce(ctx context.Context) ([]string, error) {
	bindings, err := w.rooms.OpenRooms(ctx)
	if err != nil {
		return nil, fmt.Errorf("timers: list open changes: %w", err)
	}
	seen := map[id.ChangeID]bool{}
	var fired []string
	var errs []error
	for _, b := range bindings {
		if seen[b.ChangeID] {
			continue
		}
		seen[b.ChangeID] = true
		names, err := w.fire(ctx, b.ChangeID)
		fired = append(fired, names...)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return fired, errors.Join(errs...)
}

// fire runs the change once to learn its deadlines, then the OnTimeout of each one that is due.
func (w *workflowTimers) fire(ctx context.Context, cid id.ChangeID) ([]string, error) {
	out, err := w.eng.Advance(ctx, cid, nil)
	if err != nil {
		return nil, fmt.Errorf("timers: run %s: %w", cid, err)
	}
	now := w.clock.Now()
	var fired []string
	for _, t := range out.Timers {
		key := fmt.Sprintf("%s|%s|%d", cid, t.Step, t.Due.UnixNano())
		if t.Due.After(now) || w.fired[key] {
			continue
		}
		if _, err := w.eng.Timeout(ctx, cid, t.Step); err != nil {
			return fired, fmt.Errorf("timers: fire %s of %s: %w", t.Step, cid, err)
		}
		w.fired[key] = true
		fired = append(fired, string(cid)+"/"+t.Step)
	}
	return fired, nil
}

// timerLoop fires due workflow deadlines every 15 seconds.
func (a *app) timerLoop(ctx context.Context) {
	for {
		names, err := a.timers.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			a.logger.ErrorContext(ctx, "workflow timers failed; they will be retried", "error", err)
		}
		if len(names) > 0 {
			a.logger.InfoContext(ctx, "workflow timers fired", "timers", names)
		}
		if err := clock.Wait(ctx, 15*time.Second); err != nil {
			return
		}
	}
}
