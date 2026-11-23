package summary_test

import (
	"math"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/summary"
)

// from returns the part of s that starts at marker.
func from(t *testing.T, s, marker string) string {
	t.Helper()
	i := strings.Index(s, marker)
	require.GreaterOrEqual(t, i, 0, "%q is not in the page", marker)
	return s[i:]
}

func headlineOf(t *testing.T, in summary.Input) string {
	t.Helper()
	lines := strings.Split(summary.Render(in), "\n")
	require.True(t, strings.HasPrefix(lines[2], "## "), "the third line is the headline: %q", lines[2])
	return strings.TrimPrefix(lines[2], "## ")
}

func blockedInput(a policy.Action) summary.Input {
	return summary.Input{Head: head, Action: a, Fast: greenFast(), Proof: proof.Outcome{Kind: proof.KindBlocked},
		Findings: []summary.Finding{provenE1("F1", true)}}
}

// TestShadowSaysWouldBlock: shadow mode never says "blocks", in the headline, on a finding or on a clause.
func TestShadowSaysWouldBlock(t *testing.T) {
	t.Parallel()
	in := blockedInput(shadow())
	in.Fast = withClause(greenFast(), clause(fastcheck.ClauseSecrets, fastcheck.StatusUnmet, "a token in run.sh"), gate.VerdictFail)
	in.Fast = withClause(in.Fast, clause(fastcheck.ClauseApproval, fastcheck.StatusUnknown, "reviews unreadable"), gate.VerdictFail)
	out := summary.Render(in)
	require.Contains(t, headlineOf(t, in), "Would block because")
	require.Contains(t, out, "would block the merge")
	require.Contains(t, out, "would block because a token in run.sh")
	require.Contains(t, out, "would block until known: reviews unreadable")
	require.Contains(t, out, "Shadow mode")
	for _, banned := range []string{"blocks the merge", "Blocked:", "blocks because", "blocks until", "Proven: 1 blocking"} {
		require.NotContains(t, out, banned)
	}
}

// TestEnforceSaysBlocks: in enforce mode with the proof required, the same facts say "blocks".
func TestEnforceSaysBlocks(t *testing.T) {
	t.Parallel()
	in := blockedInput(enforce(true))
	require.Equal(t, "Proven: 1 blocking finding · enforce · `a1b2c3d`", headlineOf(t, in))
	require.Contains(t, summary.Render(in), "blocks the merge")
	require.NotContains(t, summary.Render(in), "would block")
}

// TestAdvisoryProofDoesNotSoundLikeABlock: enforce mode with an advisory proof check is neutral, so the page says so.
func TestAdvisoryProofDoesNotSoundLikeABlock(t *testing.T) {
	t.Parallel()
	in := blockedInput(enforce(false))
	require.Equal(t, "Proven: 1 blocking finding, advisory only · enforce · `a1b2c3d`", headlineOf(t, in))
	out := summary.Render(in)
	require.Contains(t, out, "blocking, advisory only")
	require.NotContains(t, out, "blocks the merge")
}

// TestModeIsReadStrictly: a mode that is not shadow reads as enforce, as package proof reads it, and a zero action is
// shadow like policy.Action.Effective.
func TestModeIsReadStrictly(t *testing.T) {
	t.Parallel()
	in := blockedInput(policy.Action{Mode: "SHADOW "})
	require.Contains(t, headlineOf(t, in), "· enforce ·")
	require.Contains(t, summary.Render(in), "blocks the merge")
	in.Action = policy.Action{}
	require.Contains(t, headlineOf(t, in), "· shadow ·")
}

