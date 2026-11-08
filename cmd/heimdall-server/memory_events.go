package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/knowledge"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type workflowScopeResolver struct {
	store ports.EventStore
	vcs   vcsPort
}

func (r workflowScopeResolver) GroupsForEvent(ctx context.Context, event change.Event) ([]string, error) {
	p, identity, err := r.trustedPolicyAndActor(ctx, event)
	if err != nil {
		return nil, err
	}
	person, ok := mappedPerson(p, identity)
	if !ok {
		return nil, fmt.Errorf("workflow actor %q has no trusted person mapping", identity)
	}
	return append([]string(nil), p.People[person].Teams...), nil
}

func (r workflowScopeResolver) ExtractionModelForEvent(ctx context.Context, event change.Event) (string, error) {
	p, _, err := r.trustedPolicyAndActor(ctx, event)
	if err != nil {
		return "", err
	}
	if p.Models.Extraction == "" {
		return "", fmt.Errorf("trusted policy has no extraction model")
	}
	return p.Models.Extraction, nil
}

// trustedPolicyAndActor reads the policy from the trusted base revision and resolves the acting identity.
func (r workflowScopeResolver) trustedPolicyAndActor(ctx context.Context, event change.Event) (policy.Policy, string, error) {
	if r.store == nil || r.vcs == nil {
		return policy.Policy{}, "", fmt.Errorf("workflow policy scope dependencies are incomplete")
	}
	repo, base, assignee, err := r.workflowSource(ctx, event)
	if err != nil {
		return policy.Policy{}, "", err
	}
	p, err := r.trustedPolicy(ctx, repo, base)
	if err != nil {
		return policy.Policy{}, "", err
	}
	identity, err := actorIdentity(event, assignee)
	if err != nil {
		return policy.Policy{}, "", err
	}
	return p, identity, nil
}

// workflowSource returns the repository, trusted base and assignee recorded in the source workflow stream.
func (r workflowScopeResolver) workflowSource(ctx context.Context, event change.Event) (change.Repo, id.SHA, string, error) {
	stream, err := r.store.Load(ctx, event.ChangeID)
	if err != nil {
		return "", "", "", fmt.Errorf("load source workflow stream: %w", err)
	}
	repo, base, assignee, err := sourceFromStream(stream)
	if err != nil {
		return "", "", "", err
	}
	if repo == "" {
		return "", "", "", fmt.Errorf("workflow %s has no repository source", event.ChangeID)
	}
	if base == "" {
		base, err = r.vcs.DefaultHead(ctx, repo)
		if err != nil {
			return "", "", "", fmt.Errorf("resolve workflow policy base for %s: %w", repo, err)
		}
	}
	return repo, base, assignee, nil
}

func sourceFromStream(stream []change.Event) (change.Repo, id.SHA, string, error) {
	var repo change.Repo
	var base id.SHA
	var assignee string
	for _, item := range stream {
		if item.Type == change.EventChangeTriggered {
			payload, err := change.Decode(item)
			if err != nil {
				return "", "", "", fmt.Errorf("decode trigger source: %w", err)
			}
			triggered := payload.(change.Triggered)
			repo, assignee = triggered.Repo, string(triggered.Assignee)
		}
		if item.Type == change.EventBranchEnsured {
			payload, err := change.Decode(item)
			if err != nil {
				return "", "", "", fmt.Errorf("decode trusted workflow base: %w", err)
			}
			branch := payload.(change.BranchEnsured)
			if repo == "" {
				repo = branch.Repo
			}
			if base == "" {
				base = branch.BaseSHA
			}
		}
	}
	return repo, base, assignee, nil
}

func (r workflowScopeResolver) trustedPolicy(ctx context.Context, repo change.Repo, base id.SHA) (policy.Policy, error) {
	raw, err := r.vcs.ReadFile(ctx, repo, base, "heimdall.yaml")
	if err != nil {
		return policy.Policy{}, fmt.Errorf("read workflow memory policy at trusted base: %w", err)
	}
	p, err := policyyaml.Decode(raw)
	if err != nil {
		return policy.Policy{}, fmt.Errorf("decode workflow memory policy: %w", err)
	}
	return p, nil
}

func actorIdentity(event change.Event, assignee string) (string, error) {
	if event.Type == change.EventDecisionRecorded {
		payload, err := change.Decode(event)
		if err != nil {
			return "", fmt.Errorf("decode decision author: %w", err)
		}
		return string(payload.(change.DecisionRecorded).Login), nil
	}
	if event.Actor.ID != "" && (event.Actor.Kind == change.ActorHuman || event.Actor.Kind == change.ActorAgent) {
		return string(event.Actor.ID), nil
	}
	return assignee, nil
}

func mappedPerson(p policy.Policy, identity string) (string, bool) {
	for name, person := range p.People {
		if identity == name || identity == person.GitHub || identity == person.Chat || identity == person.Agent {
			return name, true
		}
	}
	return "", false
}

type workflowCursor struct{ table cursorTable }

func (c workflowCursor) LoadWorkflowCursor(ctx context.Context) (int64, error) {
	value, err := c.table.GetCursor(ctx, "memory:workflow")
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return 0, fmt.Errorf("load persisted memory sequence: %w", err)
	}
	if value == "" {
		return 0, nil
	}
	var seq int64
	if _, err := fmt.Sscan(value, &seq); err != nil || seq < 0 {
		return 0, fmt.Errorf("persisted memory sequence %q is invalid", value)
	}
	return seq, nil
}

func (c workflowCursor) SaveWorkflowCursor(ctx context.Context, sequence int64) error {
	if sequence < 0 {
		return fmt.Errorf("memory sequence cannot be negative")
	}
	if err := c.table.SetCursor(ctx, "memory:workflow", fmt.Sprint(sequence)); err != nil {
		return fmt.Errorf("persist memory sequence: %w", err)
	}
	return nil
}

var _ knowledge.WorkflowScopes = workflowScopeResolver{}
var _ knowledge.WorkflowModelResolver = workflowScopeResolver{}
var _ knowledge.WorkflowCursor = workflowCursor{}

// gateRefreshCursor stores how far the gate refresher has read the global event log.
type gateRefreshCursor struct{ table cursorTable }

// gateRefreshSource is the cursor key of the gate refresher.
const gateRefreshSource = "gate:refresh"

func (c gateRefreshCursor) LoadRefreshCursor(ctx context.Context) (int64, error) {
	value, err := c.table.GetCursor(ctx, gateRefreshSource)
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return 0, fmt.Errorf("load gate refresh sequence: %w", err)
	}
	if value == "" {
		return 0, nil
	}
	var seq int64
	if _, err := fmt.Sscan(value, &seq); err != nil || seq < 0 {
		return 0, fmt.Errorf("gate refresh sequence %q is invalid", value)
	}
	return seq, nil
}

func (c gateRefreshCursor) SaveRefreshCursor(ctx context.Context, sequence int64) error {
	if err := c.table.SetCursor(ctx, gateRefreshSource, fmt.Sprint(sequence)); err != nil {
		return fmt.Errorf("save gate refresh sequence: %w", err)
	}
	return nil
}
