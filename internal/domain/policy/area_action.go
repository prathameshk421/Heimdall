package policy

import (
	"regexp"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
)

// The action area of the policy is the action key: how Heimdall runs as a step in the customer's GitHub Actions
// (ACTION_MODE 1.2, 5, 7). An absent area means shadow mode with the defaults of Effective, so a repository that adds
// no key behaves as the safe default. Every value here is read from the merge base like the rest of the policy.

// The two modes. Shadow posts neutral checks and blocks nothing. Enforce posts success, failure or action_required.
const (
	ActionModeShadow  = "shadow"
	ActionModeEnforce = "enforce"
)

// The two places analyzer results come from.
const (
	AnalyzerSourceCodeScanning  = "code_scanning"
	AnalyzerSourceSARIFArtifact = "sarif_artifact"
)

// BreakGlassMinReason is the shortest reason a break-glass command may carry (ACTION_MODE 5.4).
const BreakGlassMinReason = 15

// ActionArea holds the action key of the policy. Its field is promoted, so a caller writes p.Action.
type ActionArea struct {
	Action Action `yaml:"action,omitempty" json:"action,omitzero"`
}

// Action is the action key. A zero field means "use the default" and Effective fills it. A zero Action is valid.
type Action struct {
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
	// ProofRequired makes heimdall/proof a check the team requires: in enforce mode it can fail or block (5.4).
	ProofRequired bool         `yaml:"proof_required,omitempty" json:"proof_required,omitempty"`
	Budget        ActionBudget `yaml:"budget,omitempty" json:"budget,omitzero"`
	// Trivial are the globs of paths that need no model (7). A change whose every path matches skips the proof run.
	Trivial []string `yaml:"trivial,omitempty" json:"trivial,omitempty"`
	// TrivialLines skips the proof run for a change of at most this many lines outside tests. Zero turns the size rule
	// off, so only Trivial decides.
	TrivialLines int `yaml:"trivial_lines,omitempty" json:"trivial_lines,omitempty"`
	// Pinned are the globs of test files that a change may not delete, rename, edit or skip without a policy owner (5.1).
	// There is no default: no globs means the pinned_tests clause is not checked.
	Pinned       []string        `yaml:"pinned,omitempty" json:"pinned,omitempty"`
	Analyzers    ActionAnalyzers `yaml:"analyzers,omitempty" json:"analyzers,omitzero"`
	DiffCoverage ActionCoverage  `yaml:"diff_coverage,omitempty" json:"diff_coverage,omitzero"`
	Sources      []string        `yaml:"sources,omitempty" json:"sources,omitempty"`
	BreakGlass   []string        `yaml:"break_glass,omitempty" json:"break_glass,omitempty"`
}

// ActionBudget caps what the proof run may spend (7).
type ActionBudget struct {
	PerPRUSD         float64      `yaml:"per_pr_usd,omitempty" json:"per_pr_usd,omitempty"`
	PerPRTokens      ActionTokens `yaml:"per_pr_tokens,omitempty" json:"per_pr_tokens,omitzero"`
	CallOutputTokens int          `yaml:"call_output_tokens,omitempty" json:"call_output_tokens,omitempty"`
	MonthlyUSD       float64      `yaml:"monthly_usd,omitempty" json:"monthly_usd,omitempty"`
}

// ActionTokens is a token cap split into what goes in and what comes out.
type ActionTokens struct {
	Input  int `yaml:"input,omitempty" json:"input,omitempty"`
	Output int `yaml:"output,omitempty" json:"output,omitempty"`
}

// ActionAnalyzers says where the analyzers clause reads results and which severity counts (5.1). With the source
// sarif_artifact the declared workflow produces the artifact, and a change to that workflow makes the clause unknown.
type ActionAnalyzers struct {
	Source      string           `yaml:"source,omitempty" json:"source,omitempty"`
	MinSeverity finding.Severity `yaml:"min_severity,omitempty" json:"min_severity,omitempty"`
	Workflow    string           `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	Artifact    string           `yaml:"artifact,omitempty" json:"artifact,omitempty"`
}

// ActionCoverage configures the diff_coverage clause (5.1). A zero Min means it is not configured, and the clause is
// "not checked". With a Min, the declared test workflow and artifact say where the coverage report comes from.
type ActionCoverage struct {
	// Min is the lowest share of changed lines that tests cover, in percent.
	Min      float64 `yaml:"min,omitempty" json:"min,omitempty"`
	Workflow string  `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	Artifact string  `yaml:"artifact,omitempty" json:"artifact,omitempty"`
}

