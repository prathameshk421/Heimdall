package fastcheck

import (
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/sarif"
)

func checkSecrets(f Facts) Clause {
	s := f.Secrets
	switch {
	case !s.Known:
		return unknown(ClauseSecrets, "the secret scan did not run over the added lines")
	case len(s.Findings) == 0:
		return met(ClauseSecrets, "no new secret in the added lines")
	}
	places := make([]string, 0, len(s.Findings))
	for _, v := range s.Findings {
		places = append(places, fmt.Sprintf("%s:%d (%s)", v.File, v.Line, v.Rule))
	}
	return unmet(ClauseSecrets, fmt.Sprintf("%s in the added lines: %s", plural(len(s.Findings), "new secret"), strings.Join(places, ", ")))
}

// checkAnalyzers fails on a new result at or above the policy severity. A source that is an artifact of a declared
// workflow is unknown when the change edits that workflow, because the change could hide a result (ACTION_MODE 2.6).
func checkAnalyzers(f Facts) Clause {
	an := f.Action.Analyzers
	if an.Source == policy.AnalyzerSourceSARIFArtifact {
		if c, stop := inputChanged(ClauseAnalyzers, f, an.Workflow); stop {
			return c
		}
	}
	if !f.Analyzers.Known {
		return unknown(ClauseAnalyzers, "the analyzer results for head are not known")
	}
	var hits []string
	for _, r := range sarif.New(f.Analyzers.Base, f.Analyzers.Head) {
		if levelSeverity(r.Level).AtLeast(an.MinSeverity) {
			hits = append(hits, fmt.Sprintf("%s in %s", r.Rule, r.File))
		}
	}
	if len(hits) == 0 {
		return met(ClauseAnalyzers, fmt.Sprintf("no new analyzer result at %s or above", an.MinSeverity))
	}
	return unmet(ClauseAnalyzers, fmt.Sprintf("%s at %s or above: %s", plural(len(hits), "new analyzer result"), an.MinSeverity, strings.Join(hits, ", ")))
}

// levelSeverity maps a SARIF level to a severity. SARIF has three levels and the policy has four severities, so an
// error counts as high, a warning as medium and a note as low. The critical severity is never reached by a level.
func levelSeverity(level string) finding.Severity {
	switch level {
	case "error":
		return finding.SeverityHigh
	case "warning":
		return finding.SeverityMedium
	case "note":
		return finding.SeverityLow
	}
	return ""
}

// inputChanged reports a clause that must stop when the change edits the workflow that produces its input. It returns
// stop false when the clause can go on.
func inputChanged(id ClauseID, f Facts, workflow string) (Clause, bool) {
	if !f.ChangedFilesKnown {
		return unknown(id, "the changed files are not known, so it is not known whether the change edits "+workflow), true
	}
	if !slices.Contains(f.ChangedFiles, workflow) {
		return Clause{}, false
	}
	approved, _ := ownerApproval(f)
	if approved {
		return Clause{}, false
	}
	return unknown(id, fmt.Sprintf("the change edits %s, which produces this input, and no policy owner approved on head", workflow)), true
}

func checkPinnedTests(f Facts) Clause {
	pinned := f.Action.Pinned
	if len(pinned) == 0 {
		return notChecked(ClausePinnedTests, "action.pinned names no test files")
	}
	if !f.Tests.Known {
		return unknown(ClausePinnedTests, "the test files at the merge base and at head are not known")
	}
	var hits []string
	for _, c := range f.Tests.Changes {
		if matchesAny(pinned, c.Path) {
			hits = append(hits, c.Path+" "+c.Kind.words())
		}
	}
	if len(hits) == 0 {
		return met(ClausePinnedTests, "no pinned test file is deleted, renamed, edited or skipped")
	}
	return exempt(ClausePinnedTests, f, "pinned test files changed: "+strings.Join(hits, ", "))
}

func (k TestChangeKind) words() string {
	switch k {
	case TestDeleted:
		return "was deleted"
	case TestRenamed:
		return "was renamed"
	case TestEdited:
		return "was edited"
	case TestSkipAdded:
		return "got a skip marker"
	}
	return "changed"
}

func checkTestIntegrity(f Facts) Clause {
	v := f.Integrity
	switch {
	case !v.Known:
		return unknown(ClauseTestIntegrity, "the test integrity rules did not run")
	case len(v.Violations) == 0:
		return met(ClauseTestIntegrity, "no test integrity rule is broken")
	}
	parts := make([]string, 0, len(v.Violations))
	for _, x := range v.Violations {
		parts = append(parts, fmt.Sprintf("%s in %s", x.Rule, x.Path))
	}
	return unmet(ClauseTestIntegrity, "test integrity: "+strings.Join(parts, ", "))
}

// checkDiffCoverage compares the share of covered changed lines with the policy minimum. A minimum of zero is not
// configured, and the clause is not checked (ACTION_MODE 5.1). The comparison uses integers so the boundary is exact.
func checkDiffCoverage(f Facts) Clause {
	cov := f.Action.DiffCoverage
	if cov.Min <= 0 {
		return notChecked(ClauseDiffCoverage, "action.diff_coverage.min is not set")
	}
	if c, stop := inputChanged(ClauseDiffCoverage, f, cov.Workflow); stop {
		return c
	}
	v := f.Coverage
	switch {
	case !v.Known:
		return unknown(ClauseDiffCoverage, "the coverage report for head is not available")
	case v.ChangedLines < 0 || v.CoveredLines < 0 || v.CoveredLines > v.ChangedLines:
		return unknown(ClauseDiffCoverage, fmt.Sprintf("the coverage numbers are inconsistent: %d of %d changed lines covered", v.CoveredLines, v.ChangedLines))
	case v.ChangedLines == 0:
		return met(ClauseDiffCoverage, "the change has no executable changed lines")
	}
	pct := float64(v.CoveredLines) * 100 / float64(v.ChangedLines)
	if float64(v.CoveredLines)*100 < cov.Min*float64(v.ChangedLines) {
		return unmet(ClauseDiffCoverage, fmt.Sprintf("%.1f%% of changed lines are covered, at least %g%% is needed", pct, cov.Min))
	}
	return met(ClauseDiffCoverage, fmt.Sprintf("%.1f%% of changed lines are covered, at least %g%% is needed", pct, cov.Min))
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
