package change

import (
	"encoding/json"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// ProfileTrialPayload is the payload of a profile_trial effect (D175): run an approved profile proposal once in the
// isolated runner on a commit, and record the trial in the profile store.
type ProfileTrialPayload struct {
	Repo       Repo   `json:"repo"`
	ProposalID string `json:"proposal_id"`
	SHA        id.SHA `json:"sha"`
}

// Validate returns every problem with the payload.
func (p ProfileTrialPayload) Validate() error {
	var v problems
	v.add("repo", checkRepo(p.Repo))
	v.require("proposal_id", reHash.MatchString(p.ProposalID), "must be 64 lowercase hex characters")
	v.add("sha", checkSHA(p.SHA))
	return v.join()
}

// DecodeProfileTrialPayload strictly decodes and validates a profile_trial payload.
func DecodeProfileTrialPayload(raw json.RawMessage) (ProfileTrialPayload, error) {
	var p ProfileTrialPayload
	if err := decodeEffectPayload(raw, []string{"repo", "proposal_id", "sha"}, &p); err != nil {
		return ProfileTrialPayload{}, fmt.Errorf("profile_trial payload: %w", err)
	}
	return p, nil
}
