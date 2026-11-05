package policy

import (
	"slices"
	"strings"
	"time"
)

// checkSlop checks the size rule (TECH_SPEC 12.3). A pattern that ignores every file is refused, because it would
// switch the size check off without anyone seeing it in the diff.
func (p Policy) checkSlop(c *checker) {
	if p.Slop.MaxChangedLines <= 0 {
		c.addf("slop.max_changed_lines must be > 0, got %d", p.Slop.MaxChangedLines)
	}
	if p.Slop.MaxChangedLines > maxChangedLines {
		c.addf("slop.max_changed_lines must be <= %d, got %d", maxChangedLines, p.Slop.MaxChangedLines)
	}
	seen := map[string]bool{}
	for i, pattern := range p.Slop.Ignore {
		if err := ValidateGlob(pattern); err != nil {
			c.addf("slop.ignore[%d]: %v", i, err)
		} else if matchesEverything(pattern) {
			c.addf("slop.ignore[%d]: %q would ignore every file", i, pattern)
		}
		if seen[pattern] {
			c.addf("slop.ignore[%d]: %q is listed twice", i, pattern)
		}
		seen[pattern] = true
	}
}

// checkTests checks where the heimdall tests live and which paths are sensitive (TECH_SPEC 12.1).
func (p Policy) checkTests(c *checker) {
	if !cleanRelativePath(p.Tests.Dir) {
		c.addf("tests.dir must be a clean relative path inside the repository, got %q", p.Tests.Dir)
	}
	for i, pattern := range p.Tests.Sensitive {
		if err := ValidateGlob(pattern); err != nil {
			c.addf("tests.sensitive[%d]: %v", i, err)
		}
	}
	if s := p.Tests.MinMutationScore; !(s >= 0 && s <= 1) {
		c.addf("tests.min_mutation_score must be between 0 and 1, got %g", s)
	}
}

// checkRunners checks the models, budgets, skill versions and timeouts the agents run under.
func (p Policy) checkRunners(c *checker) {
	p.checkModels(c)
	p.checkBudgets(c)
	p.checkSkills(c)
	p.checkTimeouts(c)
}

// checkModels checks that chat models carry the OpenRouter provider prefix, which OpenCode splits on (D143).
// The extraction model is only required to be named: the spec writes it without the prefix.
func (p Policy) checkModels(c *checker) {
	m := p.Models
	if m.Provider != "openrouter" {
		c.addf("models.provider must be openrouter, got %q", m.Provider)
	}
	chat := []struct{ key, model string }{
		{"test_author", m.TestAuthor}, {"reviewer", m.Reviewer}, {"resolver", m.Resolver},
	}
	for _, mdl := range chat {
		switch {
		case mdl.model == "":
			c.addf("models.%s must not be empty", mdl.key)
		case !strings.HasPrefix(mdl.model, "openrouter/") || len(mdl.model) == len("openrouter/"):
			c.addf("models.%s must start with openrouter/ and name a model, got %q", mdl.key, mdl.model)
		}
	}
	if m.Extraction == "" {
		c.addf("models.extraction must not be empty")
	}
	if rest, ok := strings.CutPrefix(m.Embedding, "openrouter:"); !ok || rest == "" {
		c.addf("models.embedding must be openrouter:<model>, got %q", m.Embedding)
	}
}

// checkBudgets checks that each role has a positive token budget and that the per-change cap is at least as large
// as every one of them. The spec does not give a sum rule, so none is enforced.
func (p Policy) checkBudgets(c *checker) {
	b := p.Budgets
	roles := []struct {
		key string
		val int
	}{{"test_author", b.TestAuthor}, {"reviewer", b.Reviewer}, {"resolver", b.Resolver}}
	for _, r := range roles {
		if r.val <= 0 {
			c.addf("budgets.%s must be > 0, got %d", r.key, r.val)
		}
	}
	if b.PerChange <= 0 {
		c.addf("budgets.per_change must be > 0, got %d", b.PerChange)
	}
	for _, r := range roles {
		if b.PerChange < r.val {
			c.addf("budgets.per_change must be >= each role budget, but is %d and budgets.%s is %d", b.PerChange, r.key, r.val)
		}
	}
}

func (p Policy) checkSkills(c *checker) {
	for _, s := range []struct{ key, version string }{
		{"test_author", p.Skills.TestAuthor}, {"reviewer", p.Skills.Reviewer}, {"resolver", p.Skills.Resolver},
	} {
		if !reSemver.MatchString(s.version) {
			c.addf("skills.%s must look like 1.0.0, got %q", s.key, s.version)
		}
	}
}

// checkTimeouts checks that every deadline is positive and that they grow: a reminder comes before the
// escalation and the escalation before the rejection (TECH_SPEC 16).
func (p Policy) checkTimeouts(c *checker) {
	t := p.Timeouts
	for _, d := range []struct {
		key string
		val Duration
	}{{"question", t.Question}, {"remind_after", t.RemindAfter}, {"escalate_after", t.EscalateAfter}, {"reject_after", t.RejectAfter}} {
		if d.val <= 0 {
			c.addf("timeouts.%s must be > 0", d.key)
		}
	}
	if t.RemindAfter >= t.EscalateAfter {
		c.addf("timeouts.remind_after must be shorter than timeouts.escalate_after")
	}
	if t.EscalateAfter >= t.RejectAfter {
		c.addf("timeouts.escalate_after must be shorter than timeouts.reject_after")
	}
}

// checkServices checks the CI list, the outside systems and the memory keywords.
func (p Policy) checkServices(c *checker) {
	p.checkCI(c)
	p.checkExternal(c)
	for i, kw := range p.Memory.ChatKeywords {
		if strings.TrimSpace(kw) == "" {
			c.addf("memory.chat_keywords[%d] must not be empty", i)
		}
	}
}

// checkCI checks the checks a merge must wait for. The gate cannot wait for itself, so its own check name is
// refused.
func (p Policy) checkCI(c *checker) {
	seen := map[string]bool{}
	for i, name := range p.CIRequired {
		switch {
		case strings.TrimSpace(name) == "":
			c.addf("ci_required[%d] must not be empty", i)
		case name == "heimdall/gate":
			c.addf("ci_required[%d] must not be heimdall/gate: the gate cannot wait for itself", i)
		case seen[name]:
			c.addf("ci_required[%d]: %q is listed twice", i, name)
		}
		seen[name] = true
	}
}

// checkExternal checks the GitHub, tracker, chat and runtime settings. The spec names one mode for each, so any
// other value is refused rather than guessed at.
func (p Policy) checkExternal(c *checker) {
	if p.GitHub.Mode != "poll" {
		c.addf("github.mode must be poll, got %q", p.GitHub.Mode)
	}
	if time.Duration(p.GitHub.Interval) < 5*time.Second {
		c.addf("github.interval must be >= 5s, got %s", p.GitHub.Interval)
	}
	if !slices.Contains([]string{"jira", "file"}, p.Tracker.Kind) {
		c.addf("tracker.kind must be jira or file, got %q", p.Tracker.Kind)
	}
	if p.Tracker.Mode != "poll" {
		c.addf("tracker.mode must be poll, got %q", p.Tracker.Mode)
	}
	if p.Tracker.Interval <= 0 {
		c.addf("tracker.interval must be > 0, got %s", p.Tracker.Interval)
	}
	if !slices.Contains([]string{"mattermost", "teams"}, p.Chat.Bridge) {
		c.addf("chat.bridge must be mattermost or teams, got %q", p.Chat.Bridge)
	}
}
