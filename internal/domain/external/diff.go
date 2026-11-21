package external

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Op is one line of a diff hunk.
type Op byte

// The three kinds of line.
const (
	OpKeep Op = ' ' // a context line: in the old file and in the new file
	OpDel  Op = '-' // only in the old file
	OpAdd  Op = '+' // only in the new file
)

// Hunk is one hunk of a diff, from the old file to the new one. OldStart and NewStart are the numbers in the header
// "@@ -OldStart,n +NewStart,m @@". When the hunk has no old line, OldStart is the line after which it inserts.
type Hunk struct {
	OldStart int
	NewStart int
	Ops      []Op
}

// FileDiff is the change to one file between the commit a comment was made on and head. A rename has two different
// paths. A deleted file has Deleted set. A file that is not in the list is unchanged.
type FileDiff struct {
	OldPath string
	NewPath string
	Deleted bool
	Hunks   []Hunk
}

// Diffs holds, for each older commit that a comment may sit on, the diff from that commit to head.
type Diffs map[string][]FileDiff

// ErrBadPatch is returned by ParsePatch for a patch it cannot read.
var ErrBadPatch = errors.New("external: bad patch")

// ParsePatch reads the "patch" text GitHub gives for one file (the hunks of a unified diff) into hunks. Text before the
// first "@@" is skipped. A "\ No newline" line is skipped. A hunk whose lines do not add up to its header is refused.
func ParsePatch(patch string) ([]Hunk, error) {
	if len(patch) > MaxPatchBytes {
		return nil, fmt.Errorf("%w: %d bytes, the limit is %d", ErrBadPatch, len(patch), MaxPatchBytes)
	}
	var hunks []Hunk
	var counts [][2]int // the old and new line counts that each header promises
	text := strings.TrimSuffix(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "@@") {
			h, n, err := parseHeader(line)
			if err != nil {
				return nil, err
			}
			hunks, counts = append(hunks, h), append(counts, n)
			continue
		}
		if len(hunks) == 0 || strings.HasPrefix(line, `\`) {
			continue
		}
		op, ok := lineOp(line)
		if !ok {
			return nil, fmt.Errorf("%w: line %q has no diff prefix", ErrBadPatch, truncate(line))
		}
		last := &hunks[len(hunks)-1]
		last.Ops = append(last.Ops, op)
	}
	return hunks, checkCounts(hunks, counts)
}

func lineOp(line string) (Op, bool) {
	if line == "" {
		return OpKeep, true // an editor trimmed the one space of an empty context line
	}
	switch Op(line[0]) {
	case OpKeep, OpDel, OpAdd:
		return Op(line[0]), true
	}
	return 0, false
}

// checkCounts refuses a hunk whose lines do not match its header.
func checkCounts(hunks []Hunk, counts [][2]int) error {
	for i, h := range hunks {
		var o, n int
		for _, op := range h.Ops {
			if op != OpAdd {
				o++
			}
			if op != OpDel {
				n++
			}
		}
		if o != counts[i][0] || n != counts[i][1] {
			return fmt.Errorf("%w: hunk %d has %d old and %d new lines, its header says %d and %d",
				ErrBadPatch, i+1, o, n, counts[i][0], counts[i][1])
		}
	}
	return nil
}

// parseHeader reads "@@ -a,b +c,d @@ text".
func parseHeader(line string) (Hunk, [2]int, error) {
	fields := strings.Fields(line)
	if len(fields) < 3 || fields[0] != "@@" || !strings.HasPrefix(fields[1], "-") || !strings.HasPrefix(fields[2], "+") {
		return Hunk{}, [2]int{}, fmt.Errorf("%w: header %q", ErrBadPatch, truncate(line))
	}
	oldStart, oldN, err := parseRange(fields[1][1:])
	if err != nil {
		return Hunk{}, [2]int{}, fmt.Errorf("%w: header %q", ErrBadPatch, truncate(line))
	}
	newStart, newN, err := parseRange(fields[2][1:])
	if err != nil {
		return Hunk{}, [2]int{}, fmt.Errorf("%w: header %q", ErrBadPatch, truncate(line))
	}
	return Hunk{OldStart: oldStart, NewStart: newStart}, [2]int{oldN, newN}, nil
}

// parseRange reads "start" or "start,count". A missing count is 1.
func parseRange(s string) (start, count int, err error) {
	a, b, hasCount := strings.Cut(s, ",")
	if start, err = strconv.Atoi(a); err != nil || start < 0 || start > maxLine {
		return 0, 0, ErrBadPatch
	}
	count = 1
	if hasCount {
		if count, err = strconv.Atoi(b); err != nil || count < 0 || count > MaxDiffOps {
			return 0, 0, ErrBadPatch
		}
	}
	return start, count, nil
}

func truncate(s string) string {
	if len(s) > 40 {
		return strings.ToValidUTF8(s[:40], "") + "..."
	}
	return s
}

// sweep maps a range of old lines through hunks, one hunk at a time.
type sweep struct {
	start, end  int
	offset      int // new line minus old line, for the lines after the hunks seen so far
	next        int // the first old line not yet seen
	first, last int // the new numbers of the first and the last old line of the range that survive
}

func (w *sweep) consider(newLine int) {
	if w.first == 0 {
		w.first = newLine
	}
	w.last = newLine
}

// plain keeps the old lines from..to that no hunk touches, when they are in the range.
func (w *sweep) plain(from, to int) {
	for l := max(from, w.start); l <= min(to, w.end); l++ {
		w.consider(l + w.offset)
	}
}

// hunk takes one hunk. It returns false when the hunk cannot be used.
func (w *sweep) hunk(h Hunk) bool {
	oldFirst := h.OldStart
	if !hasOld(h) {
		oldFirst++
	}
	if h.OldStart < 0 || oldFirst < w.next {
		return false
	}
	w.plain(w.next, oldFirst-1)
	oldCur, newCur := oldFirst, oldFirst+w.offset
	for _, op := range h.Ops {
		switch op {
		case OpKeep:
			if oldCur >= w.start && oldCur <= w.end {
				w.consider(newCur)
			}
			oldCur++
			newCur++
		case OpDel:
			oldCur++
		case OpAdd:
			newCur++
		default:
			return false
		}
	}
	w.next, w.offset = oldCur, newCur-oldCur
	return true
}

// mapRange maps the old lines start to end through the hunks and returns the new numbers of the first and of the last
// old line that still exists. ok is false when none exists. bad is true when the hunks cannot be used: they overlap,
// they are out of order, an op is not one of the three, or they hold more than MaxDiffOps lines.
func mapRange(hunks []Hunk, start, end int) (first, last int, ok, bad bool) {
	w := sweep{start: start, end: end, next: 1}
	total := 0
	for _, h := range hunks {
		total += len(h.Ops)
		if total > MaxDiffOps || !w.hunk(h) {
			return 0, 0, false, true
		}
	}
	w.plain(w.next, end)
	return w.first, w.last, w.first != 0, false
}

func hasOld(h Hunk) bool {
	for _, op := range h.Ops {
		if op != OpAdd {
			return true
		}
	}
	return false
}
