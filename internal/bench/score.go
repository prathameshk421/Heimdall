package bench

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testintegrity"
)

// Span is a closed range of line numbers, Start <= End, both at least 1.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// ParseSpan reads a truth span, "N" or "N-M" with 1 <= N <= M.
func ParseSpan(s string) (Span, error) {
	m := spanPattern.FindStringSubmatch(s)
	if m == nil || !validSpan(s) {
		return Span{}, fmt.Errorf("line span %q: want N or N-M with 1 <= N <= M", s)
	}
	start, err := strconv.Atoi(m[1])
	if err != nil {
		return Span{}, fmt.Errorf("line span %q: %w", s, err)
	}
	end := start
	if m[2] != "" {
		if end, err = strconv.Atoi(m[2]); err != nil {
			return Span{}, fmt.Errorf("line span %q: %w", s, err)
		}
	}
	return Span{Start: start, End: end}, nil
}

// String writes the span as the manifest does.
func (s Span) String() string {
	if s.Start == s.End {
		return strconv.Itoa(s.Start)
	}
	return strconv.Itoa(s.Start) + "-" + strconv.Itoa(s.End)
}

// Overlaps says whether the two spans share a line.
func (s Span) Overlaps(o Span) bool { return s.Start <= o.End && o.Start <= s.End }

// Hits says whether a finding at file and lines overlaps the truth: the same path and at least one shared line. A
// malformed truth span never matches.
func Hits(truth []FileTruth, file string, lines Span) bool {
	for _, ft := range truth {
		if ft.File != file {
			continue
		}
		for _, raw := range ft.Lines {
			if sp, err := ParseSpan(raw); err == nil && sp.Overlaps(lines) {
				return true
			}
		}
	}
	return false
}

// TruthMap is the truth of a bug case moved onto the head tree (doc.go, "Truth mapping").
type TruthMap struct {
	Truth []FileTruth `json:"truth"`
	// Moved counts spans whose line numbers changed between the head and the fix parent, with no edit on the lines.
	Moved int `json:"moved"`
	// Edited counts spans on lines that a commit between the head and the fix parent edited. Such a span maps to the
	// edited lines' old range, so it is an approximation.
	Edited int `json:"edited"`
	// Dropped names truth files that the head tree does not have.
	Dropped []string `json:"dropped,omitempty"`
}

// MapTruth maps truth spans from the fix parent tree onto the head tree. diffs is git diff -U0 from the head (old
// side) to the fix parent (new side). A span on unchanged lines moves by the line offset of the hunks before it. A span
// end inside an edited hunk maps to that hunk's old range (for a pure insertion, the gap span of oldSpan). A file
// added after the head is dropped. A truth span that does not parse is kept as written, so it still never matches.
func MapTruth(truth []FileTruth, diffs []FileDiff) TruthMap {
	byPath := map[string]FileDiff{}
	for _, d := range diffs {
		byPath[d.Path] = d
	}
	var out TruthMap
	for _, ft := range truth {
		d, changed := byPath[ft.File]
		if changed && d.Status == FileAdded {
			out.Dropped = append(out.Dropped, ft.File)
			continue
		}
		mapped := FileTruth{File: ft.File}
		for _, raw := range ft.Lines {
			sp, err := ParseSpan(raw)
			if err != nil || !changed {
				mapped.Lines = append(mapped.Lines, raw)
				continue
			}
			m, edited := mapSpan(sp, d.Hunks)
			switch {
			case edited:
				out.Edited++
			case m != sp:
				out.Moved++
			}
			mapped.Lines = append(mapped.Lines, m.String())
		}
		out.Truth = append(out.Truth, mapped)
	}
	return out
}

// mapSpan maps a new-side span to the old side, and reports whether any hunk edited a line of it.
func mapSpan(sp Span, hunks []Hunk) (Span, bool) {
	start, editedStart := mapLine(sp.Start, hunks)
	end, editedEnd := mapLine(sp.End, hunks)
	edited := editedStart || editedEnd
	for _, h := range hunks {
		if editsInside(h, sp) {
			edited = true
		}
	}
	return Span{Start: min(start.Start, end.Start), End: max(start.End, end.End)}, edited
}

// mapLine maps one new-side line to the old side. A line inside a hunk maps to the hunk's old range.
func mapLine(line int, hunks []Hunk) (Span, bool) {
	offset := 0
	for _, h := range hunks {
		if h.NewCount == 0 {
			// A pure deletion sits after new line NewStart.
			if line <= h.NewStart {
				break
			}
			offset += h.OldCount
			continue
		}
		if line < h.NewStart {
			break
		}
		if line <= h.NewStart+h.NewCount-1 {
			return oldRange(h), true
		}
		offset += h.OldCount - h.NewCount
	}
	return Span{Start: line + offset, End: line + offset}, false
}

// oldRange is the old-side span of a hunk, with the gap rule of oldSpan for a pure insertion.
func oldRange(h Hunk) Span {
	if h.OldCount == 0 {
		return Span{Start: max(h.OldStart, 1), End: h.OldStart + 1}
	}
	return Span{Start: h.OldStart, End: h.OldStart + h.OldCount - 1}
}

