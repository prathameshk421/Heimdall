package agenttools

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// RecordProfileProposed records on the change that the profiler proposed a sandbox profile, which ends its task
// (D175). The proposal itself is already in the profile store; this is the fact the profile row reads.
func (s *Service) RecordProfileProposed(ctx context.Context, a Assignment, proposalID string, repo change.Repo) error {
	if a.Role != workflow.RoleProfiler {
		return fmt.Errorf("%w: role %s cannot propose a profile", ErrWrongRole, a.Role)
	}
	payload := change.ProfileProposed{TaskID: a.TaskID, ProposalID: proposalID, Repo: repo}
	if err := payload.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrValidation, err)
	}
	return s.appendEvents(ctx, a.ChangeID, func(existing []change.Event) ([]change.Event, error) {
		if err := checkTaskAgent(existing, a); err != nil {
			return nil, err
		}
		v, err := change.Fold(existing)
		if err != nil {
			return nil, fmt.Errorf("fold change %s: %w", a.ChangeID, err)
		}
		if !v.Tasks[a.TaskID].Live() {
			return nil, fmt.Errorf("%w: task %s is no longer live", ErrNoTask, a.TaskID)
		}
		eid := s.ids.NewEvent()
		ev, err := s.buildEventWithID(eid, a.ChangeID, int64(len(existing)+1), change.EventProfileProposed, a.Agent,
			correlationIDOf(existing, eid), lastCausation(existing), payload, nil)
		if err != nil {
			return nil, err
		}
		return []change.Event{ev}, nil
	})
}
