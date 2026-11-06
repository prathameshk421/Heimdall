package change

import (
	"errors"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// TaskAssigned is the payload of task.assigned. BundleRef is the blob whose hash is the context bundle (TECH_SPEC 6.4).
type TaskAssigned struct {
	TaskID    id.TaskID        `json:"task_id"`
	CaseID    id.CaseID        `json:"case_id,omitempty"`
	Agent     id.AgentName     `json:"agent"`
	Role      workflow.Role    `json:"role"`
	Deadline  time.Time        `json:"deadline"`
	BundleRef workflow.BlobRef `json:"bundle_ref"`
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
	v.add("bundle_ref", checkBlob(p.BundleRef))
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

// Validate returns every problem with the payload.
func (p TaskFailed) Validate() error {
	var v problems
	v.add("task_id", checkTask(p.TaskID))
	v.add("agent", checkAgent(p.Agent))
	v.add("role", checkRole(p.Role))
	v.add("reason", checkText(p.Reason))
	return v.join()
}

// QuestionAsked is the payload of question.asked: an agent needs an answer from a person (TECH_SPEC 3.2).
type QuestionAsked struct {
	QuestionID QuestionID `json:"question_id"`
	Refs       []string   `json:"refs"`
	Sender     SenderID   `json:"sender"`
	MessageID  MessageID  `json:"message_id"`
}

// Validate returns every problem with the payload.
func (p QuestionAsked) Validate() error {
	var v problems
	_, err := ParseQuestionID(string(p.QuestionID))
	v.add("question_id", err)
	v.each("refs", p.Refs, checkText)
	_, err = ParseSenderID(string(p.Sender))
	v.add("sender", err)
	_, err = ParseMessageID(string(p.MessageID))
	v.add("message_id", err)
	return v.join()
}

// QuestionAnswered is the payload of question.answered: a person replied in the room.
type QuestionAnswered struct {
	QuestionID QuestionID `json:"question_id"`
	Sender     SenderID   `json:"sender"`
	MessageID  MessageID  `json:"message_id"`
	// Answer is the person's answer text. It is data for the agent, shown inside the untrusted wrapper (A14).
	Answer string `json:"answer"`
}

// Validate returns every problem with the payload.
func (p QuestionAnswered) Validate() error {
	var v problems
	_, err := ParseQuestionID(string(p.QuestionID))
	v.add("question_id", err)
	v.add("answer", checkText(p.Answer))
	_, err = ParseSenderID(string(p.Sender))
	v.add("sender", err)
	_, err = ParseMessageID(string(p.MessageID))
	v.add("message_id", err)
	return v.join()
}

// TestsSubmitted is the payload of tests.submitted: the test-author handed in its tests (TECH_SPEC 7).
type TestsSubmitted struct {
	Refs     []string            `json:"refs"`
	ACMap    map[string][]string `json:"ac_map"`
	SHA      id.SHA              `json:"sha"`
	TreeHash id.SHA              `json:"tree_hash"`
}

// Validate returns every problem with the payload.
func (p TestsSubmitted) Validate() error {
	var v problems
	v.each("refs", p.Refs, checkText)
	v.add("ac_map", checkACMap(p.ACMap))
	v.add("sha", checkSHA(p.SHA))
	v.add("tree_hash", checkSHA(p.TreeHash))
	return v.join()
}

// TestsVerified is the payload of tests.verified: the sandbox ran the tests (red first, D4). NeedsSignoff is set by the
// engine from policy (the sensitive paths and labels, D131, D33): when true the reducer waits for tests.approved before
// the tests can be committed.
type TestsVerified struct {
	RunIDs       []id.RunID `json:"run_ids"`
	SHA          id.SHA     `json:"sha"`
	TreeHash     id.SHA     `json:"tree_hash"`
	NeedsSignoff bool       `json:"needs_signoff"`
}

// Validate returns every problem with the payload.
func (p TestsVerified) Validate() error {
	var v problems
	v.add("run_ids", checkAtLeastOne(len(p.RunIDs)))
	v.each("run_ids", p.RunIDs, checkRunID)
	v.add("sha", checkSHA(p.SHA))
	v.add("tree_hash", checkSHA(p.TreeHash))
	return v.join()
}

// TestsRejected is the payload of tests.rejected: the tests failed red-first or another check, with reasons.
type TestsRejected struct {
	Reasons []string   `json:"reasons"`
	RunIDs  []id.RunID `json:"run_ids"`
	SHA     id.SHA     `json:"sha"`
}

// Validate returns every problem with the payload.
func (p TestsRejected) Validate() error {
	var v problems
	v.add("reasons", checkAtLeastOne(len(p.Reasons)))
	v.each("reasons", p.Reasons, checkText)
	v.each("run_ids", p.RunIDs, checkRunID)
	v.add("sha", checkSHA(p.SHA))
	return v.join()
}

// TestsCommitted is the payload of tests.committed: the verified tests were committed to the ticket's branch.
type TestsCommitted struct {
	SHA      id.SHA `json:"sha"`
	TreeHash id.SHA `json:"tree_hash"`
}

// Validate returns every problem with the payload.
func (p TestsCommitted) Validate() error {
	var v problems
	v.add("sha", checkSHA(p.SHA))
	v.add("tree_hash", checkSHA(p.TreeHash))
	return v.join()
}

// TestsApproved is the payload of tests.approved: a domain approver accepted the tests of a sensitive domain (D33).
type TestsApproved struct {
	Approver ActorID `json:"approver"`
	TreeHash id.SHA  `json:"tree_hash"`
}

// Validate returns every problem with the payload.
func (p TestsApproved) Validate() error {
	var v problems
	v.add("approver", checkActor(p.Approver))
	v.add("tree_hash", checkSHA(p.TreeHash))
	return v.join()
}

// SlopEvaluated is the payload of slop.evaluated: the pure slop rule passed or named the rules it broke (12.3).
type SlopEvaluated struct {
	Pass         bool     `json:"pass"`
	FailingRules []string `json:"failing_rules"`
}

// Validate returns every problem with the payload.
func (p SlopEvaluated) Validate() error {
	var v problems
	if p.Pass && len(p.FailingRules) > 0 {
		v.add("failing_rules", errors.New("must be empty when pass is true"))
	}
	if !p.Pass && len(p.FailingRules) == 0 {
		v.add("failing_rules", errors.New("must name a rule when pass is false"))
	}
	v.each("failing_rules", p.FailingRules, checkText)
	return v.join()
}

// CIReported is the payload of ci.reported: a check run on a commit finished (TECH_SPEC 3.2).
type CIReported struct {
	Check      string       `json:"check"`
	SHA        id.SHA       `json:"sha"`
	Conclusion CIConclusion `json:"conclusion"`
}

// Validate returns every problem with the payload.
func (p CIReported) Validate() error {
	var v problems
	v.add("check", checkText(p.Check))
	v.add("sha", checkSHA(p.SHA))
	v.add("conclusion", checkEnum(p.Conclusion, p.Conclusion.Valid()))
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
