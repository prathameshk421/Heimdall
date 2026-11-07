package change

import (
	"encoding/json"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// MaxCheckSummary is the most text the summary of a check run may carry.
const MaxCheckSummary = 2000

// CheckStatus is the state of a check run. The values are GitHub's, and the same strings as ports.CheckStatus, which
// the domain cannot import.
type CheckStatus string

// The three states of a check run.
const (
	CheckStatusQueued     CheckStatus = "queued"
	CheckStatusInProgress CheckStatus = "in_progress"
	CheckStatusCompleted  CheckStatus = "completed"
)

// CheckPayload is the payload of a github_check effect. It is the one shape every producer (the intake slop check, the
// gate) writes and the executor reads, so the producers and the executor cannot drift apart. The repository and the
// commit are in the payload because the executor needs both to post the check, and the effect key is the check's
// external id.
type CheckPayload struct {
	Repo       Repo         `json:"repo"`
	SHA        id.SHA       `json:"sha"`
	Name       string       `json:"name"`
	Status     CheckStatus  `json:"status"`
	Conclusion CIConclusion `json:"conclusion,omitempty"`
	Summary    string       `json:"summary"`
}

// Validate returns every problem with the payload.
func (p CheckPayload) Validate() error {
	var v problems
	v.add("repo", checkRepo(p.Repo))
	v.add("sha", checkSHA(p.SHA))
	v.add("name", checkText(p.Name))
	switch p.Status {
	case CheckStatusCompleted:
		v.require("conclusion", p.Conclusion.Valid(), "must be a valid conclusion when the status is completed")
	case CheckStatusInProgress, CheckStatusQueued:
		v.require("conclusion", p.Conclusion == "", "must be empty until the check completes")
	default:
		v.require("status", false, "must be queued, in_progress or completed")
	}
	v.require("summary", len(p.Summary) <= MaxCheckSummary, fmt.Sprintf("is longer than %d bytes", MaxCheckSummary))
	return v.join()
}

// DecodeCheckPayload strictly decodes and validates a github_check payload: one JSON object, every required field
// present, no unknown field, nothing after the object.
func DecodeCheckPayload(raw json.RawMessage) (CheckPayload, error) {
	var p CheckPayload
	if err := decodeEffectPayload(raw, []string{"repo", "sha", "name", "status", "summary"}, &p); err != nil {
		return CheckPayload{}, fmt.Errorf("github_check payload: %w", err)
	}
	return p, nil
}
