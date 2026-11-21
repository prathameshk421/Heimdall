package summary

import (
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// MaxBytes is the size the rendered comment never passes. GitHub documents 65,536 characters for a comment body, and
// ACTION_MODE 2.6 keeps the renderer at 60,000. It is counted in bytes, which is never less than characters.
const MaxBytes = 60_000

// Reviewer says who raised a finding. The known values are Heimdall and the tools of package external. Any other
// string is shown as text.
type Reviewer string

// The reviewers the summary names.
const (
	ReviewerHeimdall   Reviewer = "heimdall"
	ReviewerCodeRabbit          = Reviewer(external.ToolCodeRabbit)
	ReviewerCopilot             = Reviewer(external.ToolCopilot)
	ReviewerBugbot              = Reviewer(external.ToolBugbot)
	ReviewerGreptile            = Reviewer(external.ToolGreptile)
)

// EvidenceKind says what backs a proven finding.
type EvidenceKind string

// The three kinds, one per proven tier: E1 a failing test, E2 a tool result, E3 a team rule card.
const (
	EvidenceTest     EvidenceKind = "test"
	EvidenceTool     EvidenceKind = "tool"
	EvidenceRuleCard EvidenceKind = "rule_card"
)

// Evidence names what backs a proven finding. For a test, Name is the failing test. For a tool, Name is the tool and
// Rule is its rule id. For a rule card, Name is the card.
type Evidence struct {
	Kind EvidenceKind
	Name string
	Rule string
}

// Finding is one finding, ours or a third party's, with what the proof run did with it. ID is the short id a person
// uses ("F1"); a value that is not "F" and digits is shown as "F?", so an internal id never reaches the page. Result
// carries the class, the drop reason or the not-checked reason, and whether the finding blocks under the policy.
type Finding struct {
	ID        string
	Reporters []Reviewer
	Tier      finding.Tier
	State     finding.State
	Severity  finding.Severity
	Location  finding.Location
	Claim     string
	Evidence  Evidence
	Result    proof.Finding
}

// Usage is what the proof outcome does not carry: the month to date and the wall time. A false Known flag means the
// number is not given, and the line leaves it out.
type Usage struct {
	MonthUSD   float64
	MonthKnown bool
	Wall       time.Duration
	WallKnown  bool
}

// Input is everything Render reads. All of it is a value: the renderer reads no clock and no source.
type Input struct {
	// Change and Head and Version fill the hidden marker. Change is the pull request, Version counts the writes.
	Change  string
	Version int
	// Head is the full head commit id, in lower case hex.
	Head string
	// Action is the action area at the merge base. Its mode and proof_required decide how blocking is worded.
	Action   policy.Action
	Fast     fastcheck.Result
	Proof    proof.Outcome
	Findings []Finding
	Usage    Usage
}