// TestWordsForUnproven: the page says "unproven" and "not shown", and never says a finding is false or dropped.
func TestWordsForUnproven(t *testing.T) {
	t.Parallel()
	for _, c := range goldenCases() {
		out := strings.ToLower(summary.Render(c.in))
		for _, banned := range []string{"false", "dropped", "disproved", "wrong claim", "invalid"} {
			require.NotContains(t, out, banned, c.name)
		}
	}
	in := summary.Input{Head: head, Action: enforce(true), Fast: greenFast(), Proof: proof.Outcome{Kind: proof.KindProven},
		Findings: []summary.Finding{dropped("F1", summary.ReviewerCodeRabbit, proof.DropRefuted)}}
	out := summary.Render(in)
	require.Contains(t, out, "Unproven, not shown: 1 finding from 1 reviewer")
	require.Contains(t, out, "<details>")
	require.Contains(t, out, "- CodeRabbit 1: 1 refuted")
}

// TestHeadlines: the verdict in plain words for each way a run can end, in the order of precedence.
func TestHeadlines(t *testing.T) {
	t.Parallel()
	unmetFast := withClause(greenFast(), clause(fastcheck.ClauseSecrets, fastcheck.StatusUnmet, "x"), gate.VerdictFail)
	unknownFast := withClause(greenFast(), clause(fastcheck.ClauseApproval, fastcheck.StatusUnknown, "x"), gate.VerdictUnknown)
	proven := provenE1("F1", false)
	mk := func(a policy.Action, f fastcheck.Result, k proof.Kind, r proof.Reason, fs ...summary.Finding) summary.Input {
		return summary.Input{Head: head, Action: a, Fast: f, Proof: proof.Outcome{Kind: k, Reason: r}, Findings: fs}
	}
	for name, tc := range map[string]struct {
		in   summary.Input
		want string
	}{
		"clean":                  {mk(enforce(true), greenFast(), proof.KindProven, ""), "Nothing proven against this change"},
		"trivial":                {mk(enforce(true), greenFast(), proof.KindSkippedTrivial, ""), "Nothing to prove: trivial change, proof run skipped"},
		"two blocking":           {mk(enforce(true), greenFast(), proof.KindBlocked, "", provenE1("F1", true), provenE1("F2", true)), "Proven: 2 blocking findings"},
		"blocking beats fast":    {mk(enforce(true), unmetFast, proof.KindBlocked, "", provenE1("F1", true)), "Proven: 1 blocking finding"},
		"proven no block":        {mk(enforce(true), greenFast(), proof.KindProven, "", proven), "Proven: 1 finding, none blocking"},
		"fast failed":            {mk(enforce(true), unmetFast, proof.KindProven, ""), "Blocked: fast check failed (secrets)"},
		"fast beats unknown":     {mk(enforce(true), unmetFast, proof.KindUnknown, proof.ReasonBudget), "Blocked: fast check failed (secrets)"},
		"budget":                 {mk(enforce(true), greenFast(), proof.KindUnknown, proof.ReasonBudget), "Unknown: proof run hit its budget"},
		"timeout":                {mk(enforce(true), greenFast(), proof.KindUnknown, proof.ReasonDeadline), "Unknown: proof run timed out"},
		"not reproduced":         {mk(enforce(true), greenFast(), proof.KindNotReproduced, ""), "Unknown: no test showed that the change does what it says"},
		"fast unknown":           {mk(enforce(true), unknownFast, proof.KindProven, ""), "Unknown: the fast check could not decide (approval)"},
		"shadow fast unknown":    {mk(shadow(), unknownFast, proof.KindProven, ""), "Would block because the fast check is unknown (approval)"},
		"shadow fast failed":     {mk(shadow(), unmetFast, proof.KindProven, ""), "Would block because the fast check failed (secrets)"},
		"shadow blocking":        {mk(shadow(), greenFast(), proof.KindBlocked, "", provenE1("F1", true)), "Would block because 1 finding is proven"},
		"blocked, none listed":   {mk(enforce(true), greenFast(), proof.KindBlocked, ""), "Proven: a blocking finding (not listed here)"},
		"no fast result at all":  {mk(enforce(true), fastcheck.Result{}, proof.KindProven, ""), "Unknown: the fast check could not decide"},
		"undefined reason reads": {mk(enforce(true), greenFast(), proof.KindUnknown, "nonsense"), "Unknown: proof run reported no result"},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want+" · "+strings.ToLower(tc.in.Action.Effective().Mode)+" · `a1b2c3d`", headlineOf(t, tc.in))
		})
	}
}