// Effective returns the action area with every unset field filled by its default. It returns fresh slices on each
// call, so a caller can change the result without touching another caller's value.
//
// The default review sources are the bot logins of CodeRabbit, GitHub Copilot code review, Cursor Bugbot and
// Greptile. These logins are `not verified` against real comments (ACTION_MODE 6.1, work package A5). An empty list
// means the default, so the policy cannot name zero sources.
func (a Action) Effective() Action {
	if a.Mode == "" {
		a.Mode = ActionModeShadow
	}
	if a.Budget.PerPRUSD == 0 {
		a.Budget.PerPRUSD = 0.50
	}
	if a.Budget.PerPRTokens.Input == 0 {
		a.Budget.PerPRTokens.Input = 800_000
	}
	if a.Budget.PerPRTokens.Output == 0 {
		a.Budget.PerPRTokens.Output = 40_000
	}
	if a.Budget.CallOutputTokens == 0 {
		a.Budget.CallOutputTokens = 4_000
	}
	if a.Budget.MonthlyUSD == 0 {
		a.Budget.MonthlyUSD = 50
	}
	if len(a.Trivial) == 0 {
		a.Trivial = defaultTrivial()
	} else {
		a.Trivial = append([]string(nil), a.Trivial...)
	}
	if a.Analyzers.Source == "" {
		a.Analyzers.Source = AnalyzerSourceCodeScanning
	}
	if a.Analyzers.MinSeverity == "" {
		a.Analyzers.MinSeverity = finding.SeverityMedium
	}
	if len(a.Sources) == 0 {
		a.Sources = defaultSources()
	} else {
		a.Sources = append([]string(nil), a.Sources...)
	}
	a.Pinned = append([]string(nil), a.Pinned...)
	a.BreakGlass = append([]string(nil), a.BreakGlass...)
	return a
}

// Enforcing reports whether the effective mode is enforce.
func (a Action) Enforcing() bool { return a.Effective().Mode == ActionModeEnforce }

// defaultTrivial lists the paths that need no model: docs, lockfiles and generated files (ACTION_MODE 7). It is a
// function so no package-level state can be changed by a caller.
func defaultTrivial() []string {
	return []string{
		"**/*.md", "docs/**",
		"go.sum", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "Cargo.lock", "poetry.lock", "Gemfile.lock",
		"**/*.pb.go", "**/*_generated.go", "**/*.generated.*",
	}
}

// defaultSources is the list of review sources that a policy with no action area counts (ACTION_MODE 6.1). Copilot is
// listed under both of its logins; the reader accepts either, so a policy written from the older documentation and the
// real login agree. Macroscope, the Codex connector and Devin are listed because they post inline findings on the
// repositories this product was measured on (docs/product/research/13-t3code-bot-comments.md); a repository without
// them gains nothing and loses nothing.
func defaultSources() []string {
	return []string{
		"coderabbitai[bot]", "copilot-pull-request-reviewer[bot]", "Copilot", "cursor[bot]", "greptile-apps[bot]",
		"macroscopeapp[bot]", "chatgpt-codex-connector[bot]", "devin-ai-integration[bot]",
	}
}

var (
	reGitHubLogin = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
	reNoSpace     = regexp.MustCompile(`^\S+$`)
)

// checkAction checks the action area. An absent area is valid. A value that is set must be usable, and a source of
// results that is declared must name the workflow that makes it, so a change to that workflow can be seen.
func (p Policy) checkAction(c *checker) {
	a := p.Action
	if a.Mode != "" && a.Mode != ActionModeShadow && a.Mode != ActionModeEnforce {
		c.addf("action.mode must be shadow or enforce, got %q", a.Mode)
	}
	a.checkBudget(c)
	if a.TrivialLines < 0 {
		c.addf("action.trivial_lines must be >= 0, got %d", a.TrivialLines)
	}
	checkGlobList(c, "action.trivial", a.Trivial)
	checkGlobList(c, "action.pinned", a.Pinned)
	a.checkInputs(c)
	checkLogins(c, "action.sources", a.Sources, func(s string) bool { return reNoSpace.MatchString(s) })
	checkLogins(c, "action.break_glass", a.BreakGlass, reGitHubLogin.MatchString)
}

