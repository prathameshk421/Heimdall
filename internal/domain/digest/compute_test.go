package digest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/digest"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// day is the first day of the period the tests use.
func day(n int) time.Time { return time.Date(2026, 11, n, 12, 0, 0, 0, time.UTC) }

// period is a one-week window.
func period() (time.Time, time.Time) { return day(16), day(23) }

// passed is a fast check whose every clause is met, in a shadow period.
func passed(n int) digest.PRRecord {
	return digest.PRRecord{Number: n, At: day(17), Mode: digest.ModeShadow,
		Fast: digest.FastRecord{Ran: true, Clauses: metClauses()}, FastTimeMS: 40_000}
}

// metClauses is a fast check that is all met.
func metClauses() []digest.Clause {
	return []digest.Clause{
		{ID: fastcheck.ClauseSecrets, Status: fastcheck.StatusMet},
		{ID: fastcheck.ClauseAnalyzers, Status: fastcheck.StatusMet},
		{ID: fastcheck.ClausePinnedTests, Status: fastcheck.StatusMet},
		{ID: fastcheck.ClauseTestIntegrity, Status: fastcheck.StatusMet},
		{ID: fastcheck.ClauseDiffCoverage, Status: fastcheck.StatusMet},
		{ID: fastcheck.ClauseApproval, Status: fastcheck.StatusMet},
		{ID: fastcheck.ClauseMemoryRules, Status: fastcheck.StatusMet},
		{ID: fastcheck.ClauseHeimdallFiles, Status: fastcheck.StatusMet},
	}
}

