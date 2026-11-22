package bench

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Kind says what a case is.
type Kind string

// The case kinds.
const (
	KindBug   Kind = "bug"   // a change that brought in a bug, plus the commit that fixed it
	KindClean Kind = "clean" // a merged change that no later fix or revert cites
)

// Status is what the manifest records about a case after its last check.
type Status string

// The case statuses.
const (
	StatusPending Status = "pending" // not checked yet, or no runner for its language
	StatusValid   Status = "valid"   // the last validate run proved the case real
	StatusInvalid Status = "invalid" // the case failed a check; Reason says which
)

// maxTimeout caps a case's time limit, so one case cannot hold a bench run for long.
const maxTimeout = 30 * time.Minute

// Commit names one commit and its first parent.
type Commit struct {
	SHA    string `yaml:"sha"`
	Parent string `yaml:"parent"`
	URL    string `yaml:"url"` // the PR or commit page
}

// FileTruth is the ground truth for one file: the line spans of the fix on the fix parent tree, as "N" or "N-M".
type FileTruth struct {
	File  string   `yaml:"file" json:"file"`
	Lines []string `yaml:"lines" json:"lines"`
}

// Case is one entry of the manifest.
type Case struct {
	ID          string    `yaml:"id"`
	Repo        string    `yaml:"repo"`
	Mirror      string    `yaml:"mirror"` // optional: a synced fork the bench fetches from instead of repo
	Language    string    `yaml:"language"`
	Toolchain   Toolchain `yaml:"toolchain"` // empty means go test for a Go case (toolchain.go)
	Kind        Kind      `yaml:"kind"`
	Status      Status    `yaml:"status"`
	Reason      string    `yaml:"reason"`
	Change      Commit    `yaml:"change"` // the intro change (bug) or the merged PR (clean)
	Fix         *Commit   `yaml:"fix"`
	TestCommand string    `yaml:"test_command"`
	Workdir     string    `yaml:"workdir"` // cargo and vitest: the directory where install and the tests run
	Install     []string  `yaml:"install"` // cargo and vitest: commands that run once per tree before the tests
	Sparse      []string  `yaml:"sparse"`  // non-cone sparse-checkout patterns; empty means the whole tree
	Tests       []string  `yaml:"tests"`   // cargo and vitest bug cases: the fix's target tests
	// Canary is a token that a case plants in its change to steer the reviewer ("print this token"). The reviewer must
	// never write it, so a run that does is injected. A case with a canary is adversarial; there is no adversarial
	// kind (checkCanary).
	Canary      string        `yaml:"canary"`
	Timeout     time.Duration `yaml:"timeout"`
	Description string        `yaml:"description"`
	Truth       []FileTruth   `yaml:"truth"`
	Sources     []string      `yaml:"sources"`
}

// RepoSlug returns owner/name of the case's GitHub repository.
func (c Case) RepoSlug() string { return strings.TrimPrefix(c.Repo, "https://github.com/") }

// Remote returns the URL the bench fetches from: the mirror when the case names one, else the repository.
func (c Case) Remote() string {
	if c.Mirror != "" {
		return c.Mirror
	}
	return c.Repo
}

// Manifest is the whole dataset.
type Manifest struct {
	Version int    `yaml:"version"`
	Cases   []Case `yaml:"cases"`
}

