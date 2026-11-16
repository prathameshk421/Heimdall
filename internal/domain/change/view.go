package change

import (
	"fmt"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// View is everything known about one change, folded from its events (AGENT_INTERACTION_ARCH F1). It has no state
// field: what a change still needs is Missing, and what may be asked for is a Can predicate. The JSON shape and every
// fold rule are the spec in testdata/plan/view/README.md, and the goldens there are its tests.
type View struct {
	Trigger   *TriggerInfo           `json:"trigger"`
	Room      RoomID                 `json:"room"`
	Branch    *BranchInfo            `json:"branch"`
	Plan      *PlanInfo              `json:"plan"`
	Tests     *TestsInfo             `json:"tests"`
	PR        *PRInfo                `json:"pr"`
	Tasks     map[id.TaskID]TaskInfo `json:"tasks"`
	Reviews   map[string]ReviewInfo  `json:"reviews"`
	Findings  map[string]FindingInfo `json:"findings"`
	Decisions []DecisionInfo         `json:"decisions"`
	Approvals []ApprovalInfo         `json:"approvals"`
	Closed    *ClosedInfo            `json:"closed"`
	Rejected  *RejectedInfo          `json:"rejected"`
}

// TriggerInfo is the ticket that started the change.
type TriggerInfo struct {
	Ticket   id.ChangeID   `json:"ticket"`
	Repo     Repo          `json:"repo"`
	Assignee ActorID       `json:"assignee"`
	Source   TriggerSource `json:"source"`
	Criteria []string      `json:"acceptance_criteria"`
}

// BranchInfo is the ticket's branch.
type BranchInfo struct {
	Repo    Repo   `json:"repo"`
	Branch  string `json:"branch"`
	BaseSHA id.SHA `json:"base_sha"`
}

// PlanInfo is the test plan. No event carries one yet (TEST_AGENT_ARCH 4.1), so it is always nil.
type PlanInfo struct{}

// TestsInfo is the current test submission and what happened to it. A new submission replaces it.
type TestsInfo struct {
	SHA        id.SHA              `json:"sha"`
	Refs       []string            `json:"refs"`
	ACMap      map[string][]string `json:"ac_map"`
	Expected   []string            `json:"expected_paths"`
	Verified   *TestsVerifiedInfo  `json:"verified"`
	Rejected   []string            `json:"rejected"`
	ApprovedBy ActorID             `json:"approved_by"`
	Committed  *TestsCommittedInfo `json:"committed"`
}

// TestsVerifiedInfo is the sandbox's verdict on the submission.
type TestsVerifiedInfo struct {
	TreeHash     id.SHA     `json:"tree_hash"`
	NeedsSignoff bool       `json:"needs_signoff"`
	RunIDs       []id.RunID `json:"run_ids"`
}

// TestsCommittedInfo is the commit that holds the tests.
type TestsCommittedInfo struct {
	SHA      id.SHA `json:"sha"`
	TreeHash id.SHA `json:"tree_hash"`
}

// PRInfo is the pull request. Slop, CI, gate and reviews keep the head they were about, so stale is visible.
type PRInfo struct {
	Number  int                                `json:"number"`
	Author  ActorID                            `json:"author"`
	Ticket  id.ChangeID                        `json:"ticket"`
	HeadSHA id.SHA                             `json:"head_sha"`
	Picked  *PickedInfo                        `json:"picked"`
	Slop    *SlopInfo                          `json:"slop"`
	CI      map[id.SHA]map[string]CIConclusion `json:"ci"`
	Gate    *GateInfo                          `json:"gate"`
}

// PickedInfo is who reviews.
type PickedInfo struct {
	People []ActorID  `json:"people"`
	By     ActorID    `json:"by"`
	Method PickMethod `json:"method"`
}

// SlopInfo is the slop rule's result on the head it ran on.
type SlopInfo struct {
	SHA          id.SHA   `json:"sha"`
	Pass         bool     `json:"pass"`
	FailingRules []string `json:"failing_rules"`
}

// GateInfo is the last recorded gate result. Missing does not read it.
type GateInfo struct {
	SHA     id.SHA   `json:"sha"`
	OK      bool     `json:"ok"`
	Reasons []string `json:"reasons"`
}

// TaskInfo is one task. Its status changes only on task events.
type TaskInfo struct {
	Agent    id.AgentName  `json:"agent"`
	Role     workflow.Role `json:"role"`
	Deadline time.Time     `json:"deadline"`
	Status   string        `json:"status"`
	Progress string        `json:"progress"`
}

// Task statuses.
const (
	TaskAssignedStatus = "assigned"
	TaskAckedStatus    = "acked"
	TaskTimedOutStatus = "timed_out"
	TaskFailedStatus   = "failed"
)

// ReviewInfo is a reviewer's last review, stamped with the head it was on.
type ReviewInfo struct {
	HeadSHA     id.SHA          `json:"head_sha"`
	FindingRefs []id.FindingRef `json:"finding_refs"`
	Clean       bool            `json:"clean"`
	PackDate    string          `json:"pack_date"`
}

// FindingInfo is one finding. By is always the filer; ChangedBy is who moved it last.
type FindingInfo struct {
	ID         id.FindingID       `json:"id"`
	Tier       finding.Tier       `json:"tier"`
	Severity   finding.Severity   `json:"severity"`
	Confidence finding.Confidence `json:"confidence"`
	State      finding.State      `json:"state"`
	By         ActorID            `json:"by"`
	ChangedBy  ActorID            `json:"changed_by"`
	Checked    bool               `json:"checked"`
	Claim      string             `json:"claim"`
}

// DecisionInfo is one human decision.
type DecisionInfo struct {
	Kind      DecisionKind `json:"kind"`
	Target    string       `json:"target"`
	Login     ActorID      `json:"login"`
	Reason    string       `json:"reason"`
	SourceRef string       `json:"source_ref"`
}

// ApprovalInfo is one GitHub approval of a commit.
type ApprovalInfo struct {
	Login     ActorID `json:"login"`
	CommitSHA id.SHA  `json:"commit_sha"`
}

// ClosedInfo is the pull request's close.
type ClosedInfo struct {
	Merged  bool   `json:"merged"`
	HeadSHA id.SHA `json:"head_sha"`
}

// RejectedInfo is the end of a change without a merge: change.rejected or a reject decision.
type RejectedInfo struct {
	By     ActorID `json:"by"`
	Reason string  `json:"reason"`
}

// Over reports whether the change has ended: merged, closed or rejected.
func (v View) Over() bool { return v.Closed != nil || v.Rejected != nil }

// Fold reads the events in order into a View. It never refuses a fact (F1 rule 1); it fails only when a payload does
// not decode.
func Fold(events []Event) (View, error) {
	v := View{Tasks: map[id.TaskID]TaskInfo{}, Reviews: map[string]ReviewInfo{}, Findings: map[string]FindingInfo{},
		Decisions: []DecisionInfo{}, Approvals: []ApprovalInfo{}}
	for i, e := range events {
		p, err := Decode(e)
		if err != nil {
			return v, fmt.Errorf("event %d (%s): %w", i+1, e.Type, err)
		}
		v.apply(e, p)
	}
	return v, nil
}

func (v *View) apply(e Event, p Payload) {
	switch p := p.(type) {
	case Triggered:
		v.Trigger = &TriggerInfo{Ticket: p.Ticket, Repo: p.Repo, Assignee: p.Assignee, Source: p.Source, Criteria: criteriaIDs(p.Criteria)}
	case RoomOpened:
		v.Room = p.RoomID
	case BranchEnsured:
		v.Branch = &BranchInfo{Repo: p.Repo, Branch: p.Branch, BaseSHA: p.BaseSHA}
	case TestsSubmitted, TestsVerified, TestsRejected, TestsCommitted, TestsApproved:
		v.applyTests(p)
	case PROpened, PRPushed, SlopEvaluated, CIReported, GateEvaluated, ReviewersPicked, PRClosed:
		v.applyPR(p)
	case TaskAssigned, TaskAcked, TaskTimedOut, TaskFailed, TaskProgress:
		v.applyTask(p)
	case ReviewSubmitted:
		v.Reviews[string(p.Reviewer)] = ReviewInfo{HeadSHA: v.head(), FindingRefs: nonNil(p.FindingRefs),
			Clean: len(p.FindingRefs) == 0, PackDate: p.PackDate}
	case FindingFiled:
		v.applyFinding(e, FindingRecord(p))
	case FindingVerified:
		v.applyFinding(e, FindingRecord(p))
	case FindingStateChanged:
		v.applyFinding(e, FindingRecord(p))
	default:
		v.applyHuman(e, p)
	}
}

// applyHuman folds what people decide: decisions, approvals and a rejection.
func (v *View) applyHuman(e Event, p Payload) {
	switch p := p.(type) {
	case DecisionRecorded:
		v.Decisions = append(v.Decisions, DecisionInfo(p))
		if p.Kind == DecisionReject {
			v.Rejected = &RejectedInfo{By: p.Login, Reason: p.Reason}
		}
	case ApprovalRecorded:
		v.Approvals = append(v.Approvals, ApprovalInfo(p))
	case ApprovalInvalidated:
		v.Approvals = removeApproval(v.Approvals, ApprovalInfo(p))
	case Rejected:
		v.Rejected = &RejectedInfo{By: e.Actor.ID, Reason: p.Reason}
	}
}

func (v *View) applyTask(p Payload) {
	switch p := p.(type) {
	case TaskAssigned:
		v.Tasks[p.TaskID] = TaskInfo{Agent: p.Agent, Role: p.Role, Deadline: p.Deadline, Status: TaskAssignedStatus}
	case TaskAcked:
		v.setTask(p.TaskID, TaskAckedStatus, "")
	case TaskTimedOut:
		v.setTask(p.TaskID, TaskTimedOutStatus, "")
	case TaskFailed:
		v.setTask(p.TaskID, TaskFailedStatus, "")
	case TaskProgress:
		v.setTask(p.TaskID, "", p.Text)
	}
}

func (v *View) applyTests(p Payload) {
	if s, ok := p.(TestsSubmitted); ok {
		v.Tests = &TestsInfo{SHA: s.SHA, Refs: nonNil(s.Refs), ACMap: nonNilMap(s.ACMap), Expected: nonNil(s.ExpectedPaths), Rejected: []string{}}
		return
	}
	if v.Tests == nil {
		return
	}
	switch p := p.(type) {
	case TestsVerified:
		v.Tests.Verified = &TestsVerifiedInfo{TreeHash: p.TreeHash, NeedsSignoff: p.NeedsSignoff, RunIDs: nonNil(p.RunIDs)}
	case TestsRejected:
		v.Tests.Rejected = nonNil(p.Reasons)
	case TestsCommitted:
		v.Tests.Committed = &TestsCommittedInfo{SHA: p.SHA, TreeHash: p.TreeHash}
	case TestsApproved:
		v.Tests.ApprovedBy = p.Approver
	}
}

func (v *View) applyPR(p Payload) {
	if o, ok := p.(PROpened); ok {
		v.PR = &PRInfo{Number: o.Number, Author: o.Author, Ticket: o.Ticket, HeadSHA: o.HeadSHA, CI: map[id.SHA]map[string]CIConclusion{}}
		return
	}
	if c, ok := p.(PRClosed); ok {
		v.Closed = &ClosedInfo{Merged: c.Merged, HeadSHA: c.HeadSHA}
		return
	}
	if v.PR == nil {
		return
	}
	switch p := p.(type) {
	case PRPushed:
		v.PR.HeadSHA = p.HeadSHA
	case SlopEvaluated:
		v.PR.Slop = &SlopInfo{SHA: v.PR.HeadSHA, Pass: p.Pass, FailingRules: nonNil(p.FailingRules)}
	case CIReported:
		if v.PR.CI[p.SHA] == nil {
			v.PR.CI[p.SHA] = map[string]CIConclusion{}
		}
		v.PR.CI[p.SHA][p.Check] = p.Conclusion
	case GateEvaluated:
		v.PR.Gate = &GateInfo{SHA: p.SHA, OK: p.OK, Reasons: nonNil(p.Reasons)}
	case ReviewersPicked:
		v.PR.Picked = &PickedInfo{People: nonNil(p.People), By: p.By, Method: p.Method}
	}
}

func (v *View) applyFinding(e Event, p FindingRecord) {
	ref := p.Ref.String()
	if e.Type == EventFindingFiled {
		v.Findings[ref] = FindingInfo{ID: p.FindingID, Tier: p.Tier, Severity: p.Severity, Confidence: p.Confidence,
			State: p.State, By: p.By, Claim: p.Claim}
		return
	}
	f, ok := v.Findings[ref]
	if !ok {
		return
	}
	f.Tier = orKeep(p.Tier, f.Tier)
	f.Severity = orKeep(p.Severity, f.Severity)
	f.State = orKeep(p.State, f.State)
	f.Confidence = orKeep(p.Confidence, f.Confidence)
	f.ID = orKeep(p.FindingID, f.ID)
	f.ChangedBy = p.By
	if e.Type == EventFindingVerified || e.Actor.Kind == ActorSystem {
		f.Checked = true
	}
	v.Findings[ref] = f
}

func (v *View) setTask(task id.TaskID, status, progress string) {
	t, ok := v.Tasks[task]
	if !ok {
		return
	}
	if status != "" {
		t.Status = status
	}
	if progress != "" {
		t.Progress = progress
	}
	v.Tasks[task] = t
}

func (v *View) head() id.SHA {
	if v.PR == nil {
		return ""
	}
	return v.PR.HeadSHA
}

func criteriaIDs(cs []TicketCriterion) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func removeApproval(list []ApprovalInfo, a ApprovalInfo) []ApprovalInfo {
	out := []ApprovalInfo{}
	for _, x := range list {
		if x != a {
			out = append(out, x)
		}
	}
	return out
}

func orKeep[T comparable](next, prev T) T {
	var zero T
	if next == zero {
		return prev
	}
	return next
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func nonNilMap(m map[string][]string) map[string][]string {
	if m == nil {
		return map[string][]string{}
	}
	return m
}

// Stage is a short label for people (F1): the first of these that holds. Nothing branches on it; what a change needs
// is Missing.
func Stage(v View) string {
	stages := []struct {
		holds bool
		label string
	}{
		{v.Rejected != nil, "rejected"},
		{v.Closed != nil && v.Closed.Merged, "merged"},
		{v.Closed != nil, "closed without merge"},
		{v.PR != nil && v.PR.Picked != nil, "in review"},
		{v.PR != nil, "pull request open"},
		{v.Tests != nil && v.Tests.Committed != nil, "tests committed"},
		{v.Tests != nil && v.Tests.Verified != nil, "tests verified"},
		{v.Tests != nil, "tests submitted"},
		{v.Trigger != nil, "started"},
	}
	for _, s := range stages {
		if s.holds {
			return s.label
		}
	}
	return "unknown"
}