// TestComputeTables is the whole arithmetic of the report: the empty period, the mixed period, the split by mode, and
// the records that are left out. Each case is a full Report, so a change to any total is visible in one place.
func TestComputeTables(t *testing.T) {
	from, to := period()
	cases := []struct {
		name  string
		in    digest.Input
		check func(t *testing.T, r digest.Report)
	}{{
		name: "empty period",
		in:   digest.Input{Repo: "acme/pay", From: from, To: to},
		check: func(t *testing.T, r digest.Report) {
			require.Equal(t, 0, r.PRs)
			require.Empty(t, r.Shadow.Causes)
			require.Equal(t, 0, r.Shadow.Checked, "an empty period checks nothing and says nothing about readiness")
			require.Equal(t, 0, r.Shadow.PassBasisPoints, "no pull request checked is not a pass rate")
			require.Empty(t, r.Misses.UnknownReasons)
			require.Equal(t, digest.Latency{}, r.Misses.Fast)
			require.Equal(t, digest.CostStats{}, r.Shadow.Cost)
		},
	}, {
		name: "all pass, one cost",
		in: digest.Input{Repo: "acme/pay", From: from, To: to, PRs: []digest.PRRecord{
			withCost(passed(1), 250_000),
		}},
		check: func(t *testing.T, r digest.Report) {
			require.Equal(t, 1, r.PRs)
			require.Equal(t, 1, r.Shadow.Checked)
			require.Equal(t, 0, r.Shadow.Unchecked)
			require.Equal(t, 1, r.Shadow.WouldPass)
			require.Equal(t, 0, r.Shadow.WouldBlock)
			require.Equal(t, 10_000, r.Shadow.PassBasisPoints)
			require.Empty(t, r.Shadow.Causes)
			require.Equal(t, int64(250_000), r.Shadow.Cost.TotalMicros)
			require.Equal(t, int64(250_000), r.Shadow.Cost.P50Micros)
			require.Equal(t, 1, r.Shadow.Cost.Measured)
		},
	}, {
		name: "would block, one cause each",
		in: digest.Input{Repo: "acme/pay", From: from, To: to, PRs: []digest.PRRecord{
			withClause(passed(1), fastcheck.ClauseSecrets, fastcheck.StatusUnmet),
			withClause(passed(2), fastcheck.ClauseSecrets, fastcheck.StatusUnmet),
			withClause(passed(3), fastcheck.ClauseDiffCoverage, fastcheck.StatusUnknown),
			passed(4),
		}},
		check: func(t *testing.T, r digest.Report) {
			require.Equal(t, 4, r.Shadow.Checked)
			require.Equal(t, 1, r.Shadow.WouldPass)
			require.Equal(t, 3, r.Shadow.WouldBlock)
			require.Equal(t, 1, r.Shadow.WouldBlockUnknownOnly, "only the coverage record is a missing fact alone")
			require.Equal(t, 2_500, r.Shadow.PassBasisPoints)
			require.Equal(t, []digest.Cause{
				{Kind: digest.CauseFastUnmet, ID: string(fastcheck.ClauseSecrets), Count: 2},
				{Kind: digest.CauseFastUnknown, ID: string(fastcheck.ClauseDiffCoverage), Count: 1},
			}, r.Shadow.Causes)
		},
	}, {
		name: "a real block outranks an unknown",
		in: digest.Input{Repo: "acme/pay", From: from, To: to, PRs: []digest.PRRecord{
			withProof(withClause(passed(1), fastcheck.ClauseDiffCoverage, fastcheck.StatusUnknown), proof.KindProven, digest.Counts{}),
			withProof(passed(2), proof.KindBlocked, digest.Counts{In: 2, Proven: 1, Blocking: 1, NoProof: 1}),
			withProof(passed(3), proof.KindUnknown, digest.Counts{}),
		}},
		check: func(t *testing.T, r digest.Report) {
			require.Equal(t, 3, r.Shadow.Checked)
			require.Equal(t, 0, r.Shadow.WouldPass)
			require.Equal(t, 3, r.Shadow.WouldBlock)
			require.Equal(t, 2, r.Shadow.WouldBlockUnknownOnly, "the record with a known blocking finding is not unknown-only")
			require.Equal(t, 1, r.Shadow.RunBlocked)
			require.Equal(t, 1, r.Shadow.RunUnknown)
			require.Equal(t, 1, r.Shadow.Proven)
			require.Equal(t, 1, r.Shadow.Blocking)
			require.Equal(t, 1, r.Shadow.Dropped)
			require.Equal(t, []digest.Cause{
				{Kind: digest.CauseFastUnknown, ID: string(fastcheck.ClauseDiffCoverage), Count: 1},
				{Kind: digest.CauseProofBlocking, Count: 1},
				{Kind: digest.CauseProofUnknown, ID: string(proof.ReasonNoResult), Count: 1},
			}, r.Shadow.Causes)
		},
	}, {
		name: "a record with no result is not checked and is never a pass",
		in: digest.Input{Repo: "acme/pay", From: from, To: to, PRs: []digest.PRRecord{
			{Number: 1, At: day(17)},
			passed(2),
		}},
		check: func(t *testing.T, r digest.Report) {
			require.Equal(t, 1, r.Shadow.Checked)
			require.Equal(t, 1, r.Shadow.Unchecked)
			require.Equal(t, 1, r.Shadow.WouldPass)
			require.Equal(t, 10_000, r.Shadow.PassBasisPoints, "a rate is out of the checked pull requests")
		},
	}, {
		name: "the mode split",
		in: digest.Input{Repo: "acme/pay", From: from, To: to, PRs: []digest.PRRecord{
			passed(1),
			withEnforce(passed(2)),
			withEnforce(passed(3)),
		}},
		check: func(t *testing.T, r digest.Report) {
			require.Equal(t, 1, r.Shadow.Shadow)
			require.Equal(t, 2, r.Shadow.Enforce)
			require.Equal(t, 3, r.Shadow.Checked)
		},
	}, {
		name: "records outside the period and with no time are counted apart",
		in: digest.Input{Repo: "acme/pay", From: from, To: to, PRs: []digest.PRRecord{
			passed(1),
			withAt(passed(2), day(15)),
			withAt(passed(3), day(23)),
			withAt(passed(4), time.Time{}),
		}},
		check: func(t *testing.T, r digest.Report) {
			require.Equal(t, 1, r.PRs)
			require.Equal(t, 2, r.Outside, "To is exclusive, so day 23 is outside")
			require.Equal(t, 1, r.Untimed)
			require.Equal(t, 0, r.Malformed)
		},
	}, {
		name: "malformed records add to nothing",
		in: digest.Input{Repo: "acme/pay", From: from, To: to, PRs: []digest.PRRecord{
			passed(1),
			{Number: 0, At: day(17), CostMicros: 5},
			withCost(passed(3), -1),
			{Number: 4, At: day(17), FastTimeMS: -1},
			withProof(passed(5), proof.KindBlocked, digest.Counts{In: 1, Proven: 1, Blocking: 2}),
		}},
		check: func(t *testing.T, r digest.Report) {
			require.Equal(t, 1, r.PRs)
			require.Equal(t, 4, r.Malformed)
			require.Equal(t, int64(0), r.Shadow.Cost.TotalMicros, "no cost of a bad record is believed")
			require.Equal(t, 0, r.Shadow.RunBlocked)
		},
	}, {
		name: "an open period takes a record with no time",
		in: digest.Input{Repo: "acme/pay", PRs: []digest.PRRecord{
			{Number: 1},
			passed(2),
		}},
		check: func(t *testing.T, r digest.Report) {
			require.Equal(t, 2, r.PRs)
			require.Equal(t, 0, r.Untimed)
			require.Equal(t, 1, r.Shadow.Unchecked)
		},
	}, {
		name: "break-glass is counted over every record",
		in: digest.Input{Repo: "acme/pay", From: from, To: to, PRs: []digest.PRRecord{
			withBreakGlass(passed(1), 1, 0),
			withBreakGlass(passed(2), 0, 2),
		}},
		check: func(t *testing.T, r digest.Report) {
			require.Equal(t, 1, r.Shadow.BreakGlassOpen)
			require.Equal(t, 2, r.Shadow.BreakGlassReviewed)
		},
	}, {
		name: "the misses of the period",
		in: digest.Input{Repo: "acme/pay", From: from, To: to, PRs: []digest.PRRecord{
			withClause(passed(1), fastcheck.ClauseDiffCoverage, fastcheck.StatusNotChecked),
			withReason(withClause(passed(2), fastcheck.ClauseMemoryRules, fastcheck.StatusNotChecked), "no memory cards for this path"),
			withMisses(withProof(passed(3), proof.KindUnknown, digest.Counts{In: 4, Proven: 0, NotChecked: 4}),
				digest.ReasonTally{Reason: proof.ReasonBudget, Count: 2},
				digest.ReasonTally{Reason: proof.ReasonDeadline, Count: 2}),
			withProof(passed(4), proof.KindUnknown, digest.Counts{}),
		}},
		check: func(t *testing.T, r digest.Report) {
			m := r.Misses
			require.Equal(t, 1, countText(m.NotCheckedClauses, string(fastcheck.ClauseDiffCoverage)))
			require.Equal(t, 1, countText(m.NotCheckedClauses, string(fastcheck.ClauseMemoryRules)))
			require.Equal(t, 1, countText(m.NotCheckedReasons, "no memory cards for this path"))
			require.Equal(t, 2, countReason(m.CandidateNotChecked, string(proof.ReasonBudget)))
			require.Equal(t, 2, countReason(m.CandidateNotChecked, string(proof.ReasonDeadline)))
			require.Equal(t, 2, countReason(m.UnknownReasons, string(proof.ReasonNoResult)),
				"a run that names no reason is no result, and two runs named none")
			require.True(t, knownReason(m.UnknownReasons, string(proof.ReasonNoResult)))
		},
	}, {
		name: "dropped candidates are counted per reason",
		in: digest.Input{Repo: "acme/pay", From: from, To: to, PRs: []digest.PRRecord{
			withProof(passed(1), proof.KindProven,
				digest.Counts{In: 5, Proven: 0, Refuted: 2, NoProof: 2, NoLocation: 1}),
		}},
		check: func(t *testing.T, r digest.Report) {
			require.Equal(t, 5, r.Shadow.Dropped)
			require.Equal(t, 2, countReason(r.Misses.DroppedReasons, string(proof.DropRefuted)))
			require.Equal(t, 2, countReason(r.Misses.DroppedReasons, string(proof.DropNoProof)))
			require.Equal(t, 1, countReason(r.Misses.DroppedReasons, string(proof.DropNoLocation)))
			require.Equal(t, 0, countReason(r.Misses.DroppedReasons, string(proof.DropOutdated)))
		},
	}, {
		name: "an unknown status is an unknown cause, never a pass",
		in: digest.Input{Repo: "acme/pay", From: from, To: to, PRs: []digest.PRRecord{
			withClause(passed(1), fastcheck.ClauseSecrets, fastcheck.Status("weird")),
		}},
		check: func(t *testing.T, r digest.Report) {
			require.Equal(t, 0, r.Shadow.WouldPass)
			require.Equal(t, 1, r.Shadow.WouldBlock)
			require.Equal(t, 1, r.Shadow.WouldBlockUnknownOnly)
			require.Equal(t, digest.CauseFastUnknown, r.Shadow.Causes[0].Kind)
		},
	}, {
		name: "an unknown run kind is an unknown cause",
		in: digest.Input{Repo: "acme/pay", From: from, To: to, PRs: []digest.PRRecord{
			withProof(passed(1), proof.Kind("weird"), digest.Counts{}),
		}},
		check: func(t *testing.T, r digest.Report) {
			require.Equal(t, 1, r.Shadow.WouldBlock)
			require.Equal(t, 1, countReason(r.Misses.UnknownReasons, string(proof.ReasonNoResult)))
		},
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := digest.Compute(c.in)
			c.check(t, r)
			require.Equal(t, r, digest.Compute(c.in), "Compute must be deterministic")
			require.Equal(t, len(c.in.PRs), r.PRs+r.Outside+r.Untimed+r.Malformed,
				"every record is counted or left out, never lost")
		})
	}
}

