package digest

import (
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// MaxBytes is the size RenderMarkdown never passes. It counts bytes, which is never fewer than characters. The caps
// on each list keep a real report far below it; the bound is the guarantee for a hostile input.
const MaxBytes = 20_000

// Mode is the action mode a pull request was checked under (ACTION_MODE 3.3).
type Mode string

// The two modes.
const (
	ModeShadow  Mode = "shadow"
	ModeEnforce Mode = "enforce"
)

// ModeOf reads a mode. An empty value reads as shadow, which is the mode of an action area that names none. Any other
// value this package does not define reads as enforce, so a typo never counts as the advisory mode.
func ModeOf(s string) Mode {
	if s == "" || Mode(s) == ModeShadow {
		return ModeShadow
	}
	return ModeEnforce
}

// Input is everything Compute reads. All of it is a value: this package reads no clock, no source and no environment.
// The caller folds each change's view first and passes one record per pull request.
type Input struct {
	// Repo is the repository the period covers, as owner/name. It is shown, never parsed.
	Repo string `json:"repo"`
	// From and To bound the period: From inclusive, To exclusive. A zero value leaves that end of the period open.
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// PRs holds one record per pull request. The JSON name is "records", because a Report names its own count "prs":
	// a page is not a snapshot, and reading one back as the other is refused rather than read as an empty period.
	PRs []PRRecord `json:"records"`
}

// PRRecord is what one pull request's view recorded about the two checks and the money. It is the input of the digest,
// so the caller extracts it from a change.View and this package never reads that view itself.
type PRRecord struct {
	// Number is the pull request number. A value below 1 makes the record malformed.
	Number int `json:"number"`
	// Head is the head commit the record is about. It is kept for a reader of the snapshot; no count uses it.
	Head string `json:"head,omitempty"`
	// Mode is the action mode the pull request was checked under. Empty reads as shadow.
	Mode Mode `json:"mode,omitempty"`
	// At places the record in the period. A zero At cannot be placed in a bounded period, so the report counts it apart.
	At time.Time `json:"at"`
	// Fast and Proof are the two checks. Ran is false when the check never reported.
	Fast  FastRecord  `json:"fast"`
	Proof ProofRecord `json:"proof"`
	// CostMicros is every cost record of the pull request, in whole micro dollars.
	CostMicros int64 `json:"cost_usd_micros"`
	// FastTimeMS and ProofTimeMS are the measured wall times in milliseconds. Zero means not measured: no real run
	// takes under a millisecond, so the value needs no separate flag.
	FastTimeMS  int64 `json:"fast_time_ms"`
	ProofTimeMS int64 `json:"proof_time_ms"`
	// BreakGlassOpen and BreakGlassReviewed count the bypasses of the head that still wait for a review and those
	// another approver reviewed (ACTION_MODE 5.4).
	BreakGlassOpen     int `json:"breakglass_open"`
	BreakGlassReviewed int `json:"breakglass_reviewed"`
}

// FastRecord is the fast check of one head: its clauses and, when known, its wall time.
type FastRecord struct {
	Ran     bool     `json:"ran"`
	Clauses []Clause `json:"clauses,omitempty"`
}

// maxClauses bounds the clauses one record may carry. The fast check has eight (ACTION_MODE 5.1), so a longer list is
// a malformed record, not a longer report.
const maxClauses = 16

// Clause is one clause of the fast check, as the view recorded it.
type Clause struct {
	ID     fastcheck.ClauseID `json:"id"`
	Status fastcheck.Status   `json:"status"`
	Reason string             `json:"reason,omitempty"`
}

// ProofRecord is the last proof run of one head (ACTION_MODE 5.2). Kind and Reason are empty until a run reported.
type ProofRecord struct {
	Ran    bool         `json:"ran"`
	Kind   proof.Kind   `json:"kind,omitempty"`
	Reason proof.Reason `json:"reason,omitempty"`
	Counts Counts       `json:"counts"`
	// NotCheckedReasons says why each candidate finding was left unchecked, one entry per reason. Their counts add up to
	// Counts.NotChecked or less: a snapshot that predates this field has the total only.
	NotCheckedReasons []ReasonTally `json:"not_checked_reasons,omitempty"`
}

// ReasonTally is one reason of package proof and how many candidates it left unchecked.
type ReasonTally struct {
	Reason proof.Reason `json:"reason"`
	Count  int          `json:"count"`
}

// Counts is the proof tally of one run, with the wire names. Its parts add up: In is the proven, the dropped and the
// not checked together, and the proven are the blocking and those that need a person.
type Counts struct {
	In          int `json:"in"`
	Proven      int `json:"proven"`
	Blocking    int `json:"blocking"`
	NeedsPerson int `json:"needs_person"`
	Refuted     int `json:"refuted"`
	NoProof     int `json:"no_proof"`
	Outdated    int `json:"outdated"`
	NoLocation  int `json:"no_location"`
	NotChecked  int `json:"not_checked"`
}

// CountsOf converts a proof tally to the report's shape.
func CountsOf(c proof.Counts) Counts { return Counts(c) }

// Dropped is the number of candidates that were dropped, whatever the reason.
func (c Counts) Dropped() int { return c.Refuted + c.NoProof + c.Outdated + c.NoLocation }

// negative reports a count that cannot be true.
func (c Counts) negative() bool {
	all := []int{c.In, c.Proven, c.Blocking, c.NeedsPerson, c.Refuted, c.NoProof, c.Outdated, c.NoLocation, c.NotChecked}
	for _, n := range all {
		if n < 0 {
			return true
		}
	}
	return false
}

// Report is the page: the shadow digest (A4) and the misses and latency report (A12) for one period of one repository.
type Report struct {
	Repo string    `json:"repo"`
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// PRs is the number of records in the period. Outside, Untimed and Malformed count the records that were read and
	// left out, so a smaller number is never silent.
	PRs       int `json:"prs"`
	Outside   int `json:"outside"`
	Untimed   int `json:"untimed"`
	Malformed int `json:"malformed"`

	Shadow ShadowDigest `json:"shadow_digest"`
	Misses Misses       `json:"misses"`
}

// ShadowDigest is the shadow digest: what the check said, how much it would have blocked, and what it cost.
type ShadowDigest struct {
	// Shadow and Enforce split the checked pull requests by the mode they were checked under.
	Shadow  int `json:"shadow"`
	Enforce int `json:"enforce"`
	// Checked are the records one of the two checks reported on. Unchecked are the others: Heimdall did not look.
	Checked   int `json:"checked"`
	Unchecked int `json:"unchecked"`
	// WouldPass and WouldBlock count the checked pull requests. WouldBlockUnknownOnly counts those whose only cause is
	// a fact Heimdall did not have, which a team can fix before it enforces the check.
	WouldPass             int `json:"would_pass"`
	WouldBlock            int `json:"would_block"`
	WouldBlockUnknownOnly int `json:"would_block_unknown_only"`
	// PassBasisPoints is WouldPass out of Checked in hundredths of a percent: 10000 is every pull request.
	PassBasisPoints int `json:"pass_basis_points"`
	// Causes is every would-block cause, most frequent first. One pull request can have more than one cause, so the
	// counts of Causes add up to at least WouldBlock, never less.
	Causes []Cause `json:"causes"`
	// Runs counts the proof runs and how each ended.
	Runs             int `json:"runs"`
	RunBlocked       int `json:"run_blocked"`
	RunUnknown       int `json:"run_unknown"`
	RunNotReproduced int `json:"run_not_reproduced"`
	RunTrivial       int `json:"run_trivial"`
	// Proven, NeedsPerson, Blocking, Dropped and NotChecked count candidate findings, summed over the runs.
	Proven      int       `json:"proven"`
	NeedsPerson int       `json:"needs_person"`
	Blocking    int       `json:"blocking"`
	Dropped     int       `json:"dropped"`
	NotChecked  int       `json:"candidate_not_checked"`
	Cost        CostStats `json:"cost"`
	// BreakGlassOpen and BreakGlassReviewed count the bypasses over every record of the period.
	BreakGlassOpen     int `json:"breakglass_open"`
	BreakGlassReviewed int `json:"breakglass_reviewed"`
}

// CauseKind says why the check would have blocked.
type CauseKind string

// The four kinds of would-block cause.
const (
	CauseFastUnmet     CauseKind = "fast_unmet"
	CauseFastUnknown   CauseKind = "fast_unknown"
	CauseProofBlocking CauseKind = "proof_blocking"
	CauseProofUnknown  CauseKind = "proof_unknown"
)

// Cause is one reason a pull request would have blocked, and how many pull requests it applied to.
type Cause struct {
	Kind  CauseKind `json:"kind"`
	ID    string    `json:"id,omitempty"`
	Count int       `json:"count"`
}

// CostStats is the money of the period. Every amount is whole micro dollars, because a payload carries no fractions.
type CostStats struct {
	TotalMicros int64 `json:"total_usd_micros"`
	MeanMicros  int64 `json:"mean_usd_micros"`
	P50Micros   int64 `json:"p50_usd_micros"`
	P95Micros   int64 `json:"p95_usd_micros"`
	Measured    int   `json:"measured"`
}

// Misses is the report of what Heimdall did not check, did not prove, did not know, and how long it took (A12).
type Misses struct {
	// NotCheckedClauses counts the fast check clauses that were not checked, per clause.
	NotCheckedClauses []TextCount `json:"not_checked_clauses"`
	// NotCheckedReasons counts the reasons those clauses gave, per reason.
	NotCheckedReasons []TextCount `json:"not_checked_reasons"`
	// CandidateNotChecked counts the candidate findings left unchecked, per reason.
	CandidateNotChecked []ReasonCount `json:"candidate_not_checked"`
	// UnknownReasons counts the proof runs that ended unknown, per reason.
	UnknownReasons []ReasonCount `json:"unknown_reasons"`
	// DroppedReasons counts the candidate findings dropped, per reason.
	DroppedReasons []ReasonCount `json:"dropped_reasons"`
	Fast           Latency       `json:"fast"`
	Proof          Latency       `json:"proof"`
}

// TextCount is a label this package does not define, and how many times it appeared.
type TextCount struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// ReasonCount is a reason of package proof, and how many times it appeared. Known is false for a value that package
// does not define, so a reader can tell a real reason from text that only looks like one.
type ReasonCount struct {
	Reason string `json:"reason"`
	Known  bool   `json:"known"`
	Count  int    `json:"count"`
}

// Latency is the measured wall time of one check. Measured is the number of runs that had a measurement.
type Latency struct {
	Measured int   `json:"measured"`
	MeanMS   int64 `json:"mean_ms"`
	P50MS    int64 `json:"p50_ms"`
	P95MS    int64 `json:"p95_ms"`
	MaxMS    int64 `json:"max_ms"`
}
