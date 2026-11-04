// Package id defines the identifiers of the domain as distinct types, so a commit sha cannot be passed where a
// change id is wanted and a malformed value cannot get past decoding. Every type validates in its Parse function and
// again when decoded from JSON or text, so an id that exists in the domain is always well formed.
package id

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	reTicketKey = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]{0,8}$`)
	rePRChange  = regexp.MustCompile(`^pr-[a-z0-9][a-z0-9._-]*-[1-9][0-9]{0,8}$`)
	reRepo      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)
	reHex       = regexp.MustCompile(`^[0-9a-f]+$`)
	reAgent     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	reULID      = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	reRef       = regexp.MustCompile(`^F[1-9][0-9]{0,3}$`)
)

// ChangeID names a change: a ticket key such as PAY-142, or pr-<repo>-<number> for a pull request with no ticket.
type ChangeID string

// ParseChangeID validates s.
func ParseChangeID(s string) (ChangeID, error) {
	if !reTicketKey.MatchString(s) && !rePRChange.MatchString(s) {
		return "", fmt.Errorf("change id %q is neither a ticket key like PAY-142 nor pr-<repo>-<number>", s)
	}
	return ChangeID(s), nil
}

// PRChangeID is the change id of a pull request that has no ticket (D146). repo is owner/name.
func PRChangeID(repo string, number int) (ChangeID, error) {
	if !reRepo.MatchString(repo) {
		return "", fmt.Errorf("repository %q is not owner/name", repo)
	}
	if number < 1 {
		return "", fmt.Errorf("pull request number %d must be 1 or more", number)
	}
	return ParseChangeID(fmt.Sprintf("pr-%s-%d", strings.ToLower(strings.ReplaceAll(repo, "/", "-")), number))
}

// String returns the id as text.
func (c ChangeID) String() string { return string(c) }

// IsPR reports whether the change is a pull request with no ticket.
func (c ChangeID) IsPR() bool { return strings.HasPrefix(string(c), "pr-") }

// UnmarshalText validates while decoding.
func (c *ChangeID) UnmarshalText(b []byte) error {
	v, err := ParseChangeID(string(b))
	*c = v
	return err
}

// SHA is a git commit or tree hash in lowercase hexadecimal: abbreviated (7 to 40 characters) or SHA-256 (64).
type SHA string

// ParseSHA validates s.
func ParseSHA(s string) (SHA, error) {
	n := len(s)
	lengthOK := (n >= 7 && n <= 40) || n == 64
	if !reHex.MatchString(s) || !lengthOK {
		return "", fmt.Errorf("%q is not a git hash (7 to 40 or 64 lowercase hex characters)", s)
	}
	return SHA(s), nil
}

// String returns the full hash.
func (s SHA) String() string { return string(s) }

// Short is the 7-character form people see.
func (s SHA) Short() string {
	if len(s) <= 7 {
		return string(s)
	}
	return string(s[:7])
}

// UnmarshalText validates while decoding.
func (s *SHA) UnmarshalText(b []byte) error {
	v, err := ParseSHA(string(b))
	*s = v
	return err
}

// AgentName is the name of an agent in Switch.
type AgentName string

// ParseAgentName validates s against Switch's naming rule.
func ParseAgentName(s string) (AgentName, error) {
	if !reAgent.MatchString(s) {
		return "", fmt.Errorf("agent name %q must match [a-z0-9][a-z0-9._-]* and be at most 64 characters", s)
	}
	return AgentName(s), nil
}

// String returns the name as text.
func (a AgentName) String() string { return string(a) }

// UnmarshalText validates while decoding.
func (a *AgentName) UnmarshalText(b []byte) error {
	v, err := ParseAgentName(string(b))
	*a = v
	return err
}

// prefixed validates a prefix plus a ULID.
func prefixed(prefix, s string) error {
	rest, ok := strings.CutPrefix(s, prefix+"_")
	if !ok || !reULID.MatchString(rest) {
		return fmt.Errorf("%q is not %s_ followed by a 26-character ULID", s, prefix)
	}
	return nil
}

// FindingID is the internal id of a finding (fnd_ and a ULID). People see a FindingRef instead.
type FindingID string

// RunID is the id of a sandbox run (run_ and a ULID).
type RunID string

// CaseID is the id of a deliberation case (case_ and a ULID).
type CaseID string

// TaskID is the id of an agent task (tsk_ and a ULID).
type TaskID string

// ParseFindingID validates s.
func ParseFindingID(s string) (FindingID, error) {
	if err := prefixed("fnd", s); err != nil {
		return "", err
	}
	return FindingID(s), nil
}

// ParseRunID validates s.
func ParseRunID(s string) (RunID, error) {
	if err := prefixed("run", s); err != nil {
		return "", err
	}
	return RunID(s), nil
}

// ParseCaseID validates s.
func ParseCaseID(s string) (CaseID, error) {
	if err := prefixed("case", s); err != nil {
		return "", err
	}
	return CaseID(s), nil
}

// ParseTaskID validates s.
func ParseTaskID(s string) (TaskID, error) {
	if err := prefixed("tsk", s); err != nil {
		return "", err
	}
	return TaskID(s), nil
}

// String returns the id as text.
func (f FindingID) String() string { return string(f) }

// String returns the id as text.
func (r RunID) String() string { return string(r) }

// String returns the id as text.
func (c CaseID) String() string { return string(c) }

// String returns the id as text.
func (t TaskID) String() string { return string(t) }

// UnmarshalText validates while decoding.
func (f *FindingID) UnmarshalText(b []byte) error {
	v, err := ParseFindingID(string(b))
	*f = v
	return err
}

// UnmarshalText validates while decoding.
func (r *RunID) UnmarshalText(b []byte) error {
	v, err := ParseRunID(string(b))
	*r = v
	return err
}

// UnmarshalText validates while decoding.
func (c *CaseID) UnmarshalText(b []byte) error {
	v, err := ParseCaseID(string(b))
	*c = v
	return err
}

// UnmarshalText validates while decoding.
func (t *TaskID) UnmarshalText(b []byte) error {
	v, err := ParseTaskID(string(b))
	*t = v
	return err
}

// FindingRef is the short per-change name people use for a finding: F1, F2. It is stored as the number.
type FindingRef int

// ParseFindingRef validates s.
func ParseFindingRef(s string) (FindingRef, error) {
	if !reRef.MatchString(s) {
		return 0, fmt.Errorf("%q is not a finding name like F3", s)
	}
	n, err := strconv.Atoi(s[1:])
	if err != nil {
		return 0, fmt.Errorf("finding name %q: %w", s, err)
	}
	return FindingRef(n), nil
}

// NewFindingRef is the nth finding of a change, counting from 1.
func NewFindingRef(n int) FindingRef { return FindingRef(n) }

// Number is the n of Fn.
func (r FindingRef) Number() int { return int(r) }

// String is the text people see, such as F3.
func (r FindingRef) String() string { return "F" + strconv.Itoa(int(r)) }

// MarshalText encodes the ref as F3 so a stored value reads as people say it.
func (r FindingRef) MarshalText() ([]byte, error) { return []byte(r.String()), nil }

// UnmarshalText validates while decoding.
func (r *FindingRef) UnmarshalText(b []byte) error {
	v, err := ParseFindingRef(string(b))
	*r = v
	return err
}