// TestPercentileEdges: the nearest-rank percentile on the shapes that go wrong in a hand-written percentile: nothing,
// one value, two values, and a full twenty.
func TestPercentileEdges(t *testing.T) {
	from, to := period()
	cases := []struct {
		name             string
		times            []int64
		p50, p95, mean   int64
		measured, maxVal int64
	}{{
		name: "no measurement",
	}, {
		name: "one value", times: []int64{100}, p50: 100, p95: 100, mean: 100, measured: 1, maxVal: 100,
	}, {
		name: "two values take the lower p50", times: []int64{100, 300}, p50: 100, p95: 300, mean: 200, measured: 2, maxVal: 300,
	}, {
		name: "three values", times: []int64{300, 100, 200}, p50: 200, p95: 300, mean: 200, measured: 3, maxVal: 300,
	}, {
		name:  "twenty values reach the last at p95",
		times: []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
		p50:   10, p95: 19, mean: 11, measured: 20, maxVal: 20,
	}, {
		name:  "a hundred values",
		times: hundredTimes(), p50: 50, p95: 95, mean: 51, measured: 100, maxVal: 100,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prs := make([]digest.PRRecord, 0, len(c.times))
			for i, v := range c.times {
				rec := passed(i + 1)
				rec.FastTimeMS = v
				prs = append(prs, rec)
			}
			r := digest.Compute(digest.Input{Repo: "acme/pay", From: from, To: to, PRs: prs})
			got := r.Misses.Fast
			require.Equal(t, int(c.measured), got.Measured)
			require.Equal(t, c.p50, got.P50MS)
			require.Equal(t, c.p95, got.P95MS)
			require.Equal(t, c.mean, got.MeanMS)
			require.Equal(t, c.maxVal, got.MaxMS)
		})
	}
}

