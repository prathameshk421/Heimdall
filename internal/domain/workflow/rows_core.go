package workflow

import "encoding/json"

// coreRows are the steps of the core area: the room, the branch, waiting for the pull request, and the gate.
func coreRows() []Step {
	return []Step{
		{ID: "open_room", On: "jira.transitioned{to:'In Progress',assignee:human}", Do: "switch.open_room",
			With: json.RawMessage(`{"invite":["role:test-author","person:$assignee"]}`)},
		{ID: "branch", After: []string{"open_room"}, Do: "github.ensure_branch",
			With: json.RawMessage(`{"name":"{ticket}-{slug}"}`)},
		{ID: "wait_pr", After: []string{"commit"}, Do: "wait",
			With: json.RawMessage(`{"event":"pr.opened"}`)},
		{ID: "gate", On: "any", Do: "rule.merge_allowed"},
	}
}
