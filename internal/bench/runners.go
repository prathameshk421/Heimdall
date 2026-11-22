package bench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// maxMessage bounds one failure message kept in a Run, and maxBuildOutput the build output.
const (
	maxMessage     = 300
	maxBuildOutput = 4 << 10
)

// Patterns of libtest's stable text output (the default "pretty" format of cargo test).
var (
	libtestRunning = regexp.MustCompile(`^running [0-9]+ tests?$`)
	libtestLine    = regexp.MustCompile(`^test (.+) \.\.\. (ok|FAILED|ignored)(?:, .*)?$`)
	libtestSummary = regexp.MustCompile(`^test result: (?:ok|FAILED)\. ([0-9]+) passed; ([0-9]+) failed; ([0-9]+) ignored;`)
	libtestFailure = regexp.MustCompile(`^---- (.+) stdout ----$`)
)

// ParseLibtest reads the stdout and stderr of cargo test. Each "test NAME ... ok|FAILED|ignored" line is one result,
// and an ignored test is a skip. A name that occurs twice (two test binaries) keeps its worse result. The lines must
// add up to the "test result:" summaries, so garbled output is an error, not a verdict. A compile error is a build
// failure with no tests. Output with no test binary and no compile error is an error: cargo failed before it built.
func ParseLibtest(stdout, stderr []byte) (Run, error) {
	p := libtestParser{run: Run{Tests: map[string]TestResult{}, Messages: map[string]string{}}}
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		if err := p.add(strings.TrimRight(sc.Text(), "\r")); err != nil {
			return Run{}, err
		}
	}
	if err := sc.Err(); err != nil {
		return Run{}, fmt.Errorf("read cargo test output: %w", err)
	}
	compileError := hasCompileError(stderr)
	if compileError {
		p.run.BuildFailed = true
		p.run.BuildOutput = buildErrors(stderr)
	}
	if p.binaries == 0 && !compileError {
		return Run{}, fmt.Errorf("no test binary ran and no compile error: %s", lastLines(stderr, 5))
	}
	if p.lines != p.summed {
		return Run{}, fmt.Errorf("libtest: %d result lines, but the summary counts %d", p.lines, p.summed)
	}
	return p.run, nil
}

// libtestParser holds the state of one ParseLibtest call.
type libtestParser struct {
	run      Run
	binaries int    // "running N tests" lines
	lines    int    // result lines
	summed   int    // passed, failed and ignored counts of the summaries
	failing  string // the test whose failure output is being read
}

func (p *libtestParser) add(line string) error {
	switch {
	case libtestRunning.MatchString(line):
		p.binaries++
	case libtestLine.MatchString(line):
		m := libtestLine.FindStringSubmatch(line)
		p.lines++
		p.run.Tests[m[1]] = worse(p.run.Tests[m[1]], libtestResult(m[2]))
	case libtestSummary.MatchString(line):
		for _, n := range libtestSummary.FindStringSubmatch(line)[1:] {
			v, err := strconv.Atoi(n)
			if err != nil {
				return fmt.Errorf("libtest summary %q: %w", line, err)
			}
			p.summed += v
		}
	case libtestFailure.MatchString(line):
		p.failing = libtestFailure.FindStringSubmatch(line)[1]
	case p.failing != "" && strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "thread '"):
		if _, ok := p.run.Messages[p.failing]; !ok {
			p.run.Messages[p.failing] = clip(strings.TrimSpace(line), maxMessage)
		}
	}
	return nil
}

func libtestResult(word string) TestResult {
	switch word {
	case "ok":
		return ResultPass
	case "FAILED":
		return ResultFail
	}
	return ResultSkip
}

// worse keeps the worse of two results: a fail beats a pass, and a pass beats a skip.
func worse(a, b TestResult) TestResult {
	rank := map[TestResult]int{"": 0, ResultSkip: 1, ResultPass: 2, ResultFail: 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// hasCompileError says whether cargo's stderr reports a compile error: an "error[E...]" diagnostic or "could not
// compile".
func hasCompileError(stderr []byte) bool {
	for _, line := range strings.Split(string(stderr), "\n") {
		if strings.HasPrefix(line, "error[E") || strings.HasPrefix(line, "error: could not compile") {
			return true
		}
	}
	return false
}

// buildErrors keeps cargo's stderr from the first error line on, bounded.
func buildErrors(stderr []byte) string {
	s := string(stderr)
	if i := strings.Index(s, "\nerror"); i >= 0 && !strings.HasPrefix(s, "error") {
		s = s[i+1:]
	}
	return clip(s, maxBuildOutput)
}

// vitestReport is the part of vitest's JSON reporter output that the bench reads.
type vitestReport struct {
	TestResults []struct {
		Name             string `json:"name"`
		Status           string `json:"status"`
		Message          string `json:"message"`
		AssertionResults []struct {
			AncestorTitles  []string `json:"ancestorTitles"`
			Title           string   `json:"title"`
			Status          string   `json:"status"`
			FailureMessages []string `json:"failureMessages"`
		} `json:"assertionResults"`
	} `json:"testResults"`
}

// ParseVitestJSON reads vitest's JSON reporter output (--reporter=json). Each test is keyed by its test id: the file
// relative to root, a colon, and the describe titles and the test title joined by " > " (SplitTestID). A file that
// failed with no test results did not load (a syntax error, a missing module or a missing export): that is a build
// failure. A skipped, pending or todo test is a skip. A title that occurs twice (it.each) keeps its worse result.
func ParseVitestJSON(b []byte, root string) (Run, error) {
	var rep vitestReport
	if err := json.Unmarshal(b, &rep); err != nil {
		return Run{}, fmt.Errorf("vitest json: %w", err)
	}
	run := Run{Tests: map[string]TestResult{}, Messages: map[string]string{}}
	prefix := strings.TrimSuffix(root, "/") + "/"
	var build strings.Builder
	for _, f := range rep.TestResults {
		file := strings.TrimPrefix(f.Name, prefix)
		if f.Status == "failed" && len(f.AssertionResults) == 0 {
			run.BuildFailed = true
			fmt.Fprintf(&build, "%s: %s\n", file, firstLine(f.Message))
			continue
		}
		for _, a := range f.AssertionResults {
			id := file + ":" + strings.Join(append(append([]string{}, a.AncestorTitles...), a.Title), " > ")
			r := vitestResult(a.Status)
			run.Tests[id] = worse(run.Tests[id], r)
			if r == ResultFail && len(a.FailureMessages) > 0 {
				if _, ok := run.Messages[id]; !ok {
					run.Messages[id] = clip(firstLine(a.FailureMessages[0]), maxMessage)
				}
			}
		}
	}
	run.BuildOutput = clip(build.String(), maxBuildOutput)
	return run, nil
}

func vitestResult(status string) TestResult {
	switch status {
	case "passed":
		return ResultPass
	case "failed":
		return ResultFail
	}
	return ResultSkip
}

// SplitTestID splits a vitest test id into its file and its name (ParseVitestJSON). It splits at the first colon, so a
// title may hold colons and a file path may not.
func SplitTestID(id string) (file, name string, ok bool) {
	file, name, ok = strings.Cut(id, ":")
	if !ok || file == "" || name == "" {
		return "", "", false
	}
	return file, name, true
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// clip cuts s to at most n bytes at a rune boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// lastLines returns the last n non-empty lines of b, joined by " | ".
func lastLines(b []byte, n int) string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	if len(out) == 0 {
		return "no output"
	}
	return strings.Join(out, " | ")
}
