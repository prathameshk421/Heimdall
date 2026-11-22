package summary_test

import (
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/summary"
)

const head = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"

func clause(id fastcheck.ClauseID, s fastcheck.Status, reason string) fastcheck.Clause {
	return fastcheck.Clause{ID: id, Status: s, Reason: reason}
}

// greenFast is a fast check where every clause holds or is not configured.
func greenFast() fastcheck.Result {
	return fastcheck.Result{Verdict: gate.VerdictPass, Clauses: []fastcheck.Clause{
		clause(fastcheck.ClauseSecrets, fastcheck.StatusMet, "no new secret in 4 added files"),
		clause(fastcheck.ClauseAnalyzers, fastcheck.StatusMet, "no new result at or above medium"),
		clause(fastcheck.ClausePinnedTests, fastcheck.StatusMet, "no pinned test changed"),
		clause(fastcheck.ClauseTestIntegrity, fastcheck.StatusMet, "no mocked changed file, no weaker assertion"),
		clause(fastcheck.ClauseDiffCoverage, fastcheck.StatusNotChecked, "no action.diff_coverage.min in the policy"),
		clause(fastcheck.ClauseApproval, fastcheck.StatusMet, "approved on the head commit by a non-author"),
		clause(fastcheck.ClauseMemoryRules, fastcheck.StatusMet, "2 rule cards apply, all hold"),
		clause(fastcheck.ClauseHeimdallFiles, fastcheck.StatusMet, "no Heimdall file changed"),
	}}
}

// withClause returns a copy of r with one clause replaced and the verdict set.
func withClause(r fastcheck.Result, c fastcheck.Clause, v gate.Verdict) fastcheck.Result {
	out := fastcheck.Result{Verdict: v, Decision: r.Decision}
	for _, old := range r.Clauses {
		if old.ID == c.ID {
			old = c
		}
		out.Clauses = append(out.Clauses, old)
	}
	return out
}

func enforce(required bool) policy.Action {
	return policy.Action{Mode: policy.ActionModeEnforce, ProofRequired: required}
}

func shadow() policy.Action { return policy.Action{Mode: policy.ActionModeShadow} }

func spend(usd float64, in, out int) proof.Spend {
	return proof.Spend{USD: usd, InputTokens: in, OutputTokens: out}
}

func provenE1(id string, blocking bool) summary.Finding {
	return summary.Finding{
		ID: id, Reporters: []summary.Reviewer{summary.ReviewerHeimdall, summary.ReviewerCodeRabbit},
		Tier: finding.TierE1, State: finding.StateOpen, Severity: finding.SeverityCritical,
		Location: finding.Location{File: "internal/pay/refund.go", Lines: [2]int{42, 44}},
		Claim:    "A refund runs twice when the retry flag is set, so the customer is paid twice.",
		Evidence: summary.Evidence{Kind: summary.EvidenceTest, Name: "TestRefundRunsOnceWhenRetried"},
		Result:   proof.Finding{Class: proof.ClassProven, Blocking: blocking},
	}
}

func dropped(id string, who summary.Reviewer, why proof.DropReason) summary.Finding {
	return summary.Finding{
		ID: id, Reporters: []summary.Reviewer{who}, Tier: finding.TierE4, Severity: finding.SeverityMedium,
		Location: finding.Location{File: "internal/pay/client.go", Lines: [2]int{10, 10}},
		Claim:    "This call may return a nil client on the error path.",
		Result:   proof.Finding{Class: proof.ClassDropped, Dropped: why},
	}
}

func notChecked(id string, who summary.Reviewer, why proof.Reason) summary.Finding {
	return summary.Finding{
		ID: id, Reporters: []summary.Reviewer{who}, Tier: finding.TierE4, Severity: finding.SeverityMedium,
		Location: finding.Location{File: "README.md", Lines: [2]int{3, 3}},
		Claim:    "The install step names a flag that does not exist.",
		Result:   proof.Finding{Class: proof.ClassNotChecked, Reason: why},
	}
}

// goldenCase is one realistic page. The name is the golden file name.
type goldenCase struct {
	name string
	in   summary.Input
}

