package policy

import (
	"fmt"
	"path"
	"strings"
)

// MatchGlob reports whether name matches a doublestar-style pattern from the policy file. A pattern with no
// slash matches the base name at any depth, so "go.sum" and "*delete*" work anywhere. A pattern with a slash is
// anchored at the repository root, and a "**" segment matches zero or more directories.
func MatchGlob(pattern, name string) bool {
	if !strings.Contains(pattern, "/") {
		ok, err := path.Match(pattern, path.Base(name))
		return err == nil && ok
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

// matchSegments is a dynamic program over (pattern segment, path segment), so a pattern with many "**" segments
// costs O(len(pattern) * len(path)) and cannot blow up exponentially.
func matchSegments(pat, segs []string) bool {
	n, m := len(pat), len(segs)
	// reach[i][j] reports whether pat[i:] matches segs[j:].
	reach := make([][]bool, n+1)
	for i := range reach {
		reach[i] = make([]bool, m+1)
	}
	reach[n][m] = true
	for i := n - 1; i >= 0; i-- {
		for j := m; j >= 0; j-- {
			switch {
			case pat[i] == "**":
				reach[i][j] = reach[i+1][j] || (j < m && reach[i][j+1])
			case j < m:
				ok, err := path.Match(pat[i], segs[j])
				reach[i][j] = err == nil && ok && reach[i+1][j+1]
			}
		}
	}
	return reach[0][0]
}

// ValidateGlob says whether pattern is a well-formed glob for the policy file: relative, no empty or dot
// segments, and every segment a valid path.Match pattern. It accepts no pattern that path.Match would refuse.
func ValidateGlob(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("glob is empty")
	}
	if strings.HasPrefix(pattern, "/") {
		return fmt.Errorf("glob %q must be relative to the repository root", pattern)
	}
	for _, seg := range strings.Split(pattern, "/") {
		if err := validateSegment(pattern, seg); err != nil {
			return err
		}
	}
	return nil
}

func validateSegment(pattern, seg string) error {
	switch seg {
	case "":
		return fmt.Errorf("glob %q has an empty segment", pattern)
	case ".", "..":
		return fmt.Errorf("glob %q must not contain %q", pattern, seg)
	case "**":
		return nil
	}
	if _, err := path.Match(seg, ""); err != nil {
		return fmt.Errorf("glob %q is malformed: %w", pattern, err)
	}
	return nil
}

// matchesEverything reports whether a pattern matches every path, such as "**" or "*". An ignore rule like that
// would turn the slop check off, so the policy refuses it.
func matchesEverything(pattern string) bool {
	segs := strings.Split(pattern, "/")
	for len(segs) > 1 && segs[0] == "**" {
		segs = segs[1:]
	}
	return len(segs) == 1 && (segs[0] == "*" || segs[0] == "**")
}
