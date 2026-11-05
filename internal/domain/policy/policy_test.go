package policy_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

func specPolicy(t *testing.T) policy.Policy {
	t.Helper()
	b, err := os.ReadFile("testdata/policy.golden.yaml")
	require.NoError(t, err)
	var p policy.Policy
	dec := yaml.NewDecoder(bytesReader(b))
	dec.KnownFields(true) // an unknown key in the policy file is a typo, and typos must not pass silently
	require.NoError(t, dec.Decode(&p))
	return p
}

func TestTheSpecExampleDecodesAndValidates(t *testing.T) {
	p := specPolicy(t)
	require.NoError(t, p.Validate())
	require.Equal(t, 1, p.Version)
	require.Equal(t, 400, p.Slop.MaxChangedLines)
	require.Equal(t, finding.SeverityMedium, p.Blocking.MinSeverity)
	require.Equal(t, finding.TierE3, p.Blocking.MinTier)
	require.Equal(t, policy.Duration(15*time.Minute), p.Reviewers.PickTimeout)
	require.Equal(t, policy.Duration(4*time.Hour), p.Timeouts.Question)
	require.Equal(t, "bob-reviewer", p.People["bob"].Agent)
	require.Equal(t, []string{"build", "lint"}, p.CIRequired)
}

func TestThePolicyEncodesToTheGoldenJSONAndBack(t *testing.T) {
	p := specPolicy(t)
	out, err := json.Marshal(p)
	require.NoError(t, err)
	want, err := os.ReadFile("testdata/policy.golden.json")
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(out), "golden JSON: the wire format is frozen")

	var back policy.Policy
	require.NoError(t, json.Unmarshal(want, &back))
	require.True(t, policy.Equal(p, back))
	require.NoError(t, back.Validate())
}

func TestThePolicyReEncodesToYAMLWithTheSameDurations(t *testing.T) {
	out, err := yaml.Marshal(specPolicy(t))
	require.NoError(t, err)
	var back policy.Policy
	require.NoError(t, yaml.Unmarshal(out, &back))
	require.True(t, policy.Equal(specPolicy(t), back))
	require.Contains(t, string(out), "question: 4h")
}

