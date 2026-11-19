package effects

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/sarif"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// OverlaySandbox is an optional extension of ports.Sandbox that supports overlay files.
type OverlaySandbox interface {
	RunOverlay(ctx context.Context, j ports.SandboxJob, overlays []ports.File) (ports.SandboxResult, error)
}

// MemoryRecheck looks a cited memory reference up again with the scopes of the agent that cited it (E3 verification).
type MemoryRecheck interface {
	Recheck(ctx context.Context, agent id.AgentName, repo change.Repo, sha id.SHA, ref string) (verify.MemoryState, error)
}

// VerifyEvidenceDeps are the dependencies of VerifyEvidence. Memory is optional: without it an E3 verification fails
// and the outbox retries it, so a missing memory service never confirms a finding.
type VerifyEvidenceDeps struct {
	Source   Source
	Sandbox  ports.Sandbox
	Profiles ports.ProfileStore
	Memory   MemoryRecheck
	Ingest   Ingest
	IDs      ports.IDs
	Clock    ports.Clock
}

// VerifyEvidence performs verify_evidence effects (TECH_SPEC 11.1).
type VerifyEvidence struct {
	deps VerifyEvidenceDeps
}

// NewVerifyEvidence builds the executor, or refuses when a dependency is missing.
func NewVerifyEvidence(d VerifyEvidenceDeps) (VerifyEvidence, error) {
	var missing []string
	if d.Source == nil {
		missing = append(missing, "source")
	}
	if d.Sandbox == nil {
		missing = append(missing, "sandbox")
	}
	if d.Profiles == nil {
		missing = append(missing, "profiles")
	}
	if d.Ingest == nil {
		missing = append(missing, "ingest")
	}
	if d.IDs == nil {
		missing = append(missing, "ids")
	}
	if d.Clock == nil {
		missing = append(missing, "clock")
	}
	if len(missing) > 0 {
		return VerifyEvidence{}, fmt.Errorf("verify_evidence executor: missing %v", missing)
	}
	return VerifyEvidence{deps: d}, nil
}

// Execute performs evidence verification for a finding on the head commit.
func (v VerifyEvidence) Execute(ctx context.Context, fx change.Effect) error {
	if fx.Kind != change.EffectVerifyEvidence {
		return failf(nil, "verify_evidence executor: effect %s has kind %s", fx.Key, fx.Kind)
	}
	p, err := change.DecodeVerifyEvidencePayload(fx.Payload)
	if err != nil {
		return failf(err, "verify_evidence effect %s: payload refused: %v", fx.Key, err)
	}
	if err := checkTarget(fx, p.Ticket); err != nil {
		return err
	}

	if p.Memory != nil {
		return v.executeMemory(ctx, p)
	}
	if err := v.deps.Source.Fetch(ctx, p.Repo, p.HeadSHA); err != nil {
		return failf(err, "verify_evidence %s: fetch head source", p.Ticket)
	}
	if p.Check != nil {
		return v.executeCheck(ctx, p)
	}
	return v.executeRepro(ctx, p)
}

// executeRepro verifies an E1 finding: the repro test must fail on the head with an assertion.
func (v VerifyEvidence) executeRepro(ctx context.Context, p change.VerifyEvidencePayload) error {

	testBytes, err := base64.StdEncoding.DecodeString(p.TestSource)
	if err != nil {
		return failf(err, "verify_evidence %s: decode test source", p.Ticket)
	}

	overlay := ports.File{
		Path:    p.TestPath,
		Content: testBytes,
	}

	job := ports.SandboxJob{
		Kind:    ports.SandboxRepro,
		Repo:    p.Repo,
		SHA:     p.HeadSHA,
		Dir:     filepath.Dir(p.TestPath),
		Timeout: 3 * time.Minute,
	}

	// A test that does not compile, panics or runs out of time still gives a result, and that result drops the finding
	// to E4. An error means there is no result at all (the runner is busy or unreachable, or the effect ran out of
	// time), which says nothing about the claim, so the outbox tries again. Before, such an error dropped the finding
	// to E4: on KAN-23 four E1 findings fell because their runs found the runner busy.
	res, err := v.runSandbox(ctx, job, []ports.File{overlay})
	if err != nil {
		return failf(err, "verify_evidence %s: run repro test %s", p.Ticket, p.TestName)
	}
	claimHolds := judgesClaimHolds(res, p.TestName)

	if claimHolds {
		return v.ingestVerified(ctx, p.Ticket, p, finding.TierE1)
	}
	return v.ingestUnverified(ctx, p.Ticket, p)
}

