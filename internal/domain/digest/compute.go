package digest

import (
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// place says what Compute does with one record.
type place int

const (
	// placeIn: a sound record inside the period. It is counted.
	placeIn place = iota
	// placeMalformed: a record that cannot be true. It is counted apart and never adds to a total.
	placeMalformed
	// placeUntimed: a record with no time, in a period that has an end. It is counted apart.
	placeUntimed
	// placeOutside: a sound record whose time is outside the period.
	placeOutside
)

// Compute reads the records and returns the report of the period. It never fails: a record that cannot be true is
// counted in Report.Malformed and adds to nothing else, so a broken record never lowers a rate or moves a percentile.
func Compute(in Input) Report {
	r := Report{Repo: in.Repo, From: in.From, To: in.To}
	t := newTally()
	for _, rec := range in.PRs {
		switch where(in, rec) {
		case placeIn:
			r.PRs++
			t.add(rec)
		case placeMalformed:
			r.Malformed++
		case placeUntimed:
			r.Untimed++
		case placeOutside:
			r.Outside++
		}
	}
	t.fill(&r)
	return r
}

// where places one record against the period. A malformed record is placed before the period is read, because its time
// cannot be trusted either. A record with no time is placed only when the period is open at both ends.
func where(in Input, rec PRRecord) place {
	if !rec.valid() {
		return placeMalformed
	}
	if rec.At.IsZero() {
		if in.From.IsZero() && in.To.IsZero() {
			return placeIn
		}
		return placeUntimed
	}
	if !in.From.IsZero() && rec.At.Before(in.From) {
		return placeOutside
	}
	if !in.To.IsZero() && !rec.At.Before(in.To) {
		return placeOutside
	}
	return placeIn
}

// valid reports whether a record can be true. A count, a cost or a time below zero cannot, and neither can more
// clauses than the fast check has, or a tally whose parts do not add up.
func (rec PRRecord) valid() bool {
	if rec.Number < 1 || rec.CostMicros < 0 || rec.FastTimeMS < 0 || rec.ProofTimeMS < 0 {
		return false
	}
	if rec.BreakGlassOpen < 0 || rec.BreakGlassReviewed < 0 || len(rec.Fast.Clauses) > maxClauses {
		return false
	}
	return rec.Proof.Counts.consistent()
}

// consistent reports whether the tally adds up, as change.ProofCounts.Validate requires of a stored tally.
func (c Counts) consistent() bool {
	if c.negative() {
		return false
	}
	return c.In == c.Proven+c.Dropped()+c.NotChecked && c.Proven == c.Blocking+c.NeedsPerson
}

// tally is the running total. It is a local value, never package state.
type tally struct {
	shadow, enforce          int
	checked, unchecked       int
	pass, block, unknownOnly int
	causes                   map[Cause]int
	runs                     int
	blocked, unknown         int
	notReproduced, trivial   int
	proven, needsPerson      int
	blocking, dropped        int
	notChecked               int
	costs                    []int64
	fastMS                   []int64
	proofMS                  []int64
	breakOpen, breakReviewed int
	clauses                  map[string]int
	clauseReasons            map[string]int
	candidateNotChecked      map[string]int
	unknownReasons           map[string]int
	droppedReasons           map[string]int
}

func newTally() *tally {
	return &tally{causes: map[Cause]int{}, clauses: map[string]int{}, clauseReasons: map[string]int{},
		candidateNotChecked: map[string]int{}, unknownReasons: map[string]int{}, droppedReasons: map[string]int{}}
}

// add counts one sound record inside the period.
func (t *tally) add(rec PRRecord) {
	t.mode(rec.Mode)
	t.breakOpen += rec.BreakGlassOpen
	t.breakReviewed += rec.BreakGlassReviewed
	t.fast(rec.Fast)
	t.proof(rec.Proof)
	t.check(rec)
	t.costs = append(t.costs, rec.CostMicros)
}

// mode counts the mode the record was checked under.
func (t *tally) mode(m Mode) {
	if m == ModeEnforce {
		t.enforce++
		return
	}
	t.shadow++
}

// fast counts the clauses and the wall time of one fast check.
func (t *tally) fast(f FastRecord) {
	if !f.Ran {
		return
	}
	for _, c := range f.Clauses {
		t.clause(c)
	}
}

// clause counts one clause. A status this package does not define counts as unknown: a fact nobody defined is not a
// pass.
func (t *tally) clause(c Clause) {
	switch c.Status {
	case fastcheck.StatusNotChecked:
		t.clauses[string(c.ID)]++
		t.clauseReasons[c.Reason]++
	case fastcheck.StatusMet, fastcheck.StatusUnmet, fastcheck.StatusUnknown:
		// Met, unmet and unknown are counted by causesOf, which reads every record once.
	default:
		// A status nobody defined is an unknown cause, never a pass, and causesOf reads it the same way.
	}
}

// proof counts one proof run and the candidates it left.
func (t *tally) proof(p ProofRecord) {
	if !p.Ran {
		return
	}
	t.runs++
	c := p.Counts
	t.proven += c.Proven
	t.needsPerson += c.NeedsPerson
	t.blocking += c.Blocking
	t.dropped += c.Dropped()
	t.notChecked += c.NotChecked
	t.droppedReasons[string(proof.DropRefuted)] += c.Refuted
	t.droppedReasons[string(proof.DropNoProof)] += c.NoProof
	t.droppedReasons[string(proof.DropOutdated)] += c.Outdated
	t.droppedReasons[string(proof.DropNoLocation)] += c.NoLocation
	for _, r := range p.NotCheckedReasons {
		t.candidateNotChecked[string(r.Reason)] += r.Count
	}
	switch p.Kind {
	case proof.KindBlocked:
		t.blocked++
	case proof.KindNotReproduced:
		t.notReproduced++
	case proof.KindSkippedTrivial:
		t.trivial++
	case proof.KindProven:
	case proof.KindUnknown:
		t.unknown++
		t.unknownReasons[string(reasonOf(p))]++
	default:
		// A kind nobody defined: a run whose result this package cannot read is not known, never a pass.
		t.unknown++
		t.unknownReasons[string(reasonOf(p))]++
	}
}

// reasonOf returns the reason of an unknown run. A run that names none is "no_result": it reported nothing usable.
func reasonOf(p ProofRecord) proof.Reason {
	if p.Reason.Valid() {
		return p.Reason
	}
	return proof.ReasonNoResult
}

// check places one record on the pass or block side, and measures its time.
func (t *tally) check(rec PRRecord) {
	causes := causesOf(rec)
	if !rec.Fast.Ran && !rec.Proof.Ran {
		t.unchecked++
		return
	}
	t.checked++
	if rec.FastTimeMS > 0 && rec.Fast.Ran {
		t.fastMS = append(t.fastMS, rec.FastTimeMS)
	}
	if rec.ProofTimeMS > 0 && rec.Proof.Ran {
		t.proofMS = append(t.proofMS, rec.ProofTimeMS)
	}
	if len(causes) == 0 {
		t.pass++
		return
	}
	t.block++
	for _, c := range causes {
		t.causes[c]++
	}
	if unknownOnly(causes) {
		t.unknownOnly++
	}
}

// causesOf lists why a check would not pass at this head. A clause or a run that nobody defined counts as an unknown
// cause, never as a pass.
func causesOf(rec PRRecord) []Cause {
	var out []Cause
	for _, c := range rec.Fast.Clauses {
		switch c.Status {
		case fastcheck.StatusMet, fastcheck.StatusNotChecked:
		case fastcheck.StatusUnmet:
			out = append(out, Cause{Kind: CauseFastUnmet, ID: string(c.ID)})
		case fastcheck.StatusUnknown:
			out = append(out, Cause{Kind: CauseFastUnknown, ID: string(c.ID)})
		default:
			// A status nobody defined is the same as unknown: a fact nobody defined is not a pass.
			out = append(out, Cause{Kind: CauseFastUnknown, ID: string(c.ID)})
		}
	}
	if !rec.Proof.Ran {
		return out
	}
	switch rec.Proof.Kind {
	case proof.KindBlocked:
		out = append(out, Cause{Kind: CauseProofBlocking})
	case proof.KindUnknown:
		out = append(out, Cause{Kind: CauseProofUnknown, ID: string(reasonOf(rec.Proof))})
	case proof.KindProven, proof.KindNotReproduced, proof.KindSkippedTrivial:
	case "":
		out = append(out, Cause{Kind: CauseProofUnknown, ID: string(proof.ReasonNoResult)})
	default:
		out = append(out, Cause{Kind: CauseProofUnknown, ID: string(proof.ReasonNoResult)})
	}
	return out
}

// unknownOnly reports whether every cause is a missing fact, so the record would pass once the facts were known.
func unknownOnly(cs []Cause) bool {
	for _, c := range cs {
		if c.Kind == CauseFastUnmet || c.Kind == CauseProofBlocking {
			return false
		}
	}
	return true
}

// fill writes the totals and the sorted lists into the report.
func (t *tally) fill(r *Report) {
	s := &r.Shadow
	s.Shadow, s.Enforce = t.shadow, t.enforce
	s.Checked, s.Unchecked = t.checked, t.unchecked
	s.WouldPass, s.WouldBlock, s.WouldBlockUnknownOnly = t.pass, t.block, t.unknownOnly
	s.PassBasisPoints = rate(t.pass, t.checked)
	s.Causes = sortCauses(t.causes)
	s.Runs, s.RunBlocked, s.RunUnknown = t.runs, t.blocked, t.unknown
	s.RunNotReproduced, s.RunTrivial = t.notReproduced, t.trivial
	s.Proven, s.NeedsPerson, s.Blocking = t.proven, t.needsPerson, t.blocking
	s.Dropped, s.NotChecked = t.dropped, t.notChecked
	s.Cost = costStats(t.costs)
	s.BreakGlassOpen, s.BreakGlassReviewed = t.breakOpen, t.breakReviewed
	m := &r.Misses
	m.NotCheckedClauses = sortText(t.clauses)
	m.NotCheckedReasons = sortText(t.clauseReasons)
	m.CandidateNotChecked = sortReason(t.candidateNotChecked, notCheckedOrder()...)
	m.UnknownReasons = sortReason(t.unknownReasons, proof.StopReasons()...)
	m.DroppedReasons = sortReason(t.droppedReasons, dropOrder()...)
	m.Fast = latency(t.fastMS)
	m.Proof = latency(t.proofMS)
}

// rate is part out of whole in hundredths of a percent. An empty whole is 0, never 10000: nobody checked, so nothing
// passed.
func rate(part, whole int) int {
	if whole <= 0 || part <= 0 {
		return 0
	}
	if part >= whole {
		return 10_000
	}
	return (part*10_000 + whole/2) / whole
}

// sortCauses orders the causes most frequent first, then by kind in the catalog order, then by id.
func sortCauses(counts map[Cause]int) []Cause {
	out := make([]Cause, 0, len(counts))
	for c, n := range counts {
		if n > 0 {
			out = append(out, c)
			out[len(out)-1].Count = n
		}
	}
	slices.SortStableFunc(out, func(a, b Cause) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		if a.Kind != b.Kind {
			return kindOrder(a.Kind) - kindOrder(b.Kind)
		}
		return strings0(a.ID, b.ID)
	})
	return out
}

