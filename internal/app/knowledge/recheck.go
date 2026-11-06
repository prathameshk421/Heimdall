package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Recheck looks one memory reference up again for E3 verification. The groups are rebuilt from policy for the agent
// that cited the reference, so a finding cannot lean on memory its reviewer could not see. The lookup skips the
// answer cache: a re-query must see the memory as it is now (TECH_SPEC 11.1, D152). The result says whether the
// reference is found, confirmed and superseded; the E3 rule itself is verify.TierForE3.
func (s *SearchService) Recheck(ctx context.Context, agent id.AgentName, repo change.Repo, base id.SHA, ref string) (verify.MemoryState, error) {
	if agent == "" || strings.TrimSpace(ref) == "" {
		return verify.MemoryState{}, errors.New("memory re-query needs an agent and a reference")
	}
	query, err := s.scopedQuery(ctx, agent, repo, base, ports.MemoryQuery{Question: ref, Entities: []string{ref}})
	if err != nil {
		return verify.MemoryState{}, err
	}
	items, err := s.memory.Search(ctx, query)
	if err != nil {
		return verify.MemoryState{}, fmt.Errorf("search team memory: %w", err)
	}
	now := s.clock.Now()
	for i, item := range items {
		if err := item.Validate(); err != nil {
			return verify.MemoryState{}, fmt.Errorf("memory result %d is invalid: %w", i, err)
		}
		if item.Ref != ref {
			continue
		}
		return verify.MemoryState{
			Found:      true,
			Confirmed:  item.Label == ports.LabelConfirmed,
			Superseded: item.Label == ports.LabelSuperseded || (item.InvalidAt != nil && !item.InvalidAt.After(now)),
		}, nil
	}
	return verify.MemoryState{}, nil
}
