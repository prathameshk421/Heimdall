package bundle

import (
	"fmt"
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// BuildFromEvents builds the canonical role-scoped bundle from a change view.
// Event projection belongs here so every task-assignment path uses Build's
// role, ordering, reference and hashing rules.
func BuildFromEvents(role workflow.Role, changeID id.ChangeID, events []change.Event) (Bundle, error) {
	req := Request{Role: role, Change: string(changeID), BudgetTokens: roleBudget(role)}
	for _, ev := range events {
		if err := applyEvent(&req, ev); err != nil {
			return Bundle{}, fmt.Errorf("build %s context bundle from event %s: %w", role, ev.ID, err)
		}
	}
	return Build(req)
}

// BuildForCaseEvents binds a resolver's deterministic bundle to one challenged case.
func BuildForCaseEvents(changeID id.ChangeID, caseID id.CaseID, events []change.Event) (Bundle, error) {
	req := Request{Role: workflow.RoleResolver, Change: string(changeID), CaseID: string(caseID), BudgetTokens: roleBudget(workflow.RoleResolver)}
	for _, ev := range events {
		if ev.Type != change.EventCaseOpened {
			continue
		}
		p, err := change.Decode(ev)
		if err != nil {
			return Bundle{}, fmt.Errorf("decode case event %s: %w", ev.ID, err)
		}
		opened, ok := p.(change.CaseOpened)
		if ok && opened.CaseID == caseID {
			req.Criteria = append(req.Criteria, "Resolve finding "+opened.Finding.String())
		}
	}
	if len(req.Criteria) == 0 {
		return Bundle{}, fmt.Errorf("resolver case %s is not open", caseID)
	}
	return Build(req)
}

func applyEvent(req *Request, ev change.Event) error {
	p, err := change.Decode(ev)
	if err != nil {
		return fmt.Errorf("decode event: %w", err)
	}
	applyPayload(req, p)
	return nil
}

func applyPayload(req *Request, p change.Payload) {
	switch v := p.(type) {
	case change.Triggered:
		applyTriggered(req, v)
	case change.BranchEnsured:
		applyBranchEnsured(req, v)
	case change.PROpened:
		applyPROpened(req, v)
	case change.PRPushed:
		applyPRPushed(req, v)
	case change.PRClosed:
		applyPRClosed(req, v)
	case change.TestsSubmitted:
		applyTestsSubmitted(req, v)
	}
}

func applyTriggered(req *Request, p change.Triggered) {
	if p.Ticket != "" {
		req.Change = string(p.Ticket)
	}
	for _, criterion := range p.Criteria {
		req.Criteria = append(req.Criteria, criterion.ID)
		req.AcceptanceCriteria = append(req.AcceptanceCriteria, workflow.CriterionRef{ID: criterion.ID, Ref: criterion.Ref})
	}
}

func applyBranchEnsured(req *Request, p change.BranchEnsured) {
	req.BaseSHA = string(p.BaseSHA)
}

func applyPROpened(req *Request, p change.PROpened) {
	req.HeadSHA = string(p.HeadSHA)
}

func applyPRPushed(req *Request, p change.PRPushed) {
	req.HeadSHA = string(p.HeadSHA)
}

func applyPRClosed(req *Request, p change.PRClosed) {
	req.HeadSHA = string(p.HeadSHA)
}

func applyTestsSubmitted(req *Request, p change.TestsSubmitted) {
	if req.HeadSHA == "" && p.SHA != "" {
		req.HeadSHA = string(p.SHA)
	}
	if len(req.Criteria) == 0 && len(p.ACMap) > 0 {
		for criterion := range p.ACMap {
			req.Criteria = append(req.Criteria, criterion)
		}
		slices.Sort(req.Criteria)
	}
}

func roleBudget(role workflow.Role) int {
	switch role {
	case workflow.RoleTestAuthor:
		return 40000
	case workflow.RoleReviewer:
		return 60000
	case workflow.RoleResolver:
		return 30000
	case workflow.RoleProfiler:
		return 0
	default:
		return 0
	}
}
