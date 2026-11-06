package change

import (
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// Triggered is the payload of change.triggered: a ticket became work (TECH_SPEC 3.2, D50).
type Triggered struct {
	Ticket   id.ChangeID   `json:"ticket"`
	Repo     Repo          `json:"repo"`
	Assignee ActorID       `json:"assignee"`
	Source   TriggerSource `json:"source"`
}

// Validate returns every problem with the payload.
func (p Triggered) Validate() error {
	var v problems
	v.add("ticket", checkTicket(p.Ticket))
	v.add("repo", checkRepo(p.Repo))
	v.add("assignee", checkActor(p.Assignee))
	v.add("source", checkEnum(p.Source, p.Source.Valid()))
	return v.join()
}

// RoomOpened is the payload of room.opened: the Switch room for a change exists (D127).
type RoomOpened struct {
	RoomID RoomID `json:"room_id"`
}

// Validate returns every problem with the payload.
func (p RoomOpened) Validate() error {
	var v problems
	_, err := ParseRoomID(string(p.RoomID))
	v.add("room_id", err)
	return v.join()
}

// BranchEnsured is the payload of branch.ensured: the test branch exists at a base commit (TECH_SPEC 2.3).
type BranchEnsured struct {
	Repo    Repo   `json:"repo"`
	Branch  string `json:"branch"`
	BaseSHA id.SHA `json:"base_sha"`
}

// Validate returns every problem with the payload.
func (p BranchEnsured) Validate() error {
	var v problems
	v.add("repo", checkRepo(p.Repo))
	v.add("branch", checkBranch(p.Branch))
	v.add("base_sha", checkSHA(p.BaseSHA))
	return v.join()
}

// TicketChanged is the payload of ticket.changed: fields of the ticket moved (TECH_SPEC 3.2, U3).
type TicketChanged struct {
	ChangedFields []string `json:"changed_fields"`
}

// Validate returns every problem with the payload.
func (p TicketChanged) Validate() error {
	var v problems
	v.add("changed_fields", checkAtLeastOne(len(p.ChangedFields)))
	v.each("changed_fields", p.ChangedFields, checkText)
	return v.join()
}

// PROpened is the payload of pr.opened. Ticket is empty for a pull request that names no ticket (D146). SuggestedReviewers
// are agent identities resolved from CODEOWNERS and the person map at the merge base; they are consumed only if the
// human reviewer-pick timer expires.
type PROpened struct {
	Repo               Repo        `json:"repo"`
	Number             int         `json:"number"`
	HeadSHA            id.SHA      `json:"head_sha"`
	Author             ActorID     `json:"author"`
	Ticket             id.ChangeID `json:"ticket,omitempty"`
	SuggestedReviewers []ActorID   `json:"suggested_reviewers,omitempty"`
}

// Validate returns every problem with the payload.
func (p PROpened) Validate() error {
	var v problems
	v.add("repo", checkRepo(p.Repo))
	v.add("number", checkAtLeastOne(p.Number))
	v.add("head_sha", checkSHA(p.HeadSHA))
	v.add("author", checkActor(p.Author))
	if p.Ticket != "" {
		v.add("ticket", checkTicket(p.Ticket))
	}
	v.each("suggested_reviewers", p.SuggestedReviewers, checkActor)
	return v.join()
}

// PRPushed is the payload of pr.pushed: the head of a pull request moved (SLOP and re-review wait on it).
type PRPushed struct {
	Repo    Repo   `json:"repo"`
	Number  int    `json:"number"`
	HeadSHA id.SHA `json:"head_sha"`
}

// Validate returns every problem with the payload.
func (p PRPushed) Validate() error {
	var v problems
	v.add("repo", checkRepo(p.Repo))
	v.add("number", checkAtLeastOne(p.Number))
	v.add("head_sha", checkSHA(p.HeadSHA))
	return v.join()
}

// PRLinked is the payload of pr.linked: a pull request that had no ticket now names one (D146).
type PRLinked struct {
	Repo   Repo        `json:"repo"`
	Number int         `json:"number"`
	Ticket id.ChangeID `json:"ticket"`
}

// Validate returns every problem with the payload.
func (p PRLinked) Validate() error {
	var v problems
	v.add("repo", checkRepo(p.Repo))
	v.add("number", checkAtLeastOne(p.Number))
	v.add("ticket", checkTicket(p.Ticket))
	return v.join()
}

// PRClosed is the payload of pr.closed. Merged says whether the pull request was merged or only closed.
type PRClosed struct {
	Repo    Repo   `json:"repo"`
	Number  int    `json:"number"`
	HeadSHA id.SHA `json:"head_sha"`
	Merged  bool   `json:"merged"`
}

// Validate returns every problem with the payload.
func (p PRClosed) Validate() error {
	var v problems
	v.add("repo", checkRepo(p.Repo))
	v.add("number", checkAtLeastOne(p.Number))
	v.add("head_sha", checkSHA(p.HeadSHA))
	return v.join()
}

// Rejected is the payload of change.rejected: a human or the engine ended the change without a merge.
type Rejected struct {
	Reason string `json:"reason"`
}

// Validate returns every problem with the payload.
func (p Rejected) Validate() error {
	var v problems
	v.add("reason", checkText(p.Reason))
	return v.join()
}

// Merged is the payload of change.merged: the head commit reached the base branch.
type Merged struct {
	SHA id.SHA `json:"sha"`
}

// Validate returns every problem with the payload.
func (p Merged) Validate() error {
	var v problems
	v.add("sha", checkSHA(p.SHA))
	return v.join()
}
