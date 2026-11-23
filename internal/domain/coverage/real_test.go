package coverage

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The reports under testdata/real are the unedited output of real tools run on open-source projects at a
// pinned release. testdata/real/README.md names each project, tag, commit and command. The numbers below
// are the tools' own summaries and an independent line count of the same file (docs/product/research/
// 12-coverage-real-reports.md has the commands that produced them).

// realReport is one real report and what the parser must read from it.
type realReport struct {
	name   string
	file   string
	prefix string
	// files, instrumented and covered are the parser's totals in lines.
	files        int
	instrumented int
	covered      int
	// tool is what the tool printed about the same run. For LCOV it is lines hit and lines found. For a Go
	// profile it is the percentage `go tool cover -func` prints for the statements.
	toolHit, toolFound int
	toolPercent        string
}

var realGo = []realReport{
	{
		name: "uuid v1.6.0 set", file: "testdata/real/uuid-v1.6.0.set.cover.out", prefix: "github.com/google/uuid",
		files: 14, instrumented: 405, covered: 356, toolPercent: "87.5",
	},
	{
		name: "cobra v1.10.2 atomic", file: "testdata/real/cobra-v1.10.2.atomic.cover.out", prefix: "github.com/spf13/cobra",
		files: 17, instrumented: 3567, covered: 3317, toolPercent: "86.8",
	},
}

var realLCOV = []realReport{
	{
		name: "commander v15.1.0 c8", file: "testdata/real/commander-v15.1.0.c8.lcov.info",
		files: 7, instrumented: 4218, covered: 4206, toolHit: 4206, toolFound: 4218,
	},
	{
		name: "ms 3.0.0-canary.1 jest", file: "testdata/real/ms-3.0.0-canary.1.jest.lcov.info",
		files: 1, instrumented: 55, covered: 52, toolHit: 52, toolFound: 55,
	},
	{
		name: "attrs 26.1.0 coverage.py", file: "testdata/real/attrs-26.1.0.coveragepy.lcov.info",
		files: 19, instrumented: 1831, covered: 1828, toolHit: 1828, toolFound: 1831,
	},
	{
		name: "more-itertools v11.2.1 coverage.py", file: "testdata/real/more-itertools-v11.2.1.coveragepy.lcov.info",
		files: 3, instrumented: 2224, covered: 2224, toolHit: 2224, toolFound: 2224,
	},
}

func totals(res Result) (files, instrumented, covered int) {
	for _, lines := range res.Files {
		files++
		for _, hits := range lines {
			instrumented++
			if hits > 0 {
				covered++
			}
		}
	}
	return files, instrumented, covered
}

// goStatements sums the statement counts of a Go profile with no code shared with the parser, the way
// `go tool cover -func` does for its total.
func goStatements(t *testing.T, profile []byte) (covered, total int) {
	t.Helper()
	for _, record := range strings.Split(strings.TrimSpace(string(profile)), "\n")[1:] {
		fields := strings.Fields(record)
		require.Len(t, fields, 3, record)
		statements, err := strconv.Atoi(fields[1])
		require.NoError(t, err)
		count, err := strconv.Atoi(fields[2])
		require.NoError(t, err)
		total += statements
		if count > 0 {
			covered += statements
		}
	}
	return covered, total
}

func TestRealGoProfiles(t *testing.T) {
	t.Parallel()
	for _, c := range realGo {
		t.Run(c.name, func(t *testing.T) {
			data := load(t, c.file)
			covered, total := goStatements(t, data)
			// The tool counts statements. The parser counts lines, and a block spans many lines, so the two
			// totals differ in size. The percentage the tool prints is checked on the statement sums.
			require.Equal(t, c.toolPercent, fmt.Sprintf("%.1f", float64(covered)*100/float64(total)))
			res, err := ParseGoCover(data, Options{MaxBytes: 1 << 20, Prefix: c.prefix})
			require.NoError(t, err)
			files, instrumented, hit := totals(res)
			require.Equal(t, [3]int{c.files, c.instrumented, c.covered}, [3]int{files, instrumented, hit})
		})
	}
}

