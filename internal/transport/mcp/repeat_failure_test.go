package mcp

import (
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestRepeatedFailuresCountsConsecutiveMatchingFailingCalls(t *testing.T) {
	t.Parallel()
	recent := []ports.ToolCall{
		{Tool: "read_code", Outcome: "failed", Args: `{"path":"a.go"}`},
		{Tool: "read_code", Outcome: "failed", Args: `{"path":"a.go"}`},
	}
	require.Equal(t, 2, repeatedFailures(recent, "read_code", `{"path":"a.go"}`))
	require.Equal(t, 0, repeatedFailures(recent, "read_code", `{"path":"b.go"}`))
	require.Equal(t, 0, repeatedFailures(recent, "search_code", `{"path":"a.go"}`))

	mixed := []ports.ToolCall{
		{Tool: "read_code", Outcome: "ok", Args: `{"path":"a.go"}`},
		{Tool: "read_code", Outcome: "failed", Args: `{"path":"a.go"}`},
	}
	require.Equal(t, 0, repeatedFailures(mixed, "read_code", `{"path":"a.go"}`))
}