func (v VerifyEvidence) runSandbox(ctx context.Context, job ports.SandboxJob, overlays []ports.File) (ports.SandboxResult, error) {
	if s, ok := v.deps.Sandbox.(OverlaySandbox); ok {
		return s.RunOverlay(ctx, job, overlays)
	}
	return v.deps.Sandbox.Run(ctx, job)
}

func judgesClaimHolds(res ports.SandboxResult, testName string) bool {
	for _, t := range res.Tests {
		if t.Name == testName {
			return t.Outcome == ports.OutcomeFailure
		}
	}
	if len(res.Tests) > 0 {
		return res.Tests[0].Outcome == ports.OutcomeFailure
	}
	return res.Failed > 0 && res.Errored == 0
}

func (v VerifyEvidence) ingestVerified(ctx context.Context, ticket id.ChangeID, p change.VerifyEvidencePayload, tier finding.Tier) error {
	payload := change.FindingVerified{
		FindingID: p.FindingID,
		Ref:       p.FindingRef,
		Tier:      tier,
		Severity:  p.Severity,
		State:     finding.StateOpen,
		By:        change.ActorID("heimdall"),
	}
	if err := payload.Validate(); err != nil {
		return failf(err, "verify_evidence %s: finding.verified refused", ticket)
	}
	ev, err := newFact(v.deps.IDs, v.deps.Clock, change.EventFindingVerified,
		change.Actor{Kind: change.ActorSystem, ID: "heimdall"}, payload)
	if err != nil {
		return failf(err, "verify_evidence %s: build event", ticket)
	}
	if err := v.deps.Ingest.Ingest(ctx, ticket, []change.Event{ev}); err != nil {
		return failf(err, "verify_evidence %s: ingest finding.verified", ticket)
	}
	return nil
}

func (v VerifyEvidence) ingestUnverified(ctx context.Context, ticket id.ChangeID, p change.VerifyEvidencePayload) error {
	payload := change.FindingStateChanged{
		FindingID: p.FindingID,
		Ref:       p.FindingRef,
		Tier:      finding.TierE4,
		Severity:  p.Severity,
		State:     finding.StateUnverified,
		By:        change.ActorID("heimdall"),
	}
	if err := payload.Validate(); err != nil {
		return failf(err, "verify_evidence %s: finding.state_changed refused", ticket)
	}
	ev, err := newFact(v.deps.IDs, v.deps.Clock, change.EventFindingStateChanged,
		change.Actor{Kind: change.ActorSystem, ID: "heimdall"}, payload)
	if err != nil {
		return failf(err, "verify_evidence %s: build event", ticket)
	}
	if err := v.deps.Ingest.Ingest(ctx, ticket, []change.Event{ev}); err != nil {
		return failf(err, "verify_evidence %s: ingest finding.state_changed", ticket)
	}
	return nil
}

// executeCheck verifies an E2 finding: it re-runs the cited profile check on the head (and on the base when the check
// compares against it, keeping only the results the head added) and applies the E2 rule (domain/verify). A first run
// confirms the finding when a new result touches the finding's file and lines, and leaves it at E4 otherwise. A later
// run, after a push, keeps it open while such a result is still there and resolves it when none is. A check that could
// not run is an error, so the outbox retries it: an infrastructure failure is not evidence either way.
func (v VerifyEvidence) executeCheck(ctx context.Context, p change.VerifyEvidencePayload) error {
	if p.Check.ID == "" {
		// A finding filed before profile checks names a tool, which no check repeats: it is not evidence.
		return v.ingestUnverified(ctx, p.Ticket, p)
	}
	profile, ok, err := v.deps.Profiles.ActiveProfile(ctx, string(p.Repo))
	if err != nil {
		return failf(err, "verify_evidence %s: read the profile", p.Ticket)
	}
	if !ok {
		return failf(nil, "verify_evidence %s: the repository has no promoted profile, so check %s cannot run", p.Ticket, p.Check.ID)
	}
	row, found := checkRow(profile.Spec.Checks, p.Check.ID)
	if !found {
		// The submit rule only accepts a check of the profile. A profile promoted since then may have dropped it: the
		// claim can no longer be repeated, so it is not evidence and the finding stays E4.
		return v.ingestUnverified(ctx, p.Ticket, p)
	}
	results, err := v.checkResults(ctx, p, row)
	if err != nil {
		return failf(err, "verify_evidence %s: run check %s", p.Ticket, p.Check.ID)
	}
	status := verify.StatusPass
	if len(results) > 0 {
		status = verify.StatusFail
	}
	located := verify.Located(results, p.Check.File, p.Check.Lines[0], p.Check.Lines[1])

	if !p.Reverify {
		if verify.TierForE2(status, located) == finding.TierE2 {
			return v.ingestVerified(ctx, p.Ticket, p, finding.TierE2)
		}
		return v.ingestUnverified(ctx, p.Ticket, p)
	}
	if verify.ResolvedE2(status, located) {
		return v.ingestResolved(ctx, p.Ticket, p, finding.TierE2)
	}
	return v.ingestVerified(ctx, p.Ticket, p, finding.TierE2)
}

