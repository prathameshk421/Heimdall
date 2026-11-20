package fastcheck_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/sarif"
)

const (
	codeqlWorkflow = ".github/workflows/codeql.yml"
	testWorkflow   = ".github/workflows/test.yml"
)

// green is a pull request that every clause accepts. Each case below flips or removes one fact from it.
func green() fastcheck.Facts {
	return fastcheck.Facts{
		Action: policy.Action{
			Pinned:       []string{"tests/heimdall/**"},
			Analyzers:    policy.ActionAnalyzers{Source: policy.AnalyzerSourceSARIFArtifact, MinSeverity: finding.SeverityMedium, Workflow: codeqlWorkflow, Artifact: "sarif"},
			DiffCoverage: policy.ActionCoverage{Min: 80, Workflow: testWorkflow, Artifact: "coverage"},
		},
		HeadSHA:      "abc1234",
		AuthorLogin:  "dana",
		PolicyOwners: []string{"ana"},

		ChangedFilesKnown: true,
		ChangedFiles:      []string{"internal/pay/refund.go", "internal/pay/refund_test.go"},

		Secrets:   fastcheck.SecretFacts{Known: true},
		Analyzers: fastcheck.AnalyzerFacts{Known: true},
		Tests:     fastcheck.TestFacts{Known: true},
		Integrity: fastcheck.IntegrityFacts{Known: true},
		Coverage:  fastcheck.CoverageFacts{Known: true, ChangedLines: 100, CoveredLines: 90},

		ApprovalsKnown:             true,
		Approvals:                  []gate.Approval{{Login: "rekdfr", Commit: "abc1234", Approved: true}},
		ApproversForChangedDomains: []string{"rekdfr"},

		Memory: fastcheck.MemoryFacts{
			Known: true,
			Cards: []rulecard.Card{personCard("refund-idempotent", "internal/pay/**")},
			Holds: map[string]bool{"refund-idempotent": true},
		},
	}
}

func personCard(name string, appliesTo ...string) rulecard.Card {
	return rulecard.Card{
		Name: name, AppliesTo: appliesTo, Kind: rulecard.KindInvariant, Check: "is the refund idempotent?",
		Status: rulecard.StatusActive, Scope: rulecard.ScopePerson,
	}
}

func result(level, rule, file, snippet string) sarif.Result {
	return sarif.Result{Check: "codeql", Rule: rule, Level: level, File: file, Lines: [2]int{3, 3}, Finger: rule + ":" + file + ":" + snippet}
}

func statuses(r fastcheck.Result) map[fastcheck.ClauseID]fastcheck.Status {
	out := map[fastcheck.ClauseID]fastcheck.Status{}
	for _, c := range r.Clauses {
		out[c.ID] = c.Status
	}
	return out
}

func clauseOf(t *testing.T, r fastcheck.Result, id fastcheck.ClauseID) fastcheck.Clause {
	t.Helper()
	for _, c := range r.Clauses {
		if c.ID == id {
			return c
		}
	}
	require.Failf(t, "clause missing", "no clause %s", id)
	return fastcheck.Clause{}
}

func TestAllGreenFactsPass(t *testing.T) {
	r := fastcheck.Evaluate(green())
	require.Equal(t, gate.VerdictPass, r.Verdict)
	require.True(t, r.Decision.OK)
	require.Empty(t, r.Decision.Unmet)
	require.Len(t, r.Clauses, 8)
	for _, c := range r.Clauses {
		require.Equal(t, fastcheck.StatusMet, c.Status, "%s: %s", c.ID, c.Reason)
		require.NotEmpty(t, c.Reason, "every clause says why, also when it holds")
	}
	// The clauses come in the order of ACTION_MODE 5.1.
	var ids []fastcheck.ClauseID
	for _, c := range r.Clauses {
		ids = append(ids, c.ID)
	}
	require.Equal(t, []fastcheck.ClauseID{
		fastcheck.ClauseSecrets, fastcheck.ClauseAnalyzers, fastcheck.ClausePinnedTests, fastcheck.ClauseTestIntegrity,
		fastcheck.ClauseDiffCoverage, fastcheck.ClauseApproval, fastcheck.ClauseMemoryRules, fastcheck.ClauseHeimdallFiles,
	}, ids)
}