func goldenCases() []goldenCase {
	base := func() summary.Input {
		return summary.Input{Change: "412", Version: 2, Head: head, Action: enforce(true), Fast: greenFast(),
			Usage: summary.Usage{MonthUSD: 3.1, MonthKnown: true, Wall: 4*time.Minute + 12*time.Second, WallKnown: true}}
	}
	return []goldenCase{
		{"clean_nothing_proven", func() summary.Input {
			in := base()
			in.Proof = proof.Outcome{Kind: proof.KindProven, Cost: spend(0.21, 310_400, 8_100)}
			return in
		}()},
		{"enforce_one_e1_bug", func() summary.Input {
			in := base()
			in.Proof = proof.Outcome{Kind: proof.KindBlocked, Cost: spend(0.24, 412_300, 9_100)}
			in.Findings = []summary.Finding{
				provenE1("F1", true),
				dropped("F2", summary.ReviewerCodeRabbit, proof.DropNoProof),
				dropped("F3", summary.ReviewerCodeRabbit, proof.DropRefuted),
				dropped("F4", summary.ReviewerGreptile, proof.DropOutdated),
			}
			return in
		}()},
		{"shadow_would_block", func() summary.Input {
			in := base()
			in.Action = shadow()
			in.Fast = withClause(greenFast(), clause(fastcheck.ClauseSecrets, fastcheck.StatusUnmet,
				"a token-shaped value in deploy/run.sh line 12"), gate.VerdictFail)
			in.Proof = proof.Outcome{Kind: proof.KindBlocked, Cost: spend(0.31, 502_000, 11_000)}
			in.Findings = []summary.Finding{{
				ID: "F1", Reporters: []summary.Reviewer{summary.ReviewerHeimdall}, Tier: finding.TierE2,
				Severity: finding.SeverityHigh, State: finding.StateOpen,
				Location: finding.Location{File: "internal/web/query.go", Lines: [2]int{88, 88}},
				Claim:    "The query is built from the request without a placeholder, so it can be injected.",
				Evidence: summary.Evidence{Kind: summary.EvidenceTool, Name: "semgrep", Rule: "go.lang.security.sql-injection"},
				Result:   proof.Finding{Class: proof.ClassProven, Blocking: true},
			}}
			return in
		}()},
		{"third_party_noise_only", func() summary.Input {
			in := base()
			in.Proof = proof.Outcome{Kind: proof.KindProven, Cost: spend(0.18, 280_000, 6_000)}
			for i, why := range []proof.DropReason{proof.DropNoProof, proof.DropNoProof, proof.DropNoProof, proof.DropNoProof,
				proof.DropNoProof, proof.DropRefuted, proof.DropRefuted} {
				in.Findings = append(in.Findings, dropped("F"+string(rune('1'+i)), summary.ReviewerCodeRabbit, why))
			}
			in.Findings = append(in.Findings,
				dropped("F8", summary.ReviewerBugbot, proof.DropNoLocation),
				dropped("F9", summary.ReviewerGreptile, proof.DropOutdated))
			return in
		}()},
		{"budget_unknown", func() summary.Input {
			in := base()
			in.Fast = withClause(greenFast(), clause(fastcheck.ClauseApproval, fastcheck.StatusUnknown,
				"the reviews could not be read"), gate.VerdictUnknown)
			in.Proof = proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonBudget, Cost: spend(0.50, 800_000, 14_000)}
			in.Findings = []summary.Finding{
				notChecked("F1", summary.ReviewerCodeRabbit, proof.ReasonBudget),
				notChecked("F2", summary.ReviewerCodeRabbit, proof.ReasonBudget),
				notChecked("F3", summary.ReviewerCopilot, proof.ReasonBudget),
				dropped("F4", summary.ReviewerCopilot, proof.DropNoProof),
			}
			return in
		}()},
		{"trivial_skip_zero_cost", func() summary.Input {
			in := base()
			in.Proof = proof.Outcome{Kind: proof.KindSkippedTrivial}
			in.Usage.Wall = 2 * time.Second
			in.Findings = []summary.Finding{
				notChecked("F1", summary.ReviewerCodeRabbit, proof.ReasonTrivial),
				notChecked("F2", summary.ReviewerCodeRabbit, proof.ReasonTrivial),
			}
			return in
		}()},
		{"proven_no_block_needs_person", func() summary.Input {
			in := base()
			in.Action = enforce(false)
			in.Fast = withClause(greenFast(), clause(fastcheck.ClauseApproval, fastcheck.StatusUnmet,
				"no approval on the head commit"), gate.VerdictFail)
			in.Proof = proof.Outcome{Kind: proof.KindProven, Cost: spend(0.27, 350_000, 7_000)}
			in.Findings = []summary.Finding{{
				ID: "F1", Reporters: []summary.Reviewer{summary.ReviewerHeimdall}, Tier: finding.TierE3,
				Severity: finding.SeverityMedium, State: finding.StateOpen,
				Location: finding.Location{File: "internal/web/handler.go", Lines: [2]int{17, 30}},
				Claim:    "The handler returns the error text to the caller.",
				Evidence: summary.Evidence{Kind: summary.EvidenceRuleCard, Name: "no-internal-errors-in-responses"},
				Result:   proof.Finding{Class: proof.ClassProven},
			}}
			return in
		}()},
		{"break_glass_open", func() summary.Input {
			in := base()
			in.BreakGlass = &summary.BreakGlass{Login: "alice", Reason: "production outage, rollback blocked"}
			return in
		}()},
	}
}
