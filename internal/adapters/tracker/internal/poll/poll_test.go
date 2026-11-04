package poll

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)

func TestWindowStartWithoutCursorLooksBackOneOverlap(t *testing.T) {
	got, err := WindowStart("", now)
	require.NoError(t, err)
	require.True(t, got.Equal(now.Add(-Overlap)), "got %s", got)
}

func TestWindowStartSubtractsTheOverlapFromTheCursor(t *testing.T) {
	cursor := Format(now)
	got, err := WindowStart(cursor, now.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, got.Equal(now.Add(-Overlap)), "got %s", got)
}

func TestWindowStartRefusesACursorThatIsNotATime(t *testing.T) {
	_, err := WindowStart("yesterday", now)
	require.Error(t, err)
}

func TestOverlapIsTenMinutes(t *testing.T) {
	require.Equal(t, 10*time.Minute, Overlap)
}

func TestSeenReportsASecondObservationAsARepeat(t *testing.T) {
	var s Seen
	require.True(t, s.Observe("KAN-1", "10042", now))
	require.False(t, s.Observe("KAN-1", "10042", now))
}

func TestSeenKeysByBothTheTicketKeyAndTheChangelogID(t *testing.T) {
	var s Seen
	require.True(t, s.Observe("KAN-1", "10042", now))
	require.True(t, s.Observe("KAN-2", "10042", now))
	require.True(t, s.Observe("KAN-1", "10043", now))
}

func TestForgetDropsOnlyEntriesOlderThanTheWindow(t *testing.T) {
	var s Seen
	old := now.Add(-time.Hour)
	require.True(t, s.Observe("KAN-1", "1", old))
	require.True(t, s.Observe("KAN-1", "2", now))

	s.Forget(now.Add(-Overlap))

	require.True(t, s.Observe("KAN-1", "1", old), "an entry older than the window is forgotten")
	require.False(t, s.Observe("KAN-1", "2", now), "an entry inside the window is kept")
}
