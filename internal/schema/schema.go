package schema

import (
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/deliberation"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// IDs lists the registered schema ids in order. Each is the name an agent's output_schema field or a tool names.
func IDs() []string {
	return []string{"FindingIn@1", "Review@1", "TaskEnvelope@1", "TestSubmission@1", "Verdict@1"}
}

// target returns a zero value of the domain type behind a schema id, which the reflector reads.
func target(id string) (any, bool) {
	switch id {
	case "FindingIn@1":
		return finding.Input{}, true
	case "Review@1":
		return finding.Review{}, true
	case "TaskEnvelope@1":
		return workflow.TaskEnvelope{}, true
	case "TestSubmission@1":
		return testset.Submission{}, true
	case "Verdict@1":
		return deliberation.Verdict{}, true
	}
	return nil, false
}

// Schema returns the JSON Schema (draft 2020-12) of a registered id, indented and newline-terminated. The output is
// deterministic, so the golden files in testdata can be compared byte for byte.
func Schema(id string) ([]byte, error) {
	v, ok := target(id)
	if !ok {
		return nil, fmt.Errorf("schema %q is not registered; the ids are %v", id, IDs())
	}
	r := &jsonschema.Reflector{Anonymous: true, DoNotReference: true, ExpandedStruct: true}
	s := r.Reflect(v)
	s.Title = id
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode schema %s: %w", id, err)
	}
	return append(b, '\n'), nil
}
