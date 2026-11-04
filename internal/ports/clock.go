package ports

import (
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// Clock is the only source of the current time for application code (CODING_STANDARDS 2 and 4). Tests use a fake
// clock; the domain never reads time itself. Adapter: the system clock in internal/platform (task T17).
type Clock interface {
	Now() time.Time
}

// IDs makes new identifiers (TECH_SPEC 3.1). Each method returns a well-formed id of its type, so no caller
// receives a malformed one. Adapter: a ULID generator and a UUID generator in internal/platform (task T17).
type IDs interface {
	NewFinding() id.FindingID
	NewRun() id.RunID
	NewCase() id.CaseID
	NewTask() id.TaskID
	NewEvent() change.EventID
}
