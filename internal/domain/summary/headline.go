package summary

import (
	"slices"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// stance is how a blocking result is worded. Shadow mode never blocks, so it says "would block". In enforce mode a
// result blocks only when the proof check is required: otherwise the proof is advisory (ACTION_MODE 5.4).
type stance int

const (
	stanceBlocks stance = iota
	stanceAdvisory
	stanceWould
)

// modeOf is the mode as it is printed. Anything that is not shadow reads as enforce, as package proof reads it, so a
// typo in the mode never makes the summary sound softer than the check.
func modeOf(a policy.Action) string {
	if a.Effective().Mode == policy.ActionModeShadow {
		return policy.ActionModeShadow
	}
	return policy.ActionModeEnforce
}

// proofStance is how a proven blocking finding is worded under the policy.
func proofStance(a policy.Action) stance {
	a = a.Effective()
	if modeOf(a) == policy.ActionModeShadow {
		return stanceWould
	}
	switch (proof.Outcome{Kind: proof.KindBlocked}).Conclusion(a) {
	case proof.ConclusionFailure, proof.ConclusionActionRequired:
		return stanceBlocks
	case proof.ConclusionSuccess, proof.ConclusionNeutral:
		return stanceAdvisory
	}
	return stanceBlocks
}

// fastState is what the fast check says as a whole.
type fastState int

const (
	fastPass fastState = iota
	fastUnknown
	fastFail
)

// assessFast reads the fast check result the strict way. A clause that is unmet makes it fail, a clause that is
// unknown makes it unknown, and the verdict of the result can only make it worse. A result with no clause means the
// check did not report, and that is unknown: silence never helps a change.
func assessFast(r fastcheck.Result) (fastState, []fastcheck.Clause) {
	clauses := slices.Clone(r.Clauses)
	order := []fastcheck.ClauseID{
		fastcheck.ClauseSecrets, fastcheck.ClauseAnalyzers, fastcheck.ClausePinnedTests, fastcheck.ClauseTestIntegrity,
		fastcheck.ClauseDiffCoverage, fastcheck.ClauseApproval, fastcheck.ClauseMemoryRules, fastcheck.ClauseHeimdallFiles,
	}
	slices.SortStableFunc(clauses, func(a, b fastcheck.Clause) int {
		rank := func(c fastcheck.Clause) int {
			if i := slices.Index(order, c.ID); i >= 0 {
				return i
			}
			return len(order)
		}
		return rank(a) - rank(b)
	})
	state := fastPass
	for _, c := range clauses {
		switch c.Status {
		case fastcheck.StatusUnmet:
			state = max(state, fastFail)
		case fastcheck.StatusMet, fastcheck.StatusNotChecked:
			// Neither helps nor blocks.
		case fastcheck.StatusUnknown:
			fallthrough
		default:
			// Unknown, and any status nobody defined: a missing fact is never read as met.
			state = max(state, fastUnknown)
		}
	}
	switch {
	case len(clauses) == 0:
		state = max(state, fastUnknown)
	case r.Verdict == gate.VerdictFail:
		state = max(state, fastFail)
	case r.Verdict != gate.VerdictPass:
		state = max(state, fastUnknown)
	}
	return state, clauses
}

// clausesWith returns the clauses with one status, in order.
func clausesWith(clauses []fastcheck.Clause, s fastcheck.Status) []fastcheck.Clause {
	var out []fastcheck.Clause
	for _, c := range clauses {
		if c.Status == s {
			out = append(out, c)
		}
	}
	return out
}

// clauseList names up to three clauses, for the headline: "(secrets, approval and 1 more)".
func clauseList(cs []fastcheck.Clause) string {
	if len(cs) == 0 {
		return ""
	}
	var names []string
	for _, c := range cs[:min(3, len(cs))] {
		names = append(names, text(string(c.ID), maxName))
	}
	s := strings.Join(names, ", ")
	if len(cs) > 3 {
		s += " and " + strconv.Itoa(len(cs)-3) + " more"
	}
	return " (" + s + ")"
}

// reasonPhrases says each stop reason as a plain clause. The key set is proof.StopReasons.
func reasonPhrases() map[proof.Reason]string {
	return map[proof.Reason]string{
		proof.ReasonBudget:          "proof run hit its budget",
		proof.ReasonMonthlyCap:      "monthly cost cap reached, proof run did not start",
		proof.ReasonDeadline:        "proof run timed out",
		proof.ReasonSuperseded:      "a newer commit replaced this run",
		proof.ReasonForkNoKey:       "fork pull request, no model key",
		proof.ReasonCannotIsolate:   "runner cannot isolate the pull request code",
		proof.ReasonInputMissing:    "a declared input is missing",
		proof.ReasonWorkflowChanged: "the pull request changes the workflow that makes an input",
		proof.ReasonStateUnverified: "the stored state failed verification",
		proof.ReasonSandbox:         "sandbox setup or dependency fetch failed",
		proof.ReasonNoIntent:        "no pull request text or issue states an intent",
		proof.ReasonPlanRefused:     "the test plan was refused twice",
		proof.ReasonModelFailure:    "a model call failed",
		proof.ReasonNoResult:        "proof run reported no result",
	}
}

func reasonPhrase(r proof.Reason) string {
	if p, ok := reasonPhrases()[r]; ok {
		return p
	}
	return reasonPhrases()[proof.ReasonNoResult]
}

// headline is the verdict in plain words. The order is: a proven blocking finding, a failed fast check, an unknown
// proof, an unknown fast check, proven findings that do not block, a trivial skip, and last "nothing proven".
func (v view) headline() string {
	switch {
	case v.blocking > 0 || v.in.Proof.Kind == proof.KindBlocked:
		return v.blockedHeadline()
	case v.fast == fastFail:
		list := clauseList(clausesWith(v.clauses, fastcheck.StatusUnmet))
		if v.mode == policy.ActionModeShadow {
			return "Would block because the fast check failed" + list
		}
		return "Blocked: fast check failed" + list
	case v.in.Proof.Kind == proof.KindUnknown:
		return "Unknown: " + reasonPhrase(v.in.Proof.Reason)
	case v.in.Proof.Kind == proof.KindNotReproduced:
		return "Unknown: no test showed that the change does what it says"
	case v.fast == fastUnknown:
		list := clauseList(clausesWith(v.clauses, fastcheck.StatusUnknown))
		if v.mode == policy.ActionModeShadow {
			return "Would block because the fast check is unknown" + list
		}
		return "Unknown: the fast check could not decide" + list
	case len(v.proven) > 0:
		return "Proven: " + plural(len(v.proven), "finding", "findings") + ", none blocking"
	case v.in.Proof.Kind == proof.KindSkippedTrivial:
		return "Nothing to prove: trivial change, proof run skipped"
	}
	return "Nothing proven against this change"
}

func (v view) blockedHeadline() string {
	n := plural(v.blocking, "blocking finding", "blocking findings")
	switch {
	case v.stance == stanceWould && v.blocking == 0:
		return "Would block because a finding is proven"
	case v.stance == stanceWould:
		return "Would block because " + plural(v.blocking, "finding is", "findings are") + " proven"
	case v.blocking == 0:
		return "Proven: a blocking finding (not listed here)"
	case v.stance == stanceAdvisory:
		return "Proven: " + n + ", advisory only"
	}
	return "Proven: " + n
}