// TestCostPercentiles checks the money the same way, because a mean rounded down would hide a real per-pull-request
// cost.
func TestCostPercentiles(t *testing.T) {
	from, to := period()
	prs := []digest.PRRecord{
		withCost(passed(1), 300_000),
		withCost(passed(2), 100_000),
		withCost(passed(3), 200_000),
	}
	c := digest.Compute(digest.Input{Repo: "acme/pay", From: from, To: to, PRs: prs}).Shadow.Cost
	require.Equal(t, int64(600_000), c.TotalMicros)
	require.Equal(t, int64(200_000), c.MeanMicros)
	require.Equal(t, int64(200_000), c.P50Micros)
	require.Equal(t, int64(300_000), c.P95Micros)
	require.Equal(t, 3, c.Measured)
}

// TestRoundingNeverHidesSmallValues: a mean of one micro dollar of three is rounded to the nearest, never to zero.
func TestRoundingNeverHidesSmallValues(t *testing.T) {
	c := digest.Compute(digest.Input{PRs: []digest.PRRecord{withCost(passed(1), 1)}}).Shadow.Cost
	require.Equal(t, int64(1), c.MeanMicros)
}

// TestModeOf: an absent mode is shadow, which is the default of an action area; a value this package does not define is
// enforce, so a typo never sounds softer than the check.
func TestModeOf(t *testing.T) {
	require.Equal(t, digest.ModeShadow, digest.ModeOf(""))
	require.Equal(t, digest.ModeShadow, digest.ModeOf("shadow"))
	require.Equal(t, digest.ModeEnforce, digest.ModeOf("enforce"))
	require.Equal(t, digest.ModeEnforce, digest.ModeOf("ShAdow"))
}

