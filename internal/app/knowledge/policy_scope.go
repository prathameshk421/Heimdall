package knowledge

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// PolicyDecoder is supplied by the policy YAML adapter, keeping YAML outside app.
type PolicyDecoder func([]byte) (policy.Policy, error)

// PolicyCode is the one code-reading capability needed for scope resolution.
type PolicyCode interface {
	ReadFile(context.Context, change.Repo, id.SHA, string) ([]byte, error)
}

// PolicyScopes reads access rules from the change base, never the untrusted PR head.
type PolicyScopes struct {
	Code   PolicyCode
	Decode PolicyDecoder
}

// PolicyFileApproval authorizes document decisions from the policy at the document's captured default-branch SHA.
type PolicyFileApproval struct {
	Code   PolicyCode
	Decode PolicyDecoder
}

// ApproveKnowledgeFile requires a mapped policy owner or approver of the document's server-assigned group.
func (a PolicyFileApproval) ApproveKnowledgeFile(ctx context.Context, actor string, doc FileDoc) error {
	if a.Code == nil || a.Decode == nil || actor == "" || doc.Repository == "" || doc.SHA == "" || doc.Group == "" {
		return errors.New("knowledge approval is not configured")
	}
	raw, err := a.Code.ReadFile(ctx, doc.Repository, doc.SHA, "heimdall.yaml")
	if err != nil {
		return fmt.Errorf("read knowledge approval policy: %w", err)
	}
	p, err := a.Decode(raw)
	if err != nil {
		return fmt.Errorf("decode knowledge approval policy: %w", err)
	}
	if slices.Contains(p.PolicyOwners, actor) || slices.Contains(p.Approvers[doc.Group], actor) {
		return nil
	}
	return fmt.Errorf("person %q is not a policy owner or approver for group %q", actor, doc.Group)
}

// GroupsForAgent resolves team groups in the policy at the merge base and fails closed on any missing or invalid file.
// An agent with no person in the policy gets no team groups.
func (s PolicyScopes) GroupsForAgent(ctx context.Context, agent id.AgentName, repo change.Repo, base id.SHA) ([]string, error) {
	if s.Code == nil || s.Decode == nil {
		return nil, errors.New("knowledge policy reader is not configured")
	}
	raw, err := s.Code.ReadFile(ctx, repo, base, "heimdall.yaml")
	if err != nil {
		return nil, fmt.Errorf("read memory policy at base: %w", err)
	}
	p, err := s.Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("decode memory policy at base: %w", err)
	}
	for _, person := range p.People {
		if person.Agent == string(agent) {
			return append([]string(nil), person.Teams...), nil
		}
	}
	// An agent no person owns has no team memory: it sees only the shared group, which every caller adds.
	return nil, nil
}

// PersonForAgent resolves the policy person bound to an agent at the trusted base SHA.
// Unlike GroupsForAgent it fails closed: approval and forget need a real person.
func (s PolicyScopes) PersonForAgent(ctx context.Context, agent id.AgentName, repo change.Repo, base id.SHA) (string, policy.Policy, error) {
	if s.Code == nil || s.Decode == nil {
		return "", policy.Policy{}, errors.New("knowledge policy reader is not configured")
	}
	raw, err := s.Code.ReadFile(ctx, repo, base, "heimdall.yaml")
	if err != nil {
		return "", policy.Policy{}, fmt.Errorf("read memory policy at base: %w", err)
	}
	p, err := s.Decode(raw)
	if err != nil {
		return "", policy.Policy{}, fmt.Errorf("decode memory policy at base: %w", err)
	}
	for name, person := range p.People {
		if person.Agent == string(agent) {
			return name, p, nil
		}
	}
	return "", policy.Policy{}, fmt.Errorf("agent %q has no person mapping in base policy", agent)
}
