package localrunner

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type rawEvent struct {
	Time        time.Time `json:"Time"`
	Action      string    `json:"Action"`
	Package     string    `json:"Package"`
	Test        string    `json:"Test"`
	Elapsed     float64   `json:"Elapsed"`
	Output      string    `json:"Output"`
	ImportPath  string    `json:"ImportPath"`
	FailedBuild string    `json:"FailedBuild"`
}

type testCollector struct {
	name     string
	pkg      string
	status   verify.Status
	outputs  []string
	finished bool
}

type parseState struct {
	buildOutputs map[string][]string
	buildFails   map[string]bool
	tests        map[string]*testCollector
	order        []string
	packages     []string
	pkgTests     map[string][]string
}

func newParseState() *parseState {
	return &parseState{
		buildOutputs: make(map[string][]string),
		buildFails:   make(map[string]bool),
		tests:        make(map[string]*testCollector),
		pkgTests:     make(map[string][]string),
	}
}

// ParseEvents reads a go test -json stream and builds a verify.Report and log bytes.
func ParseEvents(r io.Reader) (verify.Report, []byte, error) {
	state := newParseState()
	var rawLog bytes.Buffer
	scanner := bufio.NewScanner(io.TeeReader(r, &rawLog))
	scanner.Buffer(make([]byte, 64*1024), MaxOutputBytes)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev rawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		state.processEvent(ev)
	}
	if err := scanner.Err(); err != nil && rawLog.Len() == 0 {
		return verify.Report{}, nil, err
	}
	return state.toReport(), rawLog.Bytes(), nil
}

func (s *parseState) processEvent(ev rawEvent) {
	if ev.Action == "build-output" {
		s.buildOutputs[ev.ImportPath] = append(s.buildOutputs[ev.ImportPath], ev.Output)
		return
	}
	if ev.Action == "build-fail" {
		s.buildFails[ev.ImportPath] = true
		return
	}
	if ev.FailedBuild != "" {
		s.buildFails[ev.FailedBuild] = true
		s.buildFails[ev.Package] = true
	}
	s.handlePackageAndTestEvent(ev)
}

func (s *parseState) handlePackageAndTestEvent(ev rawEvent) {
	if ev.Package != "" && len(s.pkgTests[ev.Package]) == 0 {
		if !containsString(s.packages, ev.Package) {
			s.packages = append(s.packages, ev.Package)
		}
	}
	if ev.Test == "" {
		s.handlePackageLevelEvent(ev)
		return
	}
	s.handleTestLevelEvent(ev)
}

func (s *parseState) handlePackageLevelEvent(ev rawEvent) {
	if strings.Contains(ev.Output, "[setup failed]") || strings.Contains(ev.Output, "[build failed]") {
		s.buildFails[ev.Package] = true
	}
	if ev.Action == "fail" && len(s.pkgTests[ev.Package]) == 0 {
		s.buildFails[ev.Package] = true
	}
	s.buildOutputs[ev.Package] = append(s.buildOutputs[ev.Package], ev.Output)
}

func (s *parseState) handleTestLevelEvent(ev rawEvent) {
	key := ev.Package + "/" + ev.Test
	tc, exists := s.tests[key]
	if !exists {
		tc = &testCollector{
			name:   ev.Test,
			pkg:    ev.Package,
			status: verify.StatusError,
		}
		s.tests[key] = tc
		s.order = append(s.order, key)
		s.pkgTests[ev.Package] = append(s.pkgTests[ev.Package], key)
	}

	switch ev.Action {
	case "output":
		tc.outputs = append(tc.outputs, ev.Output)
	case "pass":
		tc.status = verify.StatusPass
		tc.finished = true
	case "fail":
		tc.finished = true
		if isPanicOutput(tc.outputs) {
			tc.status = verify.StatusError
		} else {
			tc.status = verify.StatusFail
		}
	case "skip":
		tc.status = verify.StatusSkipped
		tc.finished = true
	}
}

