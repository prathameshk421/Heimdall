package testintegrity

// Facts is everything the static rules read. The caller fills it from the diff, the policy globs and the pinned set.
// This package parses no file. The caller turns each changed file into a FileChange and each changed test file into
// a TestFile.
type Facts struct {
	// Files lists every path the change touches, with the lines it adds and removes.
	Files []FileChange
	// Pinned lists the pinned red tests: files or single test functions the author must not edit.
	Pinned []Pinned
	// Tests lists the parsed mock facts of the test files that the change adds or edits.
	Tests []TestFile
	// OwnerApproved says a policy owner approved the head commit. The caller decides it as fastcheck does: the review
	// is approved, it is on head, it is not from the author, and the login is a policy owner at the merge base.
	// OwnerKnown is false when the reviews, the head or the author are not known.
	OwnerApproved bool
	OwnerKnown    bool
}

// ChangeStatus is what happened to a path.
type ChangeStatus string

// The four things a change does to a path.
const (
	StatusAdded    ChangeStatus = "added"
	StatusModified ChangeStatus = "modified"
	StatusDeleted  ChangeStatus = "deleted"
	StatusRenamed  ChangeStatus = "renamed"
)

// FileChange is one touched path. Path is the path at head. OldPath is the path at the merge base and is set only for
// a rename. For a deleted file Path is the deleted path.
type FileChange struct {
	Path    string
	OldPath string
	Status  ChangeStatus
	// IsTest says Path matches the policy test globs. OldIsTest says OldPath did, for a rename.
	IsTest    bool
	OldIsTest bool
	// Added and Removed hold the text of each added and removed line, without the diff marker. A deleted file may leave
	// Removed empty.
	Added   []string
	Removed []string
	// EditedTests names the test functions whose body the diff touches (the function context of a hunk). The caller may
	// leave it empty. The rules also find a changed declaration line on their own.
	EditedTests []string
}

// Pinned is one pinned red test. Tests is empty when the whole file is pinned.
type Pinned struct {
	Path  string
	Tests []string
}

// MockKind says how a test replaces a part of the program.
type MockKind string

// The mock forms the rules know. The caller parses them.
const (
	// MockGoFake is a type in a Go test file that stands in for an interface or type declared in a file of the same
	// package. Target is the path of that declaring file.
	MockGoFake MockKind = "go_fake"
	// MockGoGenerated is a gomock or mockery mock. Target is the import path of the mocks package, such as
	// "github.com/acme/shop/internal/pay/mocks", or the source file the mock was generated from.
	MockGoGenerated MockKind = "go_generated"
	// MockPythonPatch is unittest.mock.patch, mocker.patch or patch.object. Target is the dotted name, such as
	// "shop.billing.refund.compute_refund".
	MockPythonPatch MockKind = "python_patch"
	// MockJSMock is jest.mock or vi.mock. Target is the module string as written, such as "../src/refund".
	MockJSMock MockKind = "js_mock"
)

// MockFact is one mock the caller found in a test file.
type MockFact struct {
	Kind   MockKind
	Target string
	// Line is the line in the test file, for the message.
	Line int
	// Added says the mock sits in a line that the change adds. An old mock in an edited test is not this change's
	// doing, so the rule skips it. The caller sets it from the diff.
	Added bool
}

// TestFile holds the parsed facts of one test file.
type TestFile struct {
	Path  string
	Mocks []MockFact
}

// Level says how hard a violation hits the clause.
type Level string

// The two levels. Block makes the test_integrity clause unmet. Warn is shown and never blocks.
const (
	LevelBlock Level = "block"
	LevelWarn  Level = "warn"
)

// Rule is the stable id of a rule. It is the Rule text of a fastcheck IntegrityViolation.
type Rule string

// The rules. The strings are stable: a policy, a comment and a bench count cite them.
const (
	RulePinnedEdited       Rule = "pinned-test-edited"
	RulePinnedDeleted      Rule = "pinned-test-deleted"
	RuleTestDeleted        Rule = "test-deleted"
	RuleAssertionDeleted   Rule = "assertion-deleted"
	RuleSkipAdded          Rule = "skip-added"
	RuleTimeoutLoosened    Rule = "timeout-loosened"
	RuleRetryLoosened      Rule = "retry-loosened"
	RuleTautology          Rule = "tautological-assertion"
	RuleMocksChangedSource Rule = "mocks-changed-file"
)

// Violation is one broken rule. ID is stable for the same rule, path and subject, so a comment can update in place.
type Violation struct {
	ID    string
	Rule  Rule
	Level Level
	Path  string
	// Subject is the test function, marker or mock target the rule names. It is empty for a whole file.
	Subject string
	// Message is one sentence for a person. It never carries file content beyond a name.
	Message string
}

// Options tune the levels that the design leaves open.
type Options struct {
	// MockLevel is the level of mocks-changed-file. The zero value is warn, because the detector is a heuristic.
	MockLevel Level
}
