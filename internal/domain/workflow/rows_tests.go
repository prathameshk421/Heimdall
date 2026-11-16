package workflow

import (
	"encoding/json"
	"time"
)

// testsRows are the steps of the tests area: writing the tests, their quality checks, sign-off, the commit and
// mutation.
func testsRows() []Step {
	return []Step{
		{ID: "write_tests", After: []string{"branch"}, Do: "agent.task", Role: RoleTestAuthor,
			Output: "TestSubmission@1", Timeout: 2 * time.Hour},
		{ID: "test_qa", After: []string{"write_tests"}, Do: "sandbox.test_quality",
			With:   json.RawMessage(`{"checks":["red_first","ac_coverage","flake","contracts"]}`),
			OnFail: &OnFail{Retry: "write_tests", Until: "task_budget", Then: "escalate"}},
		{ID: "commit", After: []string{"test_qa", "signoff"}, Do: "github.commit_tests",
			With: json.RawMessage(`{"dir":"tests/heimdall/{ticket}"}`)},
		{ID: "signoff", After: []string{"test_qa"}, Do: "human.approve_tests", When: "sensitive_domain"},
		{ID: "mutation", After: []string{"review"}, Do: "sandbox.mutation",
			With: json.RawMessage(`{"scope":"changed_lines"}`)},
	}
}