// kindOrder is a fixed place for each cause kind, so the order of two equal counts never depends on the input.
func kindOrder(k CauseKind) int {
	switch k {
	case CauseFastUnmet:
		return 0
	case CauseFastUnknown:
		return 1
	case CauseProofBlocking:
		return 2
	case CauseProofUnknown:
		return 3
	}
	return 4
}

// sortText orders text counts most frequent first, then by text.
func sortText(counts map[string]int) []TextCount {
	out := make([]TextCount, 0, len(counts))
	for s, n := range counts {
		if n > 0 {
			out = append(out, TextCount{Text: s, Count: n})
		}
	}
	slices.SortStableFunc(out, func(a, b TextCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings0(a.Text, b.Text)
	})
	return out
}

// notCheckedOrder is the order a "not checked" list reads in: the reasons of ACTION_MODE 6.3, then the two by design.
func notCheckedOrder() []proof.Reason {
	return append(proof.StopReasons(), proof.ReasonTrivial, proof.ReasonNotProvable)
}

// dropOrder is the order a drop list reads in: the four drop reasons of ACTION_MODE 6.3.
func dropOrder() []proof.DropReason {
	return []proof.DropReason{proof.DropNoProof, proof.DropRefuted, proof.DropOutdated, proof.DropNoLocation}
}

