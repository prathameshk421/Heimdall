package fastcheck

import (
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/sarif"
)

// Facts is everything the fast check reads. The caller fills it from GitHub, the git trees and the profile. Each group
// that can fail to load has a Known flag. A group whose flag is false is a fact that is not known, and its clause is
// unknown: a zero value never helps a change (as in gate.Facts).
type Facts struct {
	// Action is the action area of the policy at the merge base, never at head (invariant 5).
	Action policy.Action

	HeadSHA     id.SHA
	AuthorLogin string
	// PolicyOwners are the GitHub logins of policy.policy_owners at the merge base. One of them approving on head
	// exempts a change to a pinned test or a Heimdall file.
	PolicyOwners []string

	// ChangedFiles lists every path the change touches: the new path of each added or edited file, and both paths of a
	// rename and the old path of a delete.
	ChangedFilesKnown bool
	ChangedFiles      []string

	Secrets   SecretFacts
	Analyzers AnalyzerFacts
	Tests     TestFacts
	Integrity IntegrityFacts
	Coverage  CoverageFacts

	// ApprovalsKnown says the reviews were read. ApproversForChangedDomains are the logins the policy allows to approve
	// the domains of the changed paths.
	ApprovalsKnown             bool
	Approvals                  []gate.Approval
	ApproversForChangedDomains []string

	Memory MemoryFacts
}

// SecretFacts is the result of the secret scan over the added lines.
type SecretFacts struct {
	// Known says the scan ran over the whole diff.
	Known    bool
	Findings []SecretFinding
}

// SecretFinding is one new secret. It names where it is and which rule matched. It never holds the value, so no text
// the check posts can leak it.
type SecretFinding struct {
	File string
	Line int
	Rule string
}

// AnalyzerFacts are the customer's analyzer results for the base and for head. The clause keeps what is new
// (sarif.New), so a result that already existed at the merge base does not count against the change.
type AnalyzerFacts struct {
	Known bool
	Base  []sarif.Result
	Head  []sarif.Result
}

// TestFacts lists what the change does to test files that existed at the merge base.
type TestFacts struct {
	Known   bool
	Changes []TestChange
}

// TestChangeKind is what happened to a test file.
type TestChangeKind string

// The ways a change can touch a test file that existed at the merge base.
const (
	TestDeleted   TestChangeKind = "deleted"
	TestRenamed   TestChangeKind = "renamed"
	TestEdited    TestChangeKind = "edited"
	TestSkipAdded TestChangeKind = "skip_added"
)

// TestChange is one touched test file. Path is its path at the merge base.
type TestChange struct {
	Path string
	Kind TestChangeKind
}

// IntegrityFacts are the violations of the static test-integrity rules (ACTION_MODE 5.1, work package A6). The rules
// live elsewhere; this clause reports what they found.
type IntegrityFacts struct {
	Known      bool
	Violations []IntegrityViolation
}

// IntegrityViolation is one broken test-integrity rule, for example a test that mocks a file the change edits.
type IntegrityViolation struct {
	Path string
	Rule string
}

// CoverageFacts are the executable lines the change adds or edits, and how many of them tests cover. The caller
// computes them from the coverage report of the declared test workflow on head.
type CoverageFacts struct {
	Known        bool
	ChangedLines int
	CoveredLines int
}

// MemoryFacts are the rule cards at the merge base and what each card's check said for this change. Holds maps a card
// name to whether its rule holds. A card with no entry has not been run, which is unknown, not holding.
type MemoryFacts struct {
	Known bool
	Cards []rulecard.Card
	Holds map[string]bool
}
