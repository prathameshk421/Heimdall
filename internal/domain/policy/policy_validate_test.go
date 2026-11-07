package policy_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// Each case is a boundary or a rule that policy_test.go does not pin. A case that is accepted must validate,
// and a case that is refused must name the key path it breaks.
func TestValidateBoundariesAndRulesTheSpecTestsDoNotPin(t *testing.T) {
	cases := map[string]struct {
		mutate func(*policy.Policy)
		ok     bool
		want   string
	}{
		"slop exactly at the ceiling passes":             {func(p *policy.Policy) { p.Slop.MaxChangedLines = 100000 }, true, ""},
		"slop one line of one is the smallest allowed":   {func(p *policy.Policy) { p.Slop.MaxChangedLines = 1 }, true, ""},
		"ignore every file with a bare star is refused":  {func(p *policy.Policy) { p.Slop.Ignore = []string{"*"} }, false, "slop.ignore[0]"},
		"ignore every file with a double star suffix":    {func(p *policy.Policy) { p.Slop.Ignore = []string{"**/*"} }, false, "slop.ignore[0]"},
		"ignore only top-level directories is accepted":  {func(p *policy.Policy) { p.Slop.Ignore = []string{"*/*"} }, true, ""},
		"ignore an absolute path is refused":             {func(p *policy.Policy) { p.Slop.Ignore = []string{"/go.sum"} }, false, "slop.ignore[0]"},
		"reviewer count of one passes":                   {func(p *policy.Policy) { p.Reviewers.Count = 1 }, true, ""},
		"reviewer count of zero is refused":              {func(p *policy.Policy) { p.Reviewers.Count = 0 }, false, "reviewers.count"},
		"deliberation rounds of three pass":              {func(p *policy.Policy) { p.Deliberation.MaxRounds = 3 }, true, ""},
		"deliberation rounds of zero are refused":        {func(p *policy.Policy) { p.Deliberation.MaxRounds = 0 }, false, "deliberation.max_rounds"},
		"a remind equal to escalate is refused":          {func(p *policy.Policy) { p.Timeouts.RemindAfter = p.Timeouts.EscalateAfter }, false, "timeouts.remind_after"},
		"escalate equal to reject is refused":            {func(p *policy.Policy) { p.Timeouts.EscalateAfter = p.Timeouts.RejectAfter }, false, "timeouts.escalate_after"},
		"per change below the largest role is refused":   {func(p *policy.Policy) { p.Budgets.PerChange = p.Budgets.Reviewer }, false, "budgets.per_change"},
		"per change equal to the largest role passes":    {func(p *policy.Policy) { p.Budgets.PerChange = 400000 }, true, ""},
		"github interval of exactly five seconds passes": {func(p *policy.Policy) { p.GitHub.Interval = policy.Duration(5 * time.Second) }, true, ""},
		"github interval of four seconds is refused":     {func(p *policy.Policy) { p.GitHub.Interval = policy.Duration(4 * time.Second) }, false, "github.interval"},
		"github mode webhook is refused":                 {func(p *policy.Policy) { p.GitHub.Mode = "webhook" }, false, "github.mode"},
		"teams bridge is accepted":                       {func(p *policy.Policy) { p.Chat.Bridge = "teams" }, true, ""},
		"switch bridge is accepted":                      {func(p *policy.Policy) { p.Chat.Bridge = "switch" }, true, ""},
		"file tracker is accepted":                       {func(p *policy.Policy) { p.Tracker.Kind = "file" }, true, ""},
		"tracker with a zero interval is refused":        {func(p *policy.Policy) { p.Tracker.Interval = 0 }, false, "tracker.interval"},
		"agent name with a space is refused":             {func(p *policy.Policy) { p.People["bob"] = policy.Person{GitHub: "bob", Agent: "Bob Reviewer"} }, false, "people.bob.agent"},
		"policy owner with no github is refused":         {func(p *policy.Policy) { p.People["ana"] = policy.Person{Chat: "ana"} }, false, "people.ana.github"},
		"empty approver group is refused":                {func(p *policy.Policy) { p.Approvers["auth"] = nil }, false, "approvers.auth must name at least one person"},
		"human only domain that is an approver group":    {func(p *policy.Policy) { p.Deliberation.HumanOnlyDomains = []string{"default"} }, true, ""},
		"reviewer model with bare prefix is refused":     {func(p *policy.Policy) { p.Models.Resolver = "openrouter/" }, false, "models.resolver"},
		"confidence weight of high must stay one":        {func(p *policy.Policy) { p.Consensus.Confidence.High = 0.9 }, false, "consensus.confidence.high"},
		"min reporters other than majority is refused":   {func(p *policy.Policy) { p.Consensus.MinReporters = "all" }, false, "consensus.min_reporters"},
		"weight start of exactly the max passes":         {func(p *policy.Policy) { p.Consensus.Weight.Start = 1.0; p.Consensus.Weight.Max = 1.0 }, true, ""},
		"negative min decided is refused":                {func(p *policy.Policy) { p.Consensus.Weight.MinDecided = -1 }, false, "consensus.weight.min_decided"},
		"ci entry that is only spaces is refused":        {func(p *policy.Policy) { p.CIRequired = []string{" "} }, false, "ci_required[0]"},
		"memory keyword that is only spaces is refused":  {func(p *policy.Policy) { p.Memory.ChatKeywords = []string{"  "} }, false, "memory.chat_keywords[0]"},
		"tests dir that is the whole repository":         {func(p *policy.Policy) { p.Tests.Dir = "." }, false, "tests.dir"},
		"mutation score of exactly zero passes":          {func(p *policy.Policy) { p.Tests.MinMutationScore = 0 }, true, ""},
		"embedding model with no provider name":          {func(p *policy.Policy) { p.Models.Embedding = "openrouter:" }, false, "models.embedding"},
		"skill with a leading v is refused":              {func(p *policy.Policy) { p.Skills.Reviewer = "v1.0.0" }, false, "skills.reviewer"},
		"runtime other than opencode is refused":         {func(p *policy.Policy) { p.Agents.Runtime = "claude" }, false, "agents.runtime"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p := freshPolicy(t)
			c.mutate(&p)
			err := p.Validate()
			if c.ok {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), c.want)
		})
	}
}

