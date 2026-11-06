package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
)

// Two projects poll on their own loops (A10 item 10). A project whose poll fails on every try does not stop the other
// project from polling and triggering: each loop carries on by itself.
func TestAFailingProjectDoesNotStopAnotherProjectsPoll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	good := &fakePoller{cancelAt: 3, cancel: cancel}
	bad := &fakePoller{failFirst: 1 << 30}
	a := &app{pollers: []poller{bad, good}, logger: discardLogger(), clock: clock.System{}, pollEvery: time.Millisecond}

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.pollLoop(ctx, bad, make(chan struct{}, 1))
	}()
	a.pollLoop(ctx, good, make(chan struct{}, 1))
	<-done

	require.GreaterOrEqual(t, good.calls, 3, "the healthy project kept polling")
	require.GreaterOrEqual(t, bad.calls, 1, "the failing project was polled too")
}
