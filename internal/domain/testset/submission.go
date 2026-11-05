package testset

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
)

// Limits of a test submission. They bound what an agent may send in one call; the JSON Schema tags repeat them and
// the schema package tests that the two agree.
const (
	MaxFiles         = 32
	MaxFileBytes     = 65536
	MaxExpectedPaths = 32
	MaxTestNameChars = 200
	MaxNotesChars    = 2000
)

var reAC = regexp.MustCompile(`^AC[1-9][0-9]{0,2}$`)

// Expect is what a submission is checked against: the test directory the policy allows and the acceptance criteria
// the ticket has. Policy lives outside this package, so the caller passes both in.
type Expect struct {
	TestsDir string
	ACIDs    []string
}

// File is one test file the test-author writes. Path is relative to the repository root.
type File struct {
	Path    string `json:"path" jsonschema:"minLength=1,maxLength=512,description=Path of the test file relative to the repository root; under the tests directory"`
	Content string `json:"content" jsonschema:"minLength=1,maxLength=65536,description=The whole test file"`
}

// Submission is the input of heimdall_submit_tests (TestSubmission@1, TECH_SPEC 14.1 and 7). The server commits the
// files after the sandbox has run them red-first and checked the acceptance-criteria coverage (TECH_SPEC 12.1).
type Submission struct {
	Files              []File              `json:"files" jsonschema:"minItems=1,maxItems=32,description=The test files; all under the tests directory"`
	AcceptanceCriteria map[string][]string `json:"acceptance_criteria" jsonschema:"description=For each acceptance criterion id such as AC2; the names of the tests that cover it"`
	ExpectedPaths      []string            `json:"expected_paths" jsonschema:"maxItems=32,description=Paths outside the tests directory the change is expected to touch"`
	Notes              string              `json:"notes" jsonschema:"maxLength=2000,description=Assumptions made where the ticket was silent; written into the test header"`
}

// Validate returns every problem with the submission, joined, each naming its field path.
func (s Submission) Validate(e Expect) error {
	var errs []error
	if err := finding.CheckRepoPath(e.TestsDir); err != nil {
		errs = append(errs, fmt.Errorf("tests directory %w", err))
	}
	errs = append(errs, s.fileProblems(e.TestsDir)...)
	errs = append(errs, s.criteriaProblems(e.ACIDs)...)
	errs = append(errs, s.expectedPathProblems(e.TestsDir)...)
	if n := utf8.RuneCountInString(s.Notes); n > MaxNotesChars {
		errs = append(errs, fmt.Errorf("notes must be at most %d characters, got %d", MaxNotesChars, n))
	}
	return errors.Join(errs...)
}

func (s Submission) fileProblems(testsDir string) []error {
	var errs []error
	switch n := len(s.Files); {
	case n == 0:
		errs = append(errs, errors.New("files needs at least 1 entry"))
	case n > MaxFiles:
		errs = append(errs, fmt.Errorf("files has %d entries, at most %d", n, MaxFiles))
	}
	seen := map[string]bool{}
	dirValid := finding.CheckRepoPath(testsDir) == nil
	for i, f := range s.Files {
		at := fmt.Sprintf("files[%d]", i)
		if err := finding.CheckRepoPath(f.Path); err != nil {
			errs = append(errs, fmt.Errorf("%s.path %w", at, err))
		} else if dirValid && !strings.HasPrefix(f.Path, testsDir+"/") {
			errs = append(errs, fmt.Errorf("%s.path %q is outside the tests directory %s", at, f.Path, testsDir))
		}
		if seen[f.Path] {
			errs = append(errs, fmt.Errorf("%s.path %q is listed more than once", at, f.Path))
		}
		seen[f.Path] = true
		if strings.TrimSpace(f.Content) == "" {
			errs = append(errs, fmt.Errorf("%s.content must not be blank", at))
		}
		if len(f.Content) > MaxFileBytes {
			errs = append(errs, fmt.Errorf("%s.content must be at most %d bytes, got %d", at, MaxFileBytes, len(f.Content)))
		}
	}
	return errs
}

func (s Submission) criteriaProblems(acIDs []string) []error {
	var errs []error
	for _, id := range slices.Sorted(maps.Keys(s.AcceptanceCriteria)) {
		at := fmt.Sprintf("acceptance_criteria[%s]", id)
		if !reAC.MatchString(id) {
			errs = append(errs, fmt.Errorf("%s: the id must look like AC2", at))
		} else if !slices.Contains(acIDs, id) {
			errs = append(errs, fmt.Errorf("%s: the ticket has no such acceptance criterion", at))
		}
		for j, name := range s.AcceptanceCriteria[id] {
			if strings.TrimSpace(name) == "" {
				errs = append(errs, fmt.Errorf("%s[%d] must not be blank", at, j))
			} else if n := utf8.RuneCountInString(name); n > MaxTestNameChars {
				errs = append(errs, fmt.Errorf("%s[%d] must be at most %d characters, got %d", at, j, MaxTestNameChars, n))
			}
		}
	}
	for _, id := range acIDs {
		if len(s.AcceptanceCriteria[id]) == 0 {
			errs = append(errs, fmt.Errorf("acceptance_criteria[%s] has no test; every criterion needs at least one", id))
		}
	}
	return errs
}

func (s Submission) expectedPathProblems(testsDir string) []error {
	var errs []error
	if n := len(s.ExpectedPaths); n > MaxExpectedPaths {
		errs = append(errs, fmt.Errorf("expected_paths has %d entries, at most %d", n, MaxExpectedPaths))
	}
	dirValid := finding.CheckRepoPath(testsDir) == nil
	for i, p := range s.ExpectedPaths {
		at := fmt.Sprintf("expected_paths[%d]", i)
		if err := finding.CheckRepoPath(p); err != nil {
			errs = append(errs, fmt.Errorf("%s %w", at, err))
			continue
		}
		if dirValid && strings.HasPrefix(p, testsDir+"/") {
			errs = append(errs, fmt.Errorf("%s %q is under the tests directory; list only paths outside it", at, p))
		}
		if slices.Index(s.ExpectedPaths, p) != i {
			errs = append(errs, fmt.Errorf("%s %q is listed more than once", at, p))
		}
	}
	return errs
}
