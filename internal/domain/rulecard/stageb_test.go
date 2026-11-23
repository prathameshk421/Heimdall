package rulecard_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
)

func TestSummarizeOutcomesClassifiesAndSanitizes(t *testing.T) {
	cards := []rulecard.Card{
		{Name: "muted.md", Kind: rulecard.KindInvariant, Status: rulecard.StatusActive, Body: "b"},
		{Name: "overridden.md", Kind: rulecard.KindConvention, Status: rulecard.StatusActive, Body: "b"},
		{Name: "strong.md", Kind: rulecard.KindInvariant, Status: rulecard.StatusActive, Body: "b"},
		{Name: "unused.md", Kind: rulecard.KindJudgement, Status: rulecard.StatusActive, Body: "b"},
		{Name: "rejected.md", Kind: rulecard.KindConvention, Status: rulecard.StatusRejected, Body: "b"},
	}
	counts := map[string]rulecard.OutcomeCounts{
		"muted.md":      {Dismissed: 4, DismissalReasons: []string{"bad\u202ealert", "bad\u202ealert"}},
		"overridden.md": {Kept: 1, Overridden: 2},
		"strong.md":     {Kept: 4, Proven: 3},
	}

	got := rulecard.SummarizeOutcomes(cards, counts)
	require.Len(t, got, 4, "rejected cards must not appear in owner outcomes")
	require.Equal(t, "muted.md", got[0].CardName)
	require.Equal(t, rulecard.OutcomeStatusMuted, got[0].Status)
	require.True(t, got[0].Muted)
	require.Equal(t, []string{"badalert"}, got[0].DismissalReasons, "BiDi controls stripped and reasons deduped")

	require.Equal(t, "overridden.md", got[1].CardName)
	require.Equal(t, rulecard.OutcomeStatusWatch, got[1].Status)
	require.Equal(t, 2, got[1].Overridden)

	require.Equal(t, "strong.md", got[2].CardName)
	require.Equal(t, rulecard.OutcomeStatusStrong, got[2].Status)
	require.Equal(t, 3, got[2].Proven)
	require.Contains(t, got[2].Hint, "proof hint")

	require.Equal(t, "unused.md", got[3].CardName)
	require.Equal(t, rulecard.OutcomeStatusUnused, got[3].Status)
}

func TestExportMarkdownIdempotentAndFiltersUnapproved(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	cards := []rulecard.Card{
		{Name: "auth.md", Kind: rulecard.KindInvariant, AppliesTo: []string{"internal/auth/**"}, Check: "Require bearer token", Proof: "401 test", Body: "Never skip auth.", Status: rulecard.StatusApproved, ApprovedBy: "alice", ApprovedAt: now},
		{Name: "draft.md", Kind: rulecard.KindConvention, Body: "Unapproved candidate", Status: rulecard.StatusCandidate},
		{Name: "nope.md", Kind: rulecard.KindConvention, Body: "Rejected rule", Status: rulecard.StatusRejected},
	}

	existing := "# Team Notes\n\nKeep this untouched.\n"
	first, err := rulecard.ExportMarkdown(rulecard.TargetAgentsMD, cards, existing)
	require.NoError(t, err)
	require.Contains(t, first, "Keep this untouched.")
	require.Contains(t, first, "Never skip auth.")
	require.NotContains(t, first, "Unapproved candidate")
	require.NotContains(t, first, "Rejected rule")

	second, err := rulecard.ExportMarkdown(rulecard.TargetAgentsMD, cards, first)
	require.NoError(t, err)
	require.Equal(t, first, second, "re-exporting into AGENTS.md must be idempotent")

	cursor, err := rulecard.ExportMarkdown(rulecard.TargetCursorMDC, cards, "")
	require.NoError(t, err)
	require.Contains(t, cursor, "globs: internal/auth/**")
	require.Contains(t, cursor, "alwaysApply: false")
}

func TestImportMarkdownProducesCandidateCardsWithInferredGlobs(t *testing.T) {
	raw := "# Rules\n- Always validate tokens in `internal/auth/**` before writing.\n- Never use `time.Sleep` in tests under `internal/app/`.\n"
	cards, err := rulecard.ImportMarkdown("AGENTS.md", raw)
	require.NoError(t, err)
	require.Len(t, cards, 2)
	for _, c := range cards {
		require.Equal(t, rulecard.StatusCandidate, c.Status)
		require.NoError(t, c.Validate())
	}
	require.Equal(t, []string{"internal/auth/**"}, cards[0].AppliesTo)
	require.Equal(t, []string{"internal/app/**"}, cards[1].AppliesTo)
}
