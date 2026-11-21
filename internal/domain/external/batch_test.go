package external_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

func TestBatchCountsAndDedupes(t *testing.T) {
	pr := newPR()
	pr.Diffs = external.Diffs{older: {{OldPath: "a.go", NewPath: "a.go", Hunks: []external.Hunk{{OldStart: 5, NewStart: 5, Ops: del(1)}}}}}
	outdated := oldComment("a.go", 5)
	outdated.ID = 2
	noLoc := inline(3, "", 0)
	style := inline(4, "b.go", 3)
	style.Body = "_🧹 Nitpick_ | _🔵 Trivial_\n\n**Rename the variable.**"
	bot := inline(5, "a.go", 3)
	bot.UserLogin = "dependabot[bot]"
	dup := inline(1, "a.go", 3)
	b := pr.IngestAll([]external.Comment{inline(1, "a.go", 3), dup, outdated, noLoc, style, bot})
	require.Len(t, b.Candidates, 2)
	require.Equal(t, 6, b.Counts.Read)
	require.Equal(t, 1, b.Counts.Duplicates)
	require.Equal(t, 2, b.Counts.Candidates)
	require.Equal(t, 1, b.Counts.Style)
	require.Equal(t, 1, b.Counts.Outdated)
	require.Equal(t, 1, b.Counts.NoLocation)
	require.Equal(t, 1, b.Counts.Ignored)
	require.Equal(t, map[external.IgnoreReason]int{external.IgnoreUnknownAuthor: 1}, b.Ignored)
	require.Equal(t, []external.Drop{
		{CommentID: 2, Tool: external.ToolCodeRabbit, Reason: proof.DropOutdated, Cause: external.CauseLineDeleted},
		{CommentID: 3, Tool: external.ToolCodeRabbit, Reason: proof.DropNoLocation, Cause: external.CauseNoPath},
	}, b.Drops)
	// Most severe first: the minor finding before the nitpick.
	require.Equal(t, int64(1), b.Candidates[0].CommentID)
}

func TestBatchBounds(t *testing.T) {
	t.Run("candidates are capped, most severe kept", func(t *testing.T) {
		var cs []external.Comment
		for i := 1; i <= external.MaxCandidates+10; i++ {
			cs = append(cs, inline(int64(i), "a.go", i))
		}
		crit := inline(int64(external.MaxCandidates+11), "a.go", 1)
		crit.Body = "_⚠️ Potential issue_ | _🔴 Critical_\n\n**This breaks every call.**"
		cs = append(cs, crit)
		b := newPR().IngestAll(cs)
		require.Len(t, b.Candidates, external.MaxCandidates)
		require.Equal(t, 11, b.Counts.Overflow)
		require.Equal(t, crit.ID, b.Candidates[0].CommentID)
	})
	t.Run("comments beyond the read bound are counted, not read", func(t *testing.T) {
		cs := make([]external.Comment, external.MaxComments+5)
		for i := range cs {
			cs[i] = inline(int64(i+1), "a.go", 1)
			cs[i].UserLogin = "nobody"
		}
		b := newPR().IngestAll(cs)
		require.Equal(t, external.MaxComments, b.Counts.Read)
		require.Equal(t, 5, b.Counts.Unread)
	})
	t.Run("a huge body is cut before it is read", func(t *testing.T) {
		c := inline(1, "a.go", 1)
		c.Body = "_⚠️ Potential issue_ | _🟡 Minor_\n\n" + strings.Repeat("é", external.MaxBodyBytes)
		got := newPR().Ingest(c)
		require.Equal(t, external.OutcomeCandidate, got.Outcome)
		require.LessOrEqual(t, len([]rune(got.Candidate.Claim)), 600)
	})
}
