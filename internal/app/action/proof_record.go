package action

import (
	"context"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// ProofRecordIn names the proof run whose result the gate records.
type ProofRecordIn struct {
	ChangeID id.ChangeID
	Repo     change.Repo
	Number   int
	// WorkflowRun is the GitHub Actions run that uploaded the artifact.
	WorkflowRun int64
	// Artifact is the uploaded proof artifact, or nil when the run uploaded none. An artifact that is missing, refused
	// or about another run is recorded as unknown: no result, never as a pass (ACTION_MODE 2.7, 5.4).
	Artifact []byte
}

// ProofRecord is what the gate recorded for one proof run.
type ProofRecord struct {
	// Accepted is true only when the artifact passed every check.
	Accepted bool
	Outcome  proof.Outcome
	// Appended are the event types written, in order.
	Appended []change.EventType
	Version  int64
	// Refused names why the artifact was not believed, as text for the caller's log. It holds no file content.
	Refused string
}

// RecordProof records the result of one proof run: proof.reported and, when the run spent money, cost.recorded. It reads
// the log, checks the artifact against the run the gate serves, and appends what the artifact claimed. A refused
// artifact becomes proof.reported with the kind unknown and the reason no result, so a lost or forged upload can never
// leave the proof check passing. A repeat on the same input appends nothing (ACTION_MODE 2.5).
func (g *Gate) RecordProof(ctx context.Context, in ProofRecordIn) (ProofRecord, error) {
	for range maxRunTries {
		res, err := g.recordProofOnce(ctx, in)
		if !errors.Is(err, ports.ErrVersionConflict) {
			return res, err
		}
	}
	return ProofRecord{}, fmt.Errorf("action proof record on %s: still conflicting after %d tries", in.ChangeID, maxRunTries)
}

// recordProofOnce is one attempt.
func (g *Gate) recordProofOnce(ctx context.Context, in ProofRecordIn) (ProofRecord, error) {
	events, err := g.load(ctx, in.ChangeID)
	if err != nil {
		return ProofRecord{}, err
	}
	view, err := change.Fold(events)
	if err != nil {
		return ProofRecord{}, fmt.Errorf("action proof record on %s: fold: %w", in.ChangeID, err)
	}
	if view.PR == nil {
		return ProofRecord{Refused: "the log holds no pull request for this change"}, nil
	}
	r := &run{g: g, in: RunIn{ChangeID: in.ChangeID, Repo: in.Repo, Number: in.Number}, events: events, view: view,
		head: view.PR.HeadSHA, base: view.PR.Base}
	r.readPolicy(ctx)
	want := ArtifactKey{Head: r.head, Base: r.base, PolicyHash: r.hash, WorkflowRun: in.WorkflowRun}
	a, parseErr := ParseProofArtifact(in.Artifact, want)
	rec := ProofRecord{Accepted: parseErr == nil}
	if parseErr != nil {
		rec.Refused = parseErr.Error()
		a = unknownArtifact(r, want)
	}
	if err := r.applyBlocking(&a); err != nil {
		// The artifact's blocking flags disagree with the policy at the merge base. The policy is the trusted input, so
		// the artifact is refused rather than corrected (ACTION_MODE 2.7: the artifact is untrusted data).
		rec.Accepted, rec.Refused = false, err.Error()
		a = unknownArtifact(r, want)
	}
	rec.Outcome = a.Outcome()
	if currentReport(r.view, a) {
		rec.Version = int64(len(r.events))
		return rec, nil
	}
	if err := r.recordArtifact(ctx, a); err != nil {
		return rec, err
	}
	rec.Appended = r.res.Appended
	rec.Version = int64(len(r.events))
	return rec, nil
}

// recordArtifact appends the records of one proof result: proof.reported and, when the run spent money, cost.recorded.
// It is the one write of a proof result. RecordProof calls it for an artifact the gate read, and Gate.Run calls it for
// the result a row asks it to make itself (a trivial skip, an unknown), so the log holds the same events either way.
func (r *run) recordArtifact(ctx context.Context, a ProofArtifact) error {
	evs, err := r.proofEvents(a)
	if err != nil {
		return err
	}
	return r.appendEvents(ctx, evs...)
}

// unknownArtifact is the record of a run whose artifact the gate does not believe: unknown, no result, no cost. The
// model id is kept from the run's proof.started when the log holds one, so the summary can still name it.
func unknownArtifact(r *run, want ArtifactKey) ProofArtifact {
	a := ProofArtifact{Head: want.Head, Base: want.Base, PolicyHash: want.PolicyHash, WorkflowRun: want.WorkflowRun,
		Kind: proof.KindUnknown, Reason: proof.ReasonNoResult}
	if p := r.view.Action; p != nil && p.Proof != nil && p.Proof.Head == want.Head {
		a.Model = p.Proof.Model
	}
	return a
}

// applyBlocking checks every proven candidate's blocking flag against the policy at the merge base. The flag drives the
// check conclusion, and the artifact is untrusted, so the gate recomputes it with gate.BlockingSet and refuses a
// disagreement: a writer who uploaded a forged artifact cannot make a finding pass, and cannot hide one behind a false
// flag either.
func (r *run) applyBlocking(a *ProofArtifact) error {
	for i := range a.Candidates {
		c := &a.Candidates[i]
		if c.Class != proof.ClassProven {
			continue
		}
		sev, err := finding.ParseSeverity(c.Severity)
		if err != nil {
			return fmt.Errorf("%w: candidates[%d].severity: %w", ErrProofArtifact, i, err)
		}
		want := provenBlocks(r.pol, sev)
		if c.Blocking != want {
			return fmt.Errorf("%w: candidates[%d] says blocking=%t and the policy says %t", ErrProofArtifact, i, c.Blocking, want)
		}
	}
	return nil
}

// provenBlocks is the policy's blocking rule for one proven E1 finding. The proof run applies the same rule when it
// builds the artifact, so the two can never disagree; a policy the gate could not read blocks every row of the table,
// so the summary never passes on one either.
func provenBlocks(pol policy.Policy, sev finding.Severity) bool {
	states := []gate.FindingState{{Tier: finding.TierE1, Severity: sev, State: finding.StateOpen}}
	return len(gate.BlockingSet(states, pol.Blocking)) > 0
}

// currentReport reports whether the log already holds this run's finished proof.reported for these inputs.
func currentReport(view change.View, a ProofArtifact) bool {
	p := view.Action
	if p == nil || p.Proof == nil || !p.Proof.Done {
		return false
	}
	return p.Proof.WorkflowRun == a.WorkflowRun && p.Proof.Head == a.Head && p.Proof.Base == a.Base &&
		p.Proof.PolicyHash == a.PolicyHash
}

// proofEvents builds the records a proof run leaves: proof.reported and, when the run spent money, cost.recorded. A skip
// costs nothing, so it writes one event (ACTION_MODE 7).
func (r *run) proofEvents(a ProofArtifact) ([]change.Event, error) {
	reported, err := r.build(change.EventProofReported, a.Payload(), 0)
	if err != nil {
		return nil, fmt.Errorf("action proof record on %s: proof.reported: %w", r.in.ChangeID, err)
	}
	out := []change.Event{reported}
	if a.Cost == (change.Cost{}) {
		return out, nil
	}
	cost, err := r.build(change.EventCostRecorded, a.CostRecorded(), 1)
	if err != nil {
		return nil, fmt.Errorf("action proof record on %s: cost.recorded: %w", r.in.ChangeID, err)
	}
	return append(out, cost), nil
}
