package verify

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"
)

// Rejection is one test the red-first rule does not accept, with the reason a person can act on. Reason starts
// with the test name so a log line or a chat message reads on its own.
type Rejection struct {
	Test   string
	Reason string
}

// Verdict is the outcome of the red-first rule: the tests it accepts, the ones it turns away with a reason, and the
// acceptance criteria that no accepted test covers.
type Verdict struct {
	Accepted  []string
	Rejected  []Rejection
	Uncovered []string
}

// Judge applies the red-first rule of TECH_SPEC 12.1 to the run without the feature (before) and the run with it
// (after). A test is accepted only when it fails on an assertion without the feature and passes with it. Every test
// that appears in either run is judged, so a test that vanishes is named rather than ignored.
func Judge(before, after Report, expected map[string][]string) Verdict {
	beforeCases := indexCases(before)
	afterCases := indexCases(after)
	names := unionNames(beforeCases, afterCases)

	var v Verdict
	accepted := make(map[string]bool, len(names))
	for _, name := range names {
		reason, ok := judgeOne(name, beforeCases[name], afterCases[name])
		if ok {
			v.Accepted = append(v.Accepted, name)
			accepted[name] = true
			continue
		}
		v.Rejected = append(v.Rejected, Rejection{Test: name, Reason: reason})
	}
	for _, ac := range slices.Sorted(maps.Keys(expected)) {
		if !coveredByAny(expected[ac], accepted) {
			v.Uncovered = append(v.Uncovered, ac)
		}
	}
	return v
}

func indexCases(r Report) map[string][]Case {
	byName := make(map[string][]Case)
	for _, c := range r.Cases() {
		byName[c.Name] = append(byName[c.Name], c)
	}
	return byName
}

func unionNames(indexes ...map[string][]Case) []string {
	seen := make(map[string]bool)
	for _, idx := range indexes {
		for name := range idx {
			seen[name] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

func coveredByAny(tests []string, accepted map[string]bool) bool {
	for _, name := range tests {
		if accepted[name] {
			return true
		}
	}
	return false
}

// judgeOne decides one test name. The checks run from the headline rejection to the smallest, so a test that
// passes without the feature is reported as that even if it also has another problem.
func judgeOne(name string, before, after []Case) (string, bool) {
	reason, rejected := beforeProblem(name, before)
	if rejected {
		return reason, false
	}
	if len(after) == 0 {
		return fmt.Sprintf("%s does not run with the feature", name), false
	}
	for _, c := range after {
		if c.Status != StatusPass {
			return fmt.Sprintf("%s does not pass with the feature: status %s", name, c.Status), false
		}
	}
	return "", true
}

func beforeProblem(name string, before []Case) (string, bool) {
	if len(before) == 0 {
		return fmt.Sprintf("%s does not run without the feature", name), true
	}
	for _, c := range before {
		if c.Status == StatusPass {
			return fmt.Sprintf("%s passes without the feature", name), true
		}
	}
	for _, c := range before {
		if c.Status == StatusSkipped {
			return fmt.Sprintf("%s is skipped without the feature", name), true
		}
	}
	for _, c := range before {
		if c.Status == StatusError {
			return setupError(name, c), true
		}
	}
	for _, c := range before {
		if c.Status != StatusFail {
			return fmt.Sprintf("%s has status %q without the feature, want fail", name, c.Status), true
		}
		if looksLikeSetupError(c) {
			return setupError(name, c), true
		}
	}
	return "", false
}

func setupError(name string, c Case) string {
	return fmt.Sprintf("%s fails with a compile or setup error, not an assertion: %s", name, reasonDetail(c))
}

// reasonDetail is the failure message and the first line of its body, short enough for one line of output.
func reasonDetail(c Case) string {
	detail := strings.TrimSpace(c.Message)
	if text := firstLine(c.Text); text != "" {
		if detail == "" {
			detail = text
		} else {
			detail += ": " + text
		}
	}
	return shortDetail(detail, 200)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// setupErrorMarkers are the phrases a compiler, the module loader or a panic writes when a test never reached an
// assertion. A <failure> carrying one of them is a setup failure, not a red test (TECH_SPEC 12.1).
func setupErrorMarkers() []string {
	return []string{
		"undefined:",
		"cannot find package",
		"no required module",
		"build failed",
		"setup failed",
		"cannot find module",
		"no non-test go files",
		"syntax error",
		"declared and not used",
		"imported and not used",
		"cannot use ",
		"panic: runtime error: invalid memory",
	}
}

func looksLikeSetupError(c Case) bool {
	haystack := strings.ToLower(c.Message + "\n" + c.Text)
	for _, marker := range setupErrorMarkers() {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}

func shortDetail(s string, limit int) string {
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