// Each mutation breaks one rule. The policy must be refused and the message must name the key path.
func TestValidateRefusesEachBrokenRuleAndNamesTheKey(t *testing.T) {
	mutations := map[string]struct {
		mutate func(*policy.Policy)
		want   string
	}{
		"version 2":              {func(p *policy.Policy) { p.Version = 2 }, "version must be 1"},
		"no workflow name":       {func(p *policy.Policy) { p.Workflow = " " }, "workflow must not be empty"},
		"approver not in people": {func(p *policy.Policy) { p.Approvers["auth"] = []string{"zed"} }, `approvers.auth: "zed" is not in people`},
		"approver without a github login": {func(p *policy.Policy) {
			p.Approvers["default"] = []string{"dana2"}
			p.People["dana2"] = policy.Person{Chat: "d"}
		}, "people.dana2.github is required"},
		"no default approver group":         {func(p *policy.Policy) { delete(p.Approvers, "default") }, "approvers.default is required"},
		"policy owner not in people":        {func(p *policy.Policy) { p.PolicyOwners = []string{"nobody"} }, `policy_owners: "nobody" is not in people`},
		"no policy owner":                   {func(p *policy.Policy) { p.PolicyOwners = nil }, "policy_owners must name at least one person"},
		"domain maps to unknown group":      {func(p *policy.Policy) { p.Domains["x/**"] = "ghost" }, `domains["x/**"]: "ghost" is not an approver group`},
		"bad glob":                          {func(p *policy.Policy) { p.Domains["internal/[a"] = "auth" }, `domains["internal/[a"]: glob`},
		"required reviewer unknown":         {func(p *policy.Policy) { p.Reviewers.Required["api/**"] = []string{"zed"} }, `reviewers.required["api/**"]: "zed" is not in people`},
		"required reviewer has no agent":    {func(p *policy.Policy) { p.Reviewers.Required["api/**"] = []string{"dana"} }, `reviewers.required["api/**"]: "dana" has no agent`},
		"reviewer count above agents":       {func(p *policy.Policy) { p.Reviewers.Count = 4 }, "reviewers.count must be between 1 and 3"},
		"reviewer pick timeout zero":        {func(p *policy.Policy) { p.Reviewers.PickTimeout = 0 }, "reviewers.pick_timeout must be > 0"},
		"slop zero lines":                   {func(p *policy.Policy) { p.Slop.MaxChangedLines = 0 }, "slop.max_changed_lines must be > 0"},
		"slop too many lines":               {func(p *policy.Policy) { p.Slop.MaxChangedLines = 100001 }, "slop.max_changed_lines must be <= 100000"},
		"slop ignore everything":            {func(p *policy.Policy) { p.Slop.Ignore = []string{"**"} }, "slop.ignore[0]"},
		"slop ignore duplicate":             {func(p *policy.Policy) { p.Slop.Ignore = []string{"go.sum", "go.sum"} }, "slop.ignore[1]"},
		"tests dir absolute":                {func(p *policy.Policy) { p.Tests.Dir = "/etc" }, "tests.dir must be a clean relative path"},
		"tests dir escapes":                 {func(p *policy.Policy) { p.Tests.Dir = "../x" }, "tests.dir must be a clean relative path"},
		"tests dir unclean":                 {func(p *policy.Policy) { p.Tests.Dir = "tests//heimdall/" }, "tests.dir must be a clean relative path"},
		"mutation score above one":          {func(p *policy.Policy) { p.Tests.MinMutationScore = 1.5 }, "tests.min_mutation_score must be between 0 and 1"},
		"sensitive glob bad":                {func(p *policy.Policy) { p.Tests.Sensitive = []string{"[x"} }, "tests.sensitive[0]"},
		"blocking severity empty":           {func(p *policy.Policy) { p.Blocking.MinSeverity = "" }, "blocking.min_severity"},
		"blocking tier E4":                  {func(p *policy.Policy) { p.Blocking.MinTier = finding.TierE4 }, "blocking.min_tier must be E1, E2 or E3"},
		"consensus one reporter":            {func(p *policy.Policy) { p.Consensus.MinHighReporters = 1 }, "consensus.min_high_reporters must be >= 2"},
		"consensus weights disordered":      {func(p *policy.Policy) { p.Consensus.Weight.Start = 0.2 }, "consensus.weight must satisfy 0 < min <= start <= max <= 1"},
		"consensus confidence disagrees":    {func(p *policy.Policy) { p.Consensus.Confidence.Low = 0.9 }, "consensus.confidence.low must be 0.2"},
		"consensus min reporters nonsense":  {func(p *policy.Policy) { p.Consensus.MinReporters = "most" }, "consensus.min_reporters"},
		"consensus block score zero":        {func(p *policy.Policy) { p.Consensus.BlockScore = 0 }, "consensus.block_score must be > 0"},
		"deliberation rounds four":          {func(p *policy.Policy) { p.Deliberation.MaxRounds = 4 }, "deliberation.max_rounds must be between 1 and 3"},
		"human only domain unknown":         {func(p *policy.Policy) { p.Deliberation.HumanOnlyDomains = []string{"fun"} }, `deliberation.human_only_domains[0]: "fun"`},
		"models provider unknown":           {func(p *policy.Policy) { p.Models.Provider = "acme" }, "models.provider"},
		"reviewer model without prefix":     {func(p *policy.Policy) { p.Models.Reviewer = "deepseek/deepseek-v4-flash" }, "models.reviewer must start with openrouter/"},
		"extraction model empty":            {func(p *policy.Policy) { p.Models.Extraction = "" }, "models.extraction must not be empty"},
		"embedding model malformed":         {func(p *policy.Policy) { p.Models.Embedding = "voyage" }, "models.embedding"},
		"budget zero":                       {func(p *policy.Policy) { p.Budgets.Reviewer = 0 }, "budgets.reviewer must be > 0"},
		"per change below a role":           {func(p *policy.Policy) { p.Budgets.PerChange = 1000 }, "budgets.per_change must be >= each role budget"},
		"skill version malformed":           {func(p *policy.Policy) { p.Skills.Resolver = "1.0" }, "skills.resolver"},
		"timeouts disordered":               {func(p *policy.Policy) { p.Timeouts.RemindAfter = policy.Duration(9 * time.Hour) }, "timeouts.remind_after must be shorter than timeouts.escalate_after"},
		"timeouts question missing":         {func(p *policy.Policy) { p.Timeouts.Question = 0 }, "timeouts.question must be > 0"},
		"ci required duplicate":             {func(p *policy.Policy) { p.CIRequired = []string{"build", "build"} }, "ci_required[1]"},
		"ci required names the gate itself": {func(p *policy.Policy) { p.CIRequired = []string{"heimdall/gate"} }, "ci_required[0]"},
		"github mode unknown":               {func(p *policy.Policy) { p.GitHub.Mode = "carrier-pigeon" }, "github.mode"},
		"github interval too short":         {func(p *policy.Policy) { p.GitHub.Interval = policy.Duration(time.Second) }, "github.interval must be >= 5s"},
		"tracker kind unknown":              {func(p *policy.Policy) { p.Tracker.Kind = "trello" }, "tracker.kind"},
		"chat bridge unknown":               {func(p *policy.Policy) { p.Chat.Bridge = "irc" }, "chat.bridge"},
		"agents runtime unknown":            {func(p *policy.Policy) { p.Agents.Runtime = "codex" }, "agents.runtime"},
		"duplicate github login":            {func(p *policy.Policy) { p.People["eli"] = policy.Person{GitHub: "ALICE", Agent: "eli-reviewer"} }, "people.eli.github"},
		"duplicate agent":                   {func(p *policy.Policy) { p.People["eli"] = policy.Person{GitHub: "eli", Agent: "bob-reviewer"} }, "people.eli.agent"},
		"keys openrouter empty":             {func(p *policy.Policy) { p.Keys.OpenRouter = "" }, "keys.openrouter must not be empty"},
		"pack age zero":                     {func(p *policy.Policy) { p.Pack.MaxAgeDays = 0 }, "pack.max_age_days must be > 0"},
		"memory keyword blank":              {func(p *policy.Policy) { p.Memory.ChatKeywords = []string{" "} }, "memory.chat_keywords[0]"},
	}
	for name, m := range mutations {
		t.Run(name, func(t *testing.T) {
			p := freshPolicy(t)
			m.mutate(&p)
			err := p.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), m.want)
		})
	}
}

