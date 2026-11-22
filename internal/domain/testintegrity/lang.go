package testintegrity

import (
	"path"
	"regexp"
	"strings"
)

// Language is the language of a file, found from its extension.
type Language string

// The languages the line rules know. Any other file is LangUnknown and only the file-level rules apply to it.
const (
	LangUnknown Language = ""
	LangGo      Language = "go"
	LangPython  Language = "python"
	LangJS      Language = "js"
	LangJava    Language = "java"
	LangRust    Language = "rust"
)

// LanguageOf maps a path to a language by its extension. TypeScript is LangJS, Kotlin is LangJava.
func LanguageOf(p string) Language {
	switch strings.ToLower(path.Ext(p)) {
	case ".go":
		return LangGo
	case ".py":
		return LangPython
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts":
		return LangJS
	case ".java", ".kt", ".kts":
		return LangJava
	case ".rs":
		return LangRust
	}
	return LangUnknown
}

var (
	goDecl     = regexp.MustCompile(`^func\s+((?:Test|Benchmark|Fuzz|Example)\w*)\s*\(`)
	pyDecl     = regexp.MustCompile(`^\s*(?:async\s+)?def\s+(test\w*)\s*\(`)
	jsDecl     = regexp.MustCompile("\\b(?:it|test)(?:\\.(?:only|concurrent))?\\s*\\(\\s*['\"`]([^'\"`]+)['\"`]")
	javaMark   = regexp.MustCompile(`@(?:Test|ParameterizedTest|RepeatedTest)\b`)
	javaDecl   = regexp.MustCompile(`\b(?:void|fun)\s+(\w+)\s*\(`)
	rustMark   = regexp.MustCompile(`#\[(?:test|tokio::test)\b`)
	rustDecl   = regexp.MustCompile(`\bfn\s+(\w+)\s*\(`)
	assertGo   = regexp.MustCompile(`\b(?:assert|require)\w*\.\w+\(|\bt\.(?:Error|Errorf|Fatal|Fatalf|Fail|FailNow)\(|\bcmp\.Diff\(`)
	assertPy   = regexp.MustCompile(`^\s*assert\b|\bself\.assert\w+\(|\bpytest\.raises\(|\bassertRaises\(`)
	assertJS   = regexp.MustCompile(`\bexpect\s*\(|\bassert(?:\.\w+)?\s*\(|\.should\b`)
	assertJava = regexp.MustCompile(`\b(?:assert[A-Z]\w*|assertThat|verify)\s*\(|\bassert\s`)
	assertRust = regexp.MustCompile(`\bassert(?:_eq|_ne)?!\(|#\[should_panic`)
	skipGo     = regexp.MustCompile(`\bt\.Skip(?:f|Now)?\(`)
	skipPy     = regexp.MustCompile(`@pytest\.mark\.(?:skip|skipif|xfail)\b|\bpytest\.(?:skip|xfail)\(|@unittest\.(?:skip|skipIf|skipUnless|expectedFailure)\b|\bself\.skipTest\(`)
	skipJS     = regexp.MustCompile(`\b(?:it|test|describe)\.skip\b|\b(?:xit|xtest|xdescribe)\s*\(`)
	skipJava   = regexp.MustCompile(`@(?:Disabled|Ignore)\b`)
	skipRust   = regexp.MustCompile(`#\[ignore\b`)
)

// declPattern returns the declaration regexp of a test function and, for languages that mark a test on the line
// before its name, the marker regexp.
func declPattern(l Language) (decl, marker *regexp.Regexp) {
	switch l {
	case LangGo:
		return goDecl, nil
	case LangPython:
		return pyDecl, nil
	case LangJS:
		return jsDecl, nil
	case LangJava:
		return javaDecl, javaMark
	case LangRust:
		return rustDecl, rustMark
	case LangUnknown:
		return nil, nil
	}
	return nil, nil
}

func assertPattern(l Language) *regexp.Regexp {
	switch l {
	case LangGo:
		return assertGo
	case LangPython:
		return assertPy
	case LangJS:
		return assertJS
	case LangJava:
		return assertJava
	case LangRust:
		return assertRust
	case LangUnknown:
		return nil
	}
	return nil
}

func skipPattern(l Language) *regexp.Regexp {
	switch l {
	case LangGo:
		return skipGo
	case LangPython:
		return skipPy
	case LangJS:
		return skipJS
	case LangJava:
		return skipJava
	case LangRust:
		return skipRust
	case LangUnknown:
		return nil
	}
	return nil
}

// declaredTests returns the names of the test functions that the lines declare, in order. Marker languages (Java,
// Kotlin, Rust) name a test on the line after the marker or on the marker line itself.
func declaredTests(l Language, lines []string) []string {
	decl, marker := declPattern(l)
	if decl == nil {
		return nil
	}
	var names []string
	pending := false
	for _, line := range lines {
		if marker != nil {
			if marker.MatchString(line) {
				pending = true
			}
			if !pending {
				continue
			}
		}
		if m := decl.FindStringSubmatch(line); m != nil {
			names = append(names, m[1])
			pending = false
		}
	}
	return names
}

// countMatches counts the lines that match re.
func countMatches(re *regexp.Regexp, lines []string) int {
	if re == nil {
		return 0
	}
	n := 0
	for _, line := range lines {
		if re.MatchString(line) {
			n++
		}
	}
	return n
}
