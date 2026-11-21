package action

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testintegrity"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The limits of one proof run. The wall time and the budgets are the design's (ACTION_MODE 5.2, 7); the rest bound what
// a model, a pull request or a sandbox can make this package hold.
const (
	// proofTimeout is the wall time of one proof run (ACTION_MODE 5.2: 15 minutes).
	proofTimeout = 15 * time.Minute
	// maxProofTurns bounds the model answers of one run. A turn is one answer, whatever it asks for.
	maxProofTurns = 40
	// unitTimeout bounds one sandbox run of one package's tests. The port's own limit is 10 minutes.
	unitTimeout = 5 * time.Minute
	// maxProposals bounds the findings one run proves. Each proposal costs up to three sandbox runs.
	maxProposals = 10
	// maxPromptFiles bounds the changed files listed in the prompt.
	maxPromptFiles = 100
)

// OverlaySandbox is the sandbox port with the overlay extension: it writes files into the tree before it runs. A repro
// test is an overlay, so a proof needs it. internal/app/effects declares the same shape for the server's verification;
// R3 keeps this package from importing that one, so the interface is repeated here.
type OverlaySandbox interface {
	ports.Sandbox
	RunOverlay(ctx context.Context, j ports.SandboxJob, overlays []ports.File) (ports.SandboxResult, error)
}

// ProofConfig wires one proof use case. Every field is required: a run without an agent loop, a sandbox or a code
// reader could prove nothing.
type ProofConfig struct {
	// Loop is the one agent loop of the product (internal/app/agentloop). It holds the model and its price, and it
	// refuses a model call whose worst case could pass the run's caps, so this package never sees an unpaid call.
	Loop    ports.AgentLoop
	Sandbox OverlaySandbox
	// Code reads the base version of a file, for the tree with the suspected lines reverted.
	Code  ports.CodeReader
	Clock ports.Clock
	// ModelName is the model id the run sends to the loop and records.
	ModelName string
}

// Claim is one third-party comment the ingest step turned into a candidate (ACTION_MODE 6). The proof run gives it to
// the model as a claim to try to prove. It is never evidence by itself.
type Claim struct {
	Source    external.Tool
	CommentID int64
	Location  finding.Location
	Claim     string
	Severity  finding.Severity
}

// ExistingFinding is a finding the log already holds, so a proof result can name its ref. A candidate the log does not
// hold yet has no ref until the gate files it.
type ExistingFinding struct {
	Ref      id.FindingRef
	Location finding.Location
	Claim    string
	Severity finding.Severity
}

// ProofIn is the input of one proof run. Every fact it needs is a value: this package reads no clock of its own, no
// state ref and no GitHub.
type ProofIn struct {
	ChangeID id.ChangeID
	Repo     change.Repo
	Number   int
	// Head, Base and PolicyHash are the run's input. The gate accepts a result only for the same three (2.5).
	Head        id.SHA
	Base        id.SHA
	PolicyHash  string
	WorkflowRun int64
	// Action is the action area at the merge base, and Policy the whole policy there for the blocking rule.
	Action policy.Action
	Policy policy.Policy
	// Files are the changed files, as GitHub reports them: the trivial rule and the tool list read them.
	Files []ports.ChangedFile
	// Checkout is the directory of the head tree. The tools read nothing outside it.
	Checkout string
	// Claims are the third-party candidates, and Existing the findings the log already holds.
	Claims   []Claim
	Existing []ExistingFinding
	// MonthKnown and MonthSpentUSD are the monthly spend before this run. A month total that was not read is not
	// believed, and then no monthly stop is made: the provider's own limit is the hard stop.
	MonthKnown    bool
	MonthSpentUSD float64
}

// validate refuses an input the run could not make a sound result from. The policy hash and the run id are checked by
// change.ProofReported itself, so a result cannot be built for an input the log will refuse.
func (in ProofIn) validate() error {
	if _, err := id.ParseChangeID(string(in.ChangeID)); err != nil {
		return fmt.Errorf("proof: %w", err)
	}
	if _, err := id.ParseSHA(string(in.Head)); err != nil {
		return fmt.Errorf("proof: head: %w", err)
	}
	if _, err := id.ParseSHA(string(in.Base)); err != nil {
		return fmt.Errorf("proof: base: %w", err)
	}
	if err := (change.ProofReported{WorkflowRun: in.WorkflowRun, Head: in.Head, Base: in.Base,
		PolicyHash: in.PolicyHash, Model: "proof", Kind: proof.KindSkippedTrivial}).Validate(); err != nil {
		return fmt.Errorf("proof: input: %w", err)
	}
	if in.Checkout == "" {
		return errors.New("proof: the head checkout is required: the tools read only it")
	}
	return nil
}

