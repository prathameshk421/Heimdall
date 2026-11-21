package change

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// The core area of the event catalog (TECH_SPEC 3.2): a change's start and end, its room and branch, the work
// tasks and steps, the ticket, the pull request, human decisions and approvals, and the gate. The catalog order is
// All in event.go.
const (
	EventChangeTriggered     EventType = "change.triggered"
	EventRoomOpened          EventType = "room.opened"
	EventBranchEnsured       EventType = "branch.ensured"
	EventTaskAssigned        EventType = "task.assigned"
	EventTaskAcked           EventType = "task.acked"
	EventTaskTimedOut        EventType = "task.timed_out"
	EventTaskFailed          EventType = "task.failed"
	EventTaskProgress        EventType = "task.progress"
	EventTaskSuperseded      EventType = "task.superseded"
	EventTaskRewoken         EventType = "task.rewoken"
	EventProviderProbed      EventType = "provider.probed"
	EventStepRetried         EventType = "step.retried"
	EventStepEscalated       EventType = "step.escalated"
	EventStepResumed         EventType = "step.resumed"
	EventProfileProposed     EventType = "profile.proposed"
	EventTicketChanged       EventType = "ticket.changed"
	EventPROpened            EventType = "pr.opened"
	EventPRPushed            EventType = "pr.pushed"
	EventPRLinked            EventType = "pr.linked"
	EventPRClosed            EventType = "pr.closed"
	EventPRReopened          EventType = "pr.reopened"
	EventDecisionRecorded    EventType = "decision.recorded"
	EventApprovalRecorded    EventType = "approval.recorded"
	EventApprovalInvalidated EventType = "approval.invalidated"
	EventGateEvaluated       EventType = "gate.evaluated"
	EventChangeRejected      EventType = "change.rejected"
	EventChangeMerged        EventType = "change.merged"
)

// corePayloads is the registry of the core area.
func corePayloads() map[EventType]payloadEntry {
	return map[EventType]payloadEntry{
		EventChangeTriggered:     entry[Triggered](),
		EventRoomOpened:          entry[RoomOpened](),
		EventBranchEnsured:       entry[BranchEnsured](),
		EventTaskAssigned:        entry[TaskAssigned](),
		EventTaskAcked:           entry[TaskAcked](),
		EventTaskTimedOut:        entry[TaskTimedOut](),
		EventTaskFailed:          entry[TaskFailed](),
		EventTaskProgress:        entry[TaskProgress](),
		EventTaskSuperseded:      entry[TaskSuperseded](),
		EventTaskRewoken:         entry[TaskRewoken](),
		EventProviderProbed:      entry[ProviderProbed](),
		EventStepRetried:         entry[StepRetried](),
		EventStepEscalated:       entry[StepEscalated](),
		EventStepResumed:         entry[StepResumed](),
		EventProfileProposed:     entry[ProfileProposed](),
		EventTicketChanged:       entry[TicketChanged](),
		EventPROpened:            entry[PROpened](),
		EventPRPushed:            entry[PRPushed](),
		EventPRLinked:            entry[PRLinked](),
		EventPRClosed:            entry[PRClosed](),
		EventPRReopened:          entry[PRReopened](),
		EventDecisionRecorded:    entry[DecisionRecorded](),
		EventApprovalRecorded:    entry[ApprovalRecorded](),
		EventApprovalInvalidated: entry[ApprovalInvalidated](),
		EventGateEvaluated:       entry[GateEvaluated](),
		EventChangeRejected:      entry[Rejected](),
		EventChangeMerged:        entry[Merged](),
	}
}

// Triggered is the payload of change.triggered: a ticket became work (TECH_SPEC 3.2, D50).
type Triggered struct {
	Ticket   id.ChangeID       `json:"ticket"`
	Repo     Repo              `json:"repo"`
	Assignee ActorID           `json:"assignee"`
	Source   TriggerSource     `json:"source"`
	Criteria []TicketCriterion `json:"acceptance_criteria,omitempty"`
	// DescriptionRef is the stored full ticket text, read by agents through read_untrusted.
	DescriptionRef workflow.BlobRef `json:"description_ref,omitempty"`
}

// TicketCriterion is the validated, immutable ticket requirement captured when work is triggered.
type TicketCriterion struct {
	ID   string           `json:"id"`
	Text string           `json:"text"`
	Hash string           `json:"hash"`
	Ref  workflow.BlobRef `json:"ref"`
}