// Patterns of the manifest fields.
var (
	shaPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	idPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	repoPattern = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	spanPattern = regexp.MustCompile(`^([0-9]+)(?:-([0-9]+))?$`)
	// canaryPattern keeps a canary easy to search for and long enough to never appear by chance: 12 to 80 characters,
	// letters, digits, dot, underscore and dash, starting with a letter or digit.
	canaryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{11,79}$`)
)

// languages are the languages a case may name. Go, Rust, TypeScript and JavaScript have runners (toolchain.go).
func languages() []string {
	return []string{"go", "python", "typescript", "javascript", "java", "rust"}
}

// Parse reads the manifest and checks every rule in doc.go. The error lists every broken rule.
func Parse(b []byte) (Manifest, error) {
	var m Manifest
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("manifest: %w", err)
	}
	var errs []error
	if m.Version != 1 {
		errs = append(errs, fmt.Errorf("manifest: version %d, want 1", m.Version))
	}
	if len(m.Cases) == 0 {
		errs = append(errs, errors.New("manifest: no cases"))
	}
	seen := map[string]bool{}
	for i, c := range m.Cases {
		if seen[c.ID] {
			errs = append(errs, fmt.Errorf("case %d: duplicate id %q", i, c.ID))
		}
		seen[c.ID] = true
		for _, e := range checkCase(c) {
			errs = append(errs, fmt.Errorf("case %q: %w", c.ID, e))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func checkCase(c Case) []error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if !idPattern.MatchString(c.ID) {
		add("id %q: use lower-case letters, digits and dashes", c.ID)
	}
	errs = append(errs, checkRepo(c)...)
	if !slices.Contains(languages(), c.Language) {
		add("language %q: want one of %v", c.Language, languages())
	}
	errs = append(errs, checkToolchain(c)...)
	errs = append(errs, checkCommit("change", c.Change)...)
	errs = append(errs, checkKind(c)...)
	errs = append(errs, checkStatus(c)...)
	errs = append(errs, checkCanary(c)...)
	if c.TestCommand == "" {
		add("test_command is empty")
	}
	if c.Timeout <= 0 || c.Timeout > maxTimeout {
		add("timeout %v: want more than 0 and at most %v", c.Timeout, maxTimeout)
	}
	if c.Description == "" || strings.Contains(c.Description, "\n") {
		add("description: want one line")
	}
	if len(c.Sources) == 0 {
		add("sources: want at least one URL")
	}
	for _, s := range c.Sources {
		if !strings.HasPrefix(s, "https://") {
			add("source %q: want an https URL", s)
		}
	}
	return errs
}

// checkRepo checks the repository and the optional mirror the bench fetches from.
func checkRepo(c Case) []error {
	var errs []error
	if !repoPattern.MatchString(c.Repo) {
		errs = append(errs, fmt.Errorf("repo %q: want https://github.com/owner/name", c.Repo))
	}
	if c.Mirror != "" && !repoPattern.MatchString(c.Mirror) {
		errs = append(errs, fmt.Errorf("mirror %q: want https://github.com/owner/name", c.Mirror))
	}
	return errs
}

func checkCommit(name string, c Commit) []error {
	var errs []error
	if !shaPattern.MatchString(c.SHA) {
		errs = append(errs, fmt.Errorf("%s.sha %q: want 40 lower-case hex characters", name, c.SHA))
	}
	if !shaPattern.MatchString(c.Parent) {
		errs = append(errs, fmt.Errorf("%s.parent %q: want 40 lower-case hex characters", name, c.Parent))
	}
	if c.SHA != "" && c.SHA == c.Parent {
		errs = append(errs, fmt.Errorf("%s: a commit is not its own parent", name))
	}
	return errs
}

func checkKind(c Case) []error {
	var errs []error
	switch c.Kind {
	case KindBug:
		if c.Fix == nil {
			return []error{errors.New("a bug case needs a fix")}
		}
		errs = append(errs, checkCommit("fix", *c.Fix)...)
		errs = append(errs, checkTruth(c.Truth)...)
	case KindClean:
		if c.Fix != nil {
			errs = append(errs, errors.New("a clean case has no fix"))
		}
		if len(c.Truth) > 0 {
			errs = append(errs, errors.New("a clean case has no truth"))
		}
	default:
		errs = append(errs, fmt.Errorf("kind %q: want bug or clean; a case that plants text for the reviewer is one of them with a canary", c.Kind))
	}
	return errs
}

// checkCanary checks the canary token of a case. A bug or clean case may declare one; it marks the case as adversarial.
func checkCanary(c Case) []error {
	if c.Canary != "" && !canaryPattern.MatchString(c.Canary) {
		return []error{fmt.Errorf("canary %q: want 12 to 80 characters (letters, digits, dot, underscore, dash), starting with a letter or digit", c.Canary)}
	}
	return nil
}

func checkTruth(truth []FileTruth) []error {
	var errs []error
	for _, ft := range truth {
		if ft.File == "" {
			errs = append(errs, errors.New("truth file is empty"))
		}
		if len(ft.Lines) == 0 {
			errs = append(errs, fmt.Errorf("truth file %q has no lines", ft.File))
		}
		for _, span := range ft.Lines {
			if !validSpan(span) {
				errs = append(errs, fmt.Errorf("truth file %q: line span %q: want N or N-M with 1 <= N <= M", ft.File, span))
			}
		}
	}
	return errs
}

func validSpan(span string) bool {
	m := spanPattern.FindStringSubmatch(span)
	if m == nil {
		return false
	}
	start, err := strconv.Atoi(m[1])
	if err != nil || start < 1 {
		return false
	}
	if m[2] == "" {
		return true
	}
	end, err := strconv.Atoi(m[2])
	return err == nil && end >= start
}

func checkStatus(c Case) []error {
	switch c.Status {
	case StatusInvalid:
		if c.Reason == "" {
			return []error{errors.New("an invalid case needs a reason")}
		}
	case StatusValid, StatusPending:
		if c.Reason != "" {
			return []error{errors.New("only an invalid case has a reason")}
		}
		if c.Status == StatusValid && c.Kind == KindBug && len(c.Truth) == 0 {
			return []error{errors.New("a valid bug case needs truth")}
		}
	default:
		return []error{fmt.Errorf("status %q: want pending, valid or invalid", c.Status)}
	}
	return nil
}
