package bench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TestResult is the end state of one top-level test.
type TestResult string

// The test results. A test that started and never ended (a crash or a timeout of the test binary) counts as a fail.
const (
	ResultPass TestResult = "pass"
	ResultFail TestResult = "fail"
	ResultSkip TestResult = "skip"
)

// Run is the parsed output of one go test -json run.
type Run struct {
	Tests       map[string]TestResult `json:"tests"` // top-level tests by name; subtests roll up into their parent
	BuildFailed bool                  `json:"build_failed"`
	BuildOutput string                `json:"build_output,omitempty"`
	// Messages holds the first line of each failing test's message, when the runner reports one (cargo, vitest).
	Messages map[string]string `json:"messages,omitempty"`
}

// testEvent is one line of go test -json (cmd/test2json), plus the build events of go 1.24 and later.
type testEvent struct {
	Action      string
	Test        string
	Output      string
	FailedBuild string
}

// ParseGoTestJSON reads go test -json output. Lines that are not JSON (go: downloading ...) are skipped.
func ParseGoTestJSON(b []byte) (Run, error) {
	p := runParser{run: Run{Tests: map[string]TestResult{}}, running: map[string]bool{}}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := sc.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev testEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return Run{}, fmt.Errorf("go test -json line %d: %w", n, err)
		}
		p.add(ev)
	}
	if err := sc.Err(); err != nil {
		return Run{}, fmt.Errorf("read go test -json: %w", err)
	}
	for name := range p.running {
		p.run.Tests[name] = ResultFail
	}
	p.run.BuildOutput = p.build.String()
	return p.run, nil
}

// runParser holds the state of one ParseGoTestJSON call.
type runParser struct {
	run     Run
	running map[string]bool // top-level tests that started and have not ended
	build   strings.Builder
}

func (p *runParser) add(ev testEvent) {
	switch {
	case ev.Action == "build-output":
		p.build.WriteString(ev.Output)
	case ev.Action == "build-fail" || ev.FailedBuild != "":
		p.run.BuildFailed = true
	case ev.Test == "" || strings.Contains(ev.Test, "/"):
		return
	case ev.Action == "run":
		p.running[ev.Test] = true
	case ev.Action == "pass" || ev.Action == "fail" || ev.Action == "skip":
		p.run.Tests[ev.Test] = TestResult(ev.Action)
		delete(p.running, ev.Test)
	}
}

// TestFuncs returns, in source order, the top-level Go tests (func TestXxx, no receiver) whose span holds any of the
// given lines.
func TestFuncs(src []byte, lines []int) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse test file: %w", err)
	}
	var out []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !isTestName(fn.Name.Name) {
			continue
		}
		start, end := fset.Position(fn.Pos()).Line, fset.Position(fn.End()).Line
		for _, l := range lines {
			if l >= start && l <= end {
				out = append(out, fn.Name.Name)
				break
			}
		}
	}
	return out, nil
}

// isTestName follows go test: Test, or Test followed by a rune that is not a lower-case letter.
func isTestName(name string) bool {
	rest, ok := strings.CutPrefix(name, "Test")
	if !ok {
		return false
	}
	if rest == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLower(r)
}

// RunPattern returns the go test -run pattern that matches exactly the named top-level tests.
func RunPattern(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = regexp.QuoteMeta(n)
	}
	return "^(" + strings.Join(quoted, "|") + ")$"
}
