package finding

import (
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CheckRepoPath returns an error unless p is a non-empty, clean, relative, forward-slash path inside the repository and
// at most MaxPathBytes bytes long. Findings, reviews and test sets all name files this way, so the rule lives once.
func CheckRepoPath(p string) error {
	switch {
	case p == "":
		return fmt.Errorf("must not be blank")
	case len(p) > MaxPathBytes:
		return fmt.Errorf("must be at most %d bytes, got %d", MaxPathBytes, len(p))
	case strings.ContainsFunc(p, unicode.IsControl), strings.Contains(p, `\`):
		return fmt.Errorf("%q must use forward slashes and contain no control characters", p)
	case path.IsAbs(p), p == ".", p == "..", strings.HasPrefix(p, "../"), path.Clean(p) != p:
		return fmt.Errorf("%q is not a clean relative path inside the repository", p)
	}
	return nil
}

// checkChars counts the characters of s after trimming its surrounding white space, so padding cannot satisfy a minimum.
func checkChars(s string, minChars, maxChars int) error {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	if n < minChars || n > maxChars {
		return fmt.Errorf("must be %d to %d characters, got %d", minChars, maxChars, n)
	}
	return nil
}

// checkRef accepts a memory or pack reference that is not blank and fits MaxRefBytes.
func checkRef(s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("must not be blank")
	}
	if len(s) > MaxRefBytes {
		return fmt.Errorf("must be at most %d bytes, got %d", MaxRefBytes, len(s))
	}
	return nil
}

// entries says n in words the way a reader expects: "1 entry", "9 entries".
func entries(n int) string {
	if n == 1 {
		return "1 entry"
	}
	return fmt.Sprintf("%d entries", n)
}
