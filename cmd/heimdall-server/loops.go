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
	for _, run := range a.backgroundLoops(wake) {
		wg.Add(1)
		go func() { defer wg.Done(); run(ctx) }()
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

// episodeDeliveryLoop delivers queued memory episodes to Graphiti on the drain cadence. Failed episodes stay queued.
func (a *app) episodeDeliveryLoop(ctx context.Context) {
	for {
		delivered, err := a.episodeWorker.RunOnce(ctx, 100)
		if err != nil && ctx.Err() == nil {
			a.logger.ErrorContext(ctx, "memory episode delivery failed; pending episodes will retry", "error", err)
		} else if err == nil && delivered > 0 {
			a.logger.InfoContext(ctx, "memory episodes delivered", "count", delivered)
		}
		if err := clock.Wait(ctx, a.drainTick); err != nil {
			return
		}
	}
}

// narratorLoop posts the milestone lines on the drain cadence. A failed post is retried on the next pass.
func (a *app) narratorLoop(ctx context.Context) {
	for {
		said, err := a.narrator.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			a.logger.ErrorContext(ctx, "milestone announcement failed; it will be retried", "error", err)
		} else if err == nil && said > 0 {
			a.logger.InfoContext(ctx, "milestones announced", "count", said)
		}
		if err := clock.Wait(ctx, a.drainTick); err != nil {
			return
		}
	}
}

func (a *app) roomStreamLoop(ctx context.Context) {
	var delay time.Duration
	for ctx.Err() == nil {
		started := time.Now()
		if err := a.roomStream.Run(ctx); err != nil && ctx.Err() == nil {
			delay = nextStreamDelay(delay, time.Since(started))
			a.logger.ErrorContext(ctx, "Switch room stream stopped; durable cursors will resume it", "error", err, "retry_in", delay)
			if waitErr := clock.Wait(ctx, delay); waitErr != nil {
				return
			}
		}
	}
}

// Room stream retry bounds: the first retry waits streamRetryFirst, each quick failure doubles it up to
// streamRetryCap, and a run that lasted streamHealthyRun or more counts as healthy and resets it.
const (
	streamRetryFirst = time.Second
	streamRetryCap   = time.Minute
	streamHealthyRun = 2 * time.Minute
)

// nextStreamDelay returns the wait before the next stream attempt, given the previous wait and how long the
// failed attempt ran. A message that fails every time (for example while GitHub is rate limited) then costs one
// attempt a minute, not one a second.
func nextStreamDelay(prev, ranFor time.Duration) time.Duration {
	if prev == 0 || ranFor >= streamHealthyRun {
		return streamRetryFirst
	}
	return min(prev*2, streamRetryCap)
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

// knowledgeRetentionLoop expires staged and approved files and deletes their memory episodes and blobs. It runs once
// at startup and then every knowledgeRetentionInterval; errors are logged and retried on the next pass.
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

// backgroundLoops returns the optional loops whose dependency is wired: the room stream, GitHub comments, Graphiti
// episode delivery, knowledge retention and the workflow memory projection.
func (a *app) backgroundLoops(wake chan<- struct{}) []func(context.Context) {
	var runs []func(context.Context)
	if a.roomStream != nil {
		runs = append(runs, a.roomStreamLoop)
	}
	if a.comments != nil {
		runs = append(runs, func(ctx context.Context) { a.commentLoop(ctx, wake) })
	}
	if a.narrator != nil {
		runs = append(runs, a.narratorLoop)
	}
	if a.episodeWorker != nil {
		runs = append(runs, a.episodeDeliveryLoop)
	}
	if a.retention != nil {
		runs = append(runs, a.knowledgeRetentionLoop)
	}
	if a.workflowMemory != nil {
		runs = append(runs, a.workflowMemoryLoop)
	}
	return runs
}
