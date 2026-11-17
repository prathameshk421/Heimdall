package change

import (
	"encoding/json"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// maxRunChecks bounds the checks one job runs.
const maxRunChecks = 64

// RunChecksPayload is the payload of a run_checks effect: which profile checks to run on a head. Base is the pull
// request's base commit; a check with a baseline compares the head with it.
type RunChecksPayload struct {
	Repo     Repo        `json:"repo"`
	Change   id.ChangeID `json:"change"`
	Head     id.SHA      `json:"head"`
	Base     id.SHA      `json:"base,omitempty"`
	CheckIDs []string    `json:"check_ids"`
}

// Validate returns every problem with the payload.
func (p RunChecksPayload) Validate() error {
	var v problems
	v.add("repo", checkRepo(p.Repo))
	_, err := id.ParseChangeID(string(p.Change))
	v.add("change", err)
	v.add("head", checkSHA(p.Head))
	if p.Base != "" {
		v.add("base", checkSHA(p.Base))
	}
	v.add("check_ids", checkAtLeastOne(len(p.CheckIDs)))
	v.require("check_ids", len(p.CheckIDs) <= maxRunChecks, fmt.Sprintf("has more than %d entries", maxRunChecks))
	v.each("check_ids", p.CheckIDs, checkText)
	return v.join()
}

// DecodeRunChecksPayload strictly decodes and validates a run_checks payload.
func DecodeRunChecksPayload(raw json.RawMessage) (RunChecksPayload, error) {
	var p RunChecksPayload
	if err := decodeEffectPayload(raw, []string{"repo", "change", "head", "check_ids"}, &p); err != nil {
		return RunChecksPayload{}, fmt.Errorf("run_checks payload: %w", err)
	}
	return p, nil
}