func (a Action) checkBudget(c *checker) {
	b := a.Budget
	for _, f := range []struct {
		key string
		val float64
	}{{"per_pr_usd", b.PerPRUSD}, {"monthly_usd", b.MonthlyUSD}} {
		if f.val < 0 {
			c.addf("action.budget.%s must be >= 0, got %g", f.key, f.val)
		}
	}
	for _, f := range []struct {
		key string
		val int
	}{{"per_pr_tokens.input", b.PerPRTokens.Input}, {"per_pr_tokens.output", b.PerPRTokens.Output}, {"call_output_tokens", b.CallOutputTokens}} {
		if f.val < 0 {
			c.addf("action.budget.%s must be >= 0, got %d", f.key, f.val)
		}
	}
	// The monthly cap skips a run when spend plus one PR budget is over it, so a PR budget above the cap never runs.
	eff := a.Effective().Budget
	if eff.PerPRUSD > eff.MonthlyUSD {
		c.addf("action.budget.per_pr_usd must be <= action.budget.monthly_usd, got %g and %g", eff.PerPRUSD, eff.MonthlyUSD)
	}
	if eff.CallOutputTokens > eff.PerPRTokens.Output {
		c.addf("action.budget.call_output_tokens must be <= action.budget.per_pr_tokens.output, got %d and %d",
			eff.CallOutputTokens, eff.PerPRTokens.Output)
	}
}

func (a Action) checkInputs(c *checker) {
	an := a.Analyzers
	switch an.Source {
	case "", AnalyzerSourceCodeScanning:
	case AnalyzerSourceSARIFArtifact:
		checkInput(c, "action.analyzers", an.Workflow, an.Artifact)
	default:
		c.addf("action.analyzers.source must be %s or %s, got %q", AnalyzerSourceCodeScanning, AnalyzerSourceSARIFArtifact, an.Source)
	}
	if an.MinSeverity != "" {
		if _, err := finding.ParseSeverity(string(an.MinSeverity)); err != nil {
			c.addf("action.analyzers.min_severity: %v", err)
		}
	}
	cov := a.DiffCoverage
	if !(cov.Min >= 0 && cov.Min <= 100) {
		c.addf("action.diff_coverage.min must be between 0 and 100, got %g", cov.Min)
	}
	if cov.Min > 0 {
		checkInput(c, "action.diff_coverage", cov.Workflow, cov.Artifact)
	}
}

// checkInput checks a declared input. It names the workflow that produces it and the artifact it uploads. The
// workflow is part of the policy so that a pull request which edits it cannot hide a result (ACTION_MODE 2.6).
func checkInput(c *checker, key, workflow, artifact string) {
	switch {
	case workflow == "":
		c.addf("%s.workflow is required: it names the workflow that produces the input", key)
	case !cleanRelativePath(workflow):
		c.addf("%s.workflow must be a clean relative path inside the repository, got %q", key, workflow)
	}
	if strings.TrimSpace(artifact) == "" {
		c.addf("%s.artifact is required: it names the artifact the workflow uploads", key)
	}
}

func checkGlobList(c *checker, key string, globs []string) {
	seen := map[string]bool{}
	for i, pattern := range globs {
		if err := ValidateGlob(pattern); err != nil {
			c.addf("%s[%d]: %v", key, i, err)
		} else if matchesEverything(pattern) {
			c.addf("%s[%d]: %q would match every file", key, i, pattern)
		}
		if seen[pattern] {
			c.addf("%s[%d]: %q is listed twice", key, i, pattern)
		}
		seen[pattern] = true
	}
}

// checkLogins checks a list of GitHub logins. Logins are unique without regard to case, as GitHub treats them.
func checkLogins(c *checker, key string, logins []string, valid func(string) bool) {
	seen := map[string]bool{}
	for i, l := range logins {
		if !valid(l) {
			c.addf("%s[%d]: %q is not a GitHub login", key, i, l)
			continue
		}
		lower := strings.ToLower(l)
		if seen[lower] {
			c.addf("%s[%d]: %q is listed twice", key, i, l)
		}
		seen[lower] = true
	}
}

// Protects reports whether path is one of Heimdall's own files under this action policy: heimdall.yaml, anything
// under .heimdall/, a Heimdall workflow, or a workflow the policy declares as the producer of an input in use. A
// change to such a file is never trivial and needs a policy owner (ACTION_MODE 5.1 heimdall_files, section 7). The
// fast check and the proof skip both call this, so the two can never disagree.
func (a Action) Protects(path string) bool {
	if path == "heimdall.yaml" || path == "heimdall.yml" ||
		MatchGlob(".heimdall/**", path) ||
		MatchGlob(".github/workflows/heimdall-*.yml", path) ||
		MatchGlob(".github/workflows/heimdall-*.yaml", path) {
		return true
	}
	if a.Analyzers.Source == AnalyzerSourceSARIFArtifact && a.Analyzers.Workflow != "" && path == a.Analyzers.Workflow {
		return true
	}
	return a.DiffCoverage.Min > 0 && a.DiffCoverage.Workflow != "" && path == a.DiffCoverage.Workflow
}
