package policy

import (
	"errors"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// Validate returns every problem with the policy, joined, each message naming its key path. An invalid policy
// fails closed (TECH_SPEC 16), so callers refuse to start or to gate on it.
func (p Policy) Validate() error {
	c := &checker{}
	p.checkTop(c)
	p.checkPeople(c)
	p.checkApprovers(c)
	p.checkReviewers(c)
	p.checkSlop(c)
	p.checkTests(c)
	p.checkDecisions(c)
	p.checkRunners(c)
	p.checkServices(c)
	return c.err()
}

// checker collects every problem, so one run reports them all.
type checker struct{ errs []error }

func (c *checker) addf(format string, args ...any) {
	c.errs = append(c.errs, fmt.Errorf(format, args...))
}

func (c *checker) err() error { return errors.Join(c.errs...) }

var reSemver = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// maxChangedLines bounds the slop rule. Above this a change is too large to review as one, and the number
// protects the sandbox from a policy typo that would turn the rule off.
const maxChangedLines = 100000

// builtinDomains are the domains the spec names for human-only review (TECH_SPEC 11.4). A deployment can name
// more by making them approver groups. It is a function so no package-level state can be changed by a caller.
func builtinDomains() []string { return []string{"auth", "money_movement", "data_deletion"} }

func (p Policy) checkTop(c *checker) {
	if p.Version != 1 {
		c.addf("version must be 1, got %d", p.Version)
	}
	if strings.TrimSpace(p.Workflow) == "" {
		c.addf("workflow must not be empty")
	}
	if p.Keys.OpenRouter == "" {
		c.addf("keys.openrouter must not be empty")
	}
	if p.Pack.MaxAgeDays <= 0 {
		c.addf("pack.max_age_days must be > 0, got %d", p.Pack.MaxAgeDays)
	}
	if p.Agents.Runtime != "opencode" {
		c.addf("agents.runtime must be opencode, got %q", p.Agents.Runtime)
	}
}

// checkPeople checks what each person's name must carry. Agent names are unique and follow the Switch rule
// (TECH_SPEC 3.1); GitHub logins are unique without regard to case, as GitHub treats them.
func (p Policy) checkPeople(c *checker) {
	agents := map[string]string{}
	logins := map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(p.People)) {
		person := p.People[name]
		if person.Agent != "" {
			if _, err := id.ParseAgentName(person.Agent); err != nil {
				c.addf("people.%s.agent: %v", name, err)
			} else if other, dup := agents[person.Agent]; dup {
				c.addf("people.%s.agent: %q is already the agent of people.%s", name, person.Agent, other)
			} else {
				agents[person.Agent] = name
			}
		}
		if login := strings.ToLower(person.GitHub); login != "" {
			if other, dup := logins[login]; dup {
				c.addf("people.%s.github: %q is the same login as people.%s", name, person.GitHub, other)
			} else {
				logins[login] = name
			}
		}
	}
}

// checkApprovers checks the approver groups, the policy owners and the domain map. An approver must have a GitHub
// login, because an approval counts only when GitHub records it (invariant 2).
func (p Policy) checkApprovers(c *checker) {
	if _, ok := p.Approvers["default"]; !ok {
		c.addf("approvers.default is required")
	}
	for _, group := range slices.Sorted(maps.Keys(p.Approvers)) {
		members := p.Approvers[group]
		if len(members) == 0 {
			c.addf("approvers.%s must name at least one person", group)
		}
		for _, name := range members {
			person, ok := p.People[name]
			switch {
			case !ok:
				c.addf("approvers.%s: %q is not in people", group, name)
			case person.GitHub == "":
				c.addf("people.%s.github is required: %s approves on GitHub", name, name)
			}
		}
	}
	p.checkPolicyOwners(c)
	for _, pattern := range slices.Sorted(maps.Keys(p.Domains)) {
		if err := ValidateGlob(pattern); err != nil {
			c.addf("domains[%q]: %v", pattern, err)
		}
		if _, ok := p.Approvers[p.Domains[pattern]]; !ok {
			c.addf("domains[%q]: %q is not an approver group", pattern, p.Domains[pattern])
		}
	}
}

func (p Policy) checkPolicyOwners(c *checker) {
	if len(p.PolicyOwners) == 0 {
		c.addf("policy_owners must name at least one person")
	}
	for _, name := range p.PolicyOwners {
		person, ok := p.People[name]
		switch {
		case !ok:
			c.addf("policy_owners: %q is not in people", name)
		case person.GitHub == "":
			c.addf("people.%s.github is required: a policy owner approves on GitHub", name)
		}
	}
}