// allReasonNames is every reason and drop reason package proof defines. A reason in a list is Known when it is one of
// them, whatever list it came in, so a consumer can tell a real reason from text that only looks like one.
func allReasonNames() map[string]bool {
	out := map[string]bool{}
	for _, r := range notCheckedOrder() {
		out[string(r)] = true
	}
	for _, r := range dropOrder() {
		out[string(r)] = true
	}
	return out
}

// sortReason orders reason counts most frequent first, then by the catalog order of the reason, then by text. A reason
// the package does not define goes after the ones it does, and is marked so a reader can tell them apart.
func sortReason[K ~string](counts map[string]int, catalog ...K) []ReasonCount {
	rank := map[string]int{}
	for i, r := range catalog {
		rank[string(r)] = i
	}
	known := allReasonNames()
	out := make([]ReasonCount, 0, len(counts))
	for s, n := range counts {
		if n > 0 {
			out = append(out, ReasonCount{Reason: s, Known: known[s], Count: n})
		}
	}
	slices.SortStableFunc(out, func(a, b ReasonCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		if ra, rb := reasonRank(rank, a), reasonRank(rank, b); ra != rb {
			return ra - rb
		}
		return strings0(a.Reason, b.Reason)
	})
	return out
}

// reasonRank is the catalog place of a reason, or the place after the whole catalog when it is not in it. The place
// orders the list; Known says whether package proof defines the reason at all.
func reasonRank(rank map[string]int, c ReasonCount) int {
	if r, ok := rank[c.Reason]; ok {
		return r
	}
	return len(rank)
}

