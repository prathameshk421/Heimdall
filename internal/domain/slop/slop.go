// Package slop is the door: a pure rule that turns away a pull request that is too big, has no ticket, has no
// tests, or touches paths its test author did not expect, before any agent is woken (TECH_SPEC 12.3).
package slop

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// Rule is the stable identifier of one of the four slop rules. Checks, logs and the dashboard match on it, so it
// does not change when a message is reworded.
type Rule string

// The four rules of TECH_SPEC 12.3, in the order Evaluate reports them.
const (
	RuleTicketLinked Rule = "ticket_linked"
	RuleSize         Rule = "size"
	RuleTestsPresent Rule = "tests_present"
	RuleScope        Rule = "scope"
)

// JustificationHeading is the heading whose section, if non-empty, lets a change touch paths outside its
// expected set. The spec names the section but not its heading, so the heading is chosen here and recorded in
// doc.go. The section's text is never interpreted.
const JustificationHeading = "Justification"

// File is one changed file with its added and deleted line counts, as the pull request reports them.
type File struct {
	Path    string
	Added   int
	Deleted int
}

// Facts is what the rule reads about one pull request. The caller fills it from GitHub, the tracker and the
// test author's submission. A zero value fails the rules that need a fact it does not have.
type Facts struct {
	Title              string
	Branch             string
	Body               string
	Files              []File
	TicketKey          string
	TestsChanged       bool
	HeimdallTestsExist bool
	// ExpectedPaths are the globs the test author declared the change may touch.
	ExpectedPaths []string
}

// Failure is one rule that did not hold, with a plain-language reason a person can act on.
type Failure struct {
	Rule    Rule
	Message string
}

// Result is the outcome of Evaluate. Pass is true exactly when Failures is empty.
type Result struct {
	Pass     bool
	Failures []Failure
}

// Evaluate applies the four rules to the facts. It reports every rule that fails, not only the first, so a
// author sees the whole list in one push. testsDir is policy.Tests.Dir; the tests directory is excluded from the
// size count and from the scope check.
func Evaluate(f Facts, p policy.Slop, testsDir string) Result {
	var fails []Failure
	add := func(rule Rule, format string, args ...any) {
		fails = append(fails, Failure{Rule: rule, Message: fmt.Sprintf(format, args...)})
	}
	if msg, bad := checkTicket(f, p); bad {
		add(RuleTicketLinked, "%s", msg)
	}
	if msg, bad := checkSize(f, p, testsDir); bad {
		add(RuleSize, "%s", msg)
	}
	if !f.TestsChanged && !f.HeimdallTestsExist {
		add(RuleTestsPresent, "no tests changed in this pull request and no heimdall tests exist for %s", ticketOrNone(f))
	}
	if msg, bad := checkScope(f, testsDir); bad {
		add(RuleScope, "%s", msg)
	}
	return Result{Pass: len(fails) == 0, Failures: fails}
}

func ticketOrNone(f Facts) string {
	if f.TicketKey == "" {
		return "this change (no ticket key)"
	}
	return f.TicketKey
}

// checkTicket says whether the ticket key appears in the title, the branch or the body as a whole token. The
// policy's require_ticket switch turns the rule off; the spec's default keeps it on.
func checkTicket(f Facts, p policy.Slop) (string, bool) {
	if !p.RequireTicket {
		return "", false
	}
	if f.TicketKey == "" {
		return "no ticket key was found for this change", true
	}
	for _, text := range []string{f.Title, f.Branch, f.Body} {
		if containsToken(text, f.TicketKey) {
			return "", false
		}
	}
	return fmt.Sprintf("the ticket key %s is not in the title, the branch or the body", f.TicketKey), true
}

// containsToken reports whether key occurs in text with no letter or digit on either side. The match ignores case,
// because branch names are usually lowercase. A hyphen or a dot is a boundary, so "closes PAY-14." matches PAY-14.
func containsToken(text, key string) bool {
	if key == "" {
		return false
	}
	haystack := strings.ToLower(text)
	needle := strings.ToLower(key)
	for start := 0; ; {
		i := strings.Index(haystack[start:], needle)
		if i < 0 {
			return false
		}
		at := start + i
		end := at + len(needle)
		if boundary(haystack, at-1) && boundary(haystack, end) {
			return true
		}
		start = at + 1
	}
}

// boundary reports whether the byte at i is outside a token: the start or end of the text, or a byte that is
// neither a letter nor a digit. A byte of a multi-byte UTF-8 letter reads as a letter, so it blocks the match,
// which errs toward refusing a key that sits inside a longer word.
func boundary(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	r := rune(s[i])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// checkSize counts added and deleted lines outside the ignored paths and the tests directory, and compares the
// total with the limit. A negative count is a bug in the caller, so it fails the rule rather than hiding lines.
func checkSize(f Facts, p policy.Slop, testsDir string) (string, bool) {
	total := 0
	for _, file := range f.Files {
		if file.Added < 0 || file.Deleted < 0 {
			return fmt.Sprintf("file %s has a negative line count", file.Path), true
		}
		if inTests(file.Path, testsDir) || matchesAny(file.Path, p.Ignore) {
			continue
		}
		total += file.Added + file.Deleted
	}
	if total > p.MaxChangedLines {
		return fmt.Sprintf("the change has %d changed lines outside the tests and ignored files, and the limit is %d", total, p.MaxChangedLines), true
	}
	return "", false
}

// checkScope passes when every path outside the tests directory is expected, or the body carries a non-empty
// justification section. The rule looks at the section only for emptiness.
func checkScope(f Facts, testsDir string) (string, bool) {
	var outside []string
	for _, file := range f.Files {
		if inTests(file.Path, testsDir) || matchesAny(file.Path, f.ExpectedPaths) {
			continue
		}
		outside = append(outside, file.Path)
	}
	if len(outside) == 0 || justified(f.Body) {
		return "", false
	}
	return fmt.Sprintf("paths outside the expected set and the tests directory were changed without a justification: %s", strings.Join(outside, ", ")), true
}

// inTests reports whether path is the tests directory or inside it. A sibling such as tests/heimdallx is not.
func inTests(path, testsDir string) bool {
	return path == testsDir || strings.HasPrefix(path, testsDir+"/")
}

func matchesAny(path string, globs []string) bool {
	for _, g := range globs {
		if policy.MatchGlob(g, path) {
			return true
		}
	}
	return false
}

// justified reports whether a section headed JustificationHeading (at any markdown level) holds a non-blank line
// before the next heading. The text is only checked for emptiness, never read.
func justified(body string) bool {
	inSection := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if h, ok := heading(trimmed); ok {
			inSection = strings.EqualFold(h, JustificationHeading)
			continue
		}
		if inSection && trimmed != "" {
			return true
		}
	}
	return false
}

// heading returns the text of a markdown heading line ("## Justification"), and whether the line is one.
func heading(line string) (string, bool) {
	if !strings.HasPrefix(line, "#") {
		return "", false
	}
	text := strings.TrimLeft(line, "#")
	if text == "" || (text[0] != ' ' && text[0] != '\t') {
		return "", false
	}
	return strings.TrimSpace(text), true
}