// TestEveryStopReasonHasAHeadline: no reason falls to the generic wording by accident.
func TestEveryStopReasonHasAHeadline(t *testing.T) {
	t.Parallel()
	for _, r := range proof.StopReasons() {
		h := headlineOf(t, summary.Input{Head: head, Action: enforce(true), Fast: greenFast(),
			Proof: proof.Outcome{Kind: proof.KindUnknown, Reason: r}})
		require.True(t, strings.HasPrefix(h, "Unknown: "), r)
		if r != proof.ReasonNoResult {
			require.NotContains(t, h, "reported no result", r)
		}
	}
}

// TestSilenceIsNeverSafe: a missing fast check, an E4 finding marked proven, and an impossible spend are never shown as
// good news.
func TestSilenceIsNeverSafe(t *testing.T) {
	t.Parallel()
	out := summary.Render(summary.Input{Head: head, Action: enforce(true), Proof: proof.Outcome{Kind: proof.KindProven}})
	require.Contains(t, out, "The fast check did not report")
	require.NotContains(t, out, "Nothing proven against this change")

	e4 := provenE1("F1", true)
	e4.Tier = finding.TierE4
	in := summary.Input{Head: head, Action: enforce(true), Fast: greenFast(), Proof: proof.Outcome{Kind: proof.KindProven},
		Findings: []summary.Finding{e4}}
	out = summary.Render(in)
	require.Contains(t, out, "Nothing is proven.")
	require.Contains(t, out, "no test or tool evidence")
	require.NotContains(t, out, "Proven: 1")

	inconsistent := withClause(greenFast(), clause(fastcheck.ClauseSecrets, fastcheck.StatusUnmet, "x"), gate.VerdictPass)
	require.Contains(t, headlineOf(t, summary.Input{Head: head, Action: enforce(true), Fast: inconsistent,
		Proof: proof.Outcome{Kind: proof.KindProven}}), "Blocked: fast check failed")
	failing := greenFast()
	failing.Verdict = gate.VerdictFail
	require.Contains(t, headlineOf(t, summary.Input{Head: head, Action: enforce(true), Fast: failing,
		Proof: proof.Outcome{Kind: proof.KindProven}}), "Blocked: fast check failed")
	weird := withClause(greenFast(), clause(fastcheck.ClauseSecrets, "mystery", "x"), gate.VerdictPass)
	require.Contains(t, headlineOf(t, summary.Input{Head: head, Action: enforce(true), Fast: weird,
		Proof: proof.Outcome{Kind: proof.KindProven}}), "Unknown:")
}

// TestNotCheckedIsListed: what Heimdall did not look at is on the page: clauses, an unfinished run, candidates.
func TestNotCheckedIsListed(t *testing.T) {
	t.Parallel()
	in := summary.Input{Head: head, Action: enforce(true), Fast: greenFast(),
		Proof: proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonDeadline},
		Findings: []summary.Finding{
			notChecked("F1", summary.ReviewerCopilot, proof.ReasonDeadline),
			notChecked("F2", summary.ReviewerBugbot, proof.ReasonForkNoKey),
			notChecked("F3", summary.ReviewerBugbot, proof.ReasonNotProvable),
		}}
	out := summary.Render(in)
	rest := from(t, out, "### Not checked")
	require.Contains(t, rest, "`diff_coverage`")
	require.Contains(t, rest, "Proof run: did not finish (timeout)")
	require.Contains(t, rest, "1 candidate finding not checked: timeout")
	require.Contains(t, rest, "1 candidate finding not checked: fork without key")
	require.Contains(t, rest, "1 candidate finding not checked: not provable by a test or tool")
	require.Contains(t, rest, "silence is not safety")
}