func TestValidateGlobRefusesWhatPathMatchWouldRefuse(t *testing.T) {
	for _, bad := range []string{"", "/abs", "a//b", "a/./b", "a/../b", "[a", "x[", "a/[b"} {
		require.Error(t, policy.ValidateGlob(bad), bad)
	}
	for _, good := range []string{"go.sum", "internal/auth/**", "**/migrations/**", "*delete*", "a/[ab]/c", "**"} {
		require.NoError(t, policy.ValidateGlob(good), good)
	}
}

func TestMatchGlobRespectsTheAnchorAndTheDoubleStar(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"**", "any/where/file.go", true},
		{"*", "any/where/file.go", true},
		{"*/*", "top/file.go", true},
		{"*/*", "top/deep/file.go", false},
		{"**/*.go", "a/b/c.go", true},
		{"**/*.go", "c.go", true},
		{"internal/**/x.go", "internal/x.go", true},
		{"internal/**/x.go", "other/internal/x.go", false},
	}
	for _, c := range cases {
		require.Equal(t, c.want, policy.MatchGlob(c.pattern, c.path), "%s vs %s", c.pattern, c.path)
	}
}

func TestDurationWritesTheShortestExactForm(t *testing.T) {
	cases := map[time.Duration]string{
		0:                       "0s",
		4 * time.Hour:           "4h",
		150 * time.Minute:       "150m",
		10 * time.Second:        "10s",
		500 * time.Millisecond:  "500ms",
		1500 * time.Microsecond: "1.5ms",
	}
	for in, want := range cases {
		require.Equal(t, want, policy.Duration(in).String(), "%s", in)
	}
}

func TestDurationRefusesNegativeAndBareNumbersInYAML(t *testing.T) {
	var v struct {
		D policy.Duration `yaml:"d"`
	}
	require.Error(t, yaml.Unmarshal([]byte("d: -5m\n"), &v))
	require.Error(t, yaml.Unmarshal([]byte("d: 15\n"), &v))
	require.NoError(t, yaml.Unmarshal([]byte("d: 0s\n"), &v))
	require.Equal(t, policy.Duration(0), v.D)
}

func TestTheDecoderRefusesAKeyThatHasNoField(t *testing.T) {
	var p policy.Policy
	dec := yaml.NewDecoder(strings.NewReader("version: 1\nslopp: {max_changed_lines: 1}\n"))
	dec.KnownFields(true)
	err := dec.Decode(&p)
	require.Error(t, err, "a typo in the policy file must not pass silently")
	require.Contains(t, err.Error(), "slopp")
}

func TestJSONRefusesAnUnknownDurationEvenInsideAnObject(t *testing.T) {
	var p policy.Policy
	require.Error(t, json.Unmarshal([]byte(`{"timeouts":{"question":"4h","remind_after":"soon"}}`), &p))
}

func TestEqualCountsANilListAsTheSameAsAnEmptyOne(t *testing.T) {
	a, b := freshPolicy(t), freshPolicy(t)
	a.Tests.Sensitive, b.Tests.Sensitive = nil, []string{}
	a.Memory.Rooms, b.Memory.Rooms = []string{}, nil
	require.True(t, policy.Equal(a, b))
}

func TestEqualSeesAChangedSeverityOrTierInsideTheBlockingRule(t *testing.T) {
	a, b := freshPolicy(t), freshPolicy(t)
	require.Equal(t, finding.TierE3, a.Blocking.MinTier)
	b.Blocking.MinTier = finding.TierE2
	require.False(t, policy.Equal(a, b))
}