// ownerApproved adds a policy owner's approval on head.
func ownerApproved(f *fastcheck.Facts) {
	f.Approvals = append(f.Approvals, gate.Approval{Login: "ana", Commit: f.HeadSHA, Approved: true})
}

func TestFlippingOneFactMakesExactlyThatClauseUnmet(t *testing.T) {
	cases := []struct {
		name   string
		flip   func(*fastcheck.Facts)
		clause fastcheck.ClauseID
		reason string
	}{
		{"a new secret", func(f *fastcheck.Facts) {
			f.Secrets.Findings = []fastcheck.SecretFinding{{File: "config/app.env", Line: 4, Rule: "aws-access-key"}}
		}, fastcheck.ClauseSecrets, "config/app.env:4 (aws-access-key)"},
		{"a new analyzer result at the policy severity", func(f *fastcheck.Facts) {
			f.Analyzers.Head = []sarif.Result{result("warning", "go/sql-injection", "internal/pay/refund.go", "q")}
		}, fastcheck.ClauseAnalyzers, "go/sql-injection"},
		{"a pinned test is edited", func(f *fastcheck.Facts) {
			f.Tests.Changes = []fastcheck.TestChange{{Path: "tests/heimdall/refund_test.go", Kind: fastcheck.TestEdited}}
		}, fastcheck.ClausePinnedTests, "tests/heimdall/refund_test.go"},
		{"a pinned test is deleted", func(f *fastcheck.Facts) {
			f.Tests.Changes = []fastcheck.TestChange{{Path: "tests/heimdall/refund_test.go", Kind: fastcheck.TestDeleted}}
		}, fastcheck.ClausePinnedTests, "deleted"},
		{"a skip marker is added to a pinned test", func(f *fastcheck.Facts) {
			f.Tests.Changes = []fastcheck.TestChange{{Path: "tests/heimdall/refund_test.go", Kind: fastcheck.TestSkipAdded}}
		}, fastcheck.ClausePinnedTests, "skip"},
		{"a test integrity violation", func(f *fastcheck.Facts) {
			f.Integrity.Violations = []fastcheck.IntegrityViolation{{Path: "internal/pay/refund_test.go", Rule: "mocks-changed-file"}}
		}, fastcheck.ClauseTestIntegrity, "mocks-changed-file"},
		{"diff coverage under the minimum", func(f *fastcheck.Facts) { f.Coverage.CoveredLines = 79 }, fastcheck.ClauseDiffCoverage, "79.0%"},
		{"approval on an older commit", func(f *fastcheck.Facts) { f.Approvals[0].Commit = "old1234" }, fastcheck.ClauseApproval, "old1234"},
		{"approval by the author", func(f *fastcheck.Facts) {
			f.Approvals = []gate.Approval{{Login: "dana", Commit: "abc1234", Approved: true}}
			f.ApproversForChangedDomains = []string{"dana"}
		}, fastcheck.ClauseApproval, "author"},
		{"approval by someone who does not own the domain", func(f *fastcheck.Facts) { f.ApproversForChangedDomains = []string{"ana"} }, fastcheck.ClauseApproval, "not an approver"},
		{"no approval at all", func(f *fastcheck.Facts) { f.Approvals = nil }, fastcheck.ClauseApproval, "no approval"},
		{"a memory rule does not hold", func(f *fastcheck.Facts) { f.Memory.Holds["refund-idempotent"] = false }, fastcheck.ClauseMemoryRules, "refund-idempotent"},
		{"a Heimdall workflow is changed", func(f *fastcheck.Facts) {
			f.ChangedFiles = append(f.ChangedFiles, ".github/workflows/heimdall-gate.yml")
		}, fastcheck.ClauseHeimdallFiles, "heimdall-gate.yml"},
		{"heimdall.yaml is changed", func(f *fastcheck.Facts) { f.ChangedFiles = append(f.ChangedFiles, "heimdall.yaml") }, fastcheck.ClauseHeimdallFiles, "heimdall.yaml"},
		{"the .heimdall directory is changed", func(f *fastcheck.Facts) {
			f.ChangedFiles = append(f.ChangedFiles, ".heimdall/memory/rule.md")
		}, fastcheck.ClauseHeimdallFiles, ".heimdall/memory/rule.md"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := green()
			c.flip(&f)
			r := fastcheck.Evaluate(f)
			require.Equal(t, gate.VerdictFail, r.Verdict)
			for id, st := range statuses(r) {
				if id == c.clause {
					require.Equal(t, fastcheck.StatusUnmet, st, "%s must be unmet", id)
				} else {
					require.Equal(t, fastcheck.StatusMet, st, "%s must stay met", id)
				}
			}
			require.Contains(t, clauseOf(t, r, c.clause).Reason, c.reason)
			require.Len(t, r.Decision.Unmet, 1)
			require.Equal(t, gate.ClauseID(c.clause), r.Decision.Unmet[0].ID)
			require.False(t, r.Decision.Unmet[0].Unknown)
		})
	}
}

