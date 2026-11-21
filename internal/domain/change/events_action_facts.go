package change

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// ExternalIngested is the payload of external.ingested: a third-party review comment became a candidate finding
// (ACTION_MODE 6.2). The comment id makes the record idempotent. Location is on Head, unless Side is LEFT: then the
// lines are on the base file and are not mapped. The candidate enters the log as a finding at E4 with a finding event;
// candidates dropped as outdated or with no location are only counted in proof.reported.
type ExternalIngested struct {
	Source    external.Tool    `json:"source"`
	CommentID int64            `json:"comment_id"`
	Head      id.SHA           `json:"head"`
	Location  finding.Location `json:"location"`
	Side      external.Side    `json:"side"`
	Claim     string           `json:"claim"`
	Severity  finding.Severity `json:"severity"`
}

// Validate returns every problem with the payload.
func (p ExternalIngested) Validate() error {
	var v problems
	v.add("source", checkEnum(p.Source, oneOf(p.Source, external.ToolCodeRabbit, external.ToolCopilot, external.ToolBugbot, external.ToolGreptile)))
	v.require("comment_id", p.CommentID >= 1, "must be 1 or more")
	v.add("head", checkSHA(p.Head))
	v.add("location.file", finding.CheckRepoPath(p.Location.File))
	v.require("location.lines", p.Location.Lines[0] >= 1 && p.Location.Lines[1] >= p.Location.Lines[0], "must be 1-based with the first at most the last")
	v.add("side", checkEnum(p.Side, oneOf(p.Side, external.SideRight, external.SideLeft)))
	n := utf8.RuneCountInString(p.Claim)
	v.require("claim", n >= finding.ClaimMinChars && n <= finding.ClaimMaxChars,
		fmt.Sprintf("must be %d to %d characters", finding.ClaimMinChars, finding.ClaimMaxChars))
	_, err := finding.ParseSeverity(string(p.Severity))
	v.add("severity", err)
	return v.join()
}

// SummaryPosted is the payload of summary.posted: the proof summary comment was created or updated for a head
// (ACTION_MODE 2.5). Marker is the hidden text that finds the comment again. It is a pointer for display; no decision
// reads it (2.7).
type SummaryPosted struct {
	Head      id.SHA `json:"head"`
	CommentID int64  `json:"comment_id"`
	Marker    string `json:"marker"`
}

// Validate returns every problem with the payload.
func (p SummaryPosted) Validate() error {
	var v problems
	v.add("head", checkSHA(p.Head))
	v.require("comment_id", p.CommentID >= 1, "must be 1 or more")
	v.add("marker", checkText(p.Marker))
	return v.join()
}

// BreakGlassRecorded is the payload of breakglass.recorded: a person in action.break_glass at the merge base bypassed
// the fast check on a head, with a reason (ACTION_MODE 5.4). The gate checks the person against the policy before it
// records the event. Login is that person, SourceRef the command comment. An agent cannot record it (checkActorRule).
type BreakGlassRecorded struct {
	Head      id.SHA  `json:"head"`
	Login     ActorID `json:"login"`
	Reason    string  `json:"reason"`
	SourceRef string  `json:"source_ref"`
}

// Validate returns every problem with the payload.
func (p BreakGlassRecorded) Validate() error {
	var v problems
	v.add("head", checkSHA(p.Head))
	v.add("login", checkActor(p.Login))
	v.add("reason", checkText(p.Reason))
	v.require("reason", utf8.RuneCountInString(strings.TrimSpace(p.Reason)) >= policy.BreakGlassMinReason,
		fmt.Sprintf("must be at least %d characters", policy.BreakGlassMinReason))
	v.add("source_ref", checkText(p.SourceRef))
	return v.join()
}

// BreakGlassReviewed is the payload of breakglass.reviewed: another approver reviewed the bypass of a head
// (ACTION_MODE 5.4). Until then the summary shows the bypass as open. The fold does not let the person who bypassed
// review their own bypass. An agent cannot record it.
type BreakGlassReviewed struct {
	Head      id.SHA  `json:"head"`
	Login     ActorID `json:"login"`
	SourceRef string  `json:"source_ref"`
}

// Validate returns every problem with the payload.
func (p BreakGlassReviewed) Validate() error {
	var v problems
	v.add("head", checkSHA(p.Head))
	v.add("login", checkActor(p.Login))
	v.add("source_ref", checkText(p.SourceRef))
	return v.join()
}

// StateReset is the payload of state.reset: the gate found its state log untrusted (a bad MAC or a broken chain) and
// started a new log (ACTION_MODE 2.7). It is the first event of the new log. Every produced fact before it is unknown.
type StateReset struct {
	Reason string `json:"reason"`
}

// Validate returns every problem with the payload.
func (p StateReset) Validate() error {
	var v problems
	v.add("reason", checkText(p.Reason))
	return v.join()
}
