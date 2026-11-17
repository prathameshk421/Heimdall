package change

import (
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/sarif"
)

// MaxCheckResults bounds the results one checks.reported keeps; the rest are counted in More.
const MaxCheckResults = 200

// The checks area of the event catalog (TECH_SPEC 3.2): results of check runs on a commit.
const (
	EventCIReported     EventType = "ci.reported"
	EventChecksReported EventType = "checks.reported"
)

// checksPayloads is the registry of the checks area.
func checksPayloads() map[EventType]payloadEntry {
	return map[EventType]payloadEntry{
		EventCIReported:     entry[CIReported](),
		EventChecksReported: entry[ChecksReported](),
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

// ChecksReported is the payload of checks.reported: the profile checks ran on a head (HARNESS_DESIGN B7). Results are
// the new results only (a check with a baseline reports what the head added), at most MaxCheckResults; More counts
// the rest. A check that could not run is an Errors entry, not a failure of the job.
type ChecksReported struct {
	Head    id.SHA         `json:"head"`
	Results []sarif.Result `json:"results"`
	Errors  []CheckError   `json:"errors"`
	More    int            `json:"more"`
}

// CheckError is one check that could not run, and why.
type CheckError struct {
	Check  string `json:"check"`
	Reason string `json:"reason"`
}

// Validate returns every problem with the payload.
func (p ChecksReported) Validate() error {
	var v problems
	v.add("head", checkSHA(p.Head))
	v.require("results", len(p.Results) <= MaxCheckResults, fmt.Sprintf("must hold at most %d items", MaxCheckResults))
	v.require("more", p.More >= 0, "must not be negative")
	v.each("errors", p.Errors, func(e CheckError) error {
		var w problems
		w.add("check", checkText(e.Check))
		w.add("reason", checkText(e.Reason))
		return w.join()
	})
	return v.join()
}
