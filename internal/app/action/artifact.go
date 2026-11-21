package action

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// The proof artifact is the one channel from an unprivileged proof run to the privileged gate: the run uploads it as a
// workflow artifact, and the gate reads it and records proof.reported and cost.recorded (ACTION_MODE 2.3, 2.7). It is
// untrusted input. The gate accepts it only when it names this run's head, base, policy hash and workflow run, when its
// version is one this package writes, when its body validates as a change.ProofReported, and when it fits the size
// bound. Every other case is unknown, never a pass.

// ErrProofArtifact means the artifact broke a rule of the format, so nothing in it is believed.
var ErrProofArtifact = errors.New("action: the proof artifact was refused")

// The artifact format.
const (
	// ArtifactVersion is the only version this package writes and reads. A file with another version is refused: a newer
	// run may mean something this build does not know, and guessing would be a way to pass.
	ArtifactVersion = 1
	// MaxArtifactBytes bounds the artifact, so the event the gate makes of it stays below the 64 KiB event limit
	// (ACTION_MODE 2.6).
	MaxArtifactBytes = 48 << 10
)

// ArtifactKey is what a proof artifact must say about itself to be believed: the input of the run that made it.
type ArtifactKey struct {
	Head        id.SHA
	Base        id.SHA
	PolicyHash  string
	WorkflowRun int64
}

// ArtifactCandidate is what became of one candidate in the artifact. CommentID names a third-party claim, Ref a
// finding the log already holds; a bug the review pass found has neither until the gate files it.
type ArtifactCandidate struct {
	CommentID int64            `json:"comment_id,omitempty"`
	Ref       string           `json:"ref,omitempty"`
	File      string           `json:"file,omitempty"`
	First     int              `json:"first,omitempty"`
	Last      int              `json:"last,omitempty"`
	Severity  string           `json:"severity,omitempty"`
	Claim     string           `json:"claim,omitempty"`
	Class     proof.Class      `json:"class"`
	Dropped   proof.DropReason `json:"dropped,omitempty"`
	Reason    proof.Reason     `json:"reason,omitempty"`
	Blocking  bool             `json:"blocking,omitempty"`
	// Why and Detail are what testintegrity said. They are for people and the log, never for a decision.
	Why    string `json:"why,omitempty"`
	Detail string `json:"detail,omitempty"`
	// Test is the repro test file the model wrote, when the candidate had one.
	Test string `json:"test,omitempty"`
}

// ProofArtifact is the versioned JSON a proof run uploads and the gate validates.
type ProofArtifact struct {
	Version     int                 `json:"version"`
	Head        id.SHA              `json:"head"`
	Base        id.SHA              `json:"base"`
	PolicyHash  string              `json:"policy_hash"`
	WorkflowRun int64               `json:"workflow_run"`
	Model       string              `json:"model,omitempty"`
	Kind        proof.Kind          `json:"kind"`
	Reason      proof.Reason        `json:"reason,omitempty"`
	Counts      change.ProofCounts  `json:"counts"`
	Cost        change.Cost         `json:"cost"`
	Candidates  []ArtifactCandidate `json:"candidates,omitempty"`
}

// encodeProofArtifact writes the artifact in its one form.
func encodeProofArtifact(a ProofArtifact) ([]byte, error) {
	a.Version = ArtifactVersion
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, fmt.Errorf("encode proof artifact: %w", err)
	}
	if len(raw) > MaxArtifactBytes {
		return nil, fmt.Errorf("encode proof artifact: %d bytes is over the %d byte limit", len(raw), MaxArtifactBytes)
	}
	return raw, nil
}

// ParseProofArtifact reads an artifact and checks it against the run the gate serves. Every error wraps
// ErrProofArtifact, and only a value that passes may be recorded.
func ParseProofArtifact(raw []byte, want ArtifactKey) (ProofArtifact, error) {
	if len(raw) == 0 {
		return ProofArtifact{}, fmt.Errorf("%w: it is empty", ErrProofArtifact)
	}
	if len(raw) > MaxArtifactBytes {
		return ProofArtifact{}, fmt.Errorf("%w: %d bytes is over the %d byte limit", ErrProofArtifact, len(raw), MaxArtifactBytes)
	}
	var a ProofArtifact
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return ProofArtifact{}, fmt.Errorf("%w: it is not the artifact format: %w", ErrProofArtifact, err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return ProofArtifact{}, fmt.Errorf("%w: it holds more than one JSON value", ErrProofArtifact)
	}
	if err := a.check(want); err != nil {
		return ProofArtifact{}, err
	}
	return a, nil
}

// check refuses an artifact that is not about the run the gate serves, or whose body cannot be true. The body rule is
// change.ProofReported's own, so the artifact and the event can never disagree on what a run may say.
func (a ProofArtifact) check(want ArtifactKey) error {
	switch {
	case a.Version != ArtifactVersion:
		return fmt.Errorf("%w: version %d is not the version this build reads (%d)", ErrProofArtifact, a.Version, ArtifactVersion)
	case a.Head != want.Head:
		return fmt.Errorf("%w: head %s is not the pull request head %s", ErrProofArtifact, a.Head, want.Head)
	case a.Base != want.Base:
		return fmt.Errorf("%w: base %s is not the merge base %s", ErrProofArtifact, a.Base, want.Base)
	case a.PolicyHash != want.PolicyHash:
		return fmt.Errorf("%w: policy hash %q is not the policy at the merge base", ErrProofArtifact, a.PolicyHash)
	case a.WorkflowRun != want.WorkflowRun:
		return fmt.Errorf("%w: workflow run %d is not run %d", ErrProofArtifact, a.WorkflowRun, want.WorkflowRun)
	}
	if len(a.Candidates) > change.MaxProofResults {
		return fmt.Errorf("%w: %d candidates is over the %d limit", ErrProofArtifact, len(a.Candidates), change.MaxProofResults)
	}
	if len(a.Candidates) != a.Counts.In {
		return fmt.Errorf("%w: it lists %d candidates and its counts say %d", ErrProofArtifact, len(a.Candidates), a.Counts.In)
	}
	for i, c := range a.Candidates {
		if err := c.check(); err != nil {
			return fmt.Errorf("%w: candidates[%d]: %w", ErrProofArtifact, i, err)
		}
	}
	if err := a.checkCounts(); err != nil {
		return err
	}
	if err := a.Payload().Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrProofArtifact, err)
	}
	return nil
}