// lcovSummary sums the LF and LH records of a tracefile, which is the tool's own line summary.
func lcovSummary(t *testing.T, data []byte) (hit, found int) {
	t.Helper()
	for _, record := range strings.Split(string(data), "\n") {
		name, value, ok := strings.Cut(strings.TrimSpace(record), ":")
		if !ok || (name != "LF" && name != "LH") {
			continue
		}
		n, err := strconv.Atoi(value)
		require.NoError(t, err)
		if name == "LF" {
			found += n
		} else {
			hit += n
		}
	}
	return hit, found
}

func TestRealLCOVReports(t *testing.T) {
	t.Parallel()
	for _, c := range realLCOV {
		t.Run(c.name, func(t *testing.T) {
			data := load(t, c.file)
			hit, found := lcovSummary(t, data)
			require.Equal(t, [2]int{c.toolHit, c.toolFound}, [2]int{hit, found}, "the fixture holds the tool's own totals")
			res, err := ParseLCOV(data, Options{MaxBytes: 1 << 20})
			require.NoError(t, err)
			files, instrumented, covered := totals(res)
			require.Equal(t, [3]int{c.files, c.instrumented, c.covered}, [3]int{files, instrumented, covered})
			require.Equal(t, [2]int{hit, found}, [2]int{covered, instrumented}, "parser totals equal the tool's LH and LF")
		})
	}
}

// diffCase is one diff measured against one report. changed names a file under testdata/real/changed: the
// lines a project's own git history changed, from `git diff --unified=0` on the tagged tree.
type diffCase struct {
	name            string
	report          realReport
	parse           func(*testing.T, realReport, string) Result
	changed         string
	wrongPrefix     string
	changedLines    int
	covered         int
	uncovered       int
	notInstrumented int
	measurable      bool
}

func parseGo(t *testing.T, c realReport, prefix string) Result {
	t.Helper()
	res, err := ParseGoCover(load(t, c.file), Options{MaxBytes: 1 << 20, Prefix: prefix})
	require.NoError(t, err)
	return res
}

func parseLCOV(t *testing.T, c realReport, prefix string) Result {
	t.Helper()
	res, err := ParseLCOV(load(t, c.file), Options{MaxBytes: 1 << 20, Prefix: prefix})
	require.NoError(t, err)
	return res
}

func loadChanged(t *testing.T, name string) map[string][]int {
	t.Helper()
	var changed map[string][]int
	require.NoError(t, json.Unmarshal(load(t, "testdata/real/changed/"+name+".json"), &changed))
	return changed
}

// The wrong prefix of a Go case removes a real leading part of the module path, so the paths stay
// absolute-looking and no changed file matches. For a relative LCOV path it removes a real directory.
func realDiffCases() []diffCase {
	return []diffCase{
		{name: "uuid last source commit (a comment)", report: realGo[0], parse: parseGo, changed: "uuid-last",
			wrongPrefix: "github.com/google", changedLines: 1, notInstrumented: 1, measurable: false},
		{name: "uuid last two source commits", report: realGo[0], parse: parseGo, changed: "uuid-window",
			wrongPrefix: "github.com/google", changedLines: 34, covered: 15, notInstrumented: 19, measurable: true},
		{name: "uuid last six source commits", report: realGo[0], parse: parseGo, changed: "uuid-wide",
			wrongPrefix: "github.com/google", changedLines: 244, covered: 70, uncovered: 7, notInstrumented: 167, measurable: true},
		{name: "cobra last commit (an import)", report: realGo[1], parse: parseGo, changed: "cobra-last",
			wrongPrefix: "github.com/spf13", changedLines: 1, notInstrumented: 1, measurable: false},
		{name: "cobra last five source commits", report: realGo[1], parse: parseGo, changed: "cobra-window",
			wrongPrefix: "github.com/spf13", changedLines: 24, covered: 6, notInstrumented: 18, measurable: true},
		{name: "commander last source commit", report: realLCOV[0], parse: parseLCOV, changed: "commander-last",
			changedLines: 1, covered: 1, measurable: true},
		{name: "commander last four source commits", report: realLCOV[0], parse: parseLCOV, changed: "commander-window",
			wrongPrefix: "lib", changedLines: 24, covered: 24, measurable: true},
		{name: "ms last source commit (the whole file)", report: realLCOV[1], parse: parseLCOV, changed: "ms-last",
			wrongPrefix: "src", changedLines: 205, covered: 52, uncovered: 3, notInstrumented: 150, measurable: true},
		{name: "attrs last source commit (lcov)", report: realLCOV[2], parse: parseLCOV, changed: "attrs-last",
			wrongPrefix: "src", changedLines: 10, covered: 5, notInstrumented: 5, measurable: true},
		{name: "more-itertools last five source commits (lcov)", report: realLCOV[3], parse: parseLCOV, changed: "more-itertools-window",
			wrongPrefix: "more_itertools", changedLines: 9, covered: 1, notInstrumented: 8, measurable: true},
	}
}