// TestCostLine: the cost of the run, the month when given, tokens and wall time; $0 for a skip; no number for a bad one.
func TestCostLine(t *testing.T) {
	t.Parallel()
	costOf := func(in summary.Input) string {
		out := summary.Render(in)
		return from(t, out, "Cost: ")
	}
	in := summary.Input{Head: head, Action: enforce(true), Fast: greenFast(),
		Proof: proof.Outcome{Kind: proof.KindProven, Cost: spend(0.2449, 412_300, 9_000)}}
	require.Equal(t, "Cost: $0.24 this run, 412.3k input and 9k output tokens.\n", costOf(in))
	in.Usage = summary.Usage{MonthUSD: 12.5, MonthKnown: true, Wall: 75 * time.Second, WallKnown: true}
	require.Equal(t, "Cost: $0.24 this run, $12.50 this month, 412.3k input and 9k output tokens, 1m 15s.\n", costOf(in))
	in.Proof.Cost = spend(0.001, 950, 0)
	require.Contains(t, costOf(in), "<$0.01 this run, $12.50 this month, 950 input and 0 output tokens")
	in.Proof = proof.Outcome{Kind: proof.KindSkippedTrivial, Cost: spend(9, 9, 9)}
	require.Contains(t, costOf(in), "$0.00 this run, no model call")
	require.NotContains(t, costOf(in), "tokens")
	for _, bad := range []float64{-1, math.NaN(), math.Inf(1)} {
		in.Proof = proof.Outcome{Kind: proof.KindProven, Cost: spend(bad, 1, 1)}
		require.Contains(t, costOf(in), "this run unknown, the measure is not valid")
		require.NotContains(t, costOf(in), "$0.00 this run")
	}
	in.Proof = proof.Outcome{Kind: proof.KindProven, Cost: spend(1, 2_500_000, 1)}
	in.Usage = summary.Usage{MonthUSD: math.NaN(), MonthKnown: true, Wall: 3725 * time.Second, WallKnown: true}
	require.Equal(t, "Cost: $1.00 this run, 2.5M input and 1 output tokens, 1h 02m.\n", costOf(in))
}

// TestOrderDoesNotDependOnInputOrder: the same findings in any order give the same page.
func TestOrderDoesNotDependOnInputOrder(t *testing.T) {
	t.Parallel()
	in := goldenCases()[1].in
	in.Findings = append(in.Findings, provenE1("F5", false), provenE1("F10", true),
		dropped("F11", summary.ReviewerCopilot, proof.DropOutdated), notChecked("F12", summary.ReviewerCodeRabbit, proof.ReasonBudget))
	in.Findings[len(in.Findings)-3].Severity = finding.SeverityLow
	want := summary.Render(in)
	rng := rand.New(rand.NewPCG(1, 2))
	for range 20 {
		shuffled := in
		shuffled.Findings = append([]summary.Finding(nil), in.Findings...)
		rng.Shuffle(len(shuffled.Findings), func(i, j int) {
			shuffled.Findings[i], shuffled.Findings[j] = shuffled.Findings[j], shuffled.Findings[i]
		})
		shuffled.Fast.Clauses = append([]fastcheck.Clause(nil), in.Fast.Clauses...)
		rng.Shuffle(len(shuffled.Fast.Clauses), func(i, j int) {
			shuffled.Fast.Clauses[i], shuffled.Fast.Clauses[j] = shuffled.Fast.Clauses[j], shuffled.Fast.Clauses[i]
		})
		require.Equal(t, want, summary.Render(shuffled))
	}
	proven := want[strings.Index(want, "### Proven findings"):strings.Index(want, "<details>")]
	order := []string{"**F1**", "**F10**", "**F5**"}
	last := -1
	for _, id := range order {
		i := strings.Index(proven, id)
		require.Greater(t, i, last, "blocking first, then by id: %s", id)
		last = i
	}
}

