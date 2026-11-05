package finding

import (
	"errors"
	"fmt"
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// Review is the input of heimdall_submit_review (output schema Review@1).
type Review struct {
	HeadSHA       id.SHA   `json:"head_sha" jsonschema:"minLength=7,maxLength=64,pattern=^[0-9a-f]+$,description=The head commit the review was done on"`
	FilesReviewed []string `json:"files_reviewed" jsonschema:"minItems=1,description=Every file the reviewer read; paths are relative to the repository root"`
	Clean         bool     `json:"clean" jsonschema:"description=True only when no finding is filed; then findings is empty"`
	Findings      []Input  `json:"findings" jsonschema:"maxItems=50,description=The findings; each is backed by evidence"`
}

// Validate returns every problem with the review, joined, each naming its field.
func (r Review) Validate() error {
	var errs []error
	if _, err := id.ParseSHA(string(r.HeadSHA)); err != nil {
		errs = append(errs, fmt.Errorf("head_sha %w", err))
	}
	errs = append(errs, r.filesProblems()...)
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
