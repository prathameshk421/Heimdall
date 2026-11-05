package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// EnsureBranch is the step github.ensure_branch: it makes the test branch of a change once its room exists. Its with
// object is {"name": "<template>"}, and the name is optional.
type EnsureBranch struct {
	template string
}

var _ Step = EnsureBranch{}

// ensureBranchWith is the with object of github.ensure_branch.
type ensureBranchWith struct {
	Name *string `json:"name"`
}

// NewEnsureBranch reads the with object of the step and refuses one that does not validate. An empty object takes
// change.DefaultBranchTemplate.
func NewEnsureBranch(with json.RawMessage) (EnsureBranch, error) {
	var w ensureBranchWith
	if err := decodeWith(with, nil, &w); err != nil {
		return EnsureBranch{}, fmt.Errorf("github.ensure_branch with: %w", err)
	}
	template := change.DefaultBranchTemplate
	if w.Name != nil {
		template = *w.Name
	}
	if err := change.CheckBranchTemplate(template); err != nil {
		return EnsureBranch{}, fmt.Errorf("github.ensure_branch name: %w", err)
	}
	return EnsureBranch{template: template}, nil
}

// Type returns the do name of the step.
func (EnsureBranch) Type() string { return "github.ensure_branch" }

// Done reports whether the branch exists.
func (EnsureBranch) Done(c ChangeView) bool { return c.Has(change.EventBranchEnsured) }

// Start returns nothing: the branch is requested when room.opened arrives.
func (EnsureBranch) Start(context.Context, ChangeView, json.RawMessage) (Result, error) {
	return Result{}, nil
}

// OnEvent requests the branch on room.opened. The repository and the ticket come from change.triggered, which must be
// in the view.
func (s EnsureBranch) OnEvent(_ context.Context, c ChangeView, e change.Event) (Result, error) {
	if e.Type != change.EventRoomOpened || s.Done(c) {
		return Result{}, nil
	}
	trig, err := triggerOf(c)
	if err != nil {
		return Result{}, fmt.Errorf("github.ensure_branch: %w", err)
	}
	key, err := change.IdempotencyKey(c.ID, e.Version, change.EffectGitHubBranch, "branch")
	if err != nil {
		return Result{}, fmt.Errorf("github.ensure_branch: %w", err)
	}
	payload := change.BranchPayload{Repo: trig.Repo, Ticket: trig.Ticket, Template: s.template}
	if err := payload.Validate(); err != nil {
		return Result{}, fmt.Errorf("github.ensure_branch: %w", err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Result{}, fmt.Errorf("github.ensure_branch: encode payload: %w", err)
	}
	return Result{Effects: []change.Effect{{
		Key:     key,
		Kind:    change.EffectGitHubBranch,
		Target:  string(trig.Ticket),
		Payload: raw,
	}}}, nil
}

// OnTimeout returns nothing: the step has no deadline.
func (EnsureBranch) OnTimeout(context.Context, ChangeView) (Result, error) { return Result{}, nil }

// triggerOf returns the change.triggered payload of the view.
func triggerOf(c ChangeView) (change.Triggered, error) {
	for _, e := range c.Events {
		if e.Type != change.EventChangeTriggered {
			continue
		}
		p, err := change.Decode(e)
		if err != nil {
			return change.Triggered{}, fmt.Errorf("read %s: %w", e.Type, err)
		}
		trig, ok := p.(change.Triggered)
		if !ok {
			return change.Triggered{}, fmt.Errorf("payload of %s is not change.Triggered", e.Type)
		}
		return trig, nil
	}
	return change.Triggered{}, errors.New("no change.triggered in the view")
}
