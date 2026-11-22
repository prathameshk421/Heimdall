package external

import (
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// The bounds. Each one is a fact in doc.go.
const (
	// MaxBodyBytes is the largest body that is read. GitHub's own limit for a comment is 65536 characters.
	MaxBodyBytes = 65536
	// MaxComments is the number of comments IngestAll reads for one PR. The rest are counted as unread.
	MaxComments = 1000
	// MaxCandidates is the number of candidates kept for one PR. The most severe are kept.
	MaxCandidates = 200
	// MaxRangeLines is the longest line range a comment may cover. A longer range makes no claim about a place.
	MaxRangeLines = 500
	// MaxDiffOps is the most diff lines one file may have in a Diffs value. A larger one is not used.
	MaxDiffOps = 200_000
	// MaxPatchBytes is the largest patch text ParsePatch reads.
	MaxPatchBytes = 1 << 20
	// maxLine is the largest line number that is believed.
	maxLine = 10_000_000
	// minClaim is the shortest claim a candidate may have, as FindingIn needs.
	minClaim = finding.ClaimMinChars
)

// Kind says which GitHub endpoint a comment came from. The empty value is a review comment.
type Kind string

// The three places a third-party tool writes (ACTION_MODE 6.1).
const (
	KindReviewComment Kind = "review_comment" // GET /pulls/{n}/comments: has a path and a line
	KindReview        Kind = "review"         // GET /pulls/{n}/reviews: the review body
	KindIssueComment  Kind = "issue_comment"  // GET /issues/{n}/comments: where some tools post a summary
)

// Side is the side of the diff a comment is on.
type Side string

// The two sides. RIGHT is the new file and LEFT is the old one.
const (
	SideRight Side = "RIGHT"
	SideLeft  Side = "LEFT"
)

// Comment is one comment as the GitHub API gives it. A field that the API gives as null is the zero value here: line
// numbers and positions start at 1, so 0 means "null". The caller sets Outdated from the GraphQL isOutdated flag, or
// from a null position, when it knows.
type Comment struct {
	ID                int64
	Kind              Kind
	Body              string
	Path              string
	Line              int // the line on CommitID, or 0 when GitHub no longer has one
	StartLine         int
	OriginalLine      int // the line on OriginalCommitID
	OriginalStartLine int
	Side              string
	CommitID          string
	OriginalCommitID  string
	Position          int
	Outdated          bool
	InReplyTo         int64 // in_reply_to_id, 0 for the first comment of a thread
	UserLogin         string
	UserType          string
}

// Candidate is a third-party finding that may be proven or dropped later. Lines are on the head commit, except when
// Side is LEFT: then they are on the base file and are not mapped. Severity is medium when the tool states none, and
// SeverityStated says which. Style is true when the tool calls the claim style, which is "not checked" and counted apart.
// Outdated is true when GitHub has lost the comment's line and the lines are the original pair on OriginalCommit: the
// comment is still a finding, but a proof run at head cannot use its place.
type Candidate struct {
	Tool           Tool             `json:"tool"`
	CommentID      int64            `json:"comment_id"`
	Path           string           `json:"path"`
	StartLine      int              `json:"start_line"`
	EndLine        int              `json:"end_line"`
	Side           Side             `json:"side"`
	Claim          string           `json:"claim"`
	Severity       finding.Severity `json:"severity"`
	SeverityStated bool             `json:"severity_stated"`
	Style          bool             `json:"style"`
	Outdated       bool             `json:"outdated"`
	CommitID       string           `json:"commit_id,omitempty"` // the commit the lines are on when Outdated
}

// Cause is the stable word that says why a comment was dropped, under its drop reason.
type Cause string

// The causes. The first group is under proof.DropNoLocation, the second under proof.DropOutdated.
const (
	CauseSummary       Cause = "summary"         // a review body or an issue comment: it has no file and line
	CauseNoPath        Cause = "no_path"         // no path, or a path that is not a clean relative path
	CauseNoLine        Cause = "no_line"         // a comment on the file, or a line that cannot be true
	CauseRangeTooLarge Cause = "range_too_large" // more than MaxRangeLines lines
	CauseNoCommit      Cause = "no_commit"       // no commit to place the line on

	CauseLineDeleted Cause = "line_deleted" // every line of the comment is gone at head
	CauseFileDeleted Cause = "file_deleted" // the file is gone at head
	CauseNoDiff      Cause = "no_diff"      // the comment is on an older commit and the caller gave no diff for it
	CauseBadDiff     Cause = "bad_diff"     // the diff is out of order, too large or malformed
)

// Drop says that a recognised comment gave no candidate, and why. Reason is the proof.DropReason the summary counts.
type Drop struct {
	CommentID int64            `json:"comment_id"`
	Tool      Tool             `json:"tool"`
	Reason    proof.DropReason `json:"reason"`
	Cause     Cause            `json:"cause"`
}

// IgnoreReason says why a comment is not read at all. An ignored comment is not an error and it is not counted as a drop.
type IgnoreReason string

// The reasons.
const (
	IgnoreNotBot        IgnoreReason = "not_bot"        // user.type is not Bot
	IgnoreUnknownAuthor IgnoreReason = "unknown_author" // no tool of this package has this login
	IgnoreNotInSources  IgnoreReason = "not_in_sources" // the policy does not list this login
	IgnoreReply         IgnoreReason = "reply"          // an answer in a thread, not a new finding
	IgnoreNoMarker      IgnoreReason = "no_marker"      // the right login, but the body has no mark of its tool
	IgnoreNoClaim       IgnoreReason = "no_claim"       // nothing is left of the body after the clean-up
)

// Outcome is what became of one comment.
type Outcome string

// The three outcomes.
const (
	OutcomeCandidate Outcome = "candidate"
	OutcomeDropped   Outcome = "dropped"
	OutcomeIgnored   Outcome = "ignored"
)

// Result is what Ingest says about one comment. Tool is set when the login is one of ours. Candidate is set only for
// OutcomeCandidate, Drop only for OutcomeDropped and Ignore only for OutcomeIgnored.
type Result struct {
	Outcome   Outcome      `json:"outcome"`
	CommentID int64        `json:"comment_id"`
	Tool      Tool         `json:"tool,omitempty"`
	Candidate Candidate    `json:"candidate"`
	Drop      Drop         `json:"drop"`
	Ignore    IgnoreReason `json:"ignore,omitempty"`
}
