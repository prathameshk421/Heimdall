package knowledge_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/knowledge"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type stubPolicyCode struct{ err error }

func (s stubPolicyCode) ReadFile(context.Context, change.Repo, id.SHA, string) ([]byte, error) {
	return nil, s.err
}

func TestPolicyEventTeamsMissingPolicyFailsClosedWithoutError(t *testing.T) {
	decode := func([]byte) (policy.Policy, error) { return policy.Policy{}, nil }
	ev := change.Event{Actor: change.Actor{Kind: change.ActorHuman, ID: "alice"}}

	teams, err := (knowledge.PolicyEventTeams{Code: stubPolicyCode{err: ports.ErrNotFound}, Decode: decode}).
		TeamsForEvent(context.Background(), ev, "org/legacy-repo", "abc123", nil)
	require.NoError(t, err)
	require.Empty(t, teams)

	_, err = (knowledge.PolicyEventTeams{Code: stubPolicyCode{err: errors.New("boom")}, Decode: decode}).
		TeamsForEvent(context.Background(), ev, "org/legacy-repo", "abc123", nil)
	require.Error(t, err)
}
