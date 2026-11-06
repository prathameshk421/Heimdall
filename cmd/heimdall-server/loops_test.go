package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/collaboration"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/prsync"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/trigger"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
)

// fakePoller fails its first failFirst polls, and cancels the loop's context on poll number cancelAt.
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

// recordingDrainer records that it ran, then returns when the loops stop.
type recordingDrainer struct {
	ran atomic.Bool
}

func (d *recordingDrainer) Run(ctx context.Context, _ <-chan struct{}, _ <-chan time.Time) error {
	d.ran.Store(true)
	<-ctx.Done()
	return nil
}

// blockingDrainer waits for the context to end, then for release, and records that it finished.
type blockingDrainer struct {
	release  chan struct{}
	finished atomic.Bool
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

func (d *blockingDrainer) Run(ctx context.Context, _ <-chan struct{}, _ <-chan time.Time) error {
	<-ctx.Done()
	<-d.release
	d.finished.Store(true)
	return nil
}

// A poll error is logged and the next poll still runs. The loop ends only when its context is done.
func TestPollLoopKeepsPollingAfterAPollError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fp := &fakePoller{failFirst: 3, cancelAt: 5, cancel: cancel}
	a := &app{pollers: []poller{fp}, logger: discardLogger(), clock: clock.System{}, pollEvery: time.Millisecond}

	a.pollLoop(ctx, fp, make(chan struct{}, 1))

	require.Equal(t, 5, fp.calls, "three failed polls must not stop the loop")
}

// The poll wakes the drainer after each poll, so a committed effect does not wait for the next tick.
func TestPollLoopWakesTheDrainerAfterEachPoll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fp := &fakePoller{cancelAt: 1, cancel: cancel}
	a := &app{pollers: []poller{fp}, logger: discardLogger(), clock: clock.System{}, pollEvery: time.Hour}
	wake := make(chan struct{}, 1)

	a.pollLoop(ctx, fp, wake)

	require.Len(t, wake, 1, "the last poll left a wake for the drainer")
}

// The loops run the drainer. A drainer that never starts holds every effect until it dead-letters.
func TestLoopsRunTheDrainer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &recordingDrainer{}
	a := &app{
		pollers: []poller{&fakePoller{}}, drainer: d, logger: discardLogger(), clock: clock.System{},
		pollEvery: time.Hour, drainTick: time.Hour,
	}
	done := make(chan struct{})
	go func() {
		a.loops(ctx)
		close(done)
	}()

	require.Eventually(t, d.ran.Load, 5*time.Second, time.Millisecond, "the outbox drainer must run")
	cancel()
	<-done
}

// Shutdown waits for the drainer. A loop that returned while the drainer was still at work would let the process exit
// with an effect half done.
func TestLoopsWaitForTheDrainerBeforeReturning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &blockingDrainer{release: make(chan struct{})}
	a := &app{
		pollers: []poller{&fakePoller{}}, drainer: d, logger: discardLogger(), clock: clock.System{},
		pollEvery: time.Hour, drainTick: time.Hour,
	}
	returned := make(chan struct{})
	go func() {
		a.loops(ctx)
		close(returned)
	}()

	cancel()
	require.Never(t, func() bool {
		select {
		case <-returned:
			return true
		default:
			return false
		}
	}, 50*time.Millisecond, time.Millisecond, "loops returned while the drainer was still running")

	close(d.release)
	require.Eventually(t, func() bool {
		select {
		case <-returned:
			return true
		default:
			return false
		}
	}, 5*time.Second, time.Millisecond)
	require.True(t, d.finished.Load())
}

// fakeSyncer fails its first failFirst syncs, and cancels the loop's context on sync number cancelAt.
type fakeSyncer struct {
	calls     int
	failFirst int
	cancelAt  int
	cancel    context.CancelFunc
	ran       atomic.Bool
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

// blockingSyncer waits for the context to end, then for release, and records that it finished.
type blockingSyncer struct {
	started  chan struct{}
	release  chan struct{}
	finished atomic.Bool
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

// A sync error is logged and the next sync still runs. The loop ends only when its context is done.
func TestSyncLoopKeepsSyncingAfterASyncError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fs := &fakeSyncer{failFirst: 3, cancelAt: 5, cancel: cancel}
	a := &app{syncer: fs, logger: discardLogger(), clock: clock.System{}, syncEvery: time.Millisecond}

	a.syncLoop(ctx, make(chan struct{}, 1))

	require.Equal(t, 5, fs.calls, "three failed syncs must not stop the loop")
}

// The sync wakes the drainer after each sync, so a committed effect does not wait for the next tick.
func TestSyncLoopWakesTheDrainerAfterEachSync(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fs := &fakeSyncer{cancelAt: 1, cancel: cancel}
	a := &app{syncer: fs, logger: discardLogger(), clock: clock.System{}, syncEvery: time.Hour}
	wake := make(chan struct{}, 1)

	a.syncLoop(ctx, wake)

	require.Len(t, wake, 1, "the last sync left a wake for the drainer")
}

// The loops run the syncer.
func TestLoopsRunTheSyncer(t *testing.T) {
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

	require.Eventually(t, fs.ran.Load, 5*time.Second, time.Millisecond, "the syncer must run")
	cancel()
	<-done
}

// Shutdown waits for the syncer.
func TestLoopsWaitForTheSyncerBeforeReturning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &blockingSyncer{started: make(chan struct{}, 1), release: make(chan struct{})}
	d := &recordingDrainer{}
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
	}, 50*time.Millisecond, time.Millisecond, "loops returned while the syncer was still running")

	close(s.release)
	require.Eventually(t, func() bool {
		select {
		case <-returned:
			return true
		default:
			return false
		}
	}, 5*time.Second, time.Millisecond)
	require.True(t, s.finished.Load())
}