// CandidateOutcome is what became of one candidate. CommentID names a third-party claim, Ref a finding the log already
// holds; a bug the review pass found has neither until the gate files it.
type CandidateOutcome struct {
	CommentID int64
	Ref       id.FindingRef
	Location  finding.Location
	Claim     string
	Severity  finding.Severity
	Class     proof.Class
	Dropped   proof.DropReason
	// Reason is why the candidate was not checked. Only ClassNotChecked carries one.
	Reason proof.Reason
	// Why and Detail are what testintegrity said, for people and the log.
	Why    testintegrity.ProofWhy
	Detail string
	// Test is the repro test file that proved or failed to prove the candidate.
	Test   string
	Blocks bool
}

// ProofResult is what one proof run did: the outcome the gate reads, the per-candidate results, the spend and the
// artifact bytes to upload. The artifact is always filled, so a run that stopped early still reports "unknown: reason".
type ProofResult struct {
	Head        id.SHA
	Base        id.SHA
	PolicyHash  string
	WorkflowRun int64
	Model       string
	Outcome     proof.Outcome
	Spend       proof.Spend
	Candidates  []CandidateOutcome
	// Artifact is the versioned JSON the gate validates and records (artifact.go).
	Artifact []byte
	// Trivial is the skip decision. It is why a run made no model call.
	Trivial proof.Decision
	// Turns and Stop say how the loop ended, and StopDetail holds its words when it failed.
	Turns      int
	Stop       ports.AgentStop
	StopDetail string
}

// Proof runs the unprivileged half of Action mode: it reviews the head tree with a model, tries to prove each candidate
// with a test in the sandbox, and writes the bounded result the gate records. It holds the model key and reads no
// write token; every tool that touches PR code goes through the sandbox port or reads the head checkout.
type Proof struct {
	cfg ProofConfig
}

// NewProof checks the wiring. A miswired run fails at boot, not after a model has been paid for.
func NewProof(cfg ProofConfig) (*Proof, error) {
	switch {
	case cfg.Loop == nil:
		return nil, errors.New("action proof: the agent loop is required")
	case cfg.Sandbox == nil:
		return nil, errors.New("action proof: the sandbox port is required: PR code runs only in it")
	case cfg.Code == nil:
		return nil, errors.New("action proof: the code reader is required: the fixed tree comes from the base")
	case cfg.Clock == nil:
		return nil, errors.New("action proof: the clock is required")
	case cfg.ModelName == "":
		return nil, errors.New("action proof: the model name is required")
	}
	return &Proof{cfg: cfg}, nil
}

// Run executes one proof run. The result is filled for every end, so the caller uploads a result that says
// "unknown: reason" rather than nothing. The error is for a failure of the wiring or of the model port.
func (p *Proof) Run(ctx context.Context, in ProofIn) (ProofResult, error) {
	if err := in.validate(); err != nil {
		return ProofResult{}, err
	}
	r := &proofRun{p: p, in: in, act: in.Action.Effective()}
	r.res = ProofResult{Head: in.Head, Base: in.Base, PolicyHash: in.PolicyHash, WorkflowRun: in.WorkflowRun,
		Model: p.cfg.ModelName}
	runErr := r.execute(ctx)
	// The spend goes into the result before the artifact is built from it, so the artifact records what the run cost.
	r.res.Spend = r.spent
	artifact, artifactErr := artifactOf(r)
	if artifactErr != nil {
		return r.res, fmt.Errorf("action proof on %s: %w", in.ChangeID, artifactErr)
	}
	raw, err := encodeProofArtifact(artifact)
	if err != nil {
		return r.res, fmt.Errorf("action proof on %s: %w", in.ChangeID, err)
	}
	r.res.Artifact = raw
	return r.res, runErr
}

// proofRun is the state of one Run call. It holds no state between runs: a new head is a new run.
type proofRun struct {
	p   *Proof
	in  ProofIn
	act policy.Action
	// spent is what the loop reported: the cost and the tokens of every answer.
	spent proof.Spend
	res   ProofResult
	// stop is a reason this package found that ends the run unknown, in addition to the loop's own stop.
	stop proof.Reason
	// proposals are the findings the model asked to have proven, in the order it made them.
	proposals []proposal
	// reviewed is true once the review step ran, whatever it found.
	reviewed bool
}

// execute is the run: the trivial skip, the monthly cap, the review, then the proof of each proposal.
func (r *proofRun) execute(ctx context.Context) error {
	r.res.Trivial = proof.Trivial(proofFiles(r.in.Files), r.act)
	if r.res.Trivial.Skip {
		r.res.Outcome = proof.Evaluate(proof.Facts{Action: r.act, Trivial: r.res.Trivial})
		return nil
	}
	if why := r.monthlyStop(); why != "" {
		r.stop = why
		r.res.Outcome = r.outcome()
		return nil
	}
	reviewErr := r.review(ctx)
	r.proveAll(ctx)
	r.res.Outcome = r.outcome()
	return reviewErr
}