// editsInside says whether the hunk edits a new-side line strictly inside the span, or deletes lines between two of
// its lines.
func editsInside(h Hunk, sp Span) bool {
	if h.NewCount == 0 {
		return sp.Start <= h.NewStart && h.NewStart < sp.End
	}
	return Span{Start: h.NewStart, End: h.NewStart + h.NewCount - 1}.Overlaps(sp)
}

// ScoredFinding is one finding the reviewer proposed, with the answer of the proof rule. NotRun is set, with the
// reason, when the proof runs did not happen (the cost cap or the deadline); Proof is then empty.
type ScoredFinding struct {
	File     string                    `json:"file"`
	Lines    Span                      `json:"lines"`
	Severity finding.Severity          `json:"severity"`
	Claim    string                    `json:"claim"`
	TestPath string                    `json:"test_path"`
	TestName string                    `json:"test_name"`
	Proof    testintegrity.ProofResult `json:"proof"`
	Runs     []testintegrity.Run       `json:"runs,omitempty"`
	NotRun   string                    `json:"not_run,omitempty"`
}

func (f ScoredFinding) isProven() bool {
	return f.NotRun == "" && f.Proof.Class == testintegrity.ClassProven
}

// blocks is the gate rule for a proven finding (gate.BlockingSet with min_tier met by an E1 test): at or above the
// minimum severity, and a missing or unknown severity blocks.
func (f ScoredFinding) blocks(minimum finding.Severity) bool {
	return f.isProven() && (!f.Severity.Valid() || f.Severity.AtLeast(minimum))
}

// CaseScore is the score of one reviewed case. Scored is false when the run did not finish the case (the cost cap, a
// model error, a failure to set the case up). Such a case counts toward spend and time only.
type CaseScore struct {
	ID            string         `json:"id"`
	Kind          Kind           `json:"kind"`
	Scored        bool           `json:"scored"`
	Proposed      int            `json:"proposed"`
	Proven        int            `json:"proven"`
	Unproven      map[string]int `json:"unproven"`
	Hit           bool           `json:"hit"`
	Located       bool           `json:"located"`
	FalsePositive bool           `json:"false_positive"`
	CostUSD       float64        `json:"cost_usd"`
	InputTokens   int64          `json:"input_tokens"`
	CachedTokens  int64          `json:"cached_input_tokens"`
	OutputTokens  int64          `json:"output_tokens"`
	Seconds       float64        `json:"seconds"`
}

// ScoreCase scores the findings of one case. Bug case: Hit when a proven finding overlaps the truth (already mapped
// onto the head tree), Located when any finding does. Clean case: FalsePositive when a proven finding blocks at the
// minimum severity. Unproven counts the findings that are not proven by their ProofWhy, or "not_run: <reason>".
// The caller sets Scored, the cost and the time.
func ScoreCase(kind Kind, truth []FileTruth, findings []ScoredFinding, minimum finding.Severity) CaseScore {
	s := CaseScore{Kind: kind, Proposed: len(findings), Unproven: map[string]int{}}
	for _, f := range findings {
		overlaps := kind == KindBug && Hits(truth, f.File, f.Lines)
		s.Located = s.Located || overlaps
		switch {
		case f.NotRun != "":
			s.Unproven["not_run: "+f.NotRun]++
		case f.isProven():
			s.Proven++
			s.Hit = s.Hit || overlaps
			s.FalsePositive = s.FalsePositive || (kind == KindClean && f.blocks(minimum))
		default:
			s.Unproven[string(f.Proof.Why)]++
		}
	}
	return s
}

// Ratio is a count over a count. A zero denominator has no value.
type Ratio struct {
	Num int `json:"num"`
	Den int `json:"den"`
}

// Value returns the ratio, or false when the denominator is zero.
func (r Ratio) Value() (float64, bool) {
	if r.Den == 0 {
		return 0, false
	}
	return float64(r.Num) / float64(r.Den), true
}

// String writes "num/den (pct%)", or "n/a".
func (r Ratio) String() string {
	v, ok := r.Value()
	if !ok {
		return "n/a"
	}
	return fmt.Sprintf("%d/%d (%.0f%%)", r.Num, r.Den, v*100)
}

