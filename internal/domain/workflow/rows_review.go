package workflow

import (
	"encoding/json"
	"time"
)

// reviewRows are the steps of the review area: the slop rule, picking reviewers, the reviews, verifying their
// evidence, and deliberation.
func reviewRows() []Step {
	return []Step{
		{ID: "slop", After: []string{"wait_pr"}, Do: "rule.slop", OnFail: &OnFail{Then: "reject"}},
		{ID: "pick", After: []string{"slop"}, Do: "human.pick_reviewers", Timeout: time.Minute,
			With: json.RawMessage(`{"default":"suggest"}`)},
		{ID: "review", After: []string{"pick"}, Do: "agent.fanout", Role: RoleReviewer,
			Output: "Review@1", Timeout: 30 * time.Minute, With: json.RawMessage(`{"from":"review_requests"}`)},
		{ID: "verify", After: []string{"review"}, Do: "sandbox.verify_evidence"},
		{ID: "deliberate", After: []string{"verify"}, Do: "deliberation.run", Role: RoleResolver, Timeout: 30 * time.Minute,
			With: json.RawMessage(`{"max_rounds":2}`)},
	}
}
