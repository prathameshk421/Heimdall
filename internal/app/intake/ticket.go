package intake

import (
	"regexp"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// reKeyCandidate finds ticket-key-shaped text. Each candidate is then checked for token edges and validated by
// id.ParseChangeID, so the regexp only has to be generous.
var reKeyCandidate = regexp.MustCompile(`[A-Z][A-Z0-9_]+-[0-9]+`)

// ticketKey returns the first ticket key in the branch, then in the title, or "" when neither names one. The branch
// is read first because the branch is what the test author and developer chose for the work.
func ticketKey(branch, title string) id.ChangeID {
	for _, text := range []string{branch, title} {
		if k := keyIn(text); k != "" {
			return k
		}
	}
	return ""
}

// keyIn returns the first candidate in text that is a whole token and a valid ticket key, or "".
func keyIn(text string) id.ChangeID {
	for _, loc := range reKeyCandidate.FindAllStringIndex(text, -1) {
		if !tokenEdge(text, loc[0]-1) || !tokenEdge(text, loc[1]) {
			continue
		}
		if k, err := id.ParseChangeID(text[loc[0]:loc[1]]); err == nil {
			return k
		}
	}
	return ""
}

// tokenEdge reports whether byte i of s lies outside a token: the start or end of s, or a byte that is not an ASCII
// letter or digit. A hyphen or a slash is an edge, so feature/PAY-142-refund matches.
func tokenEdge(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	switch c := s[i]; {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return false
	}
	return true
}
