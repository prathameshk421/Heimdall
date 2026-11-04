package deliberation

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// Limits of a verdict (TECH_SPEC 14.1). The rationale limits and the citation count are this build's bounds on an
// agent's output, so the schema is finite; the JSON Schema tags repeat them and the schema package tests that.
const (
	RationaleMinChars = 10
	RationaleMaxChars = 1000
	MaxCitations      = 16
	MaxQuoteChars     = 300
)

// Decision is what the resolver recommends for a disputed finding (TECH_SPEC 11.3).
type Decision string

// The decisions a resolver can return.
const (
	DecisionUphold     Decision = "uphold"
	DecisionDismiss    Decision = "dismiss"
	DecisionAmend      Decision = "amend"
	DecisionSpecChange Decision = "spec_change"
	DecisionNeedsHuman Decision = "needs_human"
)

// ParseDecision validates s against the closed set of decisions.
func ParseDecision(s string) (Decision, error) {
	switch d := Decision(s); d {
	case DecisionUphold, DecisionDismiss, DecisionAmend, DecisionSpecChange, DecisionNeedsHuman:
		return d, nil
	}
	return "", fmt.Errorf("decision %q is not one of uphold, dismiss, amend, spec_change, needs_human", s)
}

// UnmarshalText validates while decoding.
func (d *Decision) UnmarshalText(b []byte) error {
	v, err := ParseDecision(string(b))
	*d = v
	return err
}

// CitationKind says what a citation points at (TECH_SPEC 14.1).
type CitationKind string

// The citation kinds.
const (
	CitationRun      CitationKind = "run"
	CitationMemory   CitationKind = "memory"
	CitationDecision CitationKind = "decision"
	CitationFinding  CitationKind = "finding"
)

// ParseCitationKind validates s against the closed set of citation kinds.
func ParseCitationKind(s string) (CitationKind, error) {
	switch k := CitationKind(s); k {
	case CitationRun, CitationMemory, CitationDecision, CitationFinding:
		return k, nil
	}
	return "", fmt.Errorf("citation kind %q is not run, memory, decision or finding", s)
}

// UnmarshalText validates while decoding.
func (k *CitationKind) UnmarshalText(b []byte) error {
	v, err := ParseCitationKind(string(b))
	*k = v
	return err
}

// Citation is one piece of evidence a verdict relies on. The server re-verifies it before the verdict is shown.
type Citation struct {
	Kind  CitationKind `json:"kind" jsonschema:"enum=run,enum=memory,enum=decision,enum=finding"`
	Ref   string       `json:"ref" jsonschema:"minLength=1,maxLength=256,description=The cited run id; memory ref; decision ref; or finding name such as F3"`
	Quote string       `json:"quote" jsonschema:"minLength=1,maxLength=300,description=The span of the cited item the verdict relies on"`
}

// Verdict is the output of heimdall_submit_verdict: the resolver's recommendation on a case (TECH_SPEC 14.1).
type Verdict struct {
	CaseID     id.CaseID          `json:"case_id" jsonschema:"pattern=^case_[0-9A-HJKMNP-TV-Z]{26}$,description=The case this verdict settles"`
	Decision   Decision           `json:"decision" jsonschema:"enum=uphold,enum=dismiss,enum=amend,enum=spec_change,enum=needs_human"`
	Rationale  string             `json:"rationale" jsonschema:"minLength=10,maxLength=1000,description=The reasons in a few sentences; each claim is proven by a citation"`
	Citations  []Citation         `json:"citations" jsonschema:"minItems=1,maxItems=16,description=Every claim the rationale makes is cited"`
	Confidence finding.Confidence `json:"confidence" jsonschema:"enum=high,enum=medium,enum=low"`
}

// Validate returns every problem with the verdict, joined, each naming its field.
func (v Verdict) Validate() error {
	var errs []error
	if _, err := id.ParseCaseID(string(v.CaseID)); err != nil {
		errs = append(errs, fmt.Errorf("case_id %w", err))
	}
	if _, err := ParseDecision(string(v.Decision)); err != nil {
		errs = append(errs, err)
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(v.Rationale)); n < RationaleMinChars || n > RationaleMaxChars {
		errs = append(errs, fmt.Errorf("rationale must be %d to %d characters, got %d", RationaleMinChars, RationaleMaxChars, n))
	}
	if _, err := finding.ParseConfidence(string(v.Confidence)); err != nil {
		errs = append(errs, err)
	}
	switch n := len(v.Citations); {
	case n == 0:
		errs = append(errs, errors.New("citations needs at least 1 entry"))
	case n > MaxCitations:
		errs = append(errs, fmt.Errorf("citations has %d entries, at most %d", n, MaxCitations))
	}
	for i, c := range v.Citations {
		errs = append(errs, c.problems(fmt.Sprintf("citations[%d]", i))...)
	}
	return errors.Join(errs...)
}

// problems lists every problem with the citation. path is where it sits, such as "citations[0]".
func (c Citation) problems(path string) []error {
	if _, err := ParseCitationKind(string(c.Kind)); err != nil {
		return []error{fmt.Errorf("%s: kind %w", path, err)}
	}
	var errs []error
	if err := c.checkRef(); err != nil {
		errs = append(errs, fmt.Errorf("%s.ref %w", path, err))
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(c.Quote)); n < 1 || n > MaxQuoteChars {
		errs = append(errs, fmt.Errorf("%s.quote must be 1 to %d characters, got %d", path, MaxQuoteChars, n))
	}
	return errs
}

// checkRef checks the ref against the form its kind names: a run id, a finding name, or a bounded non-blank string.
func (c Citation) checkRef() error {
	switch c.Kind {
	case CitationRun:
		_, err := id.ParseRunID(c.Ref)
		return err
	case CitationFinding:
		_, err := id.ParseFindingRef(c.Ref)
		return err
	case CitationMemory, CitationDecision:
	}
	if strings.TrimSpace(c.Ref) == "" {
		return errors.New("must not be blank")
	}
	if len(c.Ref) > finding.MaxRefBytes {
		return fmt.Errorf("must be at most %d bytes, got %d", finding.MaxRefBytes, len(c.Ref))
	}
	return nil
}