// checkReviewers bounds the reviewer count to the default of three (TECH_SPEC 6.6) and checks that each
// required reviewer is someone who can be woken.
func (p Policy) checkReviewers(c *checker) {
	if p.Reviewers.Count < 1 || p.Reviewers.Count > 3 {
		c.addf("reviewers.count must be between 1 and 3, got %d", p.Reviewers.Count)
	}
	if p.Reviewers.PickTimeout <= 0 {
		c.addf("reviewers.pick_timeout must be > 0")
	}
	for _, pattern := range slices.Sorted(maps.Keys(p.Reviewers.Required)) {
		if err := ValidateGlob(pattern); err != nil {
			c.addf("reviewers.required[%q]: %v", pattern, err)
		}
		for _, name := range p.Reviewers.Required[pattern] {
			person, ok := p.People[name]
			switch {
			case !ok:
				c.addf("reviewers.required[%q]: %q is not in people", pattern, name)
			case person.Agent == "":
				c.addf("reviewers.required[%q]: %q has no agent", pattern, name)
			}
		}
	}
}

// checkDecisions is for the rules that turn findings into a decision: blocking, consensus and deliberation.
func (p Policy) checkDecisions(c *checker) {
	p.checkBlocking(c)
	p.checkConsensus(c)
	p.checkDeliberation(c)
}

func (p Policy) checkBlocking(c *checker) {
	if _, err := finding.ParseSeverity(string(p.Blocking.MinSeverity)); err != nil {
		c.addf("blocking.min_severity: %v", err)
	}
	if _, err := finding.ParseTier(string(p.Blocking.MinTier)); err != nil || p.Blocking.MinTier == finding.TierE4 {
		c.addf("blocking.min_tier must be E1, E2 or E3, got %q", p.Blocking.MinTier)
	}
}

func (p Policy) checkConsensus(c *checker) {
	cs := p.Consensus
	if cs.MinHighReporters < 2 {
		c.addf("consensus.min_high_reporters must be >= 2, got %d", cs.MinHighReporters)
	}
	if !(cs.BlockScore > 0) {
		c.addf("consensus.block_score must be > 0, got %g", cs.BlockScore)
	}
	if cs.MinReporters != "majority" {
		c.addf("consensus.min_reporters must be majority, got %q", cs.MinReporters)
	}
	checkConfidence(c, cs.Confidence)
	w := cs.Weight
	if !(0 < w.Min && w.Min <= w.Start && w.Start <= w.Max && w.Max <= 1) {
		c.addf("consensus.weight must satisfy 0 < min <= start <= max <= 1")
	}
	if w.MinDecided < 0 {
		c.addf("consensus.weight.min_decided must not be negative, got %d", w.MinDecided)
	}
}

// checkConfidence pins each level to the weight the spec gives it (TECH_SPEC 11.2b), so reviewers' confidence
// means the same thing in every deployment.
func checkConfidence(c *checker, conf Confidence) {
	want := []struct {
		name string
		got  float64
		exp  float64
	}{
		{"high", conf.High, 1.0},
		{"medium", conf.Medium, 0.5},
		{"low", conf.Low, 0.2},
	}
	for _, w := range want {
		if w.got != w.exp {
			c.addf("consensus.confidence.%s must be %g, got %g", w.name, w.exp, w.got)
		}
	}
}

func (p Policy) checkDeliberation(c *checker) {
	if p.Deliberation.MaxRounds < 1 || p.Deliberation.MaxRounds > 3 {
		c.addf("deliberation.max_rounds must be between 1 and 3, got %d", p.Deliberation.MaxRounds)
	}
	known := slices.Concat(builtinDomains(), slices.Collect(maps.Keys(p.Approvers)))
	for i, d := range p.Deliberation.HumanOnlyDomains {
		if !slices.Contains(known, d) {
			c.addf("deliberation.human_only_domains[%d]: %q is not a known domain", i, d)
		}
	}
}

// cleanRelativePath says whether dir is a clean path inside the repository: not absolute, not escaping, and
// spelled the way path.Clean would spell it.
func cleanRelativePath(dir string) bool {
	return dir != "" && !path.IsAbs(dir) && path.Clean(dir) == dir &&
		dir != "." && dir != ".." && !strings.HasPrefix(dir, "../")
}
