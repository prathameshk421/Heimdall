package coverage

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
)

// ErrTooLarge means the input is over the caller's byte limit, or it would expand past MaxLines.
var ErrTooLarge = errors.New("coverage report is too large")

// ErrMalformed means the input is not a well-formed report of its format.
var ErrMalformed = errors.New("malformed coverage report")

// MaxLine bounds one line number in any report. A larger number is refused, so an untrusted report cannot
// name a line the diff never has.
const MaxLine = 10_000_000

// MaxLines bounds the instrumented lines one report may hold across every file. It stops a small input
// from expanding into a huge result through long Go cover spans.
const MaxLines = 4_000_000

// maxHits bounds one hit count. Together with MaxLines it keeps merged counts inside an int.
const maxHits = 1 << 40

// Lines maps a line number to the number of times it ran. A line in the map is instrumented: a zero
// count means the line exists in the report and did not run. A line that is absent is not instrumented.
type Lines map[int]int

// Result is one parsed report: every instrumented file and the lines that ran in it.
type Result struct {
	Files map[string]Lines
}

// Options bounds one input and rebases its paths. The caller passes it, because the size a report may
// have and the root its paths are relative to are properties of the caller, not of the format.
type Options struct {
	// MaxBytes is the largest input accepted. It must be positive.
	MaxBytes int
	// Prefix is removed from every path after normalization. It turns a Go module path or a runner
	// workspace into the repository-relative path a diff uses. It is normalized the same way as a
	// file path. Empty means no rebase.
	Prefix string
}

// Counts is the outcome of one diff-coverage measurement.
type Counts struct {
	Covered         int
	Uncovered       int
	NotInstrumented int
}

// Percent is covered over the instrumented changed lines, as a value from 0 to 100. With no
// instrumented changed line it is 100: a line that is not instrumented does not count against coverage.
func (c Counts) Percent() float64 {
	instrumented := c.Covered + c.Uncovered
	if instrumented == 0 {
		return 100
	}
	return float64(c.Covered) * 100 / float64(instrumented)
}

// FileDiff is the measurement for one changed file.
type FileDiff struct {
	Path string
	Counts
}

// DiffReport is the measurement for every changed file and the total. Files is sorted by path.
type DiffReport struct {
	Files   []FileDiff
	Total   Counts
	Percent float64
}

