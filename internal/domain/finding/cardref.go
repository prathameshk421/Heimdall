package finding

import "strings"

// CardPrefix marks a context ref that names a rule card. Such a ref says which rule led to a finding. It grants no tier.
const CardPrefix = "card:"

// CardRef reports whether ref names a rule card. The name is empty when the ref is not well formed: it must be a plain
// file name with no path part, at most MaxRefBytes long.
func CardRef(ref string) (name string, isCard bool) {
	rest, ok := strings.CutPrefix(ref, CardPrefix)
	if !ok {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	if rest == "" || len(ref) > MaxRefBytes || strings.ContainsAny(rest, "/\\\x00 ") || strings.HasPrefix(rest, ".") {
		return "", true
	}
	return rest, true
}

// CardEvidence parses a memory_ref that cites a confirmed rule card for one file: "card:<file name>#<repo path>". The
// path is part of the ref because a card has force only for a file its owner may approve, and the check must hold for
// the file the finding names. The bool is false when ref does not cite a card this way.
func CardEvidence(ref string) (name, file string, ok bool) {
	head, file, found := strings.Cut(ref, "#")
	if !found || CheckRepoPath(file) != nil {
		return "", "", false
	}
	name, isCard := CardRef(head)
	if !isCard || name == "" {
		return "", "", false
	}
	return name, file, true
}
