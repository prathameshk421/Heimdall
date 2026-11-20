package bench

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// FileStatus says how a diff changed a file.
type FileStatus string

// The file statuses of a diff.
const (
	FileAdded    FileStatus = "added"
	FileModified FileStatus = "modified"
	FileDeleted  FileStatus = "deleted"
)

// Hunk is one hunk header of a zero-context diff. A count of 0 means a pure insertion (old side) or a pure deletion
// (new side); the start then names the line before the gap.
type Hunk struct {
	OldStart, OldCount int
	NewStart, NewCount int
}

// FileDiff is one file of a diff.
type FileDiff struct {
	Path   string // the new path, or the old path of a deleted file
	Status FileStatus
	Hunks  []Hunk
}

// hunkPattern matches "@@ -a[,b] +c[,d] @@".
var hunkPattern = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@`)

// ParseDiff reads the output of git diff -U0 --no-renames --no-color. Renames must be off, so each file has one path.
func ParseDiff(b []byte) ([]FileDiff, error) {
	var files []FileDiff
	var cur *FileDiff
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			files = append(files, FileDiff{Status: FileModified})
			cur = &files[len(files)-1]
			cur.Path = gitHeaderPath(line)
		case cur == nil:
			continue
		case strings.HasPrefix(line, "new file mode"):
			cur.Status = FileAdded
		case strings.HasPrefix(line, "deleted file mode"):
			cur.Status = FileDeleted
		case strings.HasPrefix(line, "--- a/"):
			cur.Path = diffPath(line, "--- a/")
		case strings.HasPrefix(line, "+++ b/"):
			cur.Path = diffPath(line, "+++ b/")
		case strings.HasPrefix(line, "@@ "):
			h, err := parseHunk(line)
			if err != nil {
				return nil, fmt.Errorf("diff line %d: %w", n, err)
			}
			cur.Hunks = append(cur.Hunks, h)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read diff: %w", err)
	}
	return files, nil
}

// gitHeaderPath takes the b/ path of "diff --git a/x b/x". It is a first guess for files with no ---/+++ lines
// (binary files); the ---/+++ lines replace it when present, and they do not split on spaces.
func gitHeaderPath(line string) string {
	rest := strings.TrimPrefix(line, "diff --git ")
	if i := strings.LastIndex(rest, " b/"); i >= 0 {
		return rest[i+len(" b/"):]
	}
	return rest
}

// diffPath reads a ---/+++ path. Git ends the line with a tab when the path holds a space.
func diffPath(line, prefix string) string {
	return strings.TrimSuffix(strings.TrimPrefix(line, prefix), "\t")
}

func parseHunk(line string) (Hunk, error) {
	m := hunkPattern.FindStringSubmatch(line)
	if m == nil {
		return Hunk{}, fmt.Errorf("hunk header %q", line)
	}
	var v [4]int
	for i, s := range m[1:] {
		if s == "" {
			v[i] = 1 // git omits a count of 1
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return Hunk{}, fmt.Errorf("hunk header %q: %w", line, err)
		}
		v[i] = n
	}
	return Hunk{OldStart: v[0], OldCount: v[1], NewStart: v[2], NewCount: v[3]}, nil
}

// isGoTest says whether path is a Go test file.
func isGoTest(path string) bool { return strings.HasSuffix(path, "_test.go") }

// isGoSource says whether path is Go source code that ships: not a test, not test data and not vendored code.
func isGoSource(path string) bool {
	if !strings.HasSuffix(path, ".go") || isGoTest(path) {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "testdata" || part == "vendor" {
			return false
		}
	}
	return true
}

// GoTestFiles returns the Go test files the diff added or changed, in diff order. A deleted test cannot run.
func GoTestFiles(files []FileDiff) []string {
	var out []string
	for _, f := range files {
		if f.Status != FileDeleted && isGoTest(f.Path) {
			out = append(out, f.Path)
		}
	}
	return out
}

// GoTruth returns the old-side line spans of the diff in Go source files (doc.go, "Truth"). Added files have no old
// lines and give no truth.
func GoTruth(files []FileDiff) []FileTruth {
	var out []FileTruth
	for _, f := range files {
		if f.Status == FileAdded || !isGoSource(f.Path) || len(f.Hunks) == 0 {
			continue
		}
		ft := FileTruth{File: f.Path}
		for _, h := range f.Hunks {
			ft.Lines = append(ft.Lines, oldSpan(h))
		}
		out = append(out, ft)
	}
	return out
}

// oldSpan is the old-side span of a hunk. An insertion after line N is the span N to N+1, clipped to line 1.
func oldSpan(h Hunk) string {
	start, end := h.OldStart, h.OldStart+h.OldCount-1
	if h.OldCount == 0 {
		start, end = max(h.OldStart, 1), h.OldStart+1
	}
	if start == end {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "-" + strconv.Itoa(end)
}

// AddedLines returns the new-side line numbers that the diff added to the file, in order.
func AddedLines(f FileDiff) []int {
	var out []int
	for _, h := range f.Hunks {
		for i := range h.NewCount {
			out = append(out, h.NewStart+i)
		}
	}
	return out
}

// CommitParents returns the parent SHAs from a raw commit object (git cat-file -p), in order.
func CommitParents(raw []byte) []string {
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			break // the header ends at the first blank line; the message follows
		}
		if sha, ok := strings.CutPrefix(line, "parent "); ok {
			out = append(out, sha)
		}
	}
	return out
}
