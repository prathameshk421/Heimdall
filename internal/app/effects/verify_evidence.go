package effects

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
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
	Source  Source
	Sandbox ports.Sandbox
	Memory  MemoryRecheck
	Store   ports.EventStore
	Ingest  Ingest
	IDs     ports.IDs
	Clock   ports.Clock
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
	if d.Store == nil {
		missing = append(missing, "store")
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

	l, err := load(ctx, v.deps.Store, p.Ticket)
	if err != nil {
		return err
	}
	if isAlreadyEvaluated(l, p.FindingID) {
		return nil
	}

	if p.Memory != nil {
		return v.executeMemory(ctx, l, p)
	}
	if err := v.deps.Source.Fetch(ctx, p.Repo, p.HeadSHA); err != nil {
		return failf(err, "verify_evidence %s: fetch head source", p.Ticket)
	}
	if p.Check != nil {
		return v.executeCheck(ctx, l, p)
	}
	return v.executeRepro(ctx, l, p)
}

// executeRepro verifies an E1 finding: the repro test must fail on the head with an assertion.
func (v VerifyEvidence) executeRepro(ctx context.Context, l loadedChange, p change.VerifyEvidencePayload) error {

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

	res, err := v.runSandbox(ctx, job, []ports.File{overlay})
	claimHolds := false
	if err == nil {
		claimHolds = judgesClaimHolds(res, p.TestName)
	}

	cause := findCauseEvent(l, p.FindingID)
	if cause == nil {
		return failf(nil, "verify_evidence %s: no cause event for finding %s", p.Ticket, p.FindingID)
	}

	if claimHolds {
		return v.ingestVerified(ctx, p.Ticket, cause, p, finding.TierE1)
	}
	return v.ingestUnverified(ctx, p.Ticket, cause, p)
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

func (v VerifyEvidence) ingestVerified(ctx context.Context, ticket id.ChangeID, cause *change.Event, p change.VerifyEvidencePayload, tier finding.Tier) error {
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
	ev, err := newEvent(v.deps.IDs, v.deps.Clock, *cause, change.EventFindingVerified,
		change.Actor{Kind: change.ActorSystem, ID: "heimdall"}, payload)
	if err != nil {
		return failf(err, "verify_evidence %s: build event", ticket)
	}
	if err := v.deps.Ingest.Ingest(ctx, ticket, []change.Event{ev}); err != nil {
		return failf(err, "verify_evidence %s: ingest finding.verified", ticket)
	}
	return nil
}

func (v VerifyEvidence) ingestUnverified(ctx context.Context, ticket id.ChangeID, cause *change.Event, p change.VerifyEvidencePayload) error {
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
	ev, err := newEvent(v.deps.IDs, v.deps.Clock, *cause, change.EventFindingStateChanged,
		change.Actor{Kind: change.ActorSystem, ID: "heimdall"}, payload)
	if err != nil {
		return failf(err, "verify_evidence %s: build event", ticket)
	}
	if err := v.deps.Ingest.Ingest(ctx, ticket, []change.Event{ev}); err != nil {
		return failf(err, "verify_evidence %s: ingest finding.state_changed", ticket)
	}
	return nil
}

func isAlreadyEvaluated(l loadedChange, fndID id.FindingID) bool {
	lastPushIdx := -1
	for i := len(l.events) - 1; i >= 0; i-- {
		e := l.events[i]
		if e.Type == change.EventPRPushed {
			lastPushIdx = i
			break
		}
	}

	for i := len(l.events) - 1; i >= 0; i-- {
		if lastPushIdx >= 0 && i < lastPushIdx {
			break
		}
		e := l.events[i]
		if e.Type == change.EventFindingVerified || e.Type == change.EventFindingStateChanged {
			var rec change.FindingRecord
			if err := json.Unmarshal(e.Payload, &rec); err == nil && rec.FindingID == fndID {
				return true
			}
		}
	}
	return false
}

func findCauseEvent(l loadedChange, fndID id.FindingID) *change.Event {
	for i := len(l.events) - 1; i >= 0; i-- {
		e := l.events[i]
		if e.Type == change.EventPRPushed {
			return &l.events[i]
		}
	}
	for i := len(l.events) - 1; i >= 0; i-- {
		e := l.events[i]
		if e.Type == change.EventFindingFiled {
			var rec change.FindingRecord
			if err := json.Unmarshal(e.Payload, &rec); err == nil && rec.FindingID == fndID {
				return &l.events[i]
			}
		}
	}
	return l.first(change.EventChangeTriggered)
}

// executeCheck verifies an E2 finding: it re-runs the fixed static check on the head through the runner and applies the
// E2 rule (domain/verify). A first run confirms the finding when the check fails at the named location and downgrades
// it to E4 otherwise. A later run, after a push, resolves it when the diagnostic at the location is gone and keeps it
// open when it is still there. A check that could not run is an error, so the outbox retries it: an infrastructure
// failure is not evidence either way.
func (v VerifyEvidence) executeCheck(ctx context.Context, l loadedChange, p change.VerifyEvidencePayload) error {
	job := ports.SandboxJob{
		Kind:    ports.SandboxStatic,
		Check:   ports.CheckGoVet,
		Repo:    p.Repo,
		SHA:     p.HeadSHA,
		Dir:     path.Dir(p.Check.File),
		Timeout: 3 * time.Minute,
	}
	res, err := v.deps.Sandbox.Run(ctx, job)
	if err != nil {
		return failf(err, "verify_evidence %s: run %s", p.Ticket, p.Check.Name)
	}
	status := checkStatus(res)
	if status == verify.StatusError {
		return failf(nil, "verify_evidence %s: %s gave no result", p.Ticket, p.Check.Name)
	}
	located := verify.Located(verify.ParseVet([]byte(res.Output)), p.Check.File, p.Check.Lines[0], p.Check.Lines[1])

	cause := findCauseEvent(l, p.FindingID)
	if cause == nil {
		return failf(nil, "verify_evidence %s: no cause event for finding %s", p.Ticket, p.FindingID)
	}
	if !wasVerified(l, p.FindingID) {
		if verify.TierForE2(status, located) == finding.TierE2 {
			return v.ingestVerified(ctx, p.Ticket, cause, p, finding.TierE2)
		}
		return v.ingestUnverified(ctx, p.Ticket, cause, p)
	}
	if verify.ResolvedE2(status, located) {
		return v.ingestResolved(ctx, p.Ticket, cause, p, finding.TierE2)
	}
	return v.ingestVerified(ctx, p.Ticket, cause, p, finding.TierE2)
}

// checkStatus reads a static check result: exit 0 passes, a non-zero exit fails, anything else is no result.
func checkStatus(res ports.SandboxResult) verify.Status {
	switch {
	case res.Failed > 0:
		return verify.StatusFail
	case res.Passed > 0 && res.Errored == 0:
		return verify.StatusPass
	default:
		return verify.StatusError
	}
}

// wasVerified reports whether any finding.verified event of the finding is already stored: this run is then a re-run
// after a push and not the first verification.
func wasVerified(l loadedChange, fndID id.FindingID) bool {
	for _, e := range l.events {
		if e.Type != change.EventFindingVerified {
			continue
		}
		var rec change.FindingRecord
		if err := json.Unmarshal(e.Payload, &rec); err == nil && rec.FindingID == fndID {
			return true
		}
	}
	return false
}

func (v VerifyEvidence) ingestResolved(ctx context.Context, ticket id.ChangeID, cause *change.Event, p change.VerifyEvidencePayload, tier finding.Tier) error {
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
	ev, err := newEvent(v.deps.IDs, v.deps.Clock, *cause, change.EventFindingStateChanged,
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
func (v VerifyEvidence) executeMemory(ctx context.Context, l loadedChange, p change.VerifyEvidencePayload) error {
	if v.deps.Memory == nil {
		return failf(nil, "verify_evidence %s: the memory re-query is not configured", p.Ticket)
	}
	base := baseSHAOf(l)
	if base == "" {
		return failf(nil, "verify_evidence %s: the change has no base commit, so its policy cannot be read", p.Ticket)
	}
	state, err := v.deps.Memory.Recheck(ctx, id.AgentName(p.Memory.Agent), p.Repo, base, p.Memory.Ref)
	if err != nil {
		return failf(err, "verify_evidence %s: re-query memory", p.Ticket)
	}
	cause := findCauseEvent(l, p.FindingID)
	if cause == nil {
		return failf(nil, "verify_evidence %s: no cause event for finding %s", p.Ticket, p.FindingID)
	}
	if verify.TierForE3(state) == finding.TierE3 {
		return v.ingestVerified(ctx, p.Ticket, cause, p, finding.TierE3)
	}
	return v.ingestUnverified(ctx, p.Ticket, cause, p)
}