func TestAMissingFactMakesThatClauseUnknown(t *testing.T) {
	cases := []struct {
		name   string
		drop   func(*fastcheck.Facts)
		clause fastcheck.ClauseID
	}{
		{"no secret scan", func(f *fastcheck.Facts) { f.Secrets.Known = false }, fastcheck.ClauseSecrets},
		{"no analyzer results", func(f *fastcheck.Facts) { f.Analyzers.Known = false }, fastcheck.ClauseAnalyzers},
		{"no view of the test files", func(f *fastcheck.Facts) { f.Tests.Known = false }, fastcheck.ClausePinnedTests},
		{"integrity rules did not run", func(f *fastcheck.Facts) { f.Integrity.Known = false }, fastcheck.ClauseTestIntegrity},
		{"no coverage report", func(f *fastcheck.Facts) { f.Coverage.Known = false }, fastcheck.ClauseDiffCoverage},
		{"no reviews", func(f *fastcheck.Facts) { f.ApprovalsKnown = false }, fastcheck.ClauseApproval},
		{"no head commit", func(f *fastcheck.Facts) { f.HeadSHA = "" }, fastcheck.ClauseApproval},
		{"no author", func(f *fastcheck.Facts) { f.AuthorLogin = "" }, fastcheck.ClauseApproval},
		{"memory cards not read", func(f *fastcheck.Facts) { f.Memory.Known = false }, fastcheck.ClauseMemoryRules},
		{"memory rule not run", func(f *fastcheck.Facts) { delete(f.Memory.Holds, "refund-idempotent") }, fastcheck.ClauseMemoryRules},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := green()
			c.drop(&f)
			r := fastcheck.Evaluate(f)
			require.Equal(t, gate.VerdictUnknown, r.Verdict)
			for id, st := range statuses(r) {
				if id == c.clause {
					require.Equal(t, fastcheck.StatusUnknown, st, "%s must be unknown", id)
				} else {
					require.Equal(t, fastcheck.StatusMet, st, "%s must stay met", id)
				}
			}
			require.Len(t, r.Decision.Unmet, 1)
			require.True(t, r.Decision.Unmet[0].Unknown)
			require.NotEmpty(t, r.Decision.Unmet[0].Reason)
		})
	}
}

// Unknown ChangedFiles blocks every clause that reads paths. Silence never helps.
func TestUnknownChangedFilesMakesPathClausesUnknown(t *testing.T) {
	f := green()
	f.ChangedFilesKnown = false
	r := fastcheck.Evaluate(f)
	require.Equal(t, gate.VerdictUnknown, r.Verdict)
	want := map[fastcheck.ClauseID]fastcheck.Status{
		fastcheck.ClauseSecrets: fastcheck.StatusMet, fastcheck.ClauseAnalyzers: fastcheck.StatusUnknown,
		fastcheck.ClausePinnedTests: fastcheck.StatusMet, fastcheck.ClauseTestIntegrity: fastcheck.StatusMet,
		fastcheck.ClauseDiffCoverage: fastcheck.StatusUnknown, fastcheck.ClauseApproval: fastcheck.StatusMet,
		fastcheck.ClauseMemoryRules: fastcheck.StatusUnknown, fastcheck.ClauseHeimdallFiles: fastcheck.StatusUnknown,
	}
	require.Equal(t, want, statuses(r))
}