// Diff measures how many of the changed lines a report covers. changed maps a file path to its changed
// line numbers; paths go through the same normalization as report paths, so ./a.go and a.go are one file.
// A changed line that is absent from the report is not instrumented and does not count against coverage.
func Diff(res Result, changed map[string][]int) (DiffReport, error) {
	lines, err := changedLines(changed)
	if err != nil {
		return DiffReport{}, err
	}
	paths := make([]string, 0, len(lines))
	for p := range lines {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	report := DiffReport{Files: make([]FileDiff, 0, len(paths))}
	for _, p := range paths {
		file := diffFile(res.Files[p], lines[p])
		file.Path = p
		report.Total.Covered += file.Covered
		report.Total.Uncovered += file.Uncovered
		report.Total.NotInstrumented += file.NotInstrumented
		report.Files = append(report.Files, file)
	}
	report.Percent = report.Total.Percent()
	return report, nil
}

// diffFile counts one file's changed lines against its instrumented lines.
func diffFile(instrumented Lines, changed map[int]struct{}) FileDiff {
	var file FileDiff
	for number := range changed {
		hits, ok := instrumented[number]
		switch {
		case !ok:
			file.NotInstrumented++
		case hits > 0:
			file.Covered++
		default:
			file.Uncovered++
		}
	}
	return file
}

// changedLines normalizes the changed map and rejects a line the reports cannot name.
func changedLines(changed map[string][]int) (map[string]map[int]struct{}, error) {
	paths := make([]string, 0, len(changed))
	for p := range changed {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	out := make(map[string]map[int]struct{}, len(changed))
	for _, raw := range paths {
		p, err := normalizePath(raw)
		if err != nil {
			return nil, fmt.Errorf("changed file %q: %w", raw, err)
		}
		lines := out[p]
		if lines == nil {
			lines = map[int]struct{}{}
			out[p] = lines
		}
		for _, number := range changed[raw] {
			if number < 1 || number > MaxLine {
				return nil, fmt.Errorf("changed file %q: line %d is not in 1..%d", raw, number, MaxLine)
			}
			lines[number] = struct{}{}
		}
	}
	return out, nil
}

// parseState is the shared state of one parse: the rebased prefix, the files found so far, and the
// number of instrumented lines, which MaxLines bounds.
type parseState struct {
	prefix string
	files  map[string]Lines
	lines  int
}

func newState(prefix string) *parseState {
	return &parseState{prefix: prefix, files: map[string]Lines{}}
}

// file normalizes one path from a record opened on srcLine and applies the prefix.
func (st *parseState) file(raw string, srcLine int) (string, error) {
	p, err := normalizePath(raw)
	if err != nil {
		return "", malformed(srcLine, "file %q: %v", raw, err)
	}
	if st.prefix != "" && strings.HasPrefix(p, st.prefix+"/") {
		p = p[len(st.prefix)+1:]
	}
	return p, nil
}

// add records that the instrumented line ran hits times.
func (st *parseState) add(srcLine int, file string, line, hits int) error {
	if line < 1 || line > MaxLine {
		return malformed(srcLine, "line number %d is not in 1..%d", line, MaxLine)
	}
	if hits < 0 || hits > maxHits {
		return malformed(srcLine, "hit count %d is not in 0..%d", hits, maxHits)
	}
	st.lines++
	if st.lines > MaxLines {
		return fmt.Errorf("coverage report holds more than %d instrumented lines: %w", MaxLines, ErrTooLarge)
	}
	if st.files[file] == nil {
		st.files[file] = Lines{}
	}
	st.files[file][line] += hits
	return nil
}

// checkSize validates the options and returns the normalized prefix.
func checkSize(data []byte, opt Options) (string, error) {
	if opt.MaxBytes <= 0 {
		return "", fmt.Errorf("max bytes must be positive, got %d", opt.MaxBytes)
	}
	prefix := ""
	if opt.Prefix != "" {
		p, err := normalizePath(opt.Prefix)
		if err != nil {
			return "", fmt.Errorf("prefix %q: %w", opt.Prefix, err)
		}
		prefix = p
	}
	if len(data) > opt.MaxBytes {
		return "", fmt.Errorf("coverage report is %d bytes, at most %d: %w", len(data), opt.MaxBytes, ErrTooLarge)
	}
	return prefix, nil
}

// normalizePath turns one report path into forward-slash, root-relative form and refuses a path that
// leaves the root. A drive letter is dropped, so C:\work\app\a.go and /work/app/a.go give the same shape.
func normalizePath(raw string) (string, error) {
	p := strings.ReplaceAll(raw, "\\", "/")
	if len(p) >= 2 && p[1] == ':' {
		p = p[2:]
	}
	p = path.Clean(p)
	p = strings.TrimLeft(p, "/")
	if p == "" || p == "." {
		return "", errors.New("empty path")
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", fmt.Errorf("path %q escapes the repository root", raw)
	}
	return p, nil
}

// malformed builds a strict parse error that names the source line.
func malformed(srcLine int, format string, args ...any) error {
	return fmt.Errorf("line %d: %s: %w", srcLine, fmt.Sprintf(format, args...), ErrMalformed)
}

// parseCount reads one non-negative count. smallest is the smallest accepted value: 1 for a line
// number, 0 for a hit or statement count.
func parseCount(text string, srcLine int, what string, smallest int) (int, error) {
	n, err := strconv.Atoi(text)
	if err != nil {
		return 0, malformed(srcLine, "%s %q is not a number", what, text)
	}
	if n < smallest {
		return 0, malformed(srcLine, "%s %d is below %d", what, n, smallest)
	}
	return n, nil
}

// splitLines cuts a byte input into lines and drops one trailing carriage return, so a CRLF report
// parses like an LF one.
func splitLines(data []byte) []string {
	lines := strings.Split(string(data), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines
}
