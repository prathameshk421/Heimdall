// Package id defines the identifiers of the domain as distinct types, so a commit sha cannot be passed where a
// change id is wanted and a malformed value cannot get past decoding.
//
// The types are plain strings (or ints for FindingRef), not structs. Values are validated by the constructors and
// Parse functions, and again when decoded from JSON or text, and each type has a Valid method. A conversion such as
// ChangeID("x") bypasses that validation, so code that receives a value from outside the package must go through
// Parse or UnmarshalText, and code that builds one must use the constructor. Valid reports whether a value would
// pass; it does not make an unvalidated conversion safe.
package id

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	reTicketKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,9}-[1-9][0-9]{0,8}$`)
	rePRChange  = regexp.MustCompile(`^pr-[a-z0-9][a-z0-9._-]*-[1-9][0-9]{0,8}$`)
	reRepo      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)
	reHex       = regexp.MustCompile(`^[0-9a-f]+$`)
	reAgent     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	reRef       = regexp.MustCompile(`^F[1-9][0-9]{0,3}$`)
)

// maxFindingRef is the largest finding number a FindingRef can hold and still round trip through its text form.
const maxFindingRef = 9999

// ChangeID names a change: a ticket key such as PAY-142, or pr-<repo>-<number> for a pull request with no ticket.
type ChangeID string

// ParseChangeID validates s.
func ParseChangeID(s string) (ChangeID, error) {
	if !reTicketKey.MatchString(s) && !rePRChange.MatchString(s) {
		return "", fmt.Errorf("change id %q is neither a ticket key like PAY-142 nor pr-<repo>-<number>", s)
	}
	return ChangeID(s), nil
}

// PRChangeID is the change id of a pull request that has no ticket (D146). repo is owner/name. The mapping is
// injective: a hyphen in the owner is written as two hyphens, so the single hyphen that follows the owner is the
// only separator, and a/b-c and a-b/c get different streams. PullRequest reverses it.
func PRChangeID(repo string, number int) (ChangeID, error) {
	if !reRepo.MatchString(repo) {
		return "", fmt.Errorf("repository %q is not owner/name", repo)
	}
	if number < 1 {
		return "", fmt.Errorf("pull request number %d must be 1 or more", number)
	}
	owner, name, _ := strings.Cut(strings.ToLower(repo), "/")
	escaped := strings.ReplaceAll(owner, "-", "--")
	return ParseChangeID(fmt.Sprintf("pr-%s-%s-%d", escaped, name, number))
}

// PullRequest returns the repository and number of a pull request change id. It fails for a ticket key, and for a
// pr- id that PRChangeID would not have produced, so a value returned here always maps back to the same id.
func (c ChangeID) PullRequest() (string, int, error) {
	rest, ok := strings.CutPrefix(string(c), "pr-")
	if !ok {
		return "", 0, fmt.Errorf("change %q is not a pull request id", c)
	}
	cut := strings.LastIndexByte(rest, '-')
	if cut < 0 {
		return "", 0, fmt.Errorf("change %q has no pull request number", c)
	}
	number, err := strconv.Atoi(rest[cut+1:])
	if err != nil {
		return "", 0, fmt.Errorf("change %q: pull request number: %w", c, err)
	}
	owner, name, err := splitOwnerName(rest[:cut])
	if err != nil {
		return "", 0, fmt.Errorf("change %q: %w", c, err)
	}
	repo := owner + "/" + name
	back, err := PRChangeID(repo, number)
	if err != nil || back != c {
		return "", 0, fmt.Errorf("change %q is not a canonical pull request id", c)
	}
	return repo, number, nil
}

// splitOwnerName reads the owner, where each hyphen is doubled, up to the first single hyphen, and takes the rest
// as the name.
func splitOwnerName(head string) (string, string, error) {
	var owner strings.Builder
	for i := 0; i < len(head); i++ {
		if head[i] != '-' {
			owner.WriteByte(head[i])
			continue
		}
		if i+1 < len(head) && head[i+1] == '-' {
			owner.WriteByte('-')
			i++
			continue
		}
		return owner.String(), head[i+1:], nil
	}
	return "", "", fmt.Errorf("no separator between owner and repository name")
}

// String returns the id as text.
func (c ChangeID) String() string { return string(c) }

// IsPR reports whether the change is a pull request with no ticket.
func (c ChangeID) IsPR() bool { return strings.HasPrefix(string(c), "pr-") }

// Valid reports whether the id passes ParseChangeID.
func (c ChangeID) Valid() bool {
	_, err := ParseChangeID(string(c))
	return err == nil
}

// UnmarshalText validates while decoding and leaves c unchanged on error.
func (c *ChangeID) UnmarshalText(b []byte) error {
	v, err := ParseChangeID(string(b))
	if err != nil {
		return err
	}
	*c = v
	return nil
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

// Valid reports whether the hash passes ParseSHA.
func (s SHA) Valid() bool {
	_, err := ParseSHA(string(s))
	return err == nil
}

// UnmarshalText validates while decoding and leaves s unchanged on error.
func (s *SHA) UnmarshalText(b []byte) error {
	v, err := ParseSHA(string(b))
	if err != nil {
		return err
	}
	*s = v
	return nil
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

// Valid reports whether the name passes ParseAgentName.
func (a AgentName) Valid() bool {
	_, err := ParseAgentName(string(a))
	return err == nil
}

// UnmarshalText validates while decoding and leaves a unchanged on error.
func (a *AgentName) UnmarshalText(b []byte) error {
	v, err := ParseAgentName(string(b))
	if err != nil {
		return err
	}
	*a = v
	return nil
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

// Valid reports whether the id passes ParseFindingID.
func (f FindingID) Valid() bool { _, err := ParseFindingID(string(f)); return err == nil }

// Valid reports whether the id passes ParseRunID.
func (r RunID) Valid() bool { _, err := ParseRunID(string(r)); return err == nil }

// Valid reports whether the id passes ParseCaseID.
func (c CaseID) Valid() bool { _, err := ParseCaseID(string(c)); return err == nil }

// Valid reports whether the id passes ParseTaskID.
func (t TaskID) Valid() bool { _, err := ParseTaskID(string(t)); return err == nil }

// UnmarshalText validates while decoding and leaves f unchanged on error.
func (f *FindingID) UnmarshalText(b []byte) error {
	v, err := ParseFindingID(string(b))
	if err != nil {
		return err
	}
	*f = v
	return nil
}

// UnmarshalText validates while decoding and leaves r unchanged on error.
func (r *RunID) UnmarshalText(b []byte) error {
	v, err := ParseRunID(string(b))
	if err != nil {
		return err
	}
	*r = v
	return nil
}

// UnmarshalText validates while decoding and leaves c unchanged on error.
func (c *CaseID) UnmarshalText(b []byte) error {
	v, err := ParseCaseID(string(b))
	if err != nil {
		return err
	}
	*c = v
	return nil
}

// UnmarshalText validates while decoding and leaves t unchanged on error.
func (t *TaskID) UnmarshalText(b []byte) error {
	v, err := ParseTaskID(string(b))
	if err != nil {
		return err
	}
	*t = v
	return nil
}

// FindingRef is the short per-change name people use for a finding: F1, F2. It is stored as the number, from 1 to
// 9999 so that every valid value marshals to text that unmarshals again.
type FindingRef int

// NewFindingRef is the nth finding of a change, counting from 1. It fails outside 1 to 9999.
func NewFindingRef(n int) (FindingRef, error) {
	if n < 1 || n > maxFindingRef {
		return 0, fmt.Errorf("finding number %d must be from 1 to %d", n, maxFindingRef)
	}
	return FindingRef(n), nil
}

// ParseFindingRef validates s.
func ParseFindingRef(s string) (FindingRef, error) {
	if !reRef.MatchString(s) {
		return 0, fmt.Errorf("%q is not a finding name like F3", s)
	}
	n, err := strconv.Atoi(s[1:])
	if err != nil {
		return 0, fmt.Errorf("finding name %q: %w", s, err)
	}
	return NewFindingRef(n)
}

// Number is the n of Fn.
func (r FindingRef) Number() int { return int(r) }

// String is the text people see, such as F3.
func (r FindingRef) String() string { return "F" + strconv.Itoa(int(r)) }

// Valid reports whether the ref is between F1 and F9999.
func (r FindingRef) Valid() bool { _, err := NewFindingRef(int(r)); return err == nil }

// MarshalText encodes the ref as F3 so a stored value reads as people say it. It refuses a value that could not be
// read back.
func (r FindingRef) MarshalText() ([]byte, error) {
	if !r.Valid() {
		return nil, fmt.Errorf("finding number %d cannot be written as a finding name", int(r))
	}
	return []byte(r.String()), nil
}

// UnmarshalText validates while decoding and leaves r unchanged on error.
func (r *FindingRef) UnmarshalText(b []byte) error {
	v, err := ParseFindingRef(string(b))
	if err != nil {
		return err
	}
	*r = v
	return nil
}
