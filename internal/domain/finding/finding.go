package finding

import (
	"errors"
	"fmt"
)

// Limits of a submitted finding (TECH_SPEC 14.1). The JSON Schema tags below repeat them, and the schema package
// tests that the two agree.
const (
	ClaimMinChars     = 10
	ClaimMaxChars     = 600
	ReasoningMinChars = 10
	ReasoningMaxChars = 600
	MaxEvidence       = 8
	MaxContextRefs    = 8
	MaxFindings       = 50
	MaxPathBytes      = 512
	MaxRefBytes       = 256
)

// Location is where in the head tree a finding applies. Lines are 1-based and inclusive.
type Location struct {
	File  string `json:"file" jsonschema:"minLength=1,maxLength=512,description=Path of the file relative to the repository root with forward slashes"`
	Lines [2]int `json:"lines" jsonschema:"description=First and last line of the range; 1-based and inclusive; the first is at most the last"`
}

// Input is one finding as a reviewer submits it. The server adds the id, the reporter and the state.
type Input struct {
	Claim       string     `json:"claim" jsonschema:"minLength=10,maxLength=600,description=What is wrong and why it matters in one or two sentences"`
	Category    Category   `json:"category" jsonschema:"enum=correctness,enum=security,enum=compat,enum=ops,enum=spec_drift,enum=tests,enum=style"`
	Severity    Severity   `json:"severity" jsonschema:"enum=low,enum=medium,enum=high,enum=critical"`
	Confidence  Confidence `json:"confidence" jsonschema:"enum=high,enum=medium,enum=low,description=Your confidence; it feeds consensus scoring"`
	Location    Location   `json:"location"`
	Evidence    []Evidence `json:"evidence" jsonschema:"minItems=1,maxItems=8,description=At least one piece of evidence; stronger evidence can block a merge"`
	ContextRefs []string   `json:"context_refs" jsonschema:"maxItems=8,description=Pack or memory references that informed the finding"`
}

// Validate returns every problem with the finding, joined, each naming its field.
func (f Input) Validate() error { return errors.Join(f.problems("")...) }

// problems lists every problem with the finding. at is the path prefix of the finding, such as "findings[2].", or "".
func (f Input) problems(at string) []error {
	var errs []error
	if err := checkChars(f.Claim, ClaimMinChars, ClaimMaxChars); err != nil {
		errs = append(errs, fmt.Errorf("%sclaim %w", at, err))
	}
	if _, err := ParseCategory(string(f.Category)); err != nil {
		errs = append(errs, fmt.Errorf("%s%w", at, err))
	}
	if _, err := ParseSeverity(string(f.Severity)); err != nil {
		errs = append(errs, fmt.Errorf("%s%w", at, err))
	}
	if _, err := ParseConfidence(string(f.Confidence)); err != nil {
		errs = append(errs, fmt.Errorf("%s%w", at, err))
	}
	errs = append(errs, f.Location.problems(at+"location")...)
	errs = append(errs, f.evidenceProblems(at)...)
	if n := len(f.ContextRefs); n > MaxContextRefs {
		errs = append(errs, fmt.Errorf("%scontext_refs has %s, at most %d", at, entries(n), MaxContextRefs))
	}
	for i, r := range f.ContextRefs {
		if err := checkRef(r); err != nil {
			errs = append(errs, fmt.Errorf("%scontext_refs[%d] %w", at, i, err))
		}
	}
	return errs
}

func (f Input) evidenceProblems(at string) []error {
	var errs []error
	switch n := len(f.Evidence); {
	case n == 0:
		errs = append(errs, fmt.Errorf("%sevidence needs at least 1 entry", at))
	case n > MaxEvidence:
		errs = append(errs, fmt.Errorf("%sevidence has %s, at most %d", at, entries(n), MaxEvidence))
	}
	for i, e := range f.Evidence {
		errs = append(errs, e.problems(fmt.Sprintf("%sevidence[%d]", at, i))...)
	}
	return errs
}

// problems lists every problem with the location. path is where the location sits, such as "location".
func (l Location) problems(path string) []error {
	var errs []error
	if err := CheckRepoPath(l.File); err != nil {
		errs = append(errs, fmt.Errorf("%s.file %w", path, err))
	}
	if first, last := l.Lines[0], l.Lines[1]; first < 1 || last < first {
		errs = append(errs, fmt.Errorf("%s.lines must be 1-based with the first line at most the last, got [%d, %d]", path, first, last))
	}
	return errs
}
