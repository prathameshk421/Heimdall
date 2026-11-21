package testintegrity

import (
	"path"
	"slices"
	"strconv"
	"strings"
)

// mockViolations is rule 3. A test that the change adds or edits, and that mocks a file the same change edits, can
// pass whatever the real code does. The detector is a heuristic. It reads only the mocks the caller marks as added by
// this change, and matches each target to the non-test files the change touches. False positives: a Go fake of an
// interface whose file changed for an unrelated reason, a Python patch of a name whose package __init__.py changed,
// and a JS mock of a path alias that ends the same as a changed file. False negatives: a mock built through a helper,
// a path alias the rules do not know, and a Go fake whose interface file the caller could not resolve.
func mockViolations(f Facts, o Options) []Violation {
	level := o.MockLevel
	if level != LevelBlock {
		level = LevelWarn
	}
	var changed []string
	for _, c := range f.Files {
		if !c.IsTest {
			changed = append(changed, c.Path)
		}
	}
	var out []Violation
	for _, t := range f.Tests {
		for _, m := range t.Mocks {
			if !m.Added {
				continue
			}
			if hit, ok := mockedFile(m, t.Path, changed); ok {
				out = append(out, newViolation(RuleMocksChangedSource, level, t.Path, m.Target,
					t.Path+" mocks "+hit+", a file this change edits"+lineWords(m.Line)))
			}
		}
	}
	return out
}

func lineWords(line int) string {
	if line <= 0 {
		return ""
	}
	return " (line " + strconv.Itoa(line) + ")"
}

// mockedFile returns the changed file that the mock replaces.
func mockedFile(m MockFact, testPath string, changed []string) (string, bool) {
	switch m.Kind {
	case MockGoFake, MockGoGenerated:
		return matchGo(m.Target, changed)
	case MockPythonPatch:
		return matchPython(m.Target, changed)
	case MockJSMock:
		return matchJS(m.Target, testPath, changed)
	}
	return "", false
}

func matchGo(target string, changed []string) (string, bool) {
	target = strings.TrimSuffix(target, "/")
	if strings.HasSuffix(target, ".go") {
		for _, c := range changed {
			if c == target {
				return c, true
			}
		}
		return "", false
	}
	switch path.Base(target) {
	case "mocks", "mock", "gomock", "mocks_test":
		target = path.Dir(target)
	}
	for _, c := range changed {
		dir := path.Dir(c)
		if strings.HasSuffix(c, ".go") && dir != "." && (target == dir || strings.HasSuffix(target, "/"+dir)) {
			return c, true
		}
	}
	return "", false
}

// matchPython tries the dotted name from the longest module to the shortest, so "shop.billing.refund.compute_refund"
// meets shop/billing/refund.py before it meets shop/billing.py. A prefix of one segment is tried only for a name of
// one or two segments, so a patch of "shop.x.y" does not meet a changed top-level shop.py by accident.
func matchPython(target string, changed []string) (string, bool) {
	parts := strings.Split(target, ".")
	if slices.Contains(parts, "") {
		return "", false
	}
	for n := len(parts); n >= 1; n-- {
		if n == 1 && len(parts) > 2 {
			break
		}
		rel := strings.Join(parts[:n], "/")
		for _, c := range changed {
			if samePath(c, rel+".py") || (n >= 2 && samePath(c, rel+"/__init__.py")) {
				return c, true
			}
		}
	}
	return "", false
}

// samePath says c is p, or ends with "/" and p, so a source root such as src/ does not matter.
func samePath(c, p string) bool { return c == p || strings.HasSuffix(c, "/"+p) }

// jsModule drops the extension and a trailing /index, so "./refund" and "refund.ts" and "refund/index.ts" agree.
func jsModule(p string) string {
	for _, e := range []string{".d.ts", ".tsx", ".ts", ".jsx", ".js", ".mjs", ".cjs", ".mts", ".cts"} {
		if s, ok := strings.CutSuffix(p, e); ok {
			p = s
			break
		}
	}
	return strings.TrimSuffix(p, "/index")
}

func matchJS(target, testPath string, changed []string) (string, bool) {
	var want string
	suffix := false
	switch {
	case strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../"):
		want = path.Join(path.Dir(testPath), target)
		if strings.HasPrefix(want, "../") {
			return "", false
		}
	case strings.HasPrefix(target, "@/") || strings.HasPrefix(target, "~/"):
		want, suffix = target[2:], true
	case strings.HasPrefix(target, "src/"):
		want, suffix = target, true
	default:
		return "", false
	}
	want = jsModule(want)
	for _, c := range changed {
		if LanguageOf(c) != LangJS {
			continue
		}
		m := jsModule(c)
		if m == want || (suffix && strings.HasSuffix(m, "/"+want)) {
			return c, true
		}
	}
	return "", false
}
