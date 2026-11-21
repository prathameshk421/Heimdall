package change

import (
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// ActionInfo is what the Action-mode events recorded about one change. Every part keeps the head it was about, so a
// stale value shows as stale, as the PR parts of the view do. A repeated event folds to the same view (H1): each part
// is replaced, keyed or de-duplicated, never added to blindly.
type ActionInfo struct {
	// Fast is the last fast.reported. Proof is the last run that started or finished.
	Fast  *FastInfo  `json:"fast,omitempty"`
	Proof *ProofInfo `json:"proof,omitempty"`
	// Costs are the cost records, one per workflow run and model.
	Costs []CostRecorded `json:"costs,omitempty"`
	// External are the candidates taken from third-party comments, by comment id. A comment read again replaces its record.
	External map[int64]ExternalInfo `json:"external,omitempty"`
	// Summary is the last proof summary comment posted.
	Summary    *SummaryInfo     `json:"summary,omitempty"`
	BreakGlass []BreakGlassInfo `json:"break_glass,omitempty"`
	// Reset is the last state.reset and how many there were.
	Reset *ResetInfo `json:"reset,omitempty"`
}

// FastInfo is the last fast check result and the head it was about.
type FastInfo struct {
	Head    id.SHA       `json:"head"`
	Verdict gate.Verdict `json:"verdict"`
	Clauses []FastClause `json:"clauses"`
}

// ProofInfo is the last proof run. Done is false from proof.started until proof.reported. The result parts are empty
// until then.
type ProofInfo struct {
	WorkflowRun int64        `json:"workflow_run"`
	Head        id.SHA       `json:"head"`
	Base        id.SHA       `json:"base"`
	PolicyHash  string       `json:"policy_hash"`
	Model       string       `json:"model,omitempty"`
	Done        bool         `json:"done"`
	Kind        proof.Kind   `json:"kind,omitempty"`
	Reason      proof.Reason `json:"reason,omitempty"`
	Counts      ProofCounts  `json:"counts"`
	Cost        Cost         `json:"cost"`
}

// sameRun reports whether two infos are one run. The workflow run id names a run; the head, base and policy hash are
// what it ran on.
func (p ProofInfo) sameRun(o ProofInfo) bool {
	return p.WorkflowRun == o.WorkflowRun && p.Head == o.Head && p.Base == o.Base && p.PolicyHash == o.PolicyHash
}

// FindingProofInfo is what a proof run said about one finding, stamped with the head the run was about.
type FindingProofInfo struct {
	Head     id.SHA           `json:"head"`
	Class    proof.Class      `json:"class"`
	Dropped  proof.DropReason `json:"dropped,omitempty"`
	Reason   proof.Reason     `json:"reason,omitempty"`
	Blocking bool             `json:"blocking,omitempty"`
}

// ExternalInfo is one candidate taken from a third-party comment.
type ExternalInfo ExternalIngested

// SummaryInfo is the proof summary comment: where it is and the head it was written for.
type SummaryInfo SummaryPosted

// BreakGlassInfo is one bypass of the fast check on a head. It is open until another person reviews it.
type BreakGlassInfo struct {
	Head      id.SHA  `json:"head"`
	Login     ActorID `json:"login"`
	Reason    string  `json:"reason"`
	SourceRef string  `json:"source_ref"`
	// ReviewedBy and ReviewRef are empty while the bypass is open.
	ReviewedBy ActorID `json:"reviewed_by,omitempty"`
	ReviewRef  string  `json:"review_ref,omitempty"`
}

// Open reports whether nobody has reviewed the bypass.
func (b BreakGlassInfo) Open() bool { return b.ReviewedBy == "" }

// ResetInfo is the state resets of the change: the reason of the last, and how many.
type ResetInfo struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// CostTotal is the sum of every cost record. A nil ActionInfo, a change with no Action event, has none.
func (a *ActionInfo) CostTotal() Cost {
	var t Cost
	if a == nil {
		return t
	}
	for _, c := range a.Costs {
		t.USDMicros += c.USDMicros
		t.PromptTokens += c.PromptTokens
		t.CompletionTokens += c.CompletionTokens
	}
	return t
}

// OpenBreakGlass reports whether a bypass of the head is waiting for its review.
func (a *ActionInfo) OpenBreakGlass(head id.SHA) bool {
	if a == nil {
		return false
	}
	for _, b := range a.BreakGlass {
		if b.Head == head && b.Open() {
			return true
		}
	}
	return false
}

// action returns the Action part of the view, made on first use.
func (v *View) action() *ActionInfo {
	if v.Action == nil {
		v.Action = &ActionInfo{}
	}
	return v.Action
}

// applyAction folds the Action events. Anything else that reaches it is not one, and changes nothing.
func (v *View) applyAction(p Payload) {
	switch p := p.(type) {
	case FastReported, ProofStarted, ProofReported:
		v.applyRun(p)
	case CostRecorded, ExternalIngested, SummaryPosted:
		v.applyActionRecord(p)
	case BreakGlassRecorded, BreakGlassReviewed:
		v.applyBreakGlass(p)
	case StateReset:
		v.applyReset(p)
	}
}

// applyRun folds the fast check and the proof run.
func (v *View) applyRun(p Payload) {
	a := v.action()
	switch p := p.(type) {
	case FastReported:
		a.Fast = &FastInfo{Head: p.Head, Verdict: p.Verdict, Clauses: p.Clauses}
	case ProofStarted:
		a.startProof(ProofInfo{WorkflowRun: p.WorkflowRun, Head: p.Head, Base: p.Base, PolicyHash: p.PolicyHash, Model: p.Model})
	case ProofReported:
		a.Proof = &ProofInfo{WorkflowRun: p.WorkflowRun, Head: p.Head, Base: p.Base, PolicyHash: p.PolicyHash, Model: p.Model,
			Done: true, Kind: p.Kind, Reason: p.Reason, Counts: p.Counts, Cost: p.Cost}
		v.applyCandidateResults(p)
	}
}

// startProof records a run that began. A start that repeats a run already finished (a crash after the append) does not
// open it again.
func (a *ActionInfo) startProof(next ProofInfo) {
	if a.Proof != nil && a.Proof.Done && a.Proof.sameRun(next) {
		return
	}
	a.Proof = &next
}

// applyCandidateResults marks the findings a proof run named. A result for a finding this log does not hold is a fact
// about nothing here, so it changes nothing.
func (v *View) applyCandidateResults(p ProofReported) {
	for _, r := range p.Results {
		ref := r.Ref.String()
		f, ok := v.Findings[ref]
		if !ok {
			continue
		}
		f.Proof = &FindingProofInfo{Head: p.Head, Class: r.Class, Dropped: r.Dropped, Reason: r.Reason, Blocking: r.Blocking}
		v.Findings[ref] = f
	}
}

// applyActionRecord folds the records that are keyed: costs by run and model, candidates by comment, the summary.
func (v *View) applyActionRecord(p Payload) {
	a := v.action()
	switch p := p.(type) {
	case CostRecorded:
		a.recordCost(p)
	case ExternalIngested:
		if a.External == nil {
			a.External = map[int64]ExternalInfo{}
		}
		a.External[p.CommentID] = ExternalInfo(p)
	case SummaryPosted:
		s := SummaryInfo(p)
		a.Summary = &s
	}
}

// recordCost keeps one record per workflow run and model, so a record appended twice is counted once.
func (a *ActionInfo) recordCost(p CostRecorded) {
	for i, c := range a.Costs {
		if c.WorkflowRun == p.WorkflowRun && c.Model == p.Model {
			a.Costs[i] = p
			return
		}
	}
	a.Costs = append(a.Costs, p)
}

// applyBreakGlass folds a bypass and its review.
func (v *View) applyBreakGlass(p Payload) {
	a := v.action()
	switch p := p.(type) {
	case BreakGlassRecorded:
		a.recordBreakGlass(p)
	case BreakGlassReviewed:
		a.reviewBreakGlass(p)
	}
}

// recordBreakGlass adds a bypass. The same bypass from the same command comment is one bypass.
func (a *ActionInfo) recordBreakGlass(p BreakGlassRecorded) {
	for _, b := range a.BreakGlass {
		if b.Head == p.Head && b.Login == p.Login && b.SourceRef == p.SourceRef {
			return
		}
	}
	a.BreakGlass = append(a.BreakGlass, BreakGlassInfo{Head: p.Head, Login: p.Login, Reason: p.Reason, SourceRef: p.SourceRef})
}

// reviewBreakGlass closes every open bypass of the head that somebody else made. A person's review of their own bypass
// closes nothing: another approver must post it (ACTION_MODE 5.4).
func (a *ActionInfo) reviewBreakGlass(p BreakGlassReviewed) {
	for i := range a.BreakGlass {
		b := &a.BreakGlass[i]
		if b.Head == p.Head && b.Open() && b.Login != p.Login {
			b.ReviewedBy, b.ReviewRef = p.Login, p.SourceRef
		}
	}
}

// applyReset folds state.reset: the log before it could not be trusted, so every produced fact is dropped and the
// reset is counted. Observed facts stay, because GitHub holds them (ACTION_MODE 2.1). Cost records stay too: the money
// was spent whatever the log says, and forgetting it would give the budget back.
func (v *View) applyReset(p StateReset) {
	a := v.action()
	n := 1
	if a.Reset != nil {
		n = a.Reset.Count + 1
	}
	*a = ActionInfo{Costs: a.Costs, Reset: &ResetInfo{Reason: p.Reason, Count: n}}
	for ref, f := range v.Findings {
		if f.Proof != nil {
			f.Proof = nil
			v.Findings[ref] = f
		}
	}
}