func TestAKnownFailureBeatsAnUnknown(t *testing.T) {
	f := green()
	f.Secrets.Findings = []fastcheck.SecretFinding{{File: "a.env", Line: 1, Rule: "generic-key"}}
	f.Coverage.Known = false
	r := fastcheck.Evaluate(f)
	require.Equal(t, gate.VerdictFail, r.Verdict)
	require.Equal(t, fastcheck.StatusUnmet, statuses(r)[fastcheck.ClauseSecrets])
	require.Equal(t, fastcheck.StatusUnknown, statuses(r)[fastcheck.ClauseDiffCoverage])
	require.Len(t, r.Decision.Unmet, 2)
}

func TestSecretReasonsNeverCarryTheSecret(t *testing.T) {
	f := green()
	f.Secrets.Findings = []fastcheck.SecretFinding{{File: "a.env", Line: 2, Rule: "aws-access-key"}}
	reason := clauseOf(t, fastcheck.Evaluate(f), fastcheck.ClauseSecrets).Reason
	require.Contains(t, reason, "1 new secret")
	require.NotContains(t, reason, "AKIA")
}

func TestClausesThatAreNotConfiguredAreNotChecked(t *testing.T) {
	f := green()
	f.Action.Pinned = nil
	f.Action.DiffCoverage = policy.ActionCoverage{}
	f.Tests.Known, f.Coverage.Known = false, false // nothing to read, and nothing is needed
	r := fastcheck.Evaluate(f)
	require.Equal(t, gate.VerdictPass, r.Verdict, "not checked never blocks")
	require.Equal(t, fastcheck.StatusNotChecked, statuses(r)[fastcheck.ClausePinnedTests])
	require.Equal(t, fastcheck.StatusNotChecked, statuses(r)[fastcheck.ClauseDiffCoverage])
	require.Empty(t, r.Decision.Unmet)
}

func TestAnalyzerSeverityAndNewness(t *testing.T) {
	old := result("error", "go/xss", "internal/pay/old.go", "x")
	cases := []struct {
		name string
		base []sarif.Result
		head []sarif.Result
		min  finding.Severity
		want fastcheck.Status
	}{
		{"a result that was already at base is not new", []sarif.Result{old}, []sarif.Result{old}, finding.SeverityMedium, fastcheck.StatusMet},
		{"a note is below medium", nil, []sarif.Result{result("note", "style/x", "a.go", "n")}, finding.SeverityMedium, fastcheck.StatusMet},
		{"a note is at low", nil, []sarif.Result{result("note", "style/x", "a.go", "n")}, finding.SeverityLow, fastcheck.StatusUnmet},
		{"a warning is at medium", nil, []sarif.Result{result("warning", "w", "a.go", "n")}, finding.SeverityMedium, fastcheck.StatusUnmet},
		{"a warning is below high", nil, []sarif.Result{result("warning", "w", "a.go", "n")}, finding.SeverityHigh, fastcheck.StatusMet},
		{"an error is at high", nil, []sarif.Result{result("error", "e", "a.go", "n")}, finding.SeverityHigh, fastcheck.StatusUnmet},
		{"an error is below critical", nil, []sarif.Result{result("error", "e", "a.go", "n")}, finding.SeverityCritical, fastcheck.StatusMet},
		{"a new result beside an old one", []sarif.Result{old}, []sarif.Result{old, result("error", "e", "a.go", "n")}, finding.SeverityMedium, fastcheck.StatusUnmet},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := green()
			f.Action.Analyzers.MinSeverity = c.min
			f.Analyzers.Base, f.Analyzers.Head = c.base, c.head
			require.Equal(t, c.want, statuses(fastcheck.Evaluate(f))[fastcheck.ClauseAnalyzers])
		})
	}
}

