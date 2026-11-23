package summary_test

import (
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/summary"
	"github.com/stretchr/testify/require"
)

func TestBreakGlassSummary(t *testing.T) {
	t.Parallel()
	in := summary.Input{Change: "pr-1", Version: 1, Head: head, BreakGlass: &summary.BreakGlass{Login: "<@all>", Reason: "<script> @team | injected"}}
	out := summary.Render(in)
	require.Contains(t, out, "### Break-glass")
	require.Contains(t, out, "open, awaiting review by another approver")
	require.NotContains(t, out, "<script>")
	require.NotContains(t, out, "@team")
	require.Contains(t, out, `\| injected`)
	require.Contains(t, out, "&lt;script&gt;")
	zero := summary.Render(summary.Input{Change: "pr-1", Version: 1, Head: head})
	require.NotContains(t, zero, "### Break-glass")
	require.True(t, strings.HasSuffix(out, "\n"))
}
