package testintegrity

import (
	"regexp"
	"strconv"
)

var (
	// Constant checks: the assertion holds whatever the code does.
	tautConstGo   = regexp.MustCompile(`\b(?:assert|require)\.True\(\s*t\s*,\s*true\s*\)|\b(?:assert|require)\.False\(\s*t\s*,\s*false\s*\)|\b(?:assert|require)\.Nil\(\s*t\s*,\s*nil\s*\)`)
	tautConstPy   = regexp.MustCompile(`^\s*assert\s+True\s*(?:,.*)?$|\bself\.assertTrue\(\s*True\s*\)|\bself\.assertFalse\(\s*False\s*\)`)
	tautConstJS   = regexp.MustCompile(`\bexpect\(\s*true\s*\)\.toBe(?:Truthy)?\(\s*(?:true)?\s*\)|\bexpect\(\s*false\s*\)\.toBe(?:Falsy)?\(\s*(?:false)?\s*\)|\bassert\(\s*true\s*\)`)
	tautConstJava = regexp.MustCompile(`\bassertTrue\(\s*true\s*\)|\bassertFalse\(\s*false\s*\)`)
	tautConstRust = regexp.MustCompile(`\bassert!\(\s*true\s*\)`)
	// Same-value checks: the two sides are the same text, so the assertion holds whatever the code does.
	tautPairGo   = regexp.MustCompile(`\b(?:assert|require)\.(?:Equal|EqualValues|Exactly)\(\s*\w+\s*,\s*([^,]+?)\s*,\s*([^,)]+?)\s*[,)]`)
	tautPairPy   = regexp.MustCompile(`^\s*assert\s+(\S+)\s*==\s*(\S+)\s*$|\bself\.assertEqual\(\s*([^,]+?)\s*,\s*([^,)]+?)\s*\)`)
	tautPairJS   = regexp.MustCompile(`\bexpect\(\s*([^()]+?)\s*\)\.(?:toBe|toEqual|toStrictEqual)\(\s*([^()]+?)\s*\)`)
	tautPairJava = regexp.MustCompile(`\bassertEquals\(\s*([^,]+?)\s*,\s*([^,)]+?)\s*\)`)
	tautPairRust = regexp.MustCompile(`\bassert_eq!\(\s*([^,]+?)\s*,\s*([^,)]+?)\s*\)`)
)

func tautologyPatterns(l Language) (constant, pair *regexp.Regexp) {
	switch l {
	case LangGo:
		return tautConstGo, tautPairGo
	case LangPython:
		return tautConstPy, tautPairPy
	case LangJS:
		return tautConstJS, tautPairJS
	case LangJava:
		return tautConstJava, tautPairJava
	case LangRust:
		return tautConstRust, tautPairRust
	case LangUnknown:
		return nil, nil
	}
	return nil, nil
}

// tautologies flags an added assertion that cannot fail: it checks a constant, or it compares a text with itself. It
// warns once for the file and names the count. A person who wrote assert.Equal(t, got, got) on purpose is a false
// positive, and a comparison of two different spellings of one value is a false negative.
func tautologies(c FileChange, lang Language, level Level) []Violation {
	constant, pair := tautologyPatterns(lang)
	n := 0
	for _, line := range c.Added {
		if isTautology(constant, pair, line) {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	return []Violation{newViolation(RuleTautology, level, c.Path, "", c.Path+" adds "+plural(n, "assertion")+" that cannot fail")}
}

func isTautology(constant, pair *regexp.Regexp, line string) bool {
	if constant != nil && constant.MatchString(line) {
		return true
	}
	if pair == nil {
		return false
	}
	m := pair.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	// A pattern with two alternatives fills its groups in pairs. Compare the pair that matched.
	for i := 1; i+1 < len(m); i += 2 {
		if m[i] != "" && m[i] == m[i+1] {
			return true
		}
	}
	return false
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}
