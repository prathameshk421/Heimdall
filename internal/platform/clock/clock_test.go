package clock

import (
	"context"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

var _ ports.Clock = System{}

func TestNowIsBetweenTheRealClockReadings(t *testing.T) {
	before := time.Now()
	got := System{}.Now()
	after := time.Now()
	require.False(t, got.Before(before), "Now must not be earlier than the reading taken before it")
	require.False(t, got.After(after), "Now must not be later than the reading taken after it")
}

func TestWaitZeroReturnsAtOnce(t *testing.T) {
	require.NoError(t, Wait(context.Background(), 0))
	require.NoError(t, Wait(context.Background(), -time.Second), "a negative wait is the same as zero")
}

func TestWaitShortDurationReturnsNil(t *testing.T) {
	require.NoError(t, Wait(context.Background(), time.Millisecond))
}

func TestWaitReturnsDeadlineErrorWhenContextExpires(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	err := Wait(ctx, time.Hour)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestWaitWithDoneContextReturnsItsErrorEvenForZeroWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, Wait(ctx, 0), context.Canceled)
}