func TestAnAbsentActionAreaUsesTheDefaults(t *testing.T) {
	f := green()
	f.Action = policy.Action{} // code scanning, medium, no pinned globs, no coverage minimum
	f.Analyzers.Head = []sarif.Result{result("warning", "w", "a.go", "n")}
	r := fastcheck.Evaluate(f)
	require.Equal(t, fastcheck.StatusUnmet, statuses(r)[fastcheck.ClauseAnalyzers])
	require.Equal(t, fastcheck.StatusNotChecked, statuses(r)[fastcheck.ClausePinnedTests])
	require.Equal(t, fastcheck.StatusNotChecked, statuses(r)[fastcheck.ClauseDiffCoverage])
}

func TestDiffCoverageBoundary(t *testing.T) {
	cases := []struct {
		name            string
		changed, cover  int
		want            fastcheck.Status
		reasonContained string
	}{
		{"exactly the minimum", 100, 80, fastcheck.StatusMet, "80.0%"},
		{"just under", 1000, 799, fastcheck.StatusUnmet, "79.9%"},
		{"no executable changed line", 0, 0, fastcheck.StatusMet, "no executable"},
		{"more covered than changed is not believed", 10, 11, fastcheck.StatusUnknown, "inconsistent"},
		{"a negative count is not believed", -1, 0, fastcheck.StatusUnknown, "inconsistent"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := green()
			f.Coverage.ChangedLines, f.Coverage.CoveredLines = c.changed, c.cover
			cl := clauseOf(t, fastcheck.Evaluate(f), fastcheck.ClauseDiffCoverage)
			require.Equal(t, c.want, cl.Status, cl.Reason)
			require.Contains(t, cl.Reason, c.reasonContained)
		})
	}
}

// A pull request that edits the workflow which produces a declared input cannot use that input (ACTION_MODE 2.6, Q10).
func TestAChangedInputWorkflowMakesItsClauseUnknownUntilAPolicyOwnerApproves(t *testing.T) {
	cases := []struct {
		name     string
		workflow string
		clause   fastcheck.ClauseID
	}{
		{"the analyzer workflow", codeqlWorkflow, fastcheck.ClauseAnalyzers},
		{"the coverage workflow", testWorkflow, fastcheck.ClauseDiffCoverage},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := green()
			f.ChangedFiles = append(f.ChangedFiles, c.workflow)
			r := fastcheck.Evaluate(f)
			require.Equal(t, fastcheck.StatusUnknown, statuses(r)[c.clause])
			require.Equal(t, fastcheck.StatusUnmet, statuses(r)[fastcheck.ClauseHeimdallFiles], "a declared input's workflow is a Heimdall file")
			require.Equal(t, gate.VerdictFail, r.Verdict)

			ownerApproved(&f)
			r = fastcheck.Evaluate(f)
			require.Equal(t, gate.VerdictPass, r.Verdict, "a policy owner on head clears both")
		})
	}
}

func TestAPolicyOwnerApprovalMustCountToExempt(t *testing.T) {
	changed := func(f *fastcheck.Facts) {
		f.ChangedFiles = append(f.ChangedFiles, "heimdall.yaml")
	}
	cases := []struct {
		name string
		set  func(*fastcheck.Facts)
		want fastcheck.Status
	}{
		{"owner approved on head", ownerApproved, fastcheck.StatusMet},
		{"owner approved an older commit", func(f *fastcheck.Facts) {
			f.Approvals = append(f.Approvals, gate.Approval{Login: "ana", Commit: "old1234", Approved: true})
		}, fastcheck.StatusUnmet},
		{"owner's review is not an approval", func(f *fastcheck.Facts) {
			f.Approvals = append(f.Approvals, gate.Approval{Login: "ana", Commit: "abc1234"})
		}, fastcheck.StatusUnmet},
		{"the author is an owner and approves their own change", func(f *fastcheck.Facts) {
			f.PolicyOwners = []string{"dana"}
			f.Approvals = append(f.Approvals, gate.Approval{Login: "dana", Commit: "abc1234", Approved: true})
		}, fastcheck.StatusUnmet},
		{"a non-owner approver is not enough", func(*fastcheck.Facts) {}, fastcheck.StatusUnmet},
		{"approvals not known", func(f *fastcheck.Facts) { f.ApprovalsKnown = false }, fastcheck.StatusUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := green()
			changed(&f)
			c.set(&f)
			require.Equal(t, c.want, statuses(fastcheck.Evaluate(f))[fastcheck.ClauseHeimdallFiles])
		})
	}
}

