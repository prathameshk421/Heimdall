package effects

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// ProfileTrials runs an approved proposal once in the isolated runner and records the trial in the profile store. The
// profile lifecycle service satisfies it.
type ProfileTrials interface {
	Trial(ctx context.Context, proposalID string, sha id.SHA) (ports.ProfileTrial, error)
}

// ProfileTrialDeps are the dependencies of ProfileTrial.
type ProfileTrialDeps struct {
	Trials ProfileTrials
}

// ProfileTrial performs profile_trial effects (D175). The trial's outcome lives in the profile store, which the profile
// row reads; a failed trial is a recorded outcome, not a failed job.
type ProfileTrial struct {
	deps ProfileTrialDeps
}

// NewProfileTrial builds the executor, or refuses when a dependency is missing.
func NewProfileTrial(d ProfileTrialDeps) (ProfileTrial, error) {
	if d.Trials == nil {
		return ProfileTrial{}, fmt.Errorf("profile_trial executor: missing trials")
	}
	return ProfileTrial{deps: d}, nil
}

// Execute runs the trial.
func (p ProfileTrial) Execute(ctx context.Context, fx change.Effect) error {
	if fx.Kind != change.EffectProfileTrial {
		return failf(nil, "profile_trial executor: effect %s has kind %s", fx.Key, fx.Kind)
	}
	payload, err := change.DecodeProfileTrialPayload(fx.Payload)
	if err != nil {
		return failf(err, "profile_trial effect %s: payload refused: %v", fx.Key, err)
	}
	if _, err := p.deps.Trials.Trial(ctx, payload.ProposalID, payload.SHA); err != nil {
		return failf(err, "profile_trial %s: run the trial of %s on %s: %v", fx.Key, payload.ProposalID, payload.SHA, err)
	}
	return nil
}
