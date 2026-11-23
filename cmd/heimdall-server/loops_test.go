package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/collaboration"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/prsync"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/trigger"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
)

type fakePoller struct {
	calls     int
	failFirst int
	cancelAt  int
	cancel    context.CancelFunc
}

func (f *fakePoller) PollOnce(context.Context) (trigger.PollResult, error) {
	f.calls++
	if f.cancel != nil && f.calls == f.cancelAt {
		f.cancel()
	}
	if f.calls <= f.failFirst {
		return trigger.PollResult{}, errors.New("jira unavailable")
	}
	return trigger.PollResult{Triggered: 1}, nil
}

type recordingDrainer struct {
	ran atomic.Bool
}

func (d *recordingDrainer) Run(ctx context.Context, _ <-chan struct{}, _ <-chan time.Time) error {
	d.ran.Store(true)
	<-ctx.Done()
	return nil
}

type blockingDrainer struct {
	release  chan struct{}
	finished atomic.Bool
}

func (d *blockingDrainer) Run(ctx context.Context, _ <-chan struct{}, _ <-chan time.Time) error {
	<-ctx.Done()
	<-d.release
	d.finished.Store(true)
	return nil
}

type blockingRoomStream struct {
	started chan struct{}
	stopped atomic.Bool
}

func (s *blockingRoomStream) Run(ctx context.Context) error {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	s.stopped.Store(true)
	return ctx.Err()
}

type recordingCommentPoller struct{ started chan struct{} }

func (p *recordingCommentPoller) PollOnce(context.Context) (collaboration.CommentPollResult, error) {
	select {
	case p.started <- struct{}{}:
	default:
	}
	return collaboration.CommentPollResult{}, nil
}

func TestLoopsStartAndJoinRoomStreamAndGitHubCommentConsumers(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	room := &blockingRoomStream{started: make(chan struct{}, 1)}
	comments := &recordingCommentPoller{started: make(chan struct{}, 1)}
	a := &app{pollers: []poller{&fakePoller{}}, drainer: &recordingDrainer{}, roomStream: room, comments: comments, logger: discardLogger(), clock: clock.System{}, pollEvery: time.Hour, syncEvery: time.Hour, drainTick: time.Hour}
	done := make(chan struct{})
	go func() { a.loops(ctx); close(done) }()
	select {
	case <-room.started:
	case <-time.After(5 * time.Second):
		t.Fatal("room stream was not started")
	}
	select {
	case <-comments.started:
	case <-time.After(5 * time.Second):
		t.Fatal("GitHub comment poller was not started")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loops did not join collaboration consumers")
	}
	require.True(t, room.stopped.Load(), "server shutdown must wait for the room stream")
}

type retentionSpy struct {
	calls  int
	cancel context.CancelFunc
}

func (r *retentionSpy) Expire(context.Context) (int, error) { r.calls++; r.cancel(); return 2, nil }

func TestKnowledgeRetentionLoopSweepsImmediatelyAndStopsWithContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	retention := &retentionSpy{cancel: cancel}
	(&app{retention: retention, logger: discardLogger()}).knowledgeRetentionLoop(ctx)
	require.Equal(t, 1, retention.calls)
}

type episodeDrainSpy struct {
	calls  int
	limit  int
	cancel context.CancelFunc
}

func (e *episodeDrainSpy) RunOnce(_ context.Context, limit int) (int, error) {
	e.calls++
	e.limit = limit
	e.cancel()
	return 1, nil
}

func TestEpisodeDeliveryLoopRetriesOnTheSharedDrainCadence(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drain := &episodeDrainSpy{cancel: cancel}
	(&app{episodeWorker: drain, logger: discardLogger()}).episodeDeliveryLoop(ctx)
	require.Equal(t, 1, drain.calls)
	require.Equal(t, 100, drain.limit)
}

type fakeSyncer struct {
	calls     int
	failFirst int
	cancelAt  int
	cancel    context.CancelFunc
	ran       atomic.Bool
}

func (f *fakeSyncer) Reconcile(context.Context) (prsync.Result, error) {
	return prsync.Result{}, nil
}

func (f *fakeSyncer) SyncOnce(_ context.Context) (prsync.Result, error) {
	f.ran.Store(true)
	f.calls++
	if f.cancel != nil && f.calls == f.cancelAt {
		f.cancel()
	}
	if f.calls <= f.failFirst {
		return prsync.Result{}, errors.New("github unavailable")
	}
	return prsync.Result{Synced: 1}, nil
}

type blockingSyncer struct {
	started  chan struct{}
	release  chan struct{}
	finished atomic.Bool
}

func (s *blockingSyncer) Reconcile(context.Context) (prsync.Result, error) {
	return prsync.Result{}, nil
}

func (s *blockingSyncer) SyncOnce(ctx context.Context) (prsync.Result, error) {
	if s.started != nil {
		select {
		case s.started <- struct{}{}:
		default:
		}
	}
	<-ctx.Done()
	<-s.release
	s.finished.Store(true)
	return prsync.Result{}, ctx.Err()
}

