package commands

// Source names the platform a command arrived from. The values are the keys
// the people map uses ("chat:<sender>", "github:<login>"), so they must not change.
type Source string

const (
	// SourceChat is a post in a change room. Commands start with the plain word `heimdall`.
	SourceChat Source = "chat"
	// SourceGitHub is a PR comment. Commands start with `/heimdall`.
	SourceGitHub Source = "github"
)

// Input is one post or comment as Switch or GitHub delivered it.
type Input struct {
	Text   string
	Source Source
	// Sender is the opaque platform id: the Switch sender id in chat, the login on GitHub.
	Sender string
}

// Kind says what Parse made of an Input. The zero value is KindNotCommand, so a
// result that was never filled in is ignored rather than acted on.
type Kind int

const (
	// KindNotCommand is text not addressed to Heimdall. Ignore it, no reply.
	KindNotCommand Kind = iota
	// KindCommand is a parsed command: Command is set and the sender is a mapped person.
	KindCommand
	// KindUnknown is addressed to Heimdall, but the verb is not one we know. Reply is the grammar.
	KindUnknown
	// KindBadArgs is a known verb with missing or invalid arguments. Reply says what is wrong and the usage.
	KindBadArgs
	// KindUnmapped is a command from a sender that maps to no person. Reply says how to link; nothing runs.
	KindUnmapped
)

// Result is what Parse returns. Only Command is acted on; Reply is posted as is.
// Person is the mapped person's name and is set for every kind except KindNotCommand and KindUnmapped.
type Result struct {
	Kind    Kind
	Command Command
	Verb    string
	Person  string
	Reply   string
}

// Command is one typed decision or request from a mapped person. Each
// implementation is a plain value so callers can type-switch on it.
type Command interface {
	Verb() string
}

// Uphold keeps a finding blocking. Finding is the short id, such as F3.
type Uphold struct{ Finding string }

// Verb returns the command word.
func (Uphold) Verb() string { return "uphold" }

// Dismiss clears a finding with a reason of at least 15 characters.
type Dismiss struct{ Finding, Reason string }

// Verb returns the command word.
func (Dismiss) Verb() string { return "dismiss" }

// Start starts a ticket by hand. Ticket is an upper-case key such as KAN-12.
type Start struct{ Ticket string }

// Verb returns the command word.
func (Start) Verb() string { return "start" }

// Answer gives a person's answer to one question the agent asked. Question is the short id the room shows (Q1, Q2);
// Text is the answer, as the person wrote it.
type Answer struct{ Question, Text string }

// Verb returns the command word.
func (Answer) Verb() string { return "answer" }

// Resume releases an escalated workflow step so the engine offers it again. Step is the workflow step id.
type Resume struct{ Step string }

// Verb returns the command word.
func (Resume) Verb() string { return "resume" }

// Review asks one to three reviewers. Reviewers are handles without the leading @.
type Review struct{ Reviewers []string }

// Verb returns the command word.
func (Review) Verb() string { return "review" }

// ApproveTests approves the test set for a sensitive domain.
type ApproveTests struct{}

// Verb returns the command word.
func (ApproveTests) Verb() string { return "approve-tests" }

// ApproveProfile records the policy-owner approval of a sandbox profile proposal.
type ApproveProfile struct{ ID string }

// Verb returns the command word.
func (ApproveProfile) Verb() string { return "approve-profile" }

// ApproveKnowledge authorizes one staged knowledge source for ingestion.
type ApproveKnowledge struct{ Ref string }

// Verb returns the command word.
func (ApproveKnowledge) Verb() string { return "approve-knowledge" }

// PromoteProfile selects a profile after its isolated trial has passed.
type PromoteProfile struct{ ID string }

// Verb returns the command word.
func (PromoteProfile) Verb() string { return "promote-profile" }

// Challenge asks for a finding to be re-checked against new evidence.
type Challenge struct{ Finding, Evidence string }

// Verb returns the command word.
func (Challenge) Verb() string { return "challenge" }

// Remember records an observed fact. Text keeps its line breaks.
type Remember struct{ Text string }

// Verb returns the command word.
func (Remember) Verb() string { return "remember" }

// Forget invalidates a memory fact. MemoryRef is the id the memory system shows.
type Forget struct{ MemoryRef, Reason string }

// Verb returns the command word.
func (Forget) Verb() string { return "forget" }

// SpecChange records a change to the spec. Text keeps its line breaks.
type SpecChange struct{ Text string }

// Verb returns the command word.
func (SpecChange) Verb() string { return "spec-change" }

// Reject rejects the change with a reason.
type Reject struct{ Reason string }

// Verb returns the command word.
func (Reject) Verb() string { return "reject" }

// Override reverses an applied verdict on a case.
type Override struct{ Case, Reason string }

// Verb returns the command word.
func (Override) Verb() string { return "override" }

// WaiveReviewer waives one reviewer, given as a handle without the leading @.
type WaiveReviewer struct{ Reviewer, Reason string }

// Verb returns the command word.
func (WaiveReviewer) Verb() string { return "waive-reviewer" }

// Incident records an incident episode. Text keeps its line breaks.
type Incident struct{ Text string }

// Verb returns the command word.
func (Incident) Verb() string { return "incident" }
