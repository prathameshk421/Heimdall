package external_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

const older = "older001"

func add(n int) []external.Op  { return rep(external.OpAdd, n) }
func keep(n int) []external.Op { return rep(external.OpKeep, n) }
func del(n int) []external.Op  { return rep(external.OpDel, n) }

func rep(o external.Op, n int) []external.Op {
	out := make([]external.Op, n)
	for i := range out {
		out[i] = o
	}
	return out
}

func join(parts ...[]external.Op) []external.Op {
	var out []external.Op
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// oldComment is a comment made on the older commit, still anchored there, at one line.
func oldComment(path string, line int) external.Comment {
	c := inline(7, path, line)
	c.CommitID, c.OriginalCommitID = older, older
	return c
}

// TestMapThroughDiff: a comment on an older commit moves to head, or is dropped as outdated with a cause.
func TestMapThroughDiff(t *testing.T) {
	// Two lines are added after old line 4 and old line 20 is removed: @@ -3,3 +3,5 @@ and @@ -19,3 +21,2 @@.
	hunks := []external.Hunk{
		{OldStart: 3, NewStart: 3, Ops: join(keep(2), add(2), keep(1))},
		{OldStart: 19, NewStart: 21, Ops: join(keep(1), del(1), keep(1))},
	}
	file := func(old, now string, h ...external.Hunk) external.Diffs {
		return external.Diffs{older: {{OldPath: old, NewPath: now, Hunks: h}}}
	}
	cases := []struct {
		name       string
		c          external.Comment
		diffs      external.Diffs
		start, end int
		path       string
		cause      external.Cause
	}{
		{"line before every hunk stays", oldComment("a.go", 1), file("a.go", "a.go", hunks...), 1, 1, "a.go", ""},
		{"a line moved down by an insert above it", oldComment("a.go", 5), file("a.go", "a.go", hunks...), 7, 7, "a.go", ""},
		{"a context line inside the hunk moves too", oldComment("a.go", 4), file("a.go", "a.go", hunks...), 4, 4, "a.go", ""},
		{"a line after both hunks moves by the net change", oldComment("a.go", 30), file("a.go", "a.go", hunks...), 31, 31, "a.go", ""},
		{"a deleted line is outdated", oldComment("a.go", 20), file("a.go", "a.go", hunks...), 0, 0, "", external.CauseLineDeleted},
		{"a renamed file maps to the new path", oldComment("old/a.go", 5), file("old/a.go", "new/a.go", hunks...), 7, 7, "new/a.go", ""},
		{"a rename with no hunk keeps the line", oldComment("old/a.go", 5), file("old/a.go", "new/a.go"), 5, 5, "new/a.go", ""},
		{"a file not in the diff is unchanged", oldComment("b.go", 5), file("a.go", "a.go", hunks...), 5, 5, "b.go", ""},
		{"a deleted file is outdated", oldComment("a.go", 5), external.Diffs{older: {{OldPath: "a.go", Deleted: true}}}, 0, 0, "", external.CauseFileDeleted},
		{"no diff for the older commit is outdated", oldComment("a.go", 5), external.Diffs{}, 0, 0, "", external.CauseNoDiff},
		{"a range keeps the lines that survive", func() external.Comment {
			c := oldComment("a.go", 21)
			c.StartLine, c.OriginalStartLine = 19, 19
			return c
		}(), file("a.go", "a.go", hunks...), 21, 22, "a.go", ""},
		{"a range of only deleted lines is outdated", func() external.Comment {
			c := oldComment("a.go", 20)
			c.StartLine, c.OriginalStartLine = 20, 20
			return c
		}(), file("a.go", "a.go", hunks...), 0, 0, "", external.CauseLineDeleted},
		{"a comment GitHub marked outdated keeps its original pair and is marked",
			func() external.Comment {
				c := oldComment("a.go", 5)
				c.Line, c.StartLine, c.CommitID, c.Outdated = 0, 0, head, true
				return c
			}(), file("a.go", "a.go", hunks...), 5, 5, "a.go", ""},
		{"diff hunks out of order cannot be used", oldComment("a.go", 5),
			file("a.go", "a.go", hunks[1], hunks[0]), 0, 0, "", external.CauseBadDiff},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pr := newPR()
			pr.Diffs = tc.diffs
			got := pr.Ingest(tc.c)
			if tc.cause != "" {
				require.Equal(t, external.OutcomeDropped, got.Outcome, "%+v", got)
				require.Equal(t, proof.DropOutdated, got.Drop.Reason)
				require.Equal(t, tc.cause, got.Drop.Cause)
				return
			}
			require.Equal(t, external.OutcomeCandidate, got.Outcome, "%+v", got)
			require.Equal(t, tc.path, got.Candidate.Path)
			require.Equal(t, tc.start, got.Candidate.StartLine)
			require.Equal(t, tc.end, got.Candidate.EndLine)
			require.Equal(t, external.SideRight, got.Candidate.Side)
		})
	}
}

