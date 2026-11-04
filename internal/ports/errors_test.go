package ports_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestSentinelErrorsSurviveWrappingAndStayDistinct(t *testing.T) {
	wrapped := fmt.Errorf("append PAY-142 at version 4: %w", ports.ErrVersionConflict)
	require.ErrorIs(t, wrapped, ports.ErrVersionConflict)
	require.NotErrorIs(t, wrapped, ports.ErrNotFound)
	require.NotErrorIs(t, wrapped, ports.ErrUnavailable)

	require.ErrorIs(t, fmt.Errorf("load: %w", ports.ErrNotFound), ports.ErrNotFound)
	require.ErrorIs(t, fmt.Errorf("switch post: %w", ports.ErrUnavailable), ports.ErrUnavailable)
	require.NotErrorIs(t, ports.ErrVersionConflict, ports.ErrNotFound)
}
