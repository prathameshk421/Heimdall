package finding

import (
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// Review is the input of heimdall_submit_review (output schema Review@1).
type Review struct {
	HeadSHA       id.SHA   `json:"head_sha" jsonschema:"minLength=7,maxLength=64,pattern=^[0-9a-f]+$,description=The head commit the review was done on"`
	FilesReviewed []string `json:"files_reviewed" jsonschema:"minItems=1,description=Every file the reviewer read; paths are relative to the repository root"`
	Clean         bool     `json:"clean" jsonschema:"description=True only when no finding is filed; then findings is empty"`
	Findings      []Input  `json:"findings" jsonschema:"maxItems=50,description=The findings; each is backed by evidence"`
	// RuleChecks says, for each rule card the reviewer was given, whether the change violates it, keeps it, or is out of its scope.
	RuleChecks []RuleCheck `json:"rule_checks,omitempty" jsonschema:"maxItems=12,description=One entry per rule card you checked; optional"`
}

// MaxRuleChecks is the most rule checks one review carries.
const MaxRuleChecks = 12

// RuleCheck is the answer to one rule card.
type RuleCheck struct {
	Card    string      `json:"card" jsonschema:"minLength=1,maxLength=256,description=The card file name the rule came from"`
	Verdict RuleVerdict `json:"verdict" jsonschema:"enum=violated,enum=holds,enum=not_applicable"`
	Note    string      `json:"note,omitempty" jsonschema:"maxLength=300,description=One short sentence of why"`
}

// MaxRuleNoteChars is the longest note of a rule check.
const MaxRuleNoteChars = 300

func (r Review) ruleCheckProblems() []error {
	var errs []error
	if n := len(r.RuleChecks); n > MaxRuleChecks {
		errs = append(errs, fmt.Errorf("rule_checks has %s, at most %d", entries(n), MaxRuleChecks))
	}
	for i, c := range r.RuleChecks {
		if err := checkRef(c.Card); err != nil {
			errs = append(errs, fmt.Errorf("rule_checks[%d].card %w", i, err))
		}
		if _, err := ParseRuleVerdict(string(c.Verdict)); err != nil {
			errs = append(errs, fmt.Errorf("rule_checks[%d].%w", i, err))
		}
		if n := utf8.RuneCountInString(c.Note); n > MaxRuleNoteChars {
			errs = append(errs, fmt.Errorf("rule_checks[%d].note must be at most %d characters, got %d", i, MaxRuleNoteChars, n))
		}
	}
	return errs
}

// Validate returns every problem with the review, joined, each naming its field.
func (r Review) Validate() error {
	var errs []error
	if _, err := id.ParseSHA(string(r.HeadSHA)); err != nil {
		errs = append(errs, fmt.Errorf("head_sha %w", err))
	}
	errs = append(errs, r.filesProblems()...)
	errs = append(errs, r.ruleCheckProblems()...)
	switch {
	case r.Clean && len(r.Findings) > 0:
		errs = append(errs, fmt.Errorf("clean is true but findings has %s", entries(len(r.Findings))))
	case !r.Clean && len(r.Findings) == 0:
		errs = append(errs, errors.New("findings is empty and clean is false"))
	}
	if n := len(r.Findings); n > MaxFindings {
		errs = append(errs, fmt.Errorf("findings has %s, at most %d", entries(n), MaxFindings))
	}
	for i, f := range r.Findings {
		errs = append(errs, f.problems(fmt.Sprintf("findings[%d].", i))...)
	}
	return errors.Join(errs...)
}

func (r Review) filesProblems() []error {
	var errs []error
	if len(r.FilesReviewed) == 0 {
		errs = append(errs, errors.New("files_reviewed needs at least 1 entry"))
	}
	for i, f := range r.FilesReviewed {
		if err := CheckRepoPath(f); err != nil {
			errs = append(errs, fmt.Errorf("files_reviewed[%d] %w", i, err))
		} else if slices.Index(r.FilesReviewed, f) != i {
			errs = append(errs, fmt.Errorf("files_reviewed[%d] %q is listed more than once", i, f))
		}
	}
	return errs
}

// Reviewed reports whether the review covered file, named as it is in files_reviewed.
func (r Review) Reviewed(file string) bool { return slices.Contains(r.FilesReviewed, file) }