func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	p := freshPolicy(t)
	p.Version = 9
	p.Slop.MaxChangedLines = 0
	p.Pack.MaxAgeDays = 0
	err := p.Validate()
	require.Error(t, err)
	for _, want := range []string{"version must be 1", "slop.max_changed_lines", "pack.max_age_days"} {
		require.Contains(t, err.Error(), want)
	}
}

func TestZeroPolicyIsInvalid(t *testing.T) {
	require.Error(t, policy.Policy{}.Validate())
}

func TestReviewersNeedNoGitHubLogin(t *testing.T) {
	p := freshPolicy(t)
	p.People["rev"] = policy.Person{Chat: "rev", Agent: "rev-reviewer"}
	p.Reviewers.Required["docs/**"] = []string{"rev"}
	require.NoError(t, p.Validate(), "D158: github is required only for authors and approvers")
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"internal/auth/**", "internal/auth/login.go", true},
		{"internal/auth/**", "internal/auth/deep/er/x.go", true},
		{"internal/auth/**", "internal/authz/x.go", false},
		{"go.sum", "go.sum", true},
		{"go.sum", "sub/go.sum", true}, // no slash: matches the base name at any depth
		{"api/openapi.yaml", "sub/api/openapi.yaml", false},
		{"**/migrations/**", "db/migrations/001.sql", true},
		{"**/migrations/**", "migrations/001.sql", true},
		{"backend/**/auth*", "backend/x/y/auth_test.go", true},
		{"backend/**/auth*", "backend/auth.go", true},
		{"*delete*", "pkg/user_delete.go", true},
		{"*delete*", "pkg/keep.go", false},
	}
	for _, c := range cases {
		require.Equal(t, c.want, policy.MatchGlob(c.pattern, c.path), "%s vs %s", c.pattern, c.path)
	}
}

func TestEqualTreatsEmptyAndMissingAsTheSameAndAnyRealChangeAsDifferent(t *testing.T) {
	a, b := freshPolicy(t), freshPolicy(t)
	require.True(t, policy.Equal(a, b))

	a.Slop.Ignore, b.Slop.Ignore = nil, []string{}
	require.True(t, policy.Equal(a, b), "nil and empty are the same policy")

	changes := map[string]func(*policy.Policy){
		"a number":          func(p *policy.Policy) { p.Slop.MaxChangedLines++ },
		"a map entry":       func(p *policy.Policy) { p.Approvers["default"] = []string{"alice", "bob"} },
		"a new map key":     func(p *policy.Policy) { p.Domains["x/**"] = "auth" },
		"a list order":      func(p *policy.Policy) { p.CIRequired = []string{"lint", "build"} },
		"a duration":        func(p *policy.Policy) { p.Timeouts.Question = policy.Duration(5 * time.Hour) },
		"a bool":            func(p *policy.Policy) { p.Tests.MutationBlocks = true },
		"a person":          func(p *policy.Policy) { p.People["bob"] = policy.Person{GitHub: "bobby"} },
		"an enum":           func(p *policy.Policy) { p.Blocking.MinSeverity = finding.SeverityHigh },
		"a removed owner":   func(p *policy.Policy) { p.PolicyOwners = nil },
		"a nested required": func(p *policy.Policy) { p.Reviewers.Required["api/openapi.yaml"] = []string{"ana"} },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			x, y := freshPolicy(t), freshPolicy(t)
			change(&y)
			require.False(t, policy.Equal(x, y))
			require.False(t, policy.Equal(y, x), "symmetric")
		})
	}
}
