package testintegrity

import (
	"errors"
	"fmt"
	"strings"
)

// maxDiffCells bounds the line diff of RevertLines: the changed middle of the two files, base lines times head lines.
// 4M cells is a table of 16 MB, and a change that large is no suspected line range anyway.
const maxDiffCells = 4_000_000

// RevertLines returns head with the suspected lines first to last (1-based, on the head) put back to their base text.
// It follows the line diff from base to head, not the line numbers, so a change that inserts lines above the range
// still reverts the right code. Each hunk of the diff that the range touches is reverted:
//
//   - a hunk that replaced base lines goes back to those base lines, whole, because part of a replaced block has no
//     base text of its own;
//   - a hunk that only inserted lines loses the inserted lines inside the range, and keeps the others;
//   - a hunk that only deleted base lines between two lines of the range gets them back.
//
// A hunk outside the range stays as the head has it. When the range touches no hunk, the result is head unchanged:
// the lines are the same at the base, and the caller must not call that a fixed tree. A new file is an empty base.
// A file that ends with a newline keeps it. A range outside the head, or a change too large to diff, is an error.
func RevertLines(head, base string, first, last int) (string, error) {
	h, b := splitLines(head), splitLines(base)
	if first < 1 || last < first || last > len(h) {
		return "", fmt.Errorf("lines %d-%d: want 1 <= first <= last <= %d lines at the head", first, last, len(h))
	}
	hunks, err := lineHunks(b, h)
	if err != nil {
		return "", err
	}
	lo, hi := first-1, last // the range as a 0-based half-open span of head lines
	var out []string
	next := 0 // the next head line not yet written
	for _, k := range hunks {
		out = append(out, h[next:k.newStart]...) // equal lines before the hunk
		out = append(out, revertHunk(k, h, b, lo, hi)...)
		next = k.newEnd
	}
	out = append(out, h[next:]...)
	if len(out) == 0 {
		return "", nil
	}
	text := strings.Join(out, "\n")
	if strings.HasSuffix(head, "\n") || (head == "" && strings.HasSuffix(base, "\n")) {
		text += "\n"
	}
	return text, nil
}

// revertHunk returns the lines one hunk writes: its base side when the range touches it, else its head side.
func revertHunk(k hunk, h, b []string, lo, hi int) []string {
	switch {
	case k.oldStart == k.oldEnd: // an insertion: drop the inserted lines inside the range
		var keep []string
		for i := k.newStart; i < k.newEnd; i++ {
			if i < lo || i >= hi {
				keep = append(keep, h[i])
			}
		}
		return keep
	case k.newStart == k.newEnd: // a deletion between head lines newStart-1 and newStart
		if lo < k.newStart && k.newStart < hi {
			return b[k.oldStart:k.oldEnd]
		}
		return nil
	case k.newStart < hi && lo < k.newEnd: // a replacement the range touches
		return b[k.oldStart:k.oldEnd]
	}
	return h[k.newStart:k.newEnd]
}

// hunk is one change of the line diff, as 0-based half-open spans of base (old) and head (new) lines.
type hunk struct {
	oldStart, oldEnd int
	newStart, newEnd int
}

// lineHunks is the line diff from old to new: the longest common subsequence of the changed middle, after the common
// prefix and suffix are cut off. A middle of more than maxDiffCells is refused.
func lineHunks(old, cur []string) ([]hunk, error) {
	pre, suf := commonEnds(old, cur)
	a, c := old[pre:len(old)-suf], cur[pre:len(cur)-suf]
	if len(a) == 0 && len(c) == 0 {
		return nil, nil
	}
	if int64(len(a)+1)*int64(len(c)+1) > maxDiffCells {
		return nil, errors.New("the change is too large to revert by lines")
	}
	return lcsHunks(a, c, pre), nil
}

// commonEnds is the length of the common prefix of old and cur, and of their common suffix after that prefix.
func commonEnds(old, cur []string) (pre, suf int) {
	for pre < len(old) && pre < len(cur) && old[pre] == cur[pre] {
		pre++
	}
	for suf < len(old)-pre && suf < len(cur)-pre && old[len(old)-1-suf] == cur[len(cur)-1-suf] {
		suf++
	}
	return pre, suf
}

// lcsTable holds at [i*w+j] the common subsequence length of a[i:] and c[j:], in one flat table with w = len(c)+1.
func lcsTable(a, c []string) []int32 {
	w := len(c) + 1
	lcs := make([]int32, (len(a)+1)*w)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(c) - 1; j >= 0; j-- {
			if a[i] == c[j] {
				lcs[i*w+j] = lcs[(i+1)*w+j+1] + 1
			} else {
				lcs[i*w+j] = max(lcs[(i+1)*w+j], lcs[i*w+j+1])
			}
		}
	}
	return lcs
}

// lcsHunks walks the common subsequence of a and c and returns the runs between its lines as hunks, shifted by pre.
func lcsHunks(a, c []string, pre int) []hunk {
	lcs, w := lcsTable(a, c), len(c)+1
	var out []hunk
	open := false
	var cur0 hunk
	i, j := 0, 0
	flush := func() {
		if open {
			cur0.oldEnd, cur0.newEnd = pre+i, pre+j
			out = append(out, cur0)
			open = false
		}
	}
	for i < len(a) || j < len(c) {
		switch {
		case i < len(a) && j < len(c) && a[i] == c[j]:
			flush()
			i++
			j++
			continue
		case !open:
			cur0, open = hunk{oldStart: pre + i, newStart: pre + j}, true
		}
		if j < len(c) && (i == len(a) || lcs[i*w+j+1] >= lcs[(i+1)*w+j]) {
			j++
		} else {
			i++
		}
	}
	flush()
	return out
}

// splitLines splits text into lines, without the final empty line a trailing newline leaves.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