// TestOutdatedKeepsItsAnchor: a comment that GitHub calls outdated, or whose line is null, is not dropped. It keeps
// its original pair on the original commit and says so, because 145 of the 411 real comments are like that and a drop
// loses a finding the tool did post.
func TestOutdatedKeepsItsAnchor(t *testing.T) {
	hunks := []external.Hunk{{OldStart: 3, NewStart: 3, Ops: join(keep(2), add(2), keep(1))}}
	pr := newPR()
	pr.Diffs = external.Diffs{older: {{OldPath: "a.go", NewPath: "a.go", Hunks: hunks}}}

	t.Run("a null line is anchored on the original commit", func(t *testing.T) {
		c := oldComment("a.go", 5)
		c.Line, c.StartLine, c.CommitID = 0, 0, head
		got := pr.Ingest(c)
		require.Equal(t, external.OutcomeCandidate, got.Outcome, "%+v", got)
		require.True(t, got.Candidate.Outdated)
		require.Equal(t, older, got.Candidate.CommitID)
		require.Equal(t, 5, got.Candidate.StartLine, "the original line, not the mapped one")
		require.Equal(t, 5, got.Candidate.EndLine)
	})

	t.Run("the outdated flag does the same", func(t *testing.T) {
		c := oldComment("a.go", 5)
		c.Outdated = true
		got := pr.Ingest(c)
		require.Equal(t, external.OutcomeCandidate, got.Outcome, "%+v", got)
		require.True(t, got.Candidate.Outdated)
		require.Equal(t, older, got.Candidate.CommitID)
	})

	t.Run("a comment at head is not outdated", func(t *testing.T) {
		got := pr.Ingest(inline(1, "a.go", 5))
		require.Equal(t, external.OutcomeCandidate, got.Outcome, "%+v", got)
		require.False(t, got.Candidate.Outdated)
		require.Empty(t, got.Candidate.CommitID)
	})

	t.Run("a null line with no original line is still no location", func(t *testing.T) {
		c := inline(1, "a.go", 5)
		c.Line, c.StartLine, c.OriginalLine, c.OriginalStartLine = 0, 0, 0, 0
		got := pr.Ingest(c)
		require.Equal(t, external.OutcomeDropped, got.Outcome, "%+v", got)
		require.Equal(t, external.CauseNoLine, got.Drop.Cause)
	})
}

func TestMapInsertAtTopAndPureInsert(t *testing.T) {
	// @@ -0,0 +1,3 @@ adds three lines at the top. @@ -10,0 +14,1 @@ adds one after old line 10.
	hunks := []external.Hunk{{OldStart: 0, NewStart: 1, Ops: add(3)}, {OldStart: 10, NewStart: 14, Ops: add(1)}}
	pr := newPR()
	pr.Diffs = external.Diffs{older: {{OldPath: "a.go", NewPath: "a.go", Hunks: hunks}}}
	for old, want := range map[int]int{1: 4, 10: 13, 11: 15} {
		got := pr.Ingest(oldComment("a.go", old))
		require.Equal(t, external.OutcomeCandidate, got.Outcome, "%+v", got)
		require.Equal(t, want, got.Candidate.StartLine, "old line %d", old)
	}
}

func TestHeadCommitNeedsNoDiff(t *testing.T) {
	got := newPR().Ingest(inline(1, "a.go", 12))
	require.Equal(t, external.OutcomeCandidate, got.Outcome)
	require.Equal(t, 12, got.Candidate.StartLine)
}

func TestLeftSideIsNotMapped(t *testing.T) {
	c := oldComment("a.go", 5)
	c.Side = "LEFT"
	got := newPR().Ingest(c)
	require.Equal(t, external.OutcomeCandidate, got.Outcome, "%+v", got)
	require.Equal(t, external.SideLeft, got.Candidate.Side)
	require.Equal(t, 5, got.Candidate.StartLine)
}

// TestNoLocation: a comment with no file, no line or an absurd range is dropped as no_location with a cause.
func TestNoLocation(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(c *external.Comment)
		cause external.Cause
	}{
		{"no path", func(c *external.Comment) { c.Path = "" }, external.CauseNoPath},
		{"file level comment", func(c *external.Comment) { c.Line, c.OriginalLine = 0, 0 }, external.CauseNoLine},
		{"negative line", func(c *external.Comment) { c.Line, c.OriginalLine = -4, -4 }, external.CauseNoLine},
		{"a range over the bound", func(c *external.Comment) { c.StartLine = c.Line - external.MaxRangeLines - 1 }, external.CauseRangeTooLarge},
		{"a review body", func(c *external.Comment) { c.Kind = external.KindReview }, external.CauseSummary},
		{"an unclean path", func(c *external.Comment) { c.Path = "../etc/passwd" }, external.CauseNoPath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := inline(1, "a.go", 900)
			tc.edit(&c)
			got := newPR().Ingest(c)
			require.Equal(t, external.OutcomeDropped, got.Outcome, "%+v", got)
			require.Equal(t, proof.DropNoLocation, got.Drop.Reason)
			require.Equal(t, tc.cause, got.Drop.Cause)
			require.Equal(t, external.ToolCodeRabbit, got.Tool)
		})
	}
}
