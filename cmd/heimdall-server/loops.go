package main

import (
	"context"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/prsync"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
)

// startLoops runs the loops of a in the background and returns the function that stops them. The function cancels
// the loops and waits until every loop has returned. A nil app has no loops, so its stop does nothing.
func startLoops(ctx context.Context, a *app) func() {
	if a == nil {
		return func() {}
	}
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.loops(loopCtx)
	}()
	return func() {
		cancel()
		<-done
	}
}

// loops runs the ticket poller, the outbox drainer and the drain tick until ctx is done, and returns after all three
// have returned. A loop error is logged and the loop carries on. Only a done context ends a loop.
func (a *app) loops(ctx context.Context) {
	wake := make(chan struct{}, 1)
	ticks := make(chan time.Time)
	var wg sync.WaitGroup
	// One poll loop per project, plus the drain tick and the drainer (A10 item 10).
	wg.Add(len(a.pollers) + 2)
	for _, p := range a.pollers {
		go func() {
			defer wg.Done()
			a.pollLoop(ctx, p, wake)
		}()
	}
	if a.syncer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.syncLoop(ctx, wake)
		}()
	}
	if a.roomStream != nil {
		wg.Add(1)
		go func() { defer wg.Done(); a.roomStreamLoop(ctx) }()
	}
	if a.comments != nil {
		wg.Add(1)
		go func() { defer wg.Done(); a.commentLoop(ctx, wake) }()
	}
	if a.episodeWorker != nil {
		wg.Add(1)
		go func() { defer wg.Done(); a.episodeLoop(ctx) }()
	}
	if a.workflowMemory != nil {
		wg.Add(1)
		go func() { defer wg.Done(); a.workflowMemoryLoop(ctx) }()
	}
	go func() {
		defer wg.Done()
		a.tickLoop(ctx, ticks)
	}()
	go func() {
		defer wg.Done()
		a.drainLoop(ctx, wake, ticks)
	}()
	wg.Wait()
}

func (a *app) workflowMemoryLoop(ctx context.Context) {
	for ctx.Err() == nil {
		if stats, err := a.workflowMemory.Backfill(ctx); err != nil && ctx.Err() == nil {
			a.logger.ErrorContext(ctx, "workflow memory backfill failed; stable event keys make the next pass safe", "error", err)
		} else if err == nil && stats.Queued > 0 {
			a.logger.InfoContext(ctx, "workflow memory episodes queued", "changes", stats.Changes, "events", stats.Events, "queued", stats.Queued)
		}
		if err := clock.Wait(ctx, a.syncEvery); err != nil {
			return
		}
	}
}

func (a *app) episodeLoop(ctx context.Context) {
	for ctx.Err() == nil {
		if _, err := a.episodeWorker.RunOnce(ctx, 32); err != nil && ctx.Err() == nil {
			a.logger.ErrorContext(ctx, "Graphiti knowledge delivery failed; queued episodes remain retryable", "error", err)
		}
		if err := clock.Wait(ctx, a.syncEvery); err != nil {
			return
		}
	}
}

func (a *app) roomStreamLoop(ctx context.Context) {
	for ctx.Err() == nil {
		if err := a.roomStream.Run(ctx); err != nil && ctx.Err() == nil {
			a.logger.ErrorContext(ctx, "Switch room stream stopped; durable cursors will resume it", "error", err)
			if waitErr := clock.Wait(ctx, time.Second); waitErr != nil {
				return
			}
		}
	}
}

func (a *app) commentLoop(ctx context.Context, wake chan<- struct{}) {
	for {
		result, err := a.comments.PollOnce(ctx)
		if err != nil && ctx.Err() == nil {
			a.logger.ErrorContext(ctx, "GitHub issue comment poll failed; the next poll retries", "error", err)
		}
		for _, failure := range result.Failed {
			a.logger.ErrorContext(ctx, "GitHub issue comment PR failed", "repo", failure.Repo, "pr", failure.Number, "error", failure.Err)
		}
		if err == nil && result.Comments > 0 {
			a.logger.InfoContext(ctx, "GitHub issue comments processed", "comments", result.Comments)
			select {
			case wake <- struct{}{}:
			default:
			}
		}
		if err := clock.Wait(ctx, a.syncEvery); err != nil {
			return
		}
	}
}

