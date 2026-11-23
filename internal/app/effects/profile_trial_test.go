package effects_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type trialCall struct {
	proposal string
	sha      id.SHA
}

type fakeTrials struct {
	calls []trialCall
	err   error
}

func (f *fakeTrials) Trial(_ context.Context, proposalID string, sha id.SHA) (ports.ProfileTrial, error) {
	f.calls = append(f.calls, trialCall{proposalID, sha})
	return ports.ProfileTrial{Outcome: ports.TrialPassed, Passed: 1, SHA: sha}, f.err
}

func trialEffect(t *testing.T, p change.ProfileTrialPayload) change.Effect {
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	return change.Effect{Key: "pr-acme-pay-7/profile/x/r0", Kind: change.EffectProfileTrial, Target: string(p.Repo), Payload: raw}
}

// The profile_trial job runs the approved proposal once on the head; the store records the trial (D175).
func TestProfileTrialRunsTheProposalOnTheHead(t *testing.T) {
	t.Parallel()
	trials := &fakeTrials{}
	ex, err := effects.NewProfileTrial(effects.ProfileTrialDeps{Trials: trials})
	require.NoError(t, err)
	p := change.ProfileTrialPayload{Repo: "acme/pay", ProposalID: strings.Repeat("c", 64), SHA: id.SHA(strings.Repeat("a", 40))}

	require.NoError(t, ex.Execute(context.Background(), trialEffect(t, p)))
	require.Equal(t, []trialCall{{p.ProposalID, p.SHA}}, trials.calls)

	trials.err = errors.New("runner down")
	require.ErrorContains(t, ex.Execute(context.Background(), trialEffect(t, p)), "runner down")

	other := trialEffect(t, p)
	other.Kind = change.EffectRunChecks
	require.Error(t, ex.Execute(context.Background(), other), "an effect of another kind is refused")
}
