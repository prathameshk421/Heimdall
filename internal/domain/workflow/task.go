package workflow

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// Role is what an agent does in a change.
type Role string

// The LLM roles (TECH_SPEC 6.1). The coordinator is not an LLM and gets no task.
const (
	RoleTestAuthor Role = "test-author"
	RoleReviewer   Role = "reviewer"
	RoleResolver   Role = "resolver"
	RoleProfiler   Role = "profiler"
)

// ParseRole validates s.
func ParseRole(s string) (Role, error) {
	r := Role(s)
	if !slices.Contains([]Role{RoleTestAuthor, RoleReviewer, RoleResolver, RoleProfiler}, r) {
		return "", fmt.Errorf("role %q is not test-author, reviewer, resolver or profiler", s)
	}
	return r, nil
}

// UnmarshalText validates while decoding.
func (r *Role) UnmarshalText(b []byte) error { v, err := ParseRole(string(b)); *r = v; return err }

var (
	reBlob   = regexp.MustCompile(`^blob:sha256:[0-9a-f]{64}$`)
	reSkill  = regexp.MustCompile(`^([a-z][a-z-]*)@[0-9]+\.[0-9]+\.[0-9]+$`)
	reSchema = regexp.MustCompile(`^[A-Z][A-Za-z]*@[1-9][0-9]*$`)
	reRepo   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)
	reAC     = regexp.MustCompile(`^AC[1-9][0-9]{0,2}$`)
	reSource = regexp.MustCompile(`^[a-z][a-z_]*$`)
)

// BlobRef points at stored content by its SHA-256, as blob:sha256:<64 hex>. Agents read it through a tool; the text is
// never put in the envelope.
type BlobRef string

// ParseBlobRef validates s.
func ParseBlobRef(s string) (BlobRef, error) {
	if !reBlob.MatchString(s) {
		return "", fmt.Errorf("%q is not a blob reference (blob:sha256: and 64 lowercase hex characters)", s)
	}
	return BlobRef(s), nil
}

// UnmarshalText validates while decoding.
func (b *BlobRef) UnmarshalText(t []byte) error {
	v, err := ParseBlobRef(string(t))
	*b = v
	return err
}

// TaskEnvelope is what an agent receives from heimdall_get_task (TECH_SPEC 6.3). Everything in it that an outsider
// wrote is a reference, never inline text.
type TaskEnvelope struct {
	TaskID              id.TaskID `json:"task_id"`
	CaseID              id.CaseID `json:"case_id,omitempty"`
	Role                Role      `json:"role"`
	Skill               string    `json:"skill"`
	Change              ChangeRef `json:"change"`
	Goal                string    `json:"goal"`
	Inputs              Inputs    `json:"inputs"`
	ContextBudgetTokens int       `json:"context_budget_tokens"`
	OutputSchema        string    `json:"output_schema"`
	Deadline            time.Time `json:"deadline"`
}

// ChangeRef names the change a task is about.
type ChangeRef struct {
	ID      id.ChangeID `json:"id"`
	Repo    string      `json:"repo"`
	BaseSHA id.SHA      `json:"base_sha"`
	HeadSHA id.SHA      `json:"head_sha"`
}

// Inputs are the references an agent may read.
type Inputs struct {
	Diff               BlobRef        `json:"diff"`
	FilesChanged       []string       `json:"files_changed"`
	AcceptanceCriteria []CriterionRef `json:"acceptance_criteria"`
	CI                 []CIResult     `json:"ci"`
	Untrusted          []UntrustedRef `json:"untrusted"`
	// Answers are the person's answers to this task's questions. The text is data, not instructions (A14).
	Answers []AnswerRef `json:"answers,omitempty"`
}

// AnswerRef is one answered question: its short id (Q1), the answer text, and who gave it.
type AnswerRef struct {
	ID         string `json:"id"`
	Answer     string `json:"answer"`
	AnsweredBy string `json:"answered_by"`
}

// CriterionRef points at one acceptance criterion.
type CriterionRef struct {
	ID  string  `json:"id"`
	Ref BlobRef `json:"ref"`
}

// CIResult is the outcome of one CI check on head.
type CIResult struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
}

// UntrustedRef points at text written by someone outside the system, which the agent reads only through a tool.
type UntrustedRef struct {
	Source string  `json:"source"`
	Ref    BlobRef `json:"ref"`
}

