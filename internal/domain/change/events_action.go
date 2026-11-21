package change

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// The Action area of the event catalog (ACTION_MODE 1.2, 2, 5, 6, 7): what Heimdall records when it runs as a step in
// the customer's GitHub Actions. The names proof.started, proof.reported, cost.recorded, external.ingested,
// breakglass.recorded, breakglass.reviewed and state.reset are the design's. fast.reported and summary.posted follow
// the pattern of ci.reported and checks.reported, because the design records those results but names no event for them.
// They are catalog entries after the server's events, so no existing position moves.
const (
	EventFastReported       EventType = "fast.reported"
	EventProofStarted       EventType = "proof.started"
	EventProofReported      EventType = "proof.reported"
	EventCostRecorded       EventType = "cost.recorded"
	EventExternalIngested   EventType = "external.ingested"
	EventSummaryPosted      EventType = "summary.posted"
	EventBreakGlassRecorded EventType = "breakglass.recorded"
	EventBreakGlassReviewed EventType = "breakglass.reviewed"
	EventStateReset         EventType = "state.reset"
)

// The bounds of the Action payloads. One event is at most 64 KiB (ACTION_MODE 2.6); each bound keeps its payload far
// below that.
const (
	// MaxClauseReason bounds one clause reason of fast.reported. fastcheck lists files and rule cards in a reason, so a
	// writer clips a longer one with NewFastReported.
	MaxClauseReason = 2048
	// MaxProofResults bounds the per-candidate results of one proof.reported. It is the number of candidates the
	// external package keeps for one pull request.
	MaxProofResults = 200
	// maxProofCount bounds each count of ProofCounts, so adding them cannot overflow.
	maxProofCount = 1_000_000
	// maxCostMicros and maxCostTokens bound one cost record: $1,000,000 and a trillion tokens, far above any real run.
	// A sum of the records a log can hold (4 MiB) stays inside an int64.
	maxCostMicros = 1_000_000_000_000
	maxCostTokens = 1_000_000_000_000
	// microsPerUSD is how many of the integer cost unit make one dollar. A payload holds no fractions (jsonstrict.go),
	// so money is whole micro dollars.
	microsPerUSD = 1_000_000
)

// actionPayloads is the registry of the Action area.
func actionPayloads() map[EventType]payloadEntry {
	return map[EventType]payloadEntry{
		EventFastReported:       entry[FastReported](),
		EventProofStarted:       entry[ProofStarted](),
		EventProofReported:      entry[ProofReported](),
		EventCostRecorded:       entry[CostRecorded](),
		EventExternalIngested:   entry[ExternalIngested](),
		EventSummaryPosted:      entry[SummaryPosted](),
		EventBreakGlassRecorded: entry[BreakGlassRecorded](),
		EventBreakGlassReviewed: entry[BreakGlassReviewed](),
		EventStateReset:         entry[StateReset](),
	}
}

// checkWorkflowRun requires a GitHub Actions run id.
func checkWorkflowRun(n int64) error {
	if n < 1 {
		return fmt.Errorf("has %d, needs at least 1", n)
	}
	return nil
}

// checkPolicyHash requires the sha256 of the policy at the merge base, as 64 lowercase hex characters.
func checkPolicyHash(s string) error {
	if !reHash.MatchString(s) {
		return errors.New("must be 64 lowercase hex characters")
	}
	return nil
}

// FastClause is the result of one clause of the fast check, as fastcheck.Clause says it.
type FastClause struct {
	ID     fastcheck.ClauseID `json:"id"`
	Status fastcheck.Status   `json:"status"`
	Reason string             `json:"reason"`
}

// FastReported is the payload of fast.reported: the fast check ran on a head (ACTION_MODE 5.1). Verdict is the
// verdict of the clauses, and Validate refuses one that is not, so a forged pass is not a valid event.
type FastReported struct {
	Head    id.SHA       `json:"head"`
	Verdict gate.Verdict `json:"verdict"`
	Clauses []FastClause `json:"clauses"`
}

// NewFastReported is the payload for a fastcheck result on a head. It clips a reason that is longer than
// MaxClauseReason, at a rune boundary, and says so with an ellipsis.
func NewFastReported(head id.SHA, r fastcheck.Result) FastReported {
	p := FastReported{Head: head, Verdict: r.Verdict, Clauses: make([]FastClause, 0, len(r.Clauses))}
	for _, c := range r.Clauses {
		p.Clauses = append(p.Clauses, FastClause{ID: c.ID, Status: c.Status, Reason: clipReason(c.Reason)})
	}
	return p
}

