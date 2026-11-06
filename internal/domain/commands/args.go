package commands

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// These patterns are the whole argument grammar. Anything else is refused, so
// a finding can never be a ULID and a ticket key can never carry shell text.
var (
	findingRE         = regexp.MustCompile(`^F[1-9][0-9]{0,5}$`)
	ticketRE          = regexp.MustCompile(`^[A-Z][A-Z0-9]*-[0-9]{1,9}$`)
	nameRE            = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	stepRE            = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
	handleRE          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
	profileProposalRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

const minDismissReason = 15

var (
	errFinding     = errors.New("finding must look like F3, the short id shown in the room, never a ULID")
	errTicket      = errors.New("ticket must look like KAN-12, a project key, a dash and a number")
	errNoAt        = errors.New("reviewer must start with @")
	errBadHandle   = errors.New("reviewer is not a valid handle")
	errShortReason = errors.New("reason must be at least 15 characters")
	errNoEvidence  = errors.New("challenge needs evidence: a test path at head, a run id, or a memory or decision ref")
)

// cutWord splits s at its first whitespace run. The head is the first word.
// The rest is trimmed at both ends but keeps its inner line breaks, which
// free-text commands such as remember need.
func cutWord(s string) (string, string) {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	i := strings.IndexFunc(s, unicode.IsSpace)
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimSpace(s[i:])
}

func parseProfileProposalID(args string) (string, error) {
	value, rest := cutWord(args)
	if rest != "" || !profileProposalRE.MatchString(value) {
		return "", errors.New("proposal id must be 64 lowercase hex characters")
	}
	return value, nil
}

// fail builds the bad-arguments reply: the reason, then the usage line for the
// source, so the sender sees the exact form to type.
func fail(p, use string, err error) error {
	return fmt.Errorf("%w. Usage: %s %s", err, p, use)
}

// parseFinding accepts F1 to F999999 in any case. Leading zeros and bare
// numbers are refused, so there is one spelling per finding.
func parseFinding(tok string) (string, error) {
	f := strings.ToUpper(tok)
	if !findingRE.MatchString(f) {
		return "", errFinding
	}
	return f, nil
}

// parseTicket accepts an upper-case project key, a dash and a number. Lower
// case is folded to upper.
func parseTicket(tok string) (string, error) {
	t := strings.ToUpper(tok)
	if !ticketRE.MatchString(t) {
		return "", errTicket
	}
	return t, nil
}

// parseName checks a case or memory ref. The message names the field so the
// reply says which argument was wrong.
func parseName(tok, what string) (string, error) {
	if !nameRE.MatchString(tok) {
		return "", fmt.Errorf("%s must be letters, digits, _ or -", what)
	}
	return tok, nil
}

// parseHandle takes a reviewer written as @name and returns the name without @.
func parseHandle(tok string) (string, error) {
	if !strings.HasPrefix(tok, "@") {
		return "", errNoAt
	}
	name := tok[1:]
	if !handleRE.MatchString(name) {
		return "", errBadHandle
	}
	return name, nil
}

// parseReviewers takes one to three distinct handles. Repeats are compared
// without regard to case, since GitHub logins are not case-sensitive.
func parseReviewers(args string) ([]string, error) {
	toks := strings.Fields(args)
	if len(toks) == 0 {
		return nil, errors.New("review needs one to three reviewers")
	}
	if len(toks) > 3 {
		return nil, errors.New("review takes 1 to 3 reviewers")
	}
	seen := make(map[string]bool, len(toks))
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		h, err := parseHandle(t)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(h)
		if seen[key] {
			return nil, errors.New("a reviewer is listed twice")
		}
		seen[key] = true
		out = append(out, h)
	}
	return out, nil
}
