package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A room stream that keeps failing at once backs off up to a minute, so a stuck message cannot spend a GitHub
// call every second. A stream that ran for a while before it stopped starts again from one second.
func TestRoomStreamRetryBacksOffAndResets(t *testing.T) {
	d := time.Duration(0)
	var got []time.Duration
	for range 8 {
		d = nextStreamDelay(d, time.Second)
		got = append(got, d)
	}
	require.Equal(t, []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 32 * time.Second, time.Minute, time.Minute,
	}, got)
	require.Equal(t, time.Second, nextStreamDelay(time.Minute, 5*time.Minute), "a long healthy run resets the delay")
}
