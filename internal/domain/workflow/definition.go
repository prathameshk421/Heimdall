package workflow

import (
	"encoding/json"
	"time"
)

// Workflow is the shape of a change's life (TECH_SPEC 5.1): a version and the steps. In this build it is a Go value;
// the YAML loader is Later, and it must produce the same value.
type Workflow struct {
	Version int    `json:"version"`
	Steps   []Step `json:"steps"`
}

// Step is one stage. After lists the steps that must finish first; a retry (OnFail.Retry) is the only edge that may
// point backwards. Role and Output are lifted out of With so Validate can check them; With holds the step's other
// arguments and is opaque to this package. A zero Timeout means the step declares no deadline.
type Step struct {
	ID      string          `json:"id"`
	On      string          `json:"on,omitempty"`
	After   []string        `json:"after,omitempty"`
	Do      string          `json:"do"`
	Role    Role            `json:"role,omitempty"`
	Output  string          `json:"output,omitempty"`
	Timeout time.Duration   `json:"timeout_ns,omitempty"`
	With    json.RawMessage `json:"with,omitempty"`
	OnFail  *OnFail         `json:"on_fail,omitempty"`
	When    string          `json:"when,omitempty"`
}

// OnFail says what happens when a step fails. Retry names an earlier step to run again; Then is what follows when
// the retries are used up, escalate or reject. Until is the budget that ends the retries, and is opaque here.
type OnFail struct {
	Retry string `json:"retry,omitempty"`
	Until string `json:"until,omitempty"`
	Then  string `json:"then,omitempty"`
}

// Default is the default workflow of TECH_SPEC 5.1, all fourteen steps in the order the spec lists them. The
// mutation step is marked Later in the spec and is kept, because the spec lists it in the workflow.
func Default() Workflow {
	return Workflow{Version: 1, Steps: []Step{
		{ID: "open_room", On: "jira.transitioned{to:'In Progress',assignee:human}", Do: "switch.open_room",
			With: json.RawMessage(`{"invite":["role:test-author","person:$assignee"]}`)},
		{ID: "branch", After: []string{"open_room"}, Do: "github.ensure_branch",
			With: json.RawMessage(`{"name":"{ticket}-{slug}"}`)},
		{ID: "write_tests", After: []string{"branch"}, Do: "agent.task", Role: RoleTestAuthor,
			Output: "TestSubmission@1", Timeout: 2 * time.Hour},
		{ID: "test_qa", After: []string{"write_tests"}, Do: "sandbox.test_quality",
			With:   json.RawMessage(`{"checks":["red_first","ac_coverage","flake","contracts"]}`),
			OnFail: &OnFail{Retry: "write_tests", Until: "task_budget", Then: "escalate"}},
		{ID: "commit", After: []string{"test_qa", "signoff"}, Do: "github.commit_tests",
			With: json.RawMessage(`{"dir":"tests/heimdall/{ticket}"}`)},
		{ID: "wait_pr", After: []string{"commit"}, Do: "wait",
			With: json.RawMessage(`{"event":"pr.opened"}`)},
		{ID: "slop", After: []string{"wait_pr"}, Do: "rule.slop", OnFail: &OnFail{Then: "reject"}},
		{ID: "pick", After: []string{"slop"}, Do: "human.pick_reviewers", Timeout: 15 * time.Minute,
			With: json.RawMessage(`{"default":"suggest"}`)},
		{ID: "review", After: []string{"pick"}, Do: "agent.fanout", Role: RoleReviewer,
			Output: "Review@1", Timeout: 30 * time.Minute, With: json.RawMessage(`{"from":"review_requests"}`)},
		{ID: "verify", After: []string{"review"}, Do: "sandbox.verify_evidence"},
		{ID: "signoff", After: []string{"test_qa"}, Do: "human.approve_tests", When: "sensitive_domain"},
		{ID: "mutation", After: []string{"review"}, Do: "sandbox.mutation",
			With: json.RawMessage(`{"scope":"changed_lines"}`)},
		{ID: "deliberate", After: []string{"verify"}, Do: "deliberation.run", Role: RoleResolver,
			With: json.RawMessage(`{"max_rounds":2}`)},
		{ID: "gate", On: "any", Do: "rule.merge_allowed"},
	}}
}