func (s *parseState) toReport() verify.Report {
	var rep verify.Report
	for _, pkg := range s.packages {
		var cases []verify.Case
		if s.isPackageBuildFailed(pkg) {
			cases = append(cases, s.buildFailureCase(pkg))
		}
		for _, key := range s.pkgTests[pkg] {
			tc := s.tests[key]
			cases = append(cases, s.testToCase(tc))
		}
		if len(cases) > 0 {
			rep.Suites = append(rep.Suites, verify.Suite{
				Name:  pkg,
				Cases: cases,
			})
		}
	}
	return rep
}

func (s *parseState) isPackageBuildFailed(pkg string) bool {
	return s.buildFails[pkg] || s.buildFails[pkg+".test"]
}

func (s *parseState) buildFailureCase(pkg string) verify.Case {
	out := append([]string(nil), s.buildOutputs[pkg+".test"]...)
	out = append(out, s.buildOutputs[pkg]...)
	text := strings.TrimSpace(strings.Join(out, ""))
	msg := truncateRuneBounded(text, verify.MaxMessageBytes)
	return verify.Case{
		Suite:     pkg,
		ClassName: pkg,
		Name:      "[build failed]",
		Status:    verify.StatusError,
		Message:   msg,
		Text:      msg,
	}
}

func (s *parseState) testToCase(tc *testCollector) verify.Case {
	msg := cleanAssertionMessage(tc.outputs)
	fullText := strings.TrimSpace(strings.Join(tc.outputs, ""))
	if tc.status == verify.StatusError && !tc.finished && msg == "" {
		msg = "timeout"
	}
	return verify.Case{
		Suite:     tc.pkg,
		ClassName: tc.pkg,
		Name:      tc.name,
		Status:    tc.status,
		Message:   truncateRuneBounded(msg, verify.MaxMessageBytes),
		Text:      truncateRuneBounded(fullText, verify.MaxMessageBytes),
	}
}

func cleanAssertionMessage(outputs []string) string {
	var clean []string
	for _, line := range outputs {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "=== RUN") ||
			strings.HasPrefix(trimmed, "--- PASS") ||
			strings.HasPrefix(trimmed, "--- FAIL") ||
			strings.HasPrefix(trimmed, "--- SKIP") {
			continue
		}
		if trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	return strings.Join(clean, "\n")
}

func isPanicOutput(outputs []string) bool {
	for _, line := range outputs {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "panic:") || strings.Contains(lower, "runtime error:") {
			return true
		}
	}
	return false
}

func truncateRuneBounded(s string, limit int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= limit {
		return s
	}
	s = s[:limit]
	for i := 0; i < utf8.UTFMax && len(s) > 0; i++ {
		r, size := utf8.DecodeLastRuneInString(s)
		if r != utf8.RuneError || size > 1 {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

func containsString(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}

// ReportToSandboxResult converts a verify.Report into a ports.SandboxResult.
func ReportToSandboxResult(rep verify.Report, logRef workflow.BlobRef) ports.SandboxResult {
	res := ports.SandboxResult{
		LogRef: logRef,
		Tests:  make([]ports.TestResult, 0, len(rep.Cases())),
	}
	for _, c := range rep.Cases() {
		var outcome ports.Outcome
		switch c.Status {
		case verify.StatusPass:
			outcome = ports.OutcomePassed
			res.Passed++
		case verify.StatusFail:
			outcome = ports.OutcomeFailure
			res.Failed++
		case verify.StatusError:
			outcome = ports.OutcomeError
			res.Errored++
		case verify.StatusSkipped:
			outcome = ports.OutcomeSkipped
		default:
			outcome = ports.OutcomeError
			res.Errored++
		}
		res.Tests = append(res.Tests, ports.TestResult{
			Name:    c.Name,
			Outcome: outcome,
		})
	}
	return res
}