func TestLoopsKeepRunningAfterErrors(t *testing.T) {
	t.Parallel()
	t.Run("poll loop", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		fp := &fakePoller{failFirst: 3, cancelAt: 5, cancel: cancel}
		a := &app{pollers: []poller{fp}, logger: discardLogger(), clock: clock.System{}, pollEvery: time.Millisecond}
		a.pollLoop(ctx, fp, make(chan struct{}, 1))
		require.Equal(t, 5, fp.calls, "three failed polls must not stop the loop")
	})
	t.Run("sync loop", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		fs := &fakeSyncer{failFirst: 3, cancelAt: 5, cancel: cancel}
		a := &app{syncer: fs, logger: discardLogger(), clock: clock.System{}, syncEvery: time.Millisecond}
		a.syncLoop(ctx, make(chan struct{}, 1))
		require.Equal(t, 5, fs.calls, "three failed syncs must not stop the loop")
	})
}

func TestLoopsWakeTheDrainerAfterRun(t *testing.T) {
	t.Parallel()
	t.Run("poll loop wakes drainer", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		fp := &fakePoller{cancelAt: 1, cancel: cancel}
		a := &app{pollers: []poller{fp}, logger: discardLogger(), clock: clock.System{}, pollEvery: time.Hour}
		wake := make(chan struct{}, 1)
		a.pollLoop(ctx, fp, wake)
		require.Len(t, wake, 1, "the last poll left a wake for the drainer")
	})
	t.Run("sync loop wakes drainer", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		fs := &fakeSyncer{cancelAt: 1, cancel: cancel}
		a := &app{syncer: fs, logger: discardLogger(), clock: clock.System{}, syncEvery: time.Hour}
		wake := make(chan struct{}, 1)
		a.syncLoop(ctx, wake)
		require.Len(t, wake, 1, "the last sync left a wake for the drainer")
	})
}

func TestLoopsRunDrainerAndSyncer(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fs := &fakeSyncer{}
	d := &recordingDrainer{}
	a := &app{
		pollers: []poller{&fakePoller{}}, syncer: fs, drainer: d, logger: discardLogger(), clock: clock.System{},
		pollEvery: time.Hour, syncEvery: time.Hour, drainTick: time.Hour,
	}
	done := make(chan struct{})
	go func() {
		a.loops(ctx)
		close(done)
	}()

	require.Eventually(t, d.ran.Load, 5*time.Second, time.Millisecond, "the outbox drainer must run")
	require.Eventually(t, fs.ran.Load, 5*time.Second, time.Millisecond, "the syncer must run")
	cancel()
	<-done
}

func TestLoopsWaitForDrainerAndSyncerBeforeReturning(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &blockingDrainer{release: make(chan struct{})}
	s := &blockingSyncer{started: make(chan struct{}, 1), release: make(chan struct{})}
	a := &app{
		pollers: []poller{&fakePoller{}}, syncer: s, drainer: d, logger: discardLogger(), clock: clock.System{},
		pollEvery: time.Hour, syncEvery: time.Hour, drainTick: time.Hour,
	}
	returned := make(chan struct{})
	go func() {
		a.loops(ctx)
		close(returned)
	}()

	<-s.started
	cancel()

	require.Never(t, func() bool {
		select {
		case <-returned:
			return true
		default:
			return false
		}
	}, 50*time.Millisecond, time.Millisecond, "loops returned while workers were still running")

	close(d.release)
	close(s.release)
	require.Eventually(t, func() bool {
		select {
		case <-returned:
			return true
		default:
			return false
		}
	}, 5*time.Second, time.Millisecond)
	require.True(t, d.finished.Load())
	require.True(t, s.finished.Load())
}

type steppedClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *steppedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *steppedClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

type orderSyncer struct {
	order    []string
	clk      *steppedClock
	step     time.Duration
	cancelAt int
	cancel   context.CancelFunc
}

func (o *orderSyncer) Reconcile(context.Context) (prsync.Result, error) {
	o.order = append(o.order, "reconcile")
	return prsync.Result{}, nil
}

func (o *orderSyncer) SyncOnce(context.Context) (prsync.Result, error) {
	o.order = append(o.order, "sync")
	o.clk.advance(o.step)
	if o.cancelAt > 0 && syncs(o.order) == o.cancelAt {
		o.cancel()
	}
	return prsync.Result{Synced: 1}, nil
}

func syncs(order []string) int {
	n := 0
	for _, s := range order {
		if s == "sync" {
			n++
		}
	}
	return n
}

func TestSyncLoopReconciles(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		step time.Duration
		want []string
	}{
		{"at boot before first sync", time.Minute, []string{"reconcile", "sync", "sync", "sync"}},
		{"again after interval", reconcileInterval, []string{"reconcile", "sync", "reconcile", "sync", "reconcile", "sync"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			clk := &steppedClock{at: time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)}
			rec := &orderSyncer{clk: clk, step: tc.step, cancelAt: 3, cancel: cancel}
			a := &app{syncer: rec, logger: discardLogger(), clock: clk, syncEvery: time.Millisecond}
			a.syncLoop(ctx, make(chan struct{}, 1))
			require.Equal(t, tc.want, rec.order)
		})
	}
}
