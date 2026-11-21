package policy_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// withAction returns the spec policy with the golden action area merged in, the way a heimdall.yaml carries it.
func withAction(t *testing.T) policy.Policy {
	t.Helper()
	p := specPolicy(t)
	b, err := os.ReadFile("testdata/action.golden.yaml")
	require.NoError(t, err)
	var area policy.ActionArea
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	require.NoError(t, dec.Decode(&area))
	p.ActionArea = area
	return p
}

func TestTheActionGoldenDecodesAndValidates(t *testing.T) {
	p := withAction(t)
	require.NoError(t, p.Validate())
	a := p.Action
	require.Equal(t, policy.ActionModeEnforce, a.Mode)
	require.True(t, a.ProofRequired)
	require.InDelta(t, 0.75, a.Budget.PerPRUSD, 1e-9)
	require.Equal(t, 600000, a.Budget.PerPRTokens.Input)
	require.InDelta(t, 80.0, a.DiffCoverage.Min, 1e-9)
	require.Equal(t, finding.SeverityHigh, a.Analyzers.MinSeverity)
	require.Equal(t, []string{"alice", "ana"}, a.BreakGlass)
}

// A policy with no action area is valid and means shadow mode with the defaults (ACTION_MODE 1.2).
func TestAnAbsentActionAreaIsShadowWithDefaults(t *testing.T) {
	p := specPolicy(t)
	require.NoError(t, p.Validate())
	require.Zero(t, p.Action)

	eff := p.Action.Effective()
	require.False(t, eff.Enforcing())

	got, err := json.Marshal(eff)
	require.NoError(t, err)
	want, err := os.ReadFile("testdata/action.defaults.golden.json")
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(got), "golden JSON: the default action area is frozen")

	// An absent area adds no key to the policy, so the frozen policy wire format does not move.
	out, err := json.Marshal(p)
	require.NoError(t, err)
	require.NotContains(t, string(out), `"action"`)
}

func TestEffectiveKeepsWhatIsSetAndFillsTheRest(t *testing.T) {
	a := policy.Action{Mode: policy.ActionModeEnforce, Budget: policy.ActionBudget{PerPRUSD: 1}}.Effective()
	require.True(t, a.Enforcing())
	require.InDelta(t, 1.0, a.Budget.PerPRUSD, 1e-9)
	require.InDelta(t, 50.0, a.Budget.MonthlyUSD, 1e-9)
	require.Equal(t, 4000, a.Budget.CallOutputTokens)
	require.Equal(t, 800000, a.Budget.PerPRTokens.Input)

	set := withAction(t).Action
	require.Equal(t, set, set.Effective(), "a fully set area is its own effective value")
}

func TestTheActionAreaRoundTripsThroughYAMLAndJSON(t *testing.T) {
	p := withAction(t)
	y, err := yaml.Marshal(p)
	require.NoError(t, err)
	var fromYAML policy.Policy
	require.NoError(t, yaml.Unmarshal(y, &fromYAML))
	require.True(t, policy.Equal(p, fromYAML))

	j, err := json.Marshal(p)
	require.NoError(t, err)
	var fromJSON policy.Policy
	require.NoError(t, json.Unmarshal(j, &fromJSON))
	require.True(t, policy.Equal(p, fromJSON))
}

func TestAnActionChangeIsAPolicyChange(t *testing.T) {
	a, b := withAction(t), withAction(t)
	require.True(t, policy.Equal(a, b))
	b.Action.Mode = policy.ActionModeShadow
	require.False(t, policy.Equal(a, b), "a mode flip is a policy change, so a policy owner must approve it")
	b = withAction(t)
	b.Action.BreakGlass = append(b.Action.BreakGlass, "mallory")
	require.False(t, policy.Equal(a, b))
}

