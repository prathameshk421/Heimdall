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
// mutation step is marked Later in the spec and is kept, because the spec lists it in the workflow. Each area declares
// its own rows (rows_<area>.go); the order of the steps is declared here and nowhere else.
func Default() Workflow {
	rows := map[string]Step{}
	for _, area := range [][]Step{coreRows(), testsRows(), reviewRows()} {
		for _, s := range area {
			rows[s.ID] = s
		}
	}
	order := []string{"open_room", "branch", "write_tests", "test_qa", "commit", "wait_pr", "slop", "pick", "review",
		"verify", "signoff", "mutation", "deliberate", "gate"}
	steps := make([]Step, 0, len(order))
	for _, id := range order {
		steps = append(steps, rows[id])
	}
	return Workflow{Version: 1, Steps: steps}
}