// monthlyStop is the monthly cap check before the first call (ACTION_MODE 7). A month total that was not read gives no
// stop: the rows already say the spend is unknown, and the provider's limit is the hard stop.
func (r *proofRun) monthlyStop() proof.Reason {
	if !r.in.MonthKnown {
		return ""
	}
	return proof.MonthlyStop(r.act, r.in.MonthSpentUSD)
}

// outcome turns the facts of the run into the one answer the gate shows.
func (r *proofRun) outcome() proof.Outcome {
	return proof.Evaluate(proof.Facts{Action: r.act, Trivial: r.res.Trivial, Stop: r.stopReason(),
		Tests: r.testFacts(), Findings: r.findings(), Spend: r.spent})
}

// stopReason is the reason the run ended before it finished, or "". A reason this package found wins over the loop's,
// because it is the older fact.
func (r *proofRun) stopReason() proof.Reason {
	if r.stop != "" {
		return r.stop
	}
	switch r.res.Stop {
	case ports.AgentStopDone:
		// The model said it was done, or answered with text and nothing to do. The run's results stand; a run with no
		// proposal is "not reproduced", which is not a pass.
		return ""
	case ports.AgentStopBudget:
		return proof.ReasonBudget
	case ports.AgentStopDeadline:
		return proof.ReasonDeadline
	case ports.AgentStopCancelled:
		return proof.ReasonSuperseded
	case ports.AgentStopRefused, ports.AgentStopTurns, ports.AgentStopError:
		return proof.ReasonModelFailure
	}
	return proof.ReasonModelFailure
}

// testFacts is the red-first half of the outcome. This run has no separate plan step: the accepted test is the test
// that proved a finding, so a run that proved nothing is not reproduced, never proven (ACTION_MODE 4.3).
func (r *proofRun) testFacts() proof.TestFacts {
	if !r.reviewed {
		return proof.TestFacts{}
	}
	f := proof.TestFacts{Ran: true}
	for _, c := range r.res.Candidates {
		if c.Class == proof.ClassProven {
			f.Accepted++
		} else {
			f.Rejected++
		}
	}
	return f
}

// findings is the per-candidate list proof.Evaluate tallies.
func (r *proofRun) findings() []proof.Finding {
	out := make([]proof.Finding, 0, len(r.res.Candidates))
	for _, c := range r.res.Candidates {
		out = append(out, proof.Finding{Class: c.Class, Dropped: c.Dropped, Reason: c.Reason, Blocking: c.Blocks})
	}
	return out
}

// blocksUnder applies the policy's blocking rule to a proven candidate. Every proof here is a repro test, so the tier
// is E1, and a finding the run proves is open: nobody has dismissed it yet. gate.BlockingSet is the one rule, so this
// package never restates it.
func blocksUnder(pol policy.Policy, severity finding.Severity) bool {
	if !severity.Valid() {
		// A severity that is missing or unknown is not below the minimum, as gate.BlockingSet reads it.
		return true
	}
	states := []gate.FindingState{{Tier: finding.TierE1, Severity: severity, State: finding.StateOpen}}
	return len(gate.BlockingSet(states, pol.Blocking)) > 0
}

// candidateOf is the outcome of a proposal before any run: its place, its claim and the finding it answers, if any.
func (r *proofRun) candidateOf(p proposal) CandidateOutcome {
	oc := CandidateOutcome{CommentID: p.Comment, Location: finding.Location{File: p.File, Lines: [2]int{p.First, p.Last}},
		Claim: p.Claim, Severity: p.Severity, Test: p.TestPath}
	for _, f := range r.in.Existing {
		if f.Location.File == p.File && overlaps(f.Location, oc.Location) && f.Claim == p.Claim {
			oc.Ref = f.Ref
			break
		}
	}
	return oc
}

// treeID names the tree a run was on. Prove needs two different names, and the second run of the head must share the
// first one, so the head tree is the head commit and the fixed tree is a content hash of what was reverted.
func treeID(head id.SHA, reverted []ports.File) string {
	if len(reverted) == 0 {
		return "tree:" + string(head)
	}
	sum := sha256.New()
	for _, f := range reverted {
		sum.Write([]byte(f.Path))
		sum.Write([]byte{0})
		sum.Write([]byte(strconv.Itoa(len(f.Content))))
		sum.Write([]byte{0})
		sum.Write(f.Content)
	}
	return "tree:" + string(head) + ":reverted:" + hex.EncodeToString(sum.Sum(nil))
}
