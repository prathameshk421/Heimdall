package fastcheck

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
)

// checkMemoryRules asks every rule card that can block and applies to a changed path. Only an active card of a person
// with a scope, a kind that states a rule and a check can block (rulecard.Confirmable, CanReachE3). A card that is run
// and fails is unmet. A card with no result is unknown: the check could not say that the rule holds.
func checkMemoryRules(f Facts) Clause {
	if !f.Memory.Known {
		return unknown(ClauseMemoryRules, "the rule cards at the merge base are not known")
	}
	cards := blockingCards(f.Memory.Cards)
	if len(cards) == 0 {
		return met(ClauseMemoryRules, "no rule card can block this change")
	}
	if !f.ChangedFilesKnown {
		return unknown(ClauseMemoryRules, "the changed files are not known, so it is not known which rule cards apply")
	}
	var failed, missing []string
	asked := 0
	for _, c := range cards {
		if !appliesToChange(c, f.ChangedFiles) {
			continue
		}
		asked++
		holds, ran := f.Memory.Holds[c.Name]
		switch {
		case !ran:
			missing = append(missing, c.Name)
		case !holds:
			failed = append(failed, c.Name)
		}
	}
	return memoryClause(asked, failed, missing)
}

func memoryClause(asked int, failed, missing []string) Clause {
	sort.Strings(failed)
	sort.Strings(missing)
	switch {
	case len(failed) > 0:
		return unmet(ClauseMemoryRules, "rule cards that do not hold: "+strings.Join(failed, ", "))
	case len(missing) > 0:
		return unknown(ClauseMemoryRules, "rule cards that did not run: "+strings.Join(missing, ", "))
	case asked == 0:
		return met(ClauseMemoryRules, "no rule card applies to a changed path")
	}
	return met(ClauseMemoryRules, fmt.Sprintf("%s hold", plural(asked, "rule card")))
}

func blockingCards(cards []rulecard.Card) []rulecard.Card {
	var out []rulecard.Card
	for _, c := range cards {
		if c.CanReachE3() && rulecard.Confirmable(c) == nil {
			out = append(out, c)
		}
	}
	return out
}

func appliesToChange(c rulecard.Card, files []string) bool {
	for _, file := range files {
		if matchesAny(c.AppliesTo, file) {
			return true
		}
	}
	return false
}

// checkHeimdallFiles holds the files that define the check itself: Heimdall's workflows, the action pin that sits in
// them, the policy, the .heimdall directory and the workflow of each declared input. A change to them could turn the
// check off, so only a policy owner on head clears it (ACTION_MODE 5.1).
func checkHeimdallFiles(f Facts) Clause {
	if !f.ChangedFilesKnown {
		return unknown(ClauseHeimdallFiles, "the changed files are not known")
	}
	var hits []string
	declared := declaredWorkflows(f.Action)
	for _, file := range f.ChangedFiles {
		if isHeimdallFile(file) || slices.Contains(declared, file) {
			hits = append(hits, file)
		}
	}
	if len(hits) == 0 {
		return met(ClauseHeimdallFiles, "the change touches no Heimdall workflow, policy or declared input")
	}
	return exempt(ClauseHeimdallFiles, f, "the change edits Heimdall files: "+strings.Join(hits, ", "))
}

func isHeimdallFile(file string) bool {
	return file == "heimdall.yaml" || file == "heimdall.yml" ||
		policy.MatchGlob(".heimdall/**", file) ||
		policy.MatchGlob(".github/workflows/heimdall-*.yml", file) ||
		policy.MatchGlob(".github/workflows/heimdall-*.yaml", file)
}

// declaredWorkflows are the workflows the policy names as the producers of an input in use.
func declaredWorkflows(a policy.Action) []string {
	var out []string
	if a.Analyzers.Source == policy.AnalyzerSourceSARIFArtifact && a.Analyzers.Workflow != "" {
		out = append(out, a.Analyzers.Workflow)
	}
	if a.DiffCoverage.Min > 0 && a.DiffCoverage.Workflow != "" {
		out = append(out, a.DiffCoverage.Workflow)
	}
	return out
}

func matchesAny(globs []string, name string) bool {
	for _, g := range globs {
		if policy.MatchGlob(g, name) {
			return true
		}
	}
	return false
}
