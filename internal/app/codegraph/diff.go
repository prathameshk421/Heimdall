package codegraph

import (
	"strconv"
	"strings"
)

// fileDiff is what one file gained in a unified diff, on the new side.
type fileDiff struct {
	lines map[int]bool // new-side line numbers of added lines
	added []string
	gone  []string // removed lines
}

// parseDiff reads a unified diff. It follows the hunk counts, so an added line that starts with "++" is not taken for
// a file header.
func parseDiff(diff string) map[string]*fileDiff {
	out := map[string]*fileDiff{}
	var cur *fileDiff
	oldLeft, newLeft, newLine := 0, 0, 0
	for _, line := range strings.Split(diff, "\n") {
		if oldLeft > 0 || newLeft > 0 {
			switch {
			case strings.HasPrefix(line, "+"):
				cur.lines[newLine] = true
				cur.added = append(cur.added, line[1:])
				newLine++
				newLeft--
			case strings.HasPrefix(line, "-"):
				if cur != nil {
					cur.gone = append(cur.gone, line[1:])
				}
				oldLeft--
			case strings.HasPrefix(line, "\\"):
			default:
				oldLeft, newLeft, newLine = oldLeft-1, newLeft-1, newLine+1
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "+++ "):
			cur = openFile(out, strings.TrimSpace(strings.TrimPrefix(line, "+++ ")))
		case strings.HasPrefix(line, "@@") && cur != nil:
			oldLeft, newLeft, newLine = parseHunk(line)
		}
	}
	return out
}

func openFile(out map[string]*fileDiff, name string) *fileDiff {
	name, _, _ = strings.Cut(name, "\t")
	if name == "/dev/null" {
		return nil
	}
	name = strings.TrimPrefix(name, "b/")
	if out[name] == nil {
		out[name] = &fileDiff{lines: map[int]bool{}}
	}
	return out[name]
}

// parseHunk reads "@@ -a,b +c,d @@" and returns b, d and c.
func parseHunk(line string) (oldCount, newCount, newStart int) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return 0, 0, 0
	}
	_, oldCount = rangeOf(fields[1])
	newStart, newCount = rangeOf(fields[2])
	return oldCount, newCount, newStart
}

func rangeOf(s string) (start, count int) {
	count = 1
	a, b, hasCount := strings.Cut(strings.TrimLeft(s, "+-"), ",")
	start, _ = strconv.Atoi(a)
	if hasCount {
		count, _ = strconv.Atoi(b)
	}
	return start, count
}
