package finding

import (
	"errors"
	"fmt"
	"strings"
)

// EvidenceType says which payload of an Evidence is set.
type EvidenceType string

// The evidence types (TECH_SPEC 14.1) and the tier each one can reach (11.1).
const (
	EvidenceReproTest  EvidenceType = "repro_test"
	EvidenceToolResult EvidenceType = "tool_result"
	EvidenceMemory     EvidenceType = "memory"
	EvidencePackRef    EvidenceType = "pack_ref"
	EvidenceReasoning  EvidenceType = "reasoning"
)

// ParseEvidenceType validates s against the closed set of evidence types.
func ParseEvidenceType(s string) (EvidenceType, error) {
	return parseEnum("evidence type", s, EvidenceReproTest, EvidenceToolResult, EvidenceMemory, EvidencePackRef, EvidenceReasoning)
}

// ReproTest is a test the sandbox runs on head; it must fail with an assertion for the finding to reach E1.
type ReproTest struct {
	Path    string `json:"path" jsonschema:"minLength=1,maxLength=512,description=Where the test file lives; under the test directory of the repository"`
	Name    string `json:"name" jsonschema:"minLength=1,description=The test function name"`
	Content string `json:"content" jsonschema:"minLength=1,description=The whole test file"`
}

// ToolResult names a deterministic tool run the harness repeats; the finding's location must match its report.
type ToolResult struct {
	Tool     string   `json:"tool" jsonschema:"minLength=1,description=The tool that produced the report; such as go vet or oasdiff"`
	Args     []string `json:"args" jsonschema:"description=The arguments the harness passes to the tool"`
	Location Location `json:"location"`
}

// Evidence is a tagged union: Type names the one payload that is set.
type Evidence struct {
	Type       EvidenceType `json:"type" jsonschema:"enum=repro_test,enum=tool_result,enum=memory,enum=pack_ref,enum=reasoning"`
	ReproTest  *ReproTest   `json:"repro_test,omitempty"`
	ToolResult *ToolResult  `json:"tool_result,omitempty"`
	MemoryRef  *string      `json:"memory_ref,omitempty" jsonschema:"minLength=1,maxLength=256"`
	PackRef    *string      `json:"pack_ref,omitempty" jsonschema:"minLength=1,maxLength=256"`
	Reasoning  *string      `json:"reasoning,omitempty" jsonschema:"minLength=10,maxLength=600"`
}

// Validate returns every problem with the evidence, joined.
func (e Evidence) Validate() error { return errors.Join(e.problems("evidence")...) }

// problems lists every problem with the evidence. path names it, such as "evidence[2]".
func (e Evidence) problems(path string) []error {
	if _, err := ParseEvidenceType(string(e.Type)); err != nil {
		return []error{fmt.Errorf("%s: type %q is not a known evidence type", path, e.Type)}
	}
	var errs []error
	if set := e.payloadCount(); set > 1 {
		errs = append(errs, fmt.Errorf("%s: exactly one payload may be set, got %d", path, set))
	}
	if !e.payloadSet(e.Type) {
		return append(errs, fmt.Errorf("%s: type is %s but %s is not set", path, e.Type, payloadField(e.Type)))
	}
	switch e.Type {
	case EvidenceReproTest:
		errs = append(errs, e.ReproTest.problems(path+".repro_test")...)
	case EvidenceToolResult:
		errs = append(errs, e.ToolResult.problems(path+".tool_result")...)
	case EvidenceMemory:
		errs = append(errs, refProblem(path+".memory_ref", *e.MemoryRef)...)
	case EvidencePackRef:
		errs = append(errs, refProblem(path+".pack_ref", *e.PackRef)...)
	case EvidenceReasoning:
		if err := checkChars(*e.Reasoning, ReasoningMinChars, ReasoningMaxChars); err != nil {
			errs = append(errs, fmt.Errorf("%s.reasoning %w", path, err))
		}
	}
	return errs
}

// problems lists every problem with the repro test. path is where it sits.
func (r ReproTest) problems(path string) []error {
	var errs []error
	if err := CheckRepoPath(r.Path); err != nil {
		errs = append(errs, fmt.Errorf("%s.path %w", path, err))
	}
	if strings.TrimSpace(r.Name) == "" {
		errs = append(errs, fmt.Errorf("%s.name must not be blank", path))
	}
	if strings.TrimSpace(r.Content) == "" {
		errs = append(errs, fmt.Errorf("%s.content must not be blank", path))
	}
	return errs
}

// problems lists every problem with the tool result. path is where it sits.
func (t ToolResult) problems(path string) []error {
	var errs []error
	if strings.TrimSpace(t.Tool) == "" {
		errs = append(errs, fmt.Errorf("%s.tool must not be blank", path))
	}
	return append(errs, t.Location.problems(path+".location")...)
}

// refProblem checks a memory or pack reference at path.
func refProblem(path, ref string) []error {
	if err := checkRef(ref); err != nil {
		return []error{fmt.Errorf("%s %w", path, err)}
	}
	return nil
}

// payloadCount is how many payload fields are set.
func (e Evidence) payloadCount() int {
	n := 0
	for _, set := range []bool{e.ReproTest != nil, e.ToolResult != nil, e.MemoryRef != nil, e.PackRef != nil, e.Reasoning != nil} {
		if set {
			n++
		}
	}
	return n
}

// payloadSet reports whether the payload field that belongs to t is set.
func (e Evidence) payloadSet(t EvidenceType) bool {
	switch t {
	case EvidenceReproTest:
		return e.ReproTest != nil
	case EvidenceToolResult:
		return e.ToolResult != nil
	case EvidenceMemory:
		return e.MemoryRef != nil
	case EvidencePackRef:
		return e.PackRef != nil
	case EvidenceReasoning:
		return e.Reasoning != nil
	}
	return false
}

// payloadField is the JSON name of the payload that belongs to t.
func payloadField(t EvidenceType) string {
	switch t {
	case EvidenceReproTest:
		return "repro_test"
	case EvidenceToolResult:
		return "tool_result"
	case EvidenceMemory:
		return "memory_ref"
	case EvidencePackRef:
		return "pack_ref"
	case EvidenceReasoning:
		return "reasoning"
	}
	return ""
}

// UnmarshalText validates while decoding, so a type outside the closed set cannot enter the domain through JSON.
func (t *EvidenceType) UnmarshalText(b []byte) error {
	v, err := ParseEvidenceType(string(b))
	*t = v
	return err
}

// NewReproTestEvidence is evidence of type repro_test.
func NewReproTestEvidence(r ReproTest) Evidence {
	return Evidence{Type: EvidenceReproTest, ReproTest: &r}
}

// NewToolResultEvidence is evidence of type tool_result.
func NewToolResultEvidence(r ToolResult) Evidence {
	return Evidence{Type: EvidenceToolResult, ToolResult: &r}
}

// NewMemoryEvidence is evidence of type memory.
func NewMemoryEvidence(ref string) Evidence { return Evidence{Type: EvidenceMemory, MemoryRef: &ref} }

// NewPackRefEvidence is evidence of type pack_ref.
func NewPackRefEvidence(ref string) Evidence { return Evidence{Type: EvidencePackRef, PackRef: &ref} }

// NewReasoningEvidence is evidence of type reasoning.
func NewReasoningEvidence(text string) Evidence {
	return Evidence{Type: EvidenceReasoning, Reasoning: &text}
}