// strings0 compares two strings, so a list order never depends on the input order.
func strings0(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// costStats is the money of the measured pull requests. An empty set has no mean and no percentile, which prints as 0.
func costStats(measured []int64) CostStats {
	if len(measured) == 0 {
		return CostStats{}
	}
	sorted := slices.Clone(measured)
	slices.Sort(sorted)
	var total int64
	for _, v := range sorted {
		total += v
	}
	return CostStats{TotalMicros: total, MeanMicros: mean(total, len(sorted)),
		P50Micros: percentile(sorted, 50), P95Micros: percentile(sorted, 95), Measured: len(sorted)}
}

// latency is the wall time of the measured runs.
func latency(measured []int64) Latency {
	if len(measured) == 0 {
		return Latency{}
	}
	sorted := slices.Clone(measured)
	slices.Sort(sorted)
	var total int64
	for _, v := range sorted {
		total += v
	}
	return Latency{Measured: len(sorted), MeanMS: mean(total, len(sorted)),
		P50MS: percentile(sorted, 50), P95MS: percentile(sorted, 95), MaxMS: sorted[len(sorted)-1]}
}

// mean is the sum over n, rounded to the nearest unit, so a small real amount never reads as zero.
func mean(total int64, n int) int64 {
	if n <= 0 {
		return 0
	}
	return (total + int64(n)/2) / int64(n)
}

// percentile returns the nearest-rank percentile of a sorted list: the smallest value at or above p percent of the
// list. p50 of one value is that value, p50 of two is the lower of them, and p95 of twenty is the twentieth. An empty
// list has no percentile.
func percentile(sorted []int64, p int) int64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[n-1]
	}
	rank := (p*n + 99) / 100
	if rank < 1 {
		rank = 1
	}
	return sorted[rank-1]
}
