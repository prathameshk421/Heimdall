package record

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Planted credentials never reach the record (H8).
func TestSummarizeRedactsPlantedCredentials(t *testing.T) {
	planted := map[string]string{
		"github":  "ghs_" + strings.Repeat("A", 36),
		"pat":     "github_pat_" + strings.Repeat("b", 30),
		"slack":   "xoxb-" + strings.Repeat("1", 20),
		"model":   "sk-or-v1-" + strings.Repeat("c", 40),
		"key":     "-----BEGIN OPENSSH PRIVATE KEY-----abc",
		"hexblob": strings.Repeat("deadbeef", 8),
	}
	args, err := json.Marshal(map[string]any{"path": "internal/api/refunds.go", "planted": planted})
	require.NoError(t, err)
	got := Summarize(args)
	for name, value := range planted {
		require.NotContains(t, got, value[:12], name)
	}
	require.Contains(t, got, "internal/api/refunds.go", "ordinary text stays readable")
	long := Summarize(json.RawMessage(`{"x":"` + strings.Repeat("word ", 200) + `"}`))
	require.LessOrEqual(t, len([]rune(long)), MaxSummary+1)
	require.Empty(t, Summarize(json.RawMessage(`not json`)))
}

func TestRepeatedRefusals(t *testing.T) {
	same := ports.ToolCall{Tool: "submit_review", Outcome: "refused", Refusal: "rule_checks: missing card x"}
	other := ports.ToolCall{Tool: "submit_review", Outcome: "ok"}
	require.Equal(t, 2, RepeatedRefusals([]ports.ToolCall{same, same, other, same}, same.Tool, same.Refusal))
	require.Equal(t, 0, RepeatedRefusals([]ports.ToolCall{other, same}, same.Tool, same.Refusal))
	require.Equal(t, 0, RepeatedRefusals([]ports.ToolCall{same}, same.Tool, "another reason"))
}