func TestRealDiffs(t *testing.T) {
	t.Parallel()
	for _, c := range realDiffCases() {
		t.Run(c.name, func(t *testing.T) {
			changed := loadChanged(t, c.changed)
			got, err := Diff(c.parse(t, c.report, c.report.prefix), changed)
			require.NoError(t, err)
			require.Equal(t, c.changedLines, got.ChangedLines)
			require.Equal(t, Counts{Covered: c.covered, Uncovered: c.uncovered, NotInstrumented: c.notInstrumented}, got.Total)
			require.Zero(t, got.MissingFiles)
			require.Equal(t, c.measurable, got.Measurable())
			if c.measurable {
				require.InDelta(t, float64(c.covered)*100/float64(c.covered+c.uncovered), got.Percent, 1e-9)
			}
		})
	}
}

// A last commit that changes only comments and imports is not measurable: the report calls none of its
// lines instrumented. The fast check must read that as unknown, not as the 100 that Percent returns.
func TestRealDiffOfACommentIsNotMeasurable(t *testing.T) {
	t.Parallel()
	got, err := Diff(parseGo(t, realGo[0], realGo[0].prefix), loadChanged(t, "uuid-last"))
	require.NoError(t, err)
	require.InDelta(t, 100.0, got.Percent, 1e-9)
	require.False(t, got.Measurable())
}

func TestRealDiffWithAWrongPrefixIsNotMeasurable(t *testing.T) {
	t.Parallel()
	for _, c := range realDiffCases() {
		if c.wrongPrefix == "" {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			changed := loadChanged(t, c.changed)
			got, err := Diff(c.parse(t, c.report, c.wrongPrefix), changed)
			require.NoError(t, err)
			require.False(t, got.Measurable())
			require.Positive(t, got.MissingFiles)
			// The same report with no prefix at all is no better for a Go module path.
			if c.report.prefix != "" {
				bare, err := Diff(c.parse(t, c.report, ""), changed)
				require.NoError(t, err)
				require.False(t, bare.Measurable())
			}
		})
	}
}

// The real tools wrote module paths (Go) and paths relative to the working directory (c8, Jest,
// coverage.py). A CI run can also write an absolute runner path or a ./ form. This rewrites the paths of a
// real report into those forms and shows that Options.Prefix returns the same Result.
func TestRealPathsInOtherFormsRebaseToTheSameResult(t *testing.T) {
	t.Parallel()
	const runner = "/home/runner/work/app/app"
	forms := map[string]func(string) string{
		"absolute runner path": func(p string) string { return runner + "/" + p },
		"dot slash":            func(p string) string { return "./" + p },
		"windows drive":        func(p string) string { return `D:\a\app\app\` + strings.ReplaceAll(p, "/", `\`) },
	}
	prefixes := map[string]string{"absolute runner path": runner, "dot slash": "", "windows drive": `D:\a\app\app`}
	data := string(load(t, realLCOV[0].file))
	want, err := ParseLCOV([]byte(data), Options{MaxBytes: 1 << 20})
	require.NoError(t, err)
	for name, form := range forms {
		t.Run(name, func(t *testing.T) {
			var out []string
			for _, record := range strings.Split(data, "\n") {
				if raw, ok := strings.CutPrefix(record, "SF:"); ok {
					record = "SF:" + form(raw)
				}
				out = append(out, record)
			}
			got, err := ParseLCOV([]byte(strings.Join(out, "\n")), Options{MaxBytes: 1 << 20, Prefix: prefixes[name]})
			require.NoError(t, err)
			require.Equal(t, want.Files, got.Files)
		})
	}
}
