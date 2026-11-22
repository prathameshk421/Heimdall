package testintegrity_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	ti "github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testintegrity"
)

func fileText(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

func TestRevertLinesPutsTheSuspectedChangeBackToTheBase(t *testing.T) {
	base := fileText("a", "b", "c", "d")
	head := fileText("a", "B", "c", "d")
	got, err := ti.RevertLines(head, base, 2, 2)
	require.NoError(t, err)
	require.Equal(t, base, got)
}

func TestRevertLinesFollowsTheDiffWhenLinesMoved(t *testing.T) {
	// testify-1207: the change inserts lines above the suspected ones, so the base text of the same line numbers is
	// other code. The revert must take the base side of the hunk, not the base lines at the same numbers.
	base := fileText("func On() {", "\tm.mutex.Lock()", "\tdefer m.mutex.Unlock()", "}")
	head := fileText("func On() {", "\t// reset", "\tm.mutex = &sync.Mutex{}", "\tm.mutex.Lock()", "\tdefer m.mutex.Unlock()", "}")
	got, err := ti.RevertLines(head, base, 2, 3)
	require.NoError(t, err)
	require.Equal(t, base, got, "the inserted lines go, and nothing else changes")
}

func TestRevertLinesKeepsChangesOutsideTheRange(t *testing.T) {
	base := fileText("one", "two", "three", "four", "five")
	head := fileText("ONE", "two", "three", "FOUR", "five", "six")
	got, err := ti.RevertLines(head, base, 4, 4)
	require.NoError(t, err)
	require.Equal(t, fileText("ONE", "two", "three", "four", "five", "six"), got)
}

func TestRevertLinesTakesAWholeHunkThatTheRangeTouches(t *testing.T) {
	// One hunk replaces two base lines with three head lines. A range on its middle line reverts the whole hunk,
	// because a part of a replaced block has no base text of its own.
	base := fileText("x", "old1", "old2", "y")
	head := fileText("x", "new1", "new2", "new3", "y")
	got, err := ti.RevertLines(head, base, 3, 3)
	require.NoError(t, err)
	require.Equal(t, base, got)
}

func TestRevertLinesRestoresADeletionInsideTheRange(t *testing.T) {
	base := fileText("a", "gone", "b", "c")
	head := fileText("a", "b", "c")
	got, err := ti.RevertLines(head, base, 1, 2)
	require.NoError(t, err)
	require.Equal(t, base, got)
}

func TestRevertLinesWithNoChangeInTheRangeReturnsTheHead(t *testing.T) {
	base := fileText("a", "b", "c")
	head := fileText("a", "b", "C")
	got, err := ti.RevertLines(head, base, 1, 2)
	require.NoError(t, err)
	require.Equal(t, head, got, "the lines are the same at the base, so there is nothing to revert")
}

func TestRevertLinesOfANewFileIsTheEmptyBase(t *testing.T) {
	head := fileText("package x", "func F() {}")
	got, err := ti.RevertLines(head, "", 2, 2)
	require.NoError(t, err)
	require.Equal(t, fileText("package x"), got)
}

func TestRevertLinesKeepsAFileWithNoFinalNewline(t *testing.T) {
	got, err := ti.RevertLines("a\nB", "a\nb", 2, 2)
	require.NoError(t, err)
	require.Equal(t, "a\nb", got)
}

func TestRevertLinesRefusesARangeOutsideTheHead(t *testing.T) {
	for name, r := range map[string][2]int{"zero": {0, 1}, "reversed": {3, 2}, "past the end": {2, 9}} {
		t.Run(name, func(t *testing.T) {
			_, err := ti.RevertLines(fileText("a", "b", "c"), fileText("a"), r[0], r[1])
			require.Error(t, err)
		})
	}
}

func TestRevertLinesRefusesAChangeTooLargeToDiff(t *testing.T) {
	var b, h strings.Builder
	for i := range 3000 {
		b.WriteString("base " + strings.Repeat("x", i%7) + "\n")
		h.WriteString("head " + strings.Repeat("y", i%5) + "\n")
	}
	_, err := ti.RevertLines(h.String(), b.String(), 1, 1)
	require.ErrorContains(t, err, "too large")
}
