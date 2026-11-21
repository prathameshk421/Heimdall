package agentloop

import (
	"context"
	"encoding/json"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Tool is one tool the model may call. The loop sends Spec to the model and calls Run with the raw JSON object the
// model wrote. A returned string, and a returned error, both go back to the model as the tool result.
type Tool interface {
	Spec() ports.ModelTool
	// Run decodes the arguments, does the work and returns the text the model sees.
	Run(ctx context.Context, arguments json.RawMessage) (string, error)
}

// Verdict is the caller's decision on one submission. Reason says what to fix when Accepted is false, with field
// paths, and the loop shows it to the model unchanged.
type Verdict struct {
	Accepted bool
	Reason   string
}

// Submitter is a final-answer tool, such as submit_plan or submit_finding_proof. Its Submit method holds the
// caller's validator. An accepted submission ends the loop. A refused one goes back to the model once, with its
// reason, and a second refusal ends the loop. A non-nil error is a failure of the validator itself: the model sees
// it as a tool error, and it does not count as a refusal.
type Submitter interface {
	Spec() ports.ModelTool
	Submit(ctx context.Context, arguments json.RawMessage) (Verdict, error)
}