// Summary is the score of a whole run.
type Summary struct {
	Scored     int `json:"scored"`
	Unfinished int `json:"unfinished"`
	// Recall is bug cases with a hit over scored bug cases.
	Recall Ratio `json:"recall"`
	// FalsePositives is clean cases with a proven blocking finding over scored clean cases.
	FalsePositives Ratio `json:"false_positives"`
	// Evidence is proven findings over proposed findings, on scored cases.
	Evidence Ratio          `json:"evidence"`
	Unproven map[string]int `json:"unproven"`
	// CostUSD and the token counts add up every case, scored or not, because every call was paid for.
	CostUSD      float64 `json:"cost_usd"`
	InputTokens  int64   `json:"input_tokens"`
	CachedTokens int64   `json:"cached_input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	// The percentiles are over scored cases.
	CostP50 float64 `json:"cost_p50_usd"`
	CostP95 float64 `json:"cost_p95_usd"`
	WallP50 float64 `json:"wall_p50_seconds"`
	WallP95 float64 `json:"wall_p95_seconds"`
}

// Summarize adds up the case scores.
func Summarize(cases []CaseScore) Summary {
	s := Summary{Unproven: map[string]int{}}
	var costs, walls []float64
	for _, c := range cases {
		s.CostUSD += c.CostUSD
		s.InputTokens += c.InputTokens
		s.CachedTokens += c.CachedTokens
		s.OutputTokens += c.OutputTokens
		if !c.Scored {
			s.Unfinished++
			continue
		}
		s.Scored++
		costs, walls = append(costs, c.CostUSD), append(walls, c.Seconds)
		s.Evidence.Num += c.Proven
		s.Evidence.Den += c.Proposed
		for why, n := range c.Unproven {
			s.Unproven[why] += n
		}
		switch c.Kind {
		case KindBug:
			s.Recall.Den++
			if c.Hit {
				s.Recall.Num++
			}
		case KindClean:
			s.FalsePositives.Den++
			if c.FalsePositive {
				s.FalsePositives.Num++
			}
		}
	}
	s.CostP50, s.CostP95 = Percentile(costs, 50), Percentile(costs, 95)
	s.WallP50, s.WallP95 = Percentile(walls, 50), Percentile(walls, 95)
	return s
}

// Percentile is the nearest-rank percentile: the smallest value with at least p percent of the values at or below
// it. An empty list gives 0. The input is not reordered.
func Percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	rank = min(max(rank, 1), len(sorted))
	return sorted[rank-1]
}

// Price is a model's price in USD per million tokens.
type Price struct {
	InputPerM       float64 `json:"input_per_m"`
	CachedInputPerM float64 `json:"cached_input_per_m"`
	OutputPerM      float64 `json:"output_per_m"`
}

// framingTokens is an allowance for the tokens a provider adds around the messages and tool schemas.
const framingTokens = 1000

// CallBound is an upper bound on the cost of one answer. A request of n bytes holds at most n tokens, because every
// token covers at least one byte; framingTokens covers the provider's own framing. The answer holds at most
// maxOutput tokens. Every input token is billed at the full input price, which is never below the cache-read price.
func (p Price) CallBound(requestBytes, maxOutput int) float64 {
	return (float64(requestBytes+framingTokens)*p.InputPerM + float64(maxOutput)*p.OutputPerM) / 1e6
}

// ErrCostCap means a model call could pass the run's cost cap, so the run stops before it.
var ErrCostCap = errors.New("the next model call could pass the cost cap")

// CostCap is the hard spend limit of a bench run. Allow is asked before each model call with CallBound, and Add is
// told the cost the adapter reports after it. A cost that cannot be true counts as over the cap.
type CostCap struct {
	capUSD   float64
	spentUSD float64
	broken   bool
}

// NewCostCap returns a cap of capUSD, which must be a positive finite number.
func NewCostCap(capUSD float64) (*CostCap, error) {
	if !(capUSD > 0) || math.IsInf(capUSD, 0) {
		return nil, fmt.Errorf("cost cap %v: want a positive number of dollars", capUSD)
	}
	return &CostCap{capUSD: capUSD}, nil
}

// Allow returns ErrCostCap when the spend so far plus bound could pass the cap, or when the spend is already at the
// cap. A bound that is negative or not finite is refused.
func (c *CostCap) Allow(bound float64) error {
	if c.broken || !(bound >= 0) || math.IsInf(bound, 0) || c.spentUSD >= c.capUSD || c.spentUSD+bound > c.capUSD {
		return fmt.Errorf("%w: spent $%.4f of $%.2f, next call up to $%.4f", ErrCostCap, c.spentUSD, c.capUSD, bound)
	}
	return nil
}

// Add records the cost of one answer. A cost that is negative or not finite is an error, and every later Allow fails.
func (c *CostCap) Add(cost float64) error {
	if !(cost >= 0) || math.IsInf(cost, 0) {
		c.broken = true
		return fmt.Errorf("model cost %v cannot be true", cost)
	}
	c.spentUSD += cost
	return nil
}

// Spent returns the spend so far.
func (c *CostCap) Spent() float64 { return c.spentUSD }

// Remaining returns what is left under the cap, never below zero.
func (c *CostCap) Remaining() float64 { return max(c.capUSD-c.spentUSD, 0) }

// CaseBudget returns the per-case budget for the agent loop: perCase, or what is left when that is less. It returns
// ErrCostCap when nothing is left, and an error for a per-case budget that is not positive.
func (c *CostCap) CaseBudget(perCase float64) (float64, error) {
	if !(perCase > 0) || math.IsInf(perCase, 0) {
		return 0, fmt.Errorf("per-case budget %v: want a positive number of dollars", perCase)
	}
	if c.broken || c.Remaining() <= 0 {
		return 0, ErrCostCap
	}
	return min(perCase, c.Remaining()), nil
}
