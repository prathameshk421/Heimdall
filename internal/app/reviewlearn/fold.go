package reviewlearn

import (
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// The outcome of a finding.
const (
	OutcomeUseful  = "useful"
	OutcomeNoise   = "noise"
	OutcomeNeutral = "neutral"
	OutcomeIgnored = "ignored"
	OutcomePending = "pending"
)

// row is one finding with everything the read model needs.
type row struct {
	change   id.ChangeID
	repo     change.Repo
	ref      string
	agent    id.AgentName
	category finding.Category
	claim    string
	file     string
	cardRefs []string
	state    finding.State
	reason   string
	at       time.Time
	outcome  string
}

// changeFold is the result of reading one change.
type changeFold struct {
	id        id.ChangeID
	repo      change.Repo
	base      id.SHA
	merged    bool
	rejected  bool
	order     []string
	findings  map[string]*row
	decisions []decision
	proposals []Proposal
}

type decision struct {
	event   change.Event
	payload change.DecisionRecorded
}

func foldChange(changeID id.ChangeID, events []change.Event) *changeFold {
	f := &changeFold{id: changeID, findings: map[string]*row{}}
	for _, event := range events {
		payload, err := change.Decode(event)
		if err != nil {
			continue
		}
		f.apply(event, payload)
	}
	for i := range f.decisions {
		f.applyReason(f.decisions[i].payload)
	}
	for _, r := range f.findings {
		r.repo = f.repo
		r.outcome = outcomeOf(r.state, f.merged)
	}
	f.proposals = f.propose()
	return f
}

func (f *changeFold) apply(event change.Event, payload change.Payload) {
	switch p := payload.(type) {
	case change.Triggered:
		f.repo = p.Repo
	case change.BranchEnsured:
		f.base = p.BaseSHA
	case change.Merged:
		f.merged = true
	case change.Rejected:
		f.rejected = true
	case change.FindingFiled:
		f.fileFinding(event, change.FindingRecord(p))
	case change.FindingVerified:
		f.moveFinding(event, change.FindingRecord(p))
	case change.FindingStateChanged:
		f.moveFinding(event, change.FindingRecord(p))
	case change.DecisionRecorded:
		if event.Actor.Kind == change.ActorHuman {
			f.decisions = append(f.decisions, decision{event: event, payload: p})
		}
	}
}

func (f *changeFold) fileFinding(event change.Event, rec change.FindingRecord) {
	ref := rec.Ref.String()
	r := &row{change: f.id, ref: ref, agent: id.AgentName(rec.By), category: rec.Category, claim: rec.Claim,
		cardRefs: rec.CardRefs, state: rec.State, at: event.OccurredAt}
	if rec.Location != nil {
		r.file = rec.Location.File
	}
	if _, seen := f.findings[ref]; !seen {
		f.order = append(f.order, ref)
	}
	f.findings[ref] = r
}

func (f *changeFold) moveFinding(event change.Event, rec change.FindingRecord) {
	r, ok := f.findings[rec.Ref.String()]
	if !ok {
		return
	}
	r.state = rec.State
	r.at = event.OccurredAt
}

// applyReason sets the human reason of an uphold or dismiss on the finding it targets.
func (f *changeFold) applyReason(d change.DecisionRecorded) {
	if d.Kind != change.DecisionUphold && d.Kind != change.DecisionDismiss {
		return
	}
	if r, ok := f.findings[d.Target]; ok {
		r.reason = d.Reason
	}
}

func (f *changeFold) rows() []row {
	out := make([]row, 0, len(f.order))
	for _, ref := range f.order {
		out = append(out, *f.findings[ref])
	}
	return out
}

// outcomeOf maps the final state of a finding to its outcome. A finding still unanswered when the change merged was
// ignored.
func outcomeOf(state finding.State, merged bool) string {
	switch state {
	case finding.StateUpheld, finding.StateResolved, finding.StateFixClaimed, finding.StateVerifying:
		return OutcomeUseful
	case finding.StateDismissed, finding.StateWithdrawn:
		return OutcomeNoise
	case finding.StateSpecChanged:
		return OutcomeNeutral
	case finding.StateOpen, finding.StateUnverified, finding.StateDisputed, finding.StateEscalated:
		if merged {
			return OutcomeIgnored
		}
	case finding.StateProposed:
	}
	return OutcomePending
}