const (
	maxGoalBytes    = 4000
	maxBudgetTokens = 10_000_000
	maxFilesChanged = 4096
)

func ciConclusion(c string) bool {
	return slices.Contains([]string{"success", "failure", "neutral", "cancelled", "skipped", "timed_out", "action_required", "pending"}, c)
}

// Validate says whether the envelope is one an agent could act on. The server builds envelopes, so a failure here is
// a bug in the server; it is checked anyway because the envelope is the contract with every agent.
func (e TaskEnvelope) Validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	errs = append(errs, e.validateCaseID()...)
	if _, err := ParseRole(string(e.Role)); err != nil {
		add("%v", err)
	}
	if m := reSkill.FindStringSubmatch(e.Skill); m == nil || m[1] != string(e.Role) {
		add("skill %q must be %s@<major.minor.patch>", e.Skill, e.Role)
	}
	if goal := strings.TrimSpace(e.Goal); goal == "" || len(e.Goal) > maxGoalBytes {
		add("goal must be 1 to %d bytes", maxGoalBytes)
	}
	if !reRepo.MatchString(e.Change.Repo) {
		add("change.repo %q is not owner/name", e.Change.Repo)
	}
	if e.ContextBudgetTokens < 1 || e.ContextBudgetTokens > maxBudgetTokens {
		add("context_budget_tokens %d must be 1 to %d", e.ContextBudgetTokens, maxBudgetTokens)
	}
	if !reSchema.MatchString(e.OutputSchema) {
		add("output_schema %q must look like Review@1", e.OutputSchema)
	}
	if e.Deadline.IsZero() {
		add("deadline is required: every task has one")
	}
	errs = append(errs, e.Inputs.validate()...)
	return errors.Join(errs...)
}

// validateCaseID rejects a non-empty case id that is not a CaseID. An empty id is valid for case-free agent tasks.
func (e TaskEnvelope) validateCaseID() []error {
	if e.CaseID == "" {
		return nil
	}
	if _, err := id.ParseCaseID(string(e.CaseID)); err != nil {
		return []error{fmt.Errorf("case_id: %w", err)}
	}
	return nil
}

func (in Inputs) validate() []error {
	var errs []error
	if _, err := ParseBlobRef(string(in.Diff)); err != nil {
		errs = append(errs, fmt.Errorf("inputs.diff: %w", err))
	}
	errs = append(errs, validateFiles(in.FilesChanged)...)
	errs = append(errs, validateCriteria(in.AcceptanceCriteria)...)
	errs = append(errs, validateCI(in.CI)...)
	errs = append(errs, validateUntrusted(in.Untrusted)...)
	return errs
}

func validateFiles(files []string) []error {
	var errs []error
	if len(files) > maxFilesChanged {
		errs = append(errs, fmt.Errorf("inputs.files_changed has %d entries, at most %d", len(files), maxFilesChanged))
	}
	for _, f := range files {
		// The same rule findings and test sets use (finding.CheckRepoPath), so the three cannot drift (A10 item 7).
		if err := finding.CheckRepoPath(f); err != nil {
			errs = append(errs, fmt.Errorf("inputs.files_changed: %w", err))
		}
	}
	return errs
}

func validateCriteria(cs []CriterionRef) []error {
	var errs []error
	for _, c := range cs {
		if !reAC.MatchString(c.ID) {
			errs = append(errs, fmt.Errorf("acceptance criterion id %q must look like AC2", c.ID))
		}
		if _, err := ParseBlobRef(string(c.Ref)); err != nil {
			errs = append(errs, fmt.Errorf("acceptance criterion %s: %w", c.ID, err))
		}
	}
	return errs
}

func validateCI(cis []CIResult) []error {
	var errs []error
	for _, c := range cis {
		if c.Name == "" || !ciConclusion(c.Conclusion) {
			errs = append(errs, fmt.Errorf("ci entry %q has conclusion %q", c.Name, c.Conclusion))
		}
	}
	return errs
}

func validateUntrusted(us []UntrustedRef) []error {
	var errs []error
	for _, u := range us {
		if !reSource.MatchString(u.Source) {
			errs = append(errs, fmt.Errorf("untrusted source %q must be lowercase words such as pr_body", u.Source))
		}
		if _, err := ParseBlobRef(string(u.Ref)); err != nil {
			errs = append(errs, fmt.Errorf("untrusted %s: %w", u.Source, err))
		}
	}
	return errs
}
