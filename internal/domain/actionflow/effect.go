package actionflow

import (
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// Effect is what a row asks the gate to do. It is a plain value with no I/O: the use case runs it, records the event
// it produces and reads the view again. Every effect is safe to run twice (ACTION_MODE 2.5), so a run that crashes
// after the write and before the append repeats it once. The set is closed: only this package makes an Effect.
type Effect interface{ effect() }

// ResetState asks the gate to append state.reset with Reason. The log failed verification, so the gate starts a new
// log, reads every observed fact again and treats every produced fact as unknown (ACTION_MODE 2.7).
type ResetState struct{ Reason string }

// RunFast asks the gate to evaluate the fast check on Head with fastcheck.Evaluate and to record fast.reported.
type RunFast struct{ Head id.SHA }

// IngestExternal asks the gate to record each candidate as external.ingested. Candidates holds only those not yet in
// the log with the same content for Head.
type IngestExternal struct {
	Head       id.SHA
	Candidates []change.ExternalIngested
}

// SkipProof asks the gate to record proof.reported with the kind skipped_trivial at $0 for the head. The gate fills
// the run id and the time. Detail is the sentence of proof.Trivial.
type SkipProof struct {
	Head       id.SHA
	Base       id.SHA
	PolicyHash string
	Detail     string
}

// ReportProofUnknown asks the gate to record proof.reported with the kind unknown and Reason. WorkflowRun is the run
// being closed, or 0 when none is, and then the gate uses the id of its own run.
type ReportProofUnknown struct {
	Head        id.SHA
	Base        id.SHA
	PolicyHash  string
	Reason      proof.Reason
	WorkflowRun int64
}

// PostSummary asks the gate to write the proof summary comment for Head at Version. CommentID is the comment last
// known, or 0 when none is; the adapter then finds the comment by its marker and its author, and creates it only when
// none is there (ACTION_MODE 2.5).
type PostSummary struct {
	Head      id.SHA
	Version   int
	CommentID int64
}

// PostChecks asks the gate to post the two checks of Head. Proof is nil when no proof result covers the head: the
// proof check is then not posted.
type PostChecks struct {
	Head  id.SHA
	Fast  Check
	Proof *Check
}

// Check is one check run to post: its conclusion and the first line of its output.
type Check struct {
	Conclusion proof.Conclusion
	Headline   string
}

func (ResetState) effect()         {}
func (RunFast) effect()            {}
func (IngestExternal) effect()     {}
func (SkipProof) effect()          {}
func (ReportProofUnknown) effect() {}
func (PostSummary) effect()        {}
func (PostChecks) effect()         {}
