package announce

import (
	"encoding/json"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// decode reads an event's payload into v. A payload that does not decode is never announced: a half-read line is
// worse than none.
func decode(ev change.Event, v any) bool {
	return json.Unmarshal(ev.Payload, v) == nil
}
