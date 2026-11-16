package change

import "github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"

// The checks area of the event catalog (TECH_SPEC 3.2): results of check runs on a commit.
const (
	EventCIReported EventType = "ci.reported"
)

// checksPayloads is the registry of the checks area.
func checksPayloads() map[EventType]payloadEntry {
	return map[EventType]payloadEntry{
		EventCIReported: entry[CIReported](),
	}
}

// CIReported is the payload of ci.reported: a check run on a commit finished (TECH_SPEC 3.2).
type CIReported struct {
	Check      string       `json:"check"`
	SHA        id.SHA       `json:"sha"`
	Conclusion CIConclusion `json:"conclusion"`
}

// Validate returns every problem with the payload.
func (p CIReported) Validate() error {
	var v problems
	v.add("check", checkText(p.Check))
	v.add("sha", checkSHA(p.SHA))
	v.add("conclusion", checkEnum(p.Conclusion, p.Conclusion.Valid()))
	return v.join()
}

// CIConclusion is the final result of one check run on a commit (GitHub check_run conclusions).
type CIConclusion string

// Valid reports whether c is a conclusion.
func (c CIConclusion) Valid() bool {
	return oneOf(c, CISuccess, CIFailure, CINeutral, CICancelled, CISkipped, CITimedOut, CIActionRequired)
}

// The conclusions a completed check run can have.
const (
	CISuccess        CIConclusion = "success"
	CIFailure        CIConclusion = "failure"
	CINeutral        CIConclusion = "neutral"
	CICancelled      CIConclusion = "cancelled"
	CISkipped        CIConclusion = "skipped"
	CITimedOut       CIConclusion = "timed_out"
	CIActionRequired CIConclusion = "action_required"
)