// TestUnknownReasonIsShownAsText: a reason another tool wrote is marked unknown and rendered as text, never as one of
// the reasons of package proof.
func TestUnknownReasonIsShownAsText(t *testing.T) {
	r := digest.Compute(digest.Input{PRs: []digest.PRRecord{
		withProof(passed(1), proof.KindUnknown, digest.Counts{}),
	}})
	require.Len(t, r.Misses.UnknownReasons, 1)
	page := digest.RenderMarkdown(r)
	require.Contains(t, page, "no result")
}

// helpers.

func withCost(r digest.PRRecord, micros int64) digest.PRRecord { r.CostMicros = micros; return r }

func withEnforce(r digest.PRRecord) digest.PRRecord { r.Mode = digest.ModeEnforce; return r }

func withAt(r digest.PRRecord, at time.Time) digest.PRRecord { r.At = at; return r }

func withBreakGlass(r digest.PRRecord, open, reviewed int) digest.PRRecord {
	r.BreakGlassOpen, r.BreakGlassReviewed = open, reviewed
	return r
}

// withClause sets one clause of the fast check to a status, and makes the check ran.
func withClause(r digest.PRRecord, id fastcheck.ClauseID, status fastcheck.Status) digest.PRRecord {
	r.Fast.Ran = true
	for i := range r.Fast.Clauses {
		if r.Fast.Clauses[i].ID == id {
			r.Fast.Clauses[i].Status = status
			return r
		}
	}
	r.Fast.Clauses = append(r.Fast.Clauses, digest.Clause{ID: id, Status: status})
	return r
}

// withReason gives the last not-checked clause of a fast check its reason.
func withReason(r digest.PRRecord, reason string) digest.PRRecord {
	for i := len(r.Fast.Clauses) - 1; i >= 0; i-- {
		if r.Fast.Clauses[i].Status == fastcheck.StatusNotChecked {
			r.Fast.Clauses[i].Reason = reason
			return r
		}
	}
	return r
}

// withProof gives the record a finished proof run.
func withProof(r digest.PRRecord, kind proof.Kind, counts digest.Counts) digest.PRRecord {
	r.Proof = digest.ProofRecord{Ran: true, Kind: kind, Counts: counts}
	r.ProofTimeMS = 120_000
	return r
}

// withMisses adds the reasons a run left candidates unchecked.
func withMisses(r digest.PRRecord, tallies ...digest.ReasonTally) digest.PRRecord {
	r.Proof.NotCheckedReasons = append(r.Proof.NotCheckedReasons, tallies...)
	return r
}

func countText(counts []digest.TextCount, text string) int {
	for _, c := range counts {
		if c.Text == text {
			return c.Count
		}
	}
	return 0
}

func countReason(counts []digest.ReasonCount, reason string) int {
	for _, c := range counts {
		if c.Reason == reason {
			return c.Count
		}
	}
	return 0
}

func knownReason(counts []digest.ReasonCount, reason string) bool {
	for _, c := range counts {
		if c.Reason == reason {
			return c.Known
		}
	}
	return false
}

// hundredTimes is 1 to 100, one per pull request.
func hundredTimes() []int64 {
	out := make([]int64, 0, 100)
	for i := 1; i <= 100; i++ {
		out = append(out, int64(i))
	}
	return out
}

// TestListsAreOrderedIndependentlyOfTheInput: two inputs that hold the same records in another order give the same
// report and the same page.
func TestListsAreOrderedIndependentlyOfTheInput(t *testing.T) {
	from, to := period()
	a := []digest.PRRecord{
		withClause(passed(1), fastcheck.ClauseSecrets, fastcheck.StatusUnmet),
		withClause(passed(2), fastcheck.ClauseAnalyzers, fastcheck.StatusUnmet),
		withProof(passed(3), proof.KindUnknown, digest.Counts{}),
	}
	b := []digest.PRRecord{a[2], a[0], a[1]}
	first := digest.Compute(digest.Input{Repo: "acme/pay", From: from, To: to, PRs: a})
	second := digest.Compute(digest.Input{Repo: "acme/pay", From: from, To: to, PRs: b})
	require.Equal(t, first, second)
	require.Equal(t, digest.RenderMarkdown(first), digest.RenderMarkdown(second))
	require.NotContains(t, digest.RenderMarkdown(first), "NaN")
}