// clipReason cuts s to MaxClauseReason bytes without splitting a rune.
func clipReason(s string) string {
	const ellipsis = "…"
	if len(s) <= MaxClauseReason {
		return s
	}
	cut := MaxClauseReason - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + ellipsis
}

// Validate returns every problem with the payload.
func (p FastReported) Validate() error {
	var v problems
	v.add("head", checkSHA(p.Head))
	v.add("verdict", checkEnum(p.Verdict, p.Verdict.Valid()))
	v.require("clauses", len(p.Clauses) >= 1, "needs at least one clause")
	v.require("clauses", len(p.Clauses) <= fastClauseCount, fmt.Sprintf("must hold at most %d clauses", fastClauseCount))
	seen := map[fastcheck.ClauseID]bool{}
	sound := true
	for i, c := range p.Clauses {
		at := fmt.Sprintf("clauses[%d]", i)
		v.add(at+".id", checkEnum(c.ID, validClause(c.ID)))
		v.add(at+".status", checkEnum(c.Status, validStatus(c.Status)))
		v.add(at+".reason", checkClauseReason(c.Reason))
		v.require(at+".id", !seen[c.ID], "appears twice")
		seen[c.ID] = true
		sound = sound && validClause(c.ID) && validStatus(c.Status)
	}
	if sound && p.Verdict.Valid() && len(p.Clauses) > 0 {
		v.require("verdict", p.Verdict == verdictOfClauses(p.Clauses), fmt.Sprintf("is %s, but the clauses say %s", p.Verdict, verdictOfClauses(p.Clauses)))
	}
	return v.join()
}

// fastClauseCount is the number of clauses of the fast check (ACTION_MODE 5.1).
const fastClauseCount = 8

func validClause(c fastcheck.ClauseID) bool {
	return oneOf(c, fastcheck.ClauseSecrets, fastcheck.ClauseAnalyzers, fastcheck.ClausePinnedTests, fastcheck.ClauseTestIntegrity,
		fastcheck.ClauseDiffCoverage, fastcheck.ClauseApproval, fastcheck.ClauseMemoryRules, fastcheck.ClauseHeimdallFiles)
}

func validStatus(s fastcheck.Status) bool {
	return oneOf(s, fastcheck.StatusMet, fastcheck.StatusUnmet, fastcheck.StatusUnknown, fastcheck.StatusNotChecked)
}

func checkClauseReason(s string) error {
	if err := checkLongText(s); err != nil {
		return err
	}
	if len(s) > MaxClauseReason {
		return fmt.Errorf("must be at most %d bytes", MaxClauseReason)
	}
	return nil
}

// verdictOfClauses derives the verdict the way fastcheck.Evaluate does: an unmet or unknown clause makes the decision
// not OK, and gate.VerdictOf turns that into fail or unknown. A met or not-checked clause adds nothing.
func verdictOfClauses(cs []FastClause) gate.Verdict {
	var d gate.Decision
	for _, c := range cs {
		switch c.Status {
		case fastcheck.StatusUnmet:
			d.Unmet = append(d.Unmet, gate.Clause{ID: gate.ClauseID(c.ID), Reason: c.Reason})
		case fastcheck.StatusUnknown:
			d.Unmet = append(d.Unmet, gate.Clause{ID: gate.ClauseID(c.ID), Reason: c.Reason, Unknown: true})
		case fastcheck.StatusMet, fastcheck.StatusNotChecked:
			// Neither adds to the decision.
		}
	}
	d.OK = len(d.Unmet) == 0
	return gate.VerdictOf(d)
}