// Validate returns every problem with the payload.
func (p Triggered) Validate() error {
	var v problems
	v.add("ticket", checkTicket(p.Ticket))
	v.add("repo", checkRepo(p.Repo))
	v.add("assignee", checkActor(p.Assignee))
	v.add("source", checkEnum(p.Source, p.Source.Valid()))
	if p.DescriptionRef != "" {
		_, err := workflow.ParseBlobRef(string(p.DescriptionRef))
		v.add("description_ref", err)
	}
	seen := map[string]bool{}
	for _, c := range p.Criteria {
		if !ticketCriterionID.MatchString(c.ID) {
			v.add("acceptance_criteria", errors.New("invalid criterion id"))
		}
		if seen[c.ID] {
			v.add("acceptance_criteria", errors.New("duplicate criterion id"))
		}
		seen[c.ID] = true
		if strings.TrimSpace(c.Text) == "" || len(c.Text) > 64<<10 {
			v.add("acceptance_criteria", errors.New("criterion text must be 1 to 65536 bytes"))
		}
		sum := sha256.Sum256([]byte(c.Text))
		hash := hex.EncodeToString(sum[:])
		if c.Hash != hash {
			v.add("acceptance_criteria", errors.New("criterion hash does not match text"))
		}
		if c.Ref != workflow.BlobRef("blob:sha256:"+hash) {
			v.add("acceptance_criteria", errors.New("criterion ref does not match text hash"))
		}
	}
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
	// BaseSHA is the pull request's base commit when it opened. Old events have none.
	BaseSHA id.SHA `json:"base_sha,omitempty"`
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
	if p.BaseSHA != "" {
		v.add("base_sha", checkSHA(p.BaseSHA))
	}
	return v.join()
}

// PRPushed is the payload of pr.pushed: the head of a pull request moved (SLOP and re-review wait on it).
type PRPushed struct {
	Repo    Repo   `json:"repo"`
	Number  int    `json:"number"`
	HeadSHA id.SHA `json:"head_sha"`
	// BaseSHA is the base commit at the push. Old events have none.
	BaseSHA id.SHA `json:"base_sha,omitempty"`
}

