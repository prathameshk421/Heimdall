package change

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// Tracker write stages: after tests are committed, and after the pull request merges (F1).
const (
	TrackerStageCommit = "commit"
	TrackerStageMerge  = "merge"
)

// TrackerWritePayload is the payload of a tracker_write effect: comment on a ticket and optionally move its status (F1).
type TrackerWritePayload struct {
	Ticket     id.ChangeID `json:"ticket"`
	Stage      string      `json:"stage"`
	Comment    string      `json:"comment,omitempty"`
	Transition string      `json:"transition,omitempty"`
}

// Validate returns every problem with the payload.
func (p TrackerWritePayload) Validate() error {
	var v problems
	v.add("ticket", checkTicket(p.Ticket))
	v.require("stage", slices.Contains([]string{TrackerStageCommit, TrackerStageMerge}, p.Stage), "must be commit or merge")
	v.require("comment", p.Comment != "" || p.Transition != "", "comment or transition is required")
	if p.Comment != "" {
		v.add("comment", checkText(p.Comment))
	}
	if p.Transition != "" {
		v.add("transition", checkText(p.Transition))
	}
	return v.join()
}

// DecodeTrackerWritePayload strictly decodes and validates a tracker_write payload.
func DecodeTrackerWritePayload(raw json.RawMessage) (TrackerWritePayload, error) {
	var p TrackerWritePayload
	if err := decodeEffectPayload(raw, []string{"ticket", "stage"}, &p); err != nil {
		return TrackerWritePayload{}, fmt.Errorf("tracker_write payload: %w", err)
	}
	return p, nil
}