func TestPinnedTestsOnlyCountPinnedPathsAndOwnerApprovalExempts(t *testing.T) {
	f := green()
	f.Tests.Changes = []fastcheck.TestChange{{Path: "internal/pay/refund_test.go", Kind: fastcheck.TestEdited}}
	require.Equal(t, fastcheck.StatusMet, statuses(fastcheck.Evaluate(f))[fastcheck.ClausePinnedTests], "an unpinned test may change")

	f.Tests.Changes = []fastcheck.TestChange{{Path: "tests/heimdall/x_test.go", Kind: fastcheck.TestRenamed}}
	require.Equal(t, fastcheck.StatusUnmet, statuses(fastcheck.Evaluate(f))[fastcheck.ClausePinnedTests])
	ownerApproved(&f)
	require.Equal(t, fastcheck.StatusMet, statuses(fastcheck.Evaluate(f))[fastcheck.ClausePinnedTests])

	f = green()
	f.Tests.Changes = []fastcheck.TestChange{{Path: "tests/heimdall/x_test.go", Kind: fastcheck.TestEdited}}
	f.ApprovalsKnown = false
	require.Equal(t, fastcheck.StatusUnknown, statuses(fastcheck.Evaluate(f))[fastcheck.ClausePinnedTests], "cannot tell whether an owner approved")
}

func TestMemoryRulesOnlyAskCardsThatCanBlockAndApplyToAChangedPath(t *testing.T) {
	other := personCard("auth-rule", "internal/auth/**")
	proposed := personCard("proposed-rule", "internal/pay/**")
	proposed.Status = rulecard.StatusProposed
	lens := personCard("lens-rule", "internal/pay/**")
	lens.Scope = rulecard.ScopeLens
	convention := personCard("style-rule", "internal/pay/**")
	convention.Kind = rulecard.KindConvention
	general := rulecard.Card{Name: "general", Kind: rulecard.KindInvariant, Check: "q", Status: rulecard.StatusActive, Scope: rulecard.ScopePerson}
	nocheck := personCard("nocheck-rule", "internal/pay/**")
	nocheck.Check = ""

	f := green()
	f.Memory = fastcheck.MemoryFacts{Known: true, Cards: []rulecard.Card{other, proposed, lens, convention, general, nocheck}}
	r := fastcheck.Evaluate(f)
	require.Equal(t, fastcheck.StatusMet, statuses(r)[fastcheck.ClauseMemoryRules],
		"no run result is needed for a card that cannot block or does not apply to a changed path")
}

func TestUnmetReasonsAreSortedAndNameEveryRule(t *testing.T) {
	f := green()
	f.Memory.Cards = append(f.Memory.Cards, personCard("a-rule", "internal/pay/**"), personCard("b-rule", "internal/pay/**"))
	f.Memory.Holds["a-rule"], f.Memory.Holds["b-rule"] = false, false
	reason := clauseOf(t, fastcheck.Evaluate(f), fastcheck.ClauseMemoryRules).Reason
	require.Less(t, strings.Index(reason, "a-rule"), strings.Index(reason, "b-rule"))
}

func TestEvaluateDoesNotChangeItsInput(t *testing.T) {
	f := green()
	before := fastcheck.Evaluate(f)
	_ = fastcheck.Evaluate(f)
	require.Equal(t, before, fastcheck.Evaluate(f), "pure: the same facts give the same result")
	require.Equal(t, policy.Action{
		Pinned:       []string{"tests/heimdall/**"},
		Analyzers:    policy.ActionAnalyzers{Source: policy.AnalyzerSourceSARIFArtifact, MinSeverity: finding.SeverityMedium, Workflow: codeqlWorkflow, Artifact: "sarif"},
		DiffCoverage: policy.ActionCoverage{Min: 80, Workflow: testWorkflow, Artifact: "coverage"},
	}, f.Action, "the facts' policy is not filled in place")
}