// TestRaisedBy: each reporter is named, once, in a fixed order; a tool of package external maps to its reviewer.
func TestRaisedBy(t *testing.T) {
	t.Parallel()
	f := provenE1("F1", true)
	f.Reporters = []summary.Reviewer{summary.ReviewerOf(external.ToolGreptile), summary.ReviewerBugbot, summary.ReviewerHeimdall,
		summary.ReviewerCopilot, summary.ReviewerCodeRabbit, summary.ReviewerHeimdall}
	out := summary.Render(summary.Input{Head: head, Action: enforce(true), Fast: greenFast(),
		Proof: proof.Outcome{Kind: proof.KindBlocked}, Findings: []summary.Finding{f}})
	require.Contains(t, out, "Raised by Heimdall and CodeRabbit and Copilot and Bugbot and Greptile.")
	f.Reporters = nil
	out = summary.Render(summary.Input{Head: head, Action: enforce(true), Fast: greenFast(),
		Proof: proof.Outcome{Kind: proof.KindBlocked}, Findings: []summary.Finding{f}})
	require.Contains(t, out, "Raised by unknown reviewer.")
}

// TestPlaces: the place of a finding reads as path:line, path:first-last, or just the path.
func TestPlaces(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		loc  finding.Location
		want string
	}{
		"one line": {finding.Location{File: "a/b.go", Lines: [2]int{7, 7}}, "`a/b.go:7`"},
		"range":    {finding.Location{File: "a/b.go", Lines: [2]int{7, 9}}, "`a/b.go:7-9`"},
		"no lines": {finding.Location{File: "a/b.go"}, "`a/b.go`"},
		"reversed": {finding.Location{File: "a/b.go", Lines: [2]int{9, 7}}, "`a/b.go`"},
		"no file":  {finding.Location{Lines: [2]int{1, 1}}, "no location"},
	} {
		f := provenE1("F1", true)
		f.Location = tc.loc
		out := summary.Render(summary.Input{Head: head, Action: enforce(true), Fast: greenFast(),
			Proof: proof.Outcome{Kind: proof.KindBlocked}, Findings: []summary.Finding{f}})
		require.Contains(t, out, "- **F1** "+tc.want+" ", name)
	}
}

// TestMarker: the marker round trips, is strict, and takes only safe values.
func TestMarker(t *testing.T) {
	t.Parallel()
	m := summary.Marker{Change: "412", Version: 3, Head: head}
	line := m.String()
	require.Equal(t, "<!-- heimdall:proof-summary change=412 version=3 head="+head+" -->", line)
	got, ok := summary.ParseMarker(line + "\n\n## body")
	require.True(t, ok)
	require.Equal(t, m, got)
	_, ok = summary.ParseMarker(line + "\r\nrest")
	require.True(t, ok, "a CRLF body still has its marker")

	for name, bad := range map[string]string{
		"empty":         "",
		"no marker":     "## Proven",
		"later line":    "intro\n" + line,
		"leading space": " " + line,
		"extra field":   "<!-- heimdall:proof-summary change=1 version=1 head=" + head + " extra=1 -->",
		"order":         "<!-- heimdall:proof-summary version=1 change=1 head=" + head + " -->",
		"bad version":   "<!-- heimdall:proof-summary change=1 version=-1 head=" + head + " -->",
		"padded":        "<!-- heimdall:proof-summary change=1 version=01 head=" + head + " -->",
		"upper head":    "<!-- heimdall:proof-summary change=1 version=1 head=ABCDEF1 -->",
		"short head":    "<!-- heimdall:proof-summary change=1 version=1 head=abc -->",
		"bad change":    "<!-- heimdall:proof-summary change=a/b version=1 head=" + head + " -->",
		"open":          "<!-- heimdall:proof-summary change=1 version=1 head=" + head,
	} {
		_, ok := summary.ParseMarker(bad)
		require.False(t, ok, name)
	}

	hostile := summary.Marker{Change: "1 --> <script>", Version: -5, Head: "zz"}.String()
	require.Equal(t, "<!-- heimdall:proof-summary change=unknown version=0 head=unknown -->", hostile)
	back, ok := summary.ParseMarker(hostile)
	require.True(t, ok)
	require.Equal(t, summary.Marker{Change: "unknown", Head: "unknown"}, back)
}
