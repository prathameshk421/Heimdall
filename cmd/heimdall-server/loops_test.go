package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
	a := &app{poller: fp, logger: discardLogger(), clock: clock.System{}, pollEvery: time.Millisecond}

	a.pollLoop(ctx, make(chan struct{}, 1))

	require.Equal(t, 5, fp.calls, "three failed polls must not stop the loop")
}

// The poll wakes the drainer after each poll, so a committed effect does not wait for the next tick.
func TestPollLoopWakesTheDrainerAfterEachPoll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fp := &fakePoller{cancelAt: 1, cancel: cancel}
	a := &app{poller: fp, logger: discardLogger(), clock: clock.System{}, pollEvery: time.Hour}
	wake := make(chan struct{}, 1)

	a.pollLoop(ctx, wake)

	require.Len(t, wake, 1, "the last poll left a wake for the drainer")
}

// The loops run the drainer. A drainer that never starts holds every effect until it dead-letters.
func TestLoopsRunTheDrainer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &recordingDrainer{}
	a := &app{
		poller: &fakePoller{}, drainer: d, logger: discardLogger(), clock: clock.System{},
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
		poller: &fakePoller{}, drainer: d, logger: discardLogger(), clock: clock.System{},
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