// checkCounts requires the counts to be the tally of the candidates. Without it an artifact could say "blocked, one
// blocking finding" and list only drops: the counts drive the check conclusion, so the two must agree.
func (a ProofArtifact) checkCounts() error {
	var c proof.Counts
	for _, cand := range a.Candidates {
		c.In++
		switch cand.Class {
		case proof.ClassProven:
			c.Proven++
			if cand.Blocking {
				c.Blocking++
			} else {
				c.NeedsPerson++
			}
		case proof.ClassDropped:
			switch cand.Dropped {
			case proof.DropRefuted:
				c.Refuted++
			case proof.DropNoProof:
				c.NoProof++
			case proof.DropOutdated:
				c.Outdated++
			case proof.DropNoLocation:
				c.NoLocation++
			}
		case proof.ClassNotChecked:
			c.NotChecked++
		}
	}
	if c != a.Counts.Counts() {
		return fmt.Errorf("%w: its counts say %+v and its candidates add up to %+v", ErrProofArtifact, a.Counts, c)
	}
	return nil
}

// check refuses a candidate whose class, drop reason, not-checked reason and blocking flag do not fit each other. The
// rule is change.ProofReported's; it is repeated because a candidate the log does not hold yet has no ref and so never
// reaches that payload. A table test holds the two together.
func (c ArtifactCandidate) check() error {
	if err := c.checkClass(); err != nil {
		return err
	}
	return c.checkRef()
}

// checkClass holds the class against its drop reason, its not-checked reason and its blocking flag.
func (c ArtifactCandidate) checkClass() error {
	switch c.Class {
	case proof.ClassProven:
		if c.Dropped != "" || c.Reason != "" {
			return errors.New("a proven finding has no drop reason and no not-checked reason")
		}
	case proof.ClassDropped:
		if c.Reason != "" || c.Blocking || !validDrop(c.Dropped) {
			return errors.New("a dropped finding needs one drop reason and does not block")
		}
	case proof.ClassNotChecked:
		if c.Dropped != "" || c.Blocking || !notCheckedReason(c.Reason) {
			return errors.New("a finding not checked needs one not-checked reason and does not block")
		}
	default:
		return fmt.Errorf("class %q is not one this build writes", c.Class)
	}
	return nil
}

// checkRef checks the finding ref, when the candidate names one.
func (c ArtifactCandidate) checkRef() error {
	if c.Ref == "" {
		return nil
	}
	if _, err := id.ParseFindingRef(c.Ref); err != nil {
		return fmt.Errorf("ref: %w", err)
	}
	return nil
}

// validDrop is the four drop reasons of ACTION_MODE 6.3.
func validDrop(d proof.DropReason) bool {
	return slices.Contains([]proof.DropReason{proof.DropRefuted, proof.DropNoProof, proof.DropOutdated,
		proof.DropNoLocation}, d)
}

// notCheckedReason is the six reasons a candidate may carry when it was not checked (ACTION_MODE 6.3): by design, or
// for lack of budget, time, a key or isolation.
func notCheckedReason(r proof.Reason) bool {
	return slices.Contains([]proof.Reason{proof.ReasonTrivial, proof.ReasonNotProvable, proof.ReasonBudget,
		proof.ReasonDeadline, proof.ReasonForkNoKey, proof.ReasonCannotIsolate}, r)
}

// Outcome is the result the gate shows for the artifact.
func (a ProofArtifact) Outcome() proof.Outcome {
	return proof.Outcome{Kind: a.Kind, Reason: a.Reason, Counts: a.Counts.Counts(), Cost: a.Cost.Spend()}
}

// Payload is the log record of the artifact: one proof.reported. A candidate without a ref is only counted, because the
// payload names the findings the log already holds.
func (a ProofArtifact) Payload() change.ProofReported {
	p := change.ProofReported{WorkflowRun: a.WorkflowRun, Head: a.Head, Base: a.Base, PolicyHash: a.PolicyHash,
		Model: a.Model, Kind: a.Kind, Reason: a.Reason, Counts: a.Counts, Cost: a.Cost}
	for _, c := range a.Candidates {
		ref, err := id.ParseFindingRef(c.Ref)
		if err != nil {
			continue
		}
		p.Results = append(p.Results, change.CandidateResult{Ref: ref, Class: c.Class, Dropped: c.Dropped,
			Reason: c.Reason, Blocking: c.Blocking})
	}
	return p
}

// CostRecorded is the cost.recorded payload of the artifact.
func (a ProofArtifact) CostRecorded() change.CostRecorded {
	return change.CostRecorded{WorkflowRun: a.WorkflowRun, Model: a.Model, Cost: a.Cost}
}

// ArtifactOutcome is the gate's read of a withheld or unreadable artifact: always unknown, never a pass. The reason is
// no result, because a refused artifact says nothing a check can stand on (ACTION_MODE 2.7, 5.4).
func ArtifactOutcome(raw []byte, want ArtifactKey) proof.Outcome {
	a, err := ParseProofArtifact(raw, want)
	if err != nil {
		return proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonNoResult}
	}
	return a.Outcome()
}