// Cost is what a model run spent, in whole micro dollars and tokens. A payload holds no fractions, so dollars are
// integers (jsonstrict.go). The names of the tokens are the provider's: prompt in, completion out.
type Cost struct {
	USDMicros        int64 `json:"usd_micros"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
}

// NewCost converts a spend to a cost, rounding the dollars to the nearest micro dollar. It refuses a spend that cannot
// be true (negative, not a number, or above the bound), as proof.Spend.Exceeds does, so a missing measure is never
// recorded as a cheap run.
func NewCost(s proof.Spend) (Cost, error) {
	if !(s.USD >= 0 && s.USD <= maxCostMicros/microsPerUSD) { // false for a NaN, which no ordering holds
		return Cost{}, fmt.Errorf("spend of %v dollars cannot be true", s.USD)
	}
	if s.InputTokens < 0 || s.OutputTokens < 0 {
		return Cost{}, errors.New("a token count cannot be negative")
	}
	c := Cost{USDMicros: int64(s.USD*microsPerUSD + 0.5), PromptTokens: int64(s.InputTokens), CompletionTokens: int64(s.OutputTokens)}
	return c, c.Validate()
}

// Spend is the cost as the proof package reads it.
func (c Cost) Spend() proof.Spend {
	return proof.Spend{USD: float64(c.USDMicros) / microsPerUSD, InputTokens: int(c.PromptTokens), OutputTokens: int(c.CompletionTokens)}
}

// Validate returns every problem with the cost.
func (c Cost) Validate() error {
	var v problems
	v.require("usd_micros", c.USDMicros >= 0 && c.USDMicros <= maxCostMicros, fmt.Sprintf("must be from 0 to %d", int64(maxCostMicros)))
	v.require("prompt_tokens", c.PromptTokens >= 0 && c.PromptTokens <= maxCostTokens, fmt.Sprintf("must be from 0 to %d", int64(maxCostTokens)))
	v.require("completion_tokens", c.CompletionTokens >= 0 && c.CompletionTokens <= maxCostTokens, fmt.Sprintf("must be from 0 to %d", int64(maxCostTokens)))
	return v.join()
}

// CostRecorded is the payload of cost.recorded: what one model spent in one proof run (ACTION_MODE 7). WorkflowRun is
// the run's GitHub Actions id. The design lists no run id; it is here so that a record appended twice (a crash after
// the append, H1) folds to one record instead of adding up twice.
type CostRecorded struct {
	WorkflowRun int64  `json:"workflow_run"`
	Model       string `json:"model"`
	Cost
}

// Validate returns every problem with the payload.
func (p CostRecorded) Validate() error {
	var v problems
	v.add("workflow_run", checkWorkflowRun(p.WorkflowRun))
	v.add("model", checkText(p.Model))
	v = append(v, p.Cost.Validate())
	return v.join()
}

// ProofStarted is the payload of proof.started: a proof run began (ACTION_MODE 2.5). The head, the base, the policy
// hash and the model name the run's input, so the same input is never run twice.
type ProofStarted struct {
	WorkflowRun int64  `json:"workflow_run"`
	Head        id.SHA `json:"head"`
	Base        id.SHA `json:"base"`
	PolicyHash  string `json:"policy_hash"`
	Model       string `json:"model"`
}

// Validate returns every problem with the payload.
func (p ProofStarted) Validate() error {
	var v problems
	v.add("workflow_run", checkWorkflowRun(p.WorkflowRun))
	v.add("head", checkSHA(p.Head))
	v.add("base", checkSHA(p.Base))
	v.add("policy_hash", checkPolicyHash(p.PolicyHash))
	v.add("model", checkText(p.Model))
	return v.join()
}

// ProofCounts is the tally of proof.Counts with the names of the wire format. Its parts add up: the candidates in are
// the proven, the dropped and the not checked, and the proven are the blocking and those that need a person.
type ProofCounts struct {
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

// NewProofCounts converts the tally of a proof outcome.
func NewProofCounts(c proof.Counts) ProofCounts { return ProofCounts(c) }

// Counts is the tally as the proof package reads it.
func (c ProofCounts) Counts() proof.Counts { return proof.Counts(c) }

// Validate returns every problem with the counts.
func (c ProofCounts) Validate() error {
	var v problems
	for _, f := range []struct {
		name string
		n    int
	}{{"in", c.In}, {"proven", c.Proven}, {"blocking", c.Blocking}, {"needs_person", c.NeedsPerson}, {"refuted", c.Refuted},
		{"no_proof", c.NoProof}, {"outdated", c.Outdated}, {"no_location", c.NoLocation}, {"not_checked", c.NotChecked}} {
		v.require(f.name, f.n >= 0 && f.n <= maxProofCount, fmt.Sprintf("must be from 0 to %d", maxProofCount))
	}
	if len(v) > 0 {
		return v.join()
	}
	v.require("in", c.In == c.Proven+c.Counts().Dropped()+c.NotChecked, "must equal the proven, dropped and not checked together")
	v.require("proven", c.Proven == c.Blocking+c.NeedsPerson, "must equal the blocking and the needs-person together")
	return v.join()
}