// syncLoop runs the reconcile pass at boot and again every reconcileInterval, then syncs open pull requests, reviews
// and check runs, then waits for the sync interval, until ctx is done. The reconcile pass recovers what happened while
// the server was down (TECH_SPEC 2.2, D56). After each sync it wakes the drainer, so an effect the sync committed
// runs without waiting for the next tick. A sync error is logged.
func (a *app) syncLoop(ctx context.Context, wake chan<- struct{}) {
	var lastReconcile time.Time
	for {
		if a.reconcileDue(lastReconcile) {
			a.reconcile(ctx)
			lastReconcile = a.clock.Now()
		}
		res, err := a.syncer.SyncOnce(ctx)
		a.logSync(ctx, res, err)
		select {
		case wake <- struct{}{}:
		default:
		}
		if err := clock.Wait(ctx, a.syncEvery); err != nil {
			return
		}
	}
}

// reconcileDue reports whether the reconcile pass must run: at boot, when last is zero, and then once reconcileInterval
// has passed since the last pass.
func (a *app) reconcileDue(last time.Time) bool {
	return last.IsZero() || a.clock.Now().Sub(last) >= reconcileInterval
}

// logSync logs the outcome of one pull request sync. A failed sync is logged and the next sync retries.
func (a *app) logSync(ctx context.Context, res prsync.Result, err error) {
	switch {
	case err != nil && ctx.Err() == nil:
		a.logger.ErrorContext(ctx, "pull request sync failed; the next sync retries", "error", err)
	case err == nil:
		for _, f := range res.Failed {
			a.logger.ErrorContext(ctx, "pull request sync PR failed", "repo", f.Repo, "pr", f.Number, "error", f.Err)
		}
		if res.Synced > 0 || res.Appended > 0 || res.Evaluated > 0 {
			a.logger.InfoContext(ctx, "pull requests synced", "synced", res.Synced, "appended", res.Appended, "evaluated", res.Evaluated)
		}
	}
}

// reconcile runs the pull request reconcile pass and logs what it did. A failed pass is logged, and the next pass
// retries. A pull request that cannot be read is named and the others still run.
func (a *app) reconcile(ctx context.Context) {
	res, err := a.syncer.Reconcile(ctx)
	switch {
	case err != nil && ctx.Err() == nil:
		a.logger.ErrorContext(ctx, "reconcile failed; the next pass retries", "error", err)
	case err == nil:
		for _, f := range res.Failed {
			a.logger.ErrorContext(ctx, "reconcile of pull request failed", "repo", f.Repo, "pr", f.Number, "error", f.Err)
		}
		if res.Appended > 0 {
			a.logger.InfoContext(ctx, "events recovered for pull requests that closed while the server was down", "appended", res.Appended)
		}
	}
}

// pollLoop polls the tickets, then waits for the poll interval, until ctx is done. After each poll it wakes the
// drainer, so an effect the poll committed runs without waiting for the next tick. A poll error is logged.
func (a *app) pollLoop(ctx context.Context, p poller, wake chan<- struct{}) {
	for {
		res, err := p.PollOnce(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			a.logger.ErrorContext(ctx, "ticket poll failed; the next poll retries", "error", err)
		case err == nil && res.Triggered > 0:
			a.logger.InfoContext(ctx, "changes triggered", "triggered", res.Triggered)
		}
		select {
		case wake <- struct{}{}:
		default:
		}
		if err := clock.Wait(ctx, a.pollEvery); err != nil {
			return
		}
	}
}

// tickLoop sends the drain tick every drainTick until ctx is done.
func (a *app) tickLoop(ctx context.Context, ticks chan<- time.Time) {
	for {
		if err := clock.Wait(ctx, a.drainTick); err != nil {
			return
		}
		select {
		case ticks <- a.clock.Now():
		case <-ctx.Done():
			return
		}
	}
}

// drainLoop runs the outbox drainer until ctx is done. The drainer logs its own batch errors and keeps going.
func (a *app) drainLoop(ctx context.Context, wake <-chan struct{}, ticks <-chan time.Time) {
	if err := a.drainer.Run(ctx, wake, ticks); err != nil {
		a.logger.ErrorContext(ctx, "outbox drainer stopped", "error", err)
	}
}
