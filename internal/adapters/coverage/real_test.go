package coverage

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	domaincoverage "github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/coverage"
)

// The reports under testdata/real are the output of coverage.py 7.17.4 (`coverage run -m pytest`, then
// `coverage xml`) on open-source projects at a pinned release. The only edit is the <source> element, which
// named the author's scratch directory and now names /home/runner/work/<name>/<name>. The parser reads
// no <source>. testdata/real/README.md names each project, tag, commit and command.

type realCobertura struct {
	name   string
	file   string
	files  int
	valid  int // lines-valid and `coverage report` Stmts
	hit    int // lines-covered and Stmts minus Miss
	prefix string
	// changed maps the report to the diff cases below.
	last, window string
}

var realCoberturaReports = []realCobertura{
	{
		name: "attrs 26.1.0 (branch coverage on)", file: "testdata/real/attrs-26.1.0.coveragepy.xml",
		files: 19, valid: 1831, hit: 1828, prefix: "src", last: "attrs-last", window: "attrs-window",
	},
	{
		name: "more-itertools v11.2.1", file: "testdata/real/more-itertools-v11.2.1.coveragepy.xml",
		files: 3, valid: 2224, hit: 2224, prefix: "more_itertools", last: "more-itertools-last", window: "more-itertools-window",
	},
}

func totals(res domaincoverage.Result) (files, instrumented, covered int) {
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

// rootAttr reads one attribute of the <coverage> element with a regular expression, so this check shares
// no code with the parser.
func rootAttr(t *testing.T, data []byte, name string) int {
	t.Helper()
	m := regexp.MustCompile(`<coverage [^>]*\b` + name + `="(\d+)"`).FindSubmatch(data)
	require.NotNil(t, m, name)
	n, err := strconv.Atoi(string(m[1]))
	require.NoError(t, err)
	return n
}

func TestRealCoberturaTotalsEqualTheToolSummary(t *testing.T) {
	for _, c := range realCoberturaReports {
		t.Run(c.name, func(t *testing.T) {
			data := load(t, c.file)
			require.Equal(t, [2]int{c.valid, c.hit}, [2]int{rootAttr(t, data, "lines-valid"), rootAttr(t, data, "lines-covered")},
				"the fixture holds the tool's own totals")
			res, err := ParseCobertura(data, domaincoverage.Options{MaxBytes: 1 << 20})
			require.NoError(t, err)
			files, instrumented, covered := totals(res)
			require.Equal(t, [3]int{c.files, c.valid, c.hit}, [3]int{files, instrumented, covered})
		})
	}
}

func loadChanged(t *testing.T, name string) map[string][]int {
	t.Helper()
	var changed map[string][]int
	require.NoError(t, json.Unmarshal(load(t, "testdata/real/changed/"+name+".json"), &changed))
	return changed
}

func TestRealCoberturaDiffs(t *testing.T) {
	cases := []struct {
		name            string
		report          realCobertura
		window          bool
		changedLines    int
		covered         int
		uncovered       int
		notInstrumented int
	}{
		{"attrs last source commit", realCoberturaReports[0], false, 10, 5, 0, 5},
		{"attrs last five source commits", realCoberturaReports[0], true, 66, 26, 0, 40},
		{"more-itertools last three source commits", realCoberturaReports[1], false, 1, 1, 0, 0},
		{"more-itertools last five source commits", realCoberturaReports[1], true, 9, 1, 0, 8},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name := c.report.last
			if c.window {
				name = c.report.window
			}
			res, err := ParseCobertura(load(t, c.report.file), domaincoverage.Options{MaxBytes: 1 << 20})
			require.NoError(t, err)
			got, err := domaincoverage.Diff(res, loadChanged(t, name))
			require.NoError(t, err)
			require.Equal(t, c.changedLines, got.ChangedLines)
			require.Equal(t, domaincoverage.Counts{Covered: c.covered, Uncovered: c.uncovered, NotInstrumented: c.notInstrumented}, got.Total)
			require.Zero(t, got.MissingFiles)
			require.True(t, got.Measurable())
			require.InDelta(t, 100.0, got.Percent, 1e-9)
		})
	}
}

// coverage.py writes filename relative to the directory it ran in, and the changed paths of a diff are
// relative to the repository root: the same form, so no prefix is needed. A wrong prefix that removes a
// real leading directory makes every changed file missing, and the report is not measurable.
func TestRealCoberturaWrongPrefixIsNotMeasurable(t *testing.T) {
	for _, c := range realCoberturaReports {
		t.Run(c.name, func(t *testing.T) {
			res, err := ParseCobertura(load(t, c.file), domaincoverage.Options{MaxBytes: 1 << 20, Prefix: c.prefix})
			require.NoError(t, err)
			got, err := domaincoverage.Diff(res, loadChanged(t, c.window))
			require.NoError(t, err)
			require.False(t, got.Measurable())
			require.Positive(t, got.MissingFiles)
		})
	}
}

// The report is rewritten so every filename carries an absolute runner path, a ./ or a Windows drive.
// Options.Prefix must return the Result of the original.
func TestRealCoberturaPathsInOtherFormsRebaseToTheSameResult(t *testing.T) {
	const runner = "/home/runner/work/attrs/attrs"
	data := string(load(t, realCoberturaReports[0].file))
	want, err := ParseCobertura([]byte(data), domaincoverage.Options{MaxBytes: 1 << 20})
	require.NoError(t, err)
	filename := regexp.MustCompile(`filename="([^"]+)"`)
	cases := []struct {
		name, prefix string
		form         func(string) string
	}{
		{"absolute runner path", runner, func(p string) string { return runner + "/" + p }},
		{"dot slash", "", func(p string) string { return "./" + p }},
		{"windows drive", `D:\a\attrs\attrs`, func(p string) string { return `D:\a\attrs\attrs\` + strings.ReplaceAll(p, "/", `\`) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rewritten := filename.ReplaceAllStringFunc(data, func(m string) string {
				return `filename="` + c.form(filename.FindStringSubmatch(m)[1]) + `"`
			})
			got, err := ParseCobertura([]byte(rewritten), domaincoverage.Options{MaxBytes: 1 << 20, Prefix: c.prefix})
			require.NoError(t, err)
			require.Equal(t, want.Files, got.Files)
		})
	}
}
