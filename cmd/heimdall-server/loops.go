package main

import (
	"context"
	"sync"
	"time"

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
	wg.Add(3)
	go func() {
		defer wg.Done()
		a.pollLoop(ctx, wake)
	}()
	if a.syncer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.syncLoop(ctx, wake)
		}()
	}
	if a.retention != nil {
		wg.Add(1)
		go func() { defer wg.Done(); a.knowledgeRetentionLoop(ctx) }()
	}
	if a.episodeWorker != nil {
		wg.Add(1)
		go func() { defer wg.Done(); a.episodeDeliveryLoop(ctx) }()
	}
	if a.memoryBackfill != nil {
		wg.Add(1)
		go func() { defer wg.Done(); a.workflowBackfillLoop(ctx) }()
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

// workflowBackfillLoop retries durable event projection from the last persisted global sequence after startup and on
// each drain cadence. Queue errors leave the cursor unchanged, so a later pass safely replays stable event keys.
func (a *app) workflowBackfillLoop(ctx context.Context) {
	for {
		if err := a.memoryBackfill(ctx); err != nil && ctx.Err() == nil {
			a.logger.ErrorContext(ctx, "workflow memory backfill failed; durable cursor will retry", "error", err)
		}
		if err := clock.Wait(ctx, drainTick); err != nil {
			return
		}
	}
}

func (a *app) episodeDeliveryLoop(ctx context.Context) {
	for {
		delivered, err := a.episodeWorker.RunOnce(ctx, 100)
		if err != nil && ctx.Err() == nil {
			a.logger.ErrorContext(ctx, "memory episode delivery failed; pending episodes will retry", "error", err)
		} else if err == nil && delivered > 0 {
			a.logger.InfoContext(ctx, "memory episodes delivered", "count", delivered)
		}
		if err := clock.Wait(ctx, drainTick); err != nil {
			return
		}
	}
}

// knowledgeRetentionLoop expires staged/approved files and deletes their memory episodes and blobs. It runs once at
// startup and then every interval; errors are logged and retried on the next cadence.
func (a *app) knowledgeRetentionLoop(ctx context.Context) {
	for {
		removed, err := a.retention.Expire(ctx)
		if err != nil && ctx.Err() == nil {
			a.logger.ErrorContext(ctx, "knowledge retention sweep failed; the next sweep retries", "error", err)
		} else if err == nil && removed > 0 {
			a.logger.InfoContext(ctx, "expired knowledge files removed", "count", removed)
		}
		if err := clock.Wait(ctx, knowledgeRetentionInterval); err != nil {
			return
		}
	}
}

// syncLoop syncs open pull requests, reviews and check runs, then waits for the sync interval, until ctx is done.
// After each sync it wakes the drainer, so an effect the sync committed runs without waiting for the next tick.
// A sync error is logged.
func (a *app) syncLoop(ctx context.Context, wake chan<- struct{}) {
	for {
		res, err := a.syncer.SyncOnce(ctx)
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
		select {
		case wake <- struct{}{}:
		default:
		}
		if err := clock.Wait(ctx, a.syncEvery); err != nil {
			return
		}
	}
}

// pollLoop polls the tickets, then waits for the poll interval, until ctx is done. After each poll it wakes the
// drainer, so an effect the poll committed runs without waiting for the next tick. A poll error is logged.
func (a *app) pollLoop(ctx context.Context, wake chan<- struct{}) {
	for {
		res, err := a.poller.PollOnce(ctx)
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