// Validate returns every problem with the payload.
func (p PRPushed) Validate() error {
	var v problems
	v.add("repo", checkRepo(p.Repo))
	v.add("number", checkAtLeastOne(p.Number))
	v.add("head_sha", checkSHA(p.HeadSHA))
	if p.BaseSHA != "" {
		v.add("base_sha", checkSHA(p.BaseSHA))
	}
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

// PRReopened is a pull request that was closed without a merge and is open again on GitHub. It resumes the change that
// the close ended; a merged change stays merged (found live in H11).
type PRReopened struct {
	Repo    Repo   `json:"repo"`
	Number  int    `json:"number"`
	HeadSHA id.SHA `json:"head_sha"`
}

// Validate returns every problem with the payload.
func (p PRReopened) Validate() error {
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

// StepRetried is the payload of step.retried: a step failed its check, and its on_fail retry target is open again.
// Step is the workflow step id of the retry target. Attempt is the attempt the retry starts, so the first retry is 2.
// Reasons are the structured reasons of the failed check, which the retry target reads from the view.
type StepRetried struct {
	Step    string   `json:"step"`
	Attempt int      `json:"attempt"`
	Reasons []string `json:"reasons"`
}

// Validate returns every problem with the payload.
func (p StepRetried) Validate() error {
	var v problems
	v.add("step", checkText(p.Step))
	v.require("attempt", p.Attempt >= 2, "must be 2 or more")
	v.require("reasons", len(p.Reasons) <= maxRetryReasons, "must hold at most 8 items")
	v.each("reasons", p.Reasons, checkText)
	return v.join()
}

// StepEscalated is the payload of step.escalated: a step failed its check after its budget of attempts was used up,
// so the change waits for a human. Step is the workflow step id that failed. Attempts counts the attempts made. Reason
// says why the change stopped, in one line.
type StepEscalated struct {
	Step     string `json:"step"`
	Attempts int    `json:"attempts"`
	Reason   string `json:"reason"`
}

// ProfileProposed is the payload of profile.proposed: the profiler proposed a sandbox profile for the repository
// (D175). It ends the profiler's task, as a submission does. The proposal itself, its approval, trial and promotion
// live in the profile store; ProposalID is its content identifier (64 hex).
type ProfileProposed struct {
	TaskID     id.TaskID `json:"task_id"`
	ProposalID string    `json:"proposal_id"`
	Repo       Repo      `json:"repo"`
}

// Validate returns every problem with the payload.
func (p ProfileProposed) Validate() error {
	var v problems
	v.add("task_id", checkTask(p.TaskID))
	v.require("proposal_id", reHash.MatchString(p.ProposalID), "must be 64 lowercase hex characters")
	v.add("repo", checkRepo(p.Repo))
	return v.join()
}

// StepResumed is the payload of step.resumed: a person released an escalated step (A10 item 5). Step is the workflow
// step id. By is the person who resumed it. The step's view starts again at this event, and the step is offered again.
type StepResumed struct {
	Step string  `json:"step"`
	By   ActorID `json:"by"`
}

// Validate returns every problem with the payload.
func (p StepResumed) Validate() error {
	var v problems
	v.add("step", checkText(p.Step))
	v.add("by", checkActor(p.By))
	return v.join()
}

// Validate returns every problem with the payload.
func (p StepEscalated) Validate() error {
	var v problems
	v.add("step", checkText(p.Step))
	v.require("attempts", p.Attempts >= 1, "must be 1 or more")
	v.add("reason", checkText(p.Reason))
	return v.join()
}

// TaskAssigned is the payload of task.assigned. Kind, Subject and Input say what the task is (HARNESS_DESIGN B4): the
// workflow kind, what it is about (a seat or a case) and the input it was made for (a head or an attempt), so a
// submission can name the task it ends. Old events lack them. BundleRef is the blob whose hash is the context bundle;
// new tasks leave it empty because inputs are rendered when the agent calls get_task.
type TaskAssigned struct {
	TaskID    id.TaskID        `json:"task_id"`
	CaseID    id.CaseID        `json:"case_id,omitempty"`
	Agent     id.AgentName     `json:"agent"`
	Role      workflow.Role    `json:"role"`
	Deadline  time.Time        `json:"deadline"`
	BundleRef workflow.BlobRef `json:"bundle_ref,omitempty"`
	Kind      string           `json:"kind,omitempty"`
	Subject   string           `json:"subject,omitempty"`
	Input     string           `json:"input,omitempty"`
}

// Validate returns every problem with the payload.
func (p TaskAssigned) Validate() error {
	var v problems
	v.add("task_id", checkTask(p.TaskID))
	if p.CaseID != "" {
		v.add("case_id", checkCase(p.CaseID))
	}
	v.add("agent", checkAgent(p.Agent))
	v.add("role", checkRole(p.Role))
	if p.Deadline.IsZero() {
		v.add("deadline", errors.New("is required"))
	}
	if p.BundleRef != "" {
		v.add("bundle_ref", checkBlob(p.BundleRef))
	}
	if p.Kind != "" {
		v.add("kind", checkText(p.Kind))
		v.add("subject", checkText(p.Subject))
		v.add("input", checkText(p.Input))
	}
	return v.join()
}

// TaskAcked is the payload of task.acked: the agent took the task within 60 s (TECH_SPEC 6.2).
type TaskAcked struct {
	TaskID id.TaskID     `json:"task_id"`
	Agent  id.AgentName  `json:"agent"`
	Role   workflow.Role `json:"role"`
}

// Validate returns every problem with the payload.
func (p TaskAcked) Validate() error {
	var v problems
	v.add("task_id", checkTask(p.TaskID))
	v.add("agent", checkAgent(p.Agent))
	v.add("role", checkRole(p.Role))
	return v.join()
}

// TaskTimedOut is the payload of task.timed_out: no ack or no submission before the deadline.
type TaskTimedOut struct {
	TaskID id.TaskID     `json:"task_id"`
	Agent  id.AgentName  `json:"agent"`
	Role   workflow.Role `json:"role"`
	Reason string        `json:"reason"`
}

// Validate returns every problem with the payload.
func (p TaskTimedOut) Validate() error {
	var v problems
	v.add("task_id", checkTask(p.TaskID))
	v.add("agent", checkAgent(p.Agent))
	v.add("role", checkRole(p.Role))
	v.add("reason", checkText(p.Reason))
	return v.join()
}

// TaskFailed is the payload of task.failed: the agent used up its time or cost budget (TECH_SPEC 6.2, D122).
type TaskFailed struct {
	TaskID id.TaskID     `json:"task_id"`
	Agent  id.AgentName  `json:"agent"`
	Role   workflow.Role `json:"role"`
	Reason string        `json:"reason"`
}

// TaskProgress is a durable progress report for an assignment.
type TaskProgress struct {
	TaskID id.TaskID     `json:"task_id"`
	Agent  id.AgentName  `json:"agent"`
	Role   workflow.Role `json:"role"`
	Text   string        `json:"text"`
}

// Validate checks the task identity and report text.
func (p TaskProgress) Validate() error {
	var v problems
	v.add("task_id", checkTask(p.TaskID))
	v.add("agent", checkAgent(p.Agent))
	v.add("role", checkRole(p.Role))
	v.add("text", checkText(p.Text))
	return v.join()
}

// TaskSuperseded is the payload of task.superseded: the reconciler replaced a live task because its input changed (a
// new head, a new attempt). The task is over; a new one is assigned.
type TaskSuperseded struct {
	TaskID id.TaskID `json:"task_id"`
	Reason string    `json:"reason"`
}

// Validate returns every problem with the payload.
func (p TaskSuperseded) Validate() error {
	var v problems
	v.add("task_id", checkTask(p.TaskID))
	v.add("reason", checkText(p.Reason))
	return v.join()
}

// TaskRewoken is the payload of task.rewoken: the agent was woken again for a live task. N counts the wakes, from 1.
type TaskRewoken struct {
	TaskID id.TaskID `json:"task_id"`
	N      int       `json:"n"`
	// Stall is true for the one wake after a stall with a healthy provider (liveness.Rewake).
	Stall bool `json:"stall,omitempty"`
	// Reset is set only by events from before D172, when this wake was meant to reset the agent's session. The reset
	// never ran (Switch offers an agent no session reset); such an event folds as the stall wake.
	Reset bool `json:"reset,omitempty"`
}

// Validate returns every problem with the payload.
func (p TaskRewoken) Validate() error {
	var v problems
	v.add("task_id", checkTask(p.TaskID))
	v.add("n", checkAtLeastOne(p.N))
	return v.join()
}

// ProviderProbed is the payload of provider.probed: the model provider was asked whether it answers. Health is one of
// the liveness package's values ("ok", "refusing").
type ProviderProbed struct {
	Health string `json:"health"`
	Detail string `json:"detail,omitempty"`
}

// Validate returns every problem with the payload.
func (p ProviderProbed) Validate() error {
	var v problems
	v.add("health", checkText(p.Health))
	return v.join()
}

// Validate returns every problem with the payload.
func (p TaskFailed) Validate() error {
	var v problems
	v.add("task_id", checkTask(p.TaskID))
	v.add("agent", checkAgent(p.Agent))
	v.add("role", checkRole(p.Role))
	v.add("reason", checkText(p.Reason))
	return v.join()
}

// GateEvaluated is the payload of gate.evaluated: the merge rule ran for a head (TECH_SPEC 12.4). Invariant 4 needs
// TreeHash, so the event records the tree it judged.
type GateEvaluated struct {
	OK            bool     `json:"ok"`
	Reasons       []string `json:"reasons"`
	SHA           id.SHA   `json:"sha"`
	TreeHash      id.SHA   `json:"tree_hash"`
	PolicyVersion int      `json:"policy_version"`
}

// Validate returns every problem with the payload.
func (p GateEvaluated) Validate() error {
	var v problems
	if p.OK && len(p.Reasons) > 0 {
		v.add("reasons", errors.New("must be empty when ok is true"))
	}
	if !p.OK && len(p.Reasons) == 0 {
		v.add("reasons", errors.New("must name a reason when ok is false"))
	}
	v.each("reasons", p.Reasons, checkText)
	v.add("sha", checkSHA(p.SHA))
	v.add("tree_hash", checkSHA(p.TreeHash))
	v.add("policy_version", checkAtLeastOne(p.PolicyVersion))
	return v.join()
}

// DecisionRecorded is the payload of decision.recorded: a human decision from GitHub or from the room (TECH_SPEC 11.5).
// Only an actor that is not an agent may cause it (invariant 1).
type DecisionRecorded struct {
	Kind      DecisionKind `json:"kind"`
	Target    string       `json:"target"`
	Login     ActorID      `json:"login"`
	Reason    string       `json:"reason"`
	SourceRef string       `json:"source_ref"`
}

// Validate returns every problem with the payload.
func (p DecisionRecorded) Validate() error {
	var v problems
	v.add("kind", checkEnum(p.Kind, p.Kind.Valid()))
	v.add("target", checkText(p.Target))
	v.add("login", checkActor(p.Login))
	v.add("reason", checkText(p.Reason))
	v.add("source_ref", checkText(p.SourceRef))
	return v.join()
}

// ApprovalRecorded is the payload of approval.recorded: a GitHub review approved a commit (TECH_SPEC 3.4, 12.4).
type ApprovalRecorded struct {
	Login     ActorID `json:"login"`
	CommitSHA id.SHA  `json:"commit_sha"`
}

// Validate returns every problem with the payload.
func (p ApprovalRecorded) Validate() error {
	var v problems
	v.add("login", checkActor(p.Login))
	v.add("commit_sha", checkSHA(p.CommitSHA))
	return v.join()
}

// ApprovalInvalidated is the payload of approval.invalidated: a push made an earlier approval stale.
type ApprovalInvalidated ApprovalRecorded

// Validate returns every problem with the payload.
func (p ApprovalInvalidated) Validate() error { return ApprovalRecorded(p).Validate() }

var ticketCriterionID = regexp.MustCompile(`^AC[1-9][0-9]{0,2}$`)

// maxRetryReasons bounds the structured reasons a retry carries (TECH_SPEC 5.1, on_fail).
const maxRetryReasons = 8
