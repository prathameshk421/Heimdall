package contextpack_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/contextpack"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
)

func TestCompileFramesUntrustedContextStripsBiDiAndRespectsBudget(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	cards := []rulecard.Card{
		{Name: "auth.md", AppliesTo: []string{"internal/auth/**"}, Check: "Check\u202eauth", Body: "Verify signature before decode.", Status: rulecard.StatusApproved, ApprovedBy: "alice", ApprovedAt: now},
		{Name: "muted.md", AppliesTo: []string{"internal/auth/**"}, Body: "Noisy rule", Status: rulecard.StatusActive},
		{Name: "draft.md", AppliesTo: []string{"internal/auth/**"}, Body: "Unapproved draft", Status: rulecard.StatusCandidate},
	}

	pack := contextpack.Compile(contextpack.Input{
		Reviewer:  "alice",
		Cards:     cards,
		Confirmed: map[string]bool{"auth.md": true},
		Signed:    map[string]bool{"auth.md": true},
		Outcomes: map[string]rulecard.Record{
			"muted.md": {Noise: 4},
		},
		Facts: rulecard.ChangeFacts{Files: []string{"internal/auth/verify.go"}},
		Calibration: contextpack.CalibrationSummary{
			Useful: 5, Noise: 1, NoisyCategories: []string{"style"}, ResolutionRate: 0.83,
		},
	})

	require.Equal(t, 1, pack.MutedCount)
	require.Len(t, pack.Selected, 1)
	require.True(t, strings.HasPrefix(pack.Rendered, `<untrusted_personal_context reviewer="alice">`))
	require.True(t, strings.HasSuffix(pack.Rendered, `</untrusted_personal_context>`))
	require.Contains(t, pack.Rendered, "[E3-confirmed, DSSE-signed]")
	require.Contains(t, pack.Rendered, "Checkauth")
	require.NotContains(t, pack.Rendered, "\u202e")
	require.NotContains(t, pack.Rendered, "Noisy rule")
	require.NotContains(t, pack.Rendered, "Unapproved draft")
	require.Contains(t, pack.Rendered, "noisy categories: style")
}