// checkResults runs one profile check on the head and, with a baseline, on the base, and returns what the head added.
func (v VerifyEvidence) checkResults(ctx context.Context, p change.VerifyEvidencePayload, row ports.ProfileCheck) ([]sarif.Result, error) {
	head, err := v.checkAt(ctx, p, row, p.HeadSHA, "")
	if err != nil {
		return nil, err
	}
	if !row.Baseline || p.BaseSHA == "" {
		return head, nil
	}
	if err := v.deps.Source.Fetch(ctx, p.Repo, p.BaseSHA); err != nil {
		return nil, fmt.Errorf("fetch base source: %w", err)
	}
	base, err := v.checkAt(ctx, p, row, p.BaseSHA, p.HeadSHA)
	if err != nil {
		return nil, err
	}
	return sarif.New(base, head), nil
}

// checkAt runs the check at sha. head is set for a baseline run, which runs on the base for that head.
func (v VerifyEvidence) checkAt(ctx context.Context, p change.VerifyEvidencePayload, row ports.ProfileCheck, sha, head id.SHA) ([]sarif.Result, error) {
	res, err := v.deps.Sandbox.Run(ctx, ports.SandboxJob{Kind: ports.SandboxCheck, CheckID: row.ID, Repo: p.Repo, SHA: sha,
		Change: p.Ticket, Head: head, Timeout: checkTimeout, Verify: true})
	if err != nil {
		return nil, fmt.Errorf("the check did not run: %w", err)
	}
	results, err := sarif.Parse(res.Sarif, row.ID, row.Lens)
	if err != nil {
		return nil, fmt.Errorf("the check wrote no valid SARIF: %w", err)
	}
	return results, nil
}

func (v VerifyEvidence) ingestResolved(ctx context.Context, ticket id.ChangeID, p change.VerifyEvidencePayload, tier finding.Tier) error {
	payload := change.FindingStateChanged{
		FindingID: p.FindingID,
		Ref:       p.FindingRef,
		Tier:      tier,
		Severity:  p.Severity,
		State:     finding.StateResolved,
		By:        change.ActorID("heimdall"),
	}
	if err := payload.Validate(); err != nil {
		return failf(err, "verify_evidence %s: finding.state_changed refused", ticket)
	}
	ev, err := newFact(v.deps.IDs, v.deps.Clock, change.EventFindingStateChanged,
		change.Actor{Kind: change.ActorSystem, ID: "heimdall"}, payload)
	if err != nil {
		return failf(err, "verify_evidence %s: build event", ticket)
	}
	if err := v.deps.Ingest.Ingest(ctx, ticket, []change.Event{ev}); err != nil {
		return failf(err, "verify_evidence %s: ingest finding.state_changed", ticket)
	}
	return nil
}

// executeMemory verifies an E3 finding by looking its memory reference up again. The finding stays E3 only while the
// reference is found, confirmed and not superseded; on the first run and on every re-run after a push, anything else
// drops it to E4 so it stops blocking (TECH_SPEC 11.1, D152). A lookup that fails is an error, so the outbox retries it.
func (v VerifyEvidence) executeMemory(ctx context.Context, p change.VerifyEvidencePayload) error {
	if v.deps.Memory == nil {
		return failf(nil, "verify_evidence %s: the memory re-query is not configured", p.Ticket)
	}
	base := p.BaseSHA
	if base == "" {
		return failf(nil, "verify_evidence %s: the job names no base commit, so its policy cannot be read", p.Ticket)
	}
	state, err := v.deps.Memory.Recheck(ctx, id.AgentName(p.Memory.Agent), p.Repo, base, p.Memory.Ref)
	if err != nil {
		return failf(err, "verify_evidence %s: re-query memory", p.Ticket)
	}
	if verify.TierForE3(state) == finding.TierE3 {
		return v.ingestVerified(ctx, p.Ticket, p, finding.TierE3)
	}
	return v.ingestUnverified(ctx, p.Ticket, p)
}
