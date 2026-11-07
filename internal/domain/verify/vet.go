package verify

import (
	"regexp"
	"strconv"
	"strings"
)

// VetFinding is one diagnostic from go vet, located in a file.
type VetFinding struct {
	File    string
	Line    int
	Column  int
	Message string
}

// vetLine matches "file.go:line:column: message", with an optional "./" or "vet: " prefix. Package headers such as
// "# example.com/pay" do not match.
var vetLine = regexp.MustCompile(`^(?:vet: )?(?:\./)?([^\s:]+\.go):(\d+):(\d+): (.+)$`)

// ParseVet returns the located diagnostics in out, in order. Lines that are not diagnostics are dropped.
func ParseVet(out []byte) []VetFinding {
	var findings []VetFinding
	for _, text := range strings.Split(string(out), "\n") {
		m := vetLine.FindStringSubmatch(strings.TrimSpace(text))
		if m == nil {
			continue
		}
		line, errL := strconv.Atoi(m[2])
		col, errC := strconv.Atoi(m[3])
		if errL != nil || errC != nil {
			continue
		}
		findings = append(findings, VetFinding{File: m[1], Line: line, Column: col, Message: m[4]})
	}
	return findings
}

// Located reports whether a diagnostic is in file at a line from first to last, inclusive. It is the location rule
// that confirms an E2 finding (TECH_SPEC 11.1).
func Located(findings []VetFinding, file string, first, last int) bool {
	for _, f := range findings {
		if f.File == file && f.Line >= first && f.Line <= last {
			return true
		}
	}
	return false
}