func TestValidateRefusesEachBrokenActionKeyAndNamesIt(t *testing.T) {
	mutations := map[string]struct {
		mutate func(*policy.Action)
		want   string
	}{
		"mode unknown":                 {func(a *policy.Action) { a.Mode = "loud" }, `action.mode must be shadow or enforce, got "loud"`},
		"per pr usd negative":          {func(a *policy.Action) { a.Budget.PerPRUSD = -1 }, "action.budget.per_pr_usd must be >= 0"},
		"monthly usd negative":         {func(a *policy.Action) { a.Budget.MonthlyUSD = -5 }, "action.budget.monthly_usd must be >= 0"},
		"per pr above monthly":         {func(a *policy.Action) { a.Budget.PerPRUSD = 200 }, "action.budget.per_pr_usd must be <= action.budget.monthly_usd"},
		"input tokens negative":        {func(a *policy.Action) { a.Budget.PerPRTokens.Input = -1 }, "action.budget.per_pr_tokens.input must be >= 0"},
		"output tokens negative":       {func(a *policy.Action) { a.Budget.PerPRTokens.Output = -1 }, "action.budget.per_pr_tokens.output must be >= 0"},
		"call output negative":         {func(a *policy.Action) { a.Budget.CallOutputTokens = -1 }, "action.budget.call_output_tokens must be >= 0"},
		"call output above pr output":  {func(a *policy.Action) { a.Budget.CallOutputTokens = 31000 }, "action.budget.call_output_tokens must be <= action.budget.per_pr_tokens.output"},
		"trivial lines negative":       {func(a *policy.Action) { a.TrivialLines = -1 }, "action.trivial_lines must be >= 0"},
		"trivial glob bad":             {func(a *policy.Action) { a.Trivial = []string{"[x"} }, "action.trivial[0]"},
		"trivial matches everything":   {func(a *policy.Action) { a.Trivial = []string{"**"} }, "action.trivial[0]"},
		"trivial duplicate":            {func(a *policy.Action) { a.Trivial = []string{"go.sum", "go.sum"} }, "action.trivial[1]"},
		"pinned glob bad":              {func(a *policy.Action) { a.Pinned = []string{"[x"} }, "action.pinned[0]"},
		"pinned duplicate":             {func(a *policy.Action) { a.Pinned = []string{"a/**", "a/**"} }, "action.pinned[1]"},
		"analyzers source unknown":     {func(a *policy.Action) { a.Analyzers.Source = "magic" }, "action.analyzers.source"},
		"analyzers severity unknown":   {func(a *policy.Action) { a.Analyzers.MinSeverity = "huge" }, "action.analyzers.min_severity"},
		"analyzers workflow missing":   {func(a *policy.Action) { a.Analyzers.Workflow = "" }, "action.analyzers.workflow is required"},
		"analyzers workflow escapes":   {func(a *policy.Action) { a.Analyzers.Workflow = "../x.yml" }, "action.analyzers.workflow must be a clean relative path"},
		"analyzers artifact missing":   {func(a *policy.Action) { a.Analyzers.Artifact = "" }, "action.analyzers.artifact is required"},
		"coverage min above 100":       {func(a *policy.Action) { a.DiffCoverage.Min = 101 }, "action.diff_coverage.min must be between 0 and 100"},
		"coverage min negative":        {func(a *policy.Action) { a.DiffCoverage.Min = -1 }, "action.diff_coverage.min must be between 0 and 100"},
		"coverage workflow missing":    {func(a *policy.Action) { a.DiffCoverage.Workflow = "" }, "action.diff_coverage.workflow is required"},
		"coverage workflow not a path": {func(a *policy.Action) { a.DiffCoverage.Workflow = "/etc/x.yml" }, "action.diff_coverage.workflow must be a clean relative path"},
		"coverage artifact missing":    {func(a *policy.Action) { a.DiffCoverage.Artifact = "" }, "action.diff_coverage.artifact is required"},
		"source blank":                 {func(a *policy.Action) { a.Sources = []string{" "} }, "action.sources[0]"},
		"source duplicate":             {func(a *policy.Action) { a.Sources = []string{"x[bot]", "X[bot]"} }, "action.sources[1]"},
		"break glass blank":            {func(a *policy.Action) { a.BreakGlass = []string{""} }, "action.break_glass[0]"},
		"break glass not a login":      {func(a *policy.Action) { a.BreakGlass = []string{"a b"} }, "action.break_glass[0]"},
		"break glass duplicate":        {func(a *policy.Action) { a.BreakGlass = []string{"alice", "ALICE"} }, "action.break_glass[1]"},
	}
	for name, m := range mutations {
		t.Run(name, func(t *testing.T) {
			p := withAction(t)
			m.mutate(&p.Action)
			err := p.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), m.want)
		})
	}
}

// Coverage and analyzer inputs are checked only when they are used: a zero minimum means "not configured".
func TestUnconfiguredInputsNeedNoWorkflow(t *testing.T) {
	p := specPolicy(t)
	p.Action = policy.Action{Mode: policy.ActionModeShadow, DiffCoverage: policy.ActionCoverage{Min: 0}}
	require.NoError(t, p.Validate())
	p.Action.DiffCoverage = policy.ActionCoverage{Min: 70}
	require.ErrorContains(t, p.Validate(), "action.diff_coverage.workflow is required")
}

func TestActionReportsEveryProblemAtOnce(t *testing.T) {
	p := withAction(t)
	p.Action.Mode = "x"
	p.Action.Budget.PerPRUSD = -1
	p.Action.BreakGlass = []string{""}
	err := p.Validate()
	require.Error(t, err)
	for _, want := range []string{"action.mode", "action.budget.per_pr_usd", "action.break_glass[0]"} {
		require.Contains(t, err.Error(), want)
	}
}

func TestDefaultsAreFreshEveryCall(t *testing.T) {
	a := policy.Action{}.Effective()
	a.Trivial[0] = "changed"
	require.Equal(t, "**/*.md", policy.Action{}.Effective().Trivial[0], "no package-level state can be changed by a caller")
}

// Protects is the one rule for Heimdall's own files; the fast check's heimdall_files clause and the proof's trivial
// skip both use it.
func TestActionProtects(t *testing.T) {
	var a policy.Action
	a.Analyzers.Source = policy.AnalyzerSourceSARIFArtifact
	a.Analyzers.Workflow = ".github/workflows/lint.yml"
	a.DiffCoverage.Min = 80
	a.DiffCoverage.Workflow = ".github/workflows/test.yml"
	for path, want := range map[string]bool{
		"heimdall.yaml":                         true,
		"heimdall.yml":                          true,
		".heimdall/memory/rules.md":             true,
		".github/workflows/heimdall-gate.yml":   true,
		".github/workflows/heimdall-proof.yaml": true,
		".github/workflows/lint.yml":            true,
		".github/workflows/test.yml":            true,
		".github/workflows/deploy.yml":          false,
		"docs/heimdall.yaml":                    false,
		"README.md":                             false,
	} {
		require.Equal(t, want, a.Protects(path), path)
	}

	var off policy.Action
	off.Analyzers.Source = policy.AnalyzerSourceCodeScanning
	off.Analyzers.Workflow = ".github/workflows/lint.yml"
	off.DiffCoverage.Workflow = ".github/workflows/test.yml"
	require.False(t, off.Protects(".github/workflows/lint.yml"), "a workflow is protected only while its input is in use")
	require.False(t, off.Protects(".github/workflows/test.yml"), "diff coverage off: its workflow is not an input")
}
