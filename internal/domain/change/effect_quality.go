package change

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// TestQualityPayload is the payload of a test_quality effect.
type TestQualityPayload struct {
	Repo          Repo             `json:"repo"`
	Branch        string           `json:"branch"`
	Ticket        id.ChangeID      `json:"ticket"`
	SubmissionRef workflow.BlobRef `json:"submission_ref"`
	// BaseSHA is the branch's base commit, the one the tests must fail on. HeadSHA is the pull request's head when
	// there is one: the tests must then pass there. Effects made before these fields carry neither.
	BaseSHA id.SHA `json:"base_sha,omitempty"`
	HeadSHA id.SHA `json:"head_sha,omitempty"`
}

// Validate returns every problem with the payload.
func (p TestQualityPayload) Validate() error {
	var v problems
	v.add("repo", checkRepo(p.Repo))
	if p.Branch == "" {
		v.add("branch", errors.New("is required"))
	}
	_, err := id.ParseChangeID(string(p.Ticket))
	v.add("ticket", err)
	v.add("submission_ref", checkBlob(p.SubmissionRef))
	if p.BaseSHA != "" {
		v.add("base_sha", checkSHA(p.BaseSHA))
	}
	if p.HeadSHA != "" {
		v.add("head_sha", checkSHA(p.HeadSHA))
	}
	return v.join()
}

// DecodeTestQualityPayload strictly decodes and validates a test_quality payload.
func DecodeTestQualityPayload(raw json.RawMessage) (TestQualityPayload, error) {
	var p TestQualityPayload
	if err := decodeEffectPayload(raw, []string{"repo", "branch", "ticket", "submission_ref"}, &p); err != nil {
		return TestQualityPayload{}, fmt.Errorf("test_quality payload: %w", err)
	}
	return p, nil
}
