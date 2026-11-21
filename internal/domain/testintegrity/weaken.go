package testintegrity

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// weakened is rule 2 for one file: deleted test functions, deleted assertions, a new skip marker and a loosened
// timeout or retry in a test file. It warns, except for a pinned test file, where it blocks (block is true). It adds
// the tautology rule, which has the same level, because a test that cannot fail has the same effect as one that is
// weakened.
func weakened(c FileChange, block bool) []Violation {
	level := LevelWarn
	if block {
		level = LevelBlock
	}
	if !c.IsTest {
		if c.Status == StatusRenamed && c.OldIsTest {
			return []Violation{newViolation(RuleTestDeleted, level, c.OldPath, "",
				"the test file "+c.OldPath+" moved out of the test paths to "+c.Path)}
		}
		return nil
	}
	if c.Status == StatusDeleted {
		return []Violation{newViolation(RuleTestDeleted, level, c.Path, "", "the test file "+c.Path+" was deleted")}
	}
	lang := LanguageOf(c.Path)
	var out []Violation
	out = append(out, deletedTests(c, lang, level)...)
	out = append(out, deletedAssertions(c, lang, level)...)
	out = append(out, addedSkips(c, lang, level)...)
	out = append(out, loosened(c, level)...)
	out = append(out, tautologies(c, lang, level)...)
	return out
}

func deletedTests(c FileChange, lang Language, level Level) []Violation {
	added := declaredTests(lang, c.Added)
	var out []Violation
	for _, name := range declaredTests(lang, c.Removed) {
		if !slices.Contains(added, name) {
			out = append(out, newViolation(RuleTestDeleted, level, c.Path, name,
				"the test "+name+" in "+c.Path+" was deleted"))
		}
	}
	return out
}

func deletedAssertions(c FileChange, lang Language, level Level) []Violation {
	re := assertPattern(lang)
	gone, added := countMatches(re, c.Removed), countMatches(re, c.Added)
	if gone <= added {
		return nil
	}
	return []Violation{newViolation(RuleAssertionDeleted, level, c.Path, "", fmt.Sprintf(
		"%s loses assertions: %d removed, %d added", c.Path, gone, added))}
}

func addedSkips(c FileChange, lang Language, level Level) []Violation {
	re := skipPattern(lang)
	now, before := countMatches(re, c.Added), countMatches(re, c.Removed)
	if now <= before {
		return nil
	}
	return []Violation{newViolation(RuleSkipAdded, level, c.Path, "", fmt.Sprintf(
		"%s adds %d skip marker(s) to its tests", c.Path, now-before))}
}

var (
	// Each kind finds a word, then the first number within 40 characters of it.
	timeoutNum = regexp.MustCompile(`(?i)(?:timeout|deadline)\w*[^0-9\n]{0,40}?(\d+(?:\.\d+)?)`)
	retryNum   = regexp.MustCompile(`(?i)(?:retr(?:y|ies)\w*\s*[(=:]|retryTimes|reruns|flaky)[^0-9\n]{0,40}?(\d+(?:\.\d+)?)`)
	retryMark  = regexp.MustCompile(`(?i)\bretr(?:y|ies)\w*\s*[(=:]|retryTimes|reruns|@flaky|mark\.flaky`)
	goUnit     = regexp.MustCompile(`time\.(Nanosecond|Microsecond|Millisecond|Second|Minute|Hour)`)
	suffixUnit = regexp.MustCompile(`(?i)\d\s*(ms|milliseconds?|seconds?|secs?|s|minutes?|mins?|m|h)\b`)
)

// loosened finds a timeout that grew or a retry that was added or grew. It compares the largest value of each kind
// on the removed lines with the largest on the added lines. A timeout that is new or smaller is not loosened. A line
// with no number gives no value, so a timeout passed as a variable is not seen (a false negative).
func loosened(c FileChange, level Level) []Violation {
	var out []Violation
	if was, ok := largest(timeoutNum, c.Removed); ok {
		if now, found := largest(timeoutNum, c.Added); found && now > was {
			out = append(out, newViolation(RuleTimeoutLoosened, level, c.Path, "",
				c.Path+" raises a timeout from "+trim(was)+" ms to "+trim(now)+" ms"))
		}
	}
	now, found := retryValue(c.Added)
	was, had := retryValue(c.Removed)
	if found && now > 0 && (!had || now > was) {
		out = append(out, newViolation(RuleRetryLoosened, level, c.Path, "", c.Path+" adds or raises a retry"))
	}
	return out
}

// retryValue is the largest retry count on the lines. A retry marker with no count is one retry.
func retryValue(lines []string) (float64, bool) {
	best, found := largest(retryNum, lines)
	if found {
		return best, true
	}
	if countMatches(retryMark, lines) > 0 {
		return 1, true
	}
	return 0, false
}

func largest(re *regexp.Regexp, lines []string) (float64, bool) {
	best, found := 0.0, false
	for _, line := range lines {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		n *= unitMillis(line)
		if !found || n > best {
			best, found = n, true
		}
	}
	return best, found
}

// unitMillis is the factor from the unit on the line to milliseconds. No unit means the number is taken as it is.
func unitMillis(line string) float64 {
	if m := goUnit.FindStringSubmatch(line); m != nil {
		return map[string]float64{
			"Nanosecond": 1e-6, "Microsecond": 1e-3, "Millisecond": 1, "Second": 1e3, "Minute": 6e4, "Hour": 3.6e6,
		}[m[1]]
	}
	if m := suffixUnit.FindStringSubmatch(line); m != nil {
		switch strings.ToLower(m[1]) {
		case "ms", "millisecond", "milliseconds":
			return 1
		case "s", "sec", "secs", "second", "seconds":
			return 1e3
		case "m", "min", "mins", "minute", "minutes":
			return 6e4
		case "h":
			return 3.6e6
		}
	}
	return 1
}

func trim(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
