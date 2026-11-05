package effects

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// OverlaySandbox is an optional extension of ports.Sandbox that supports overlay files.
type OverlaySandbox interface {
	RunOverlay(ctx context.Context, j ports.SandboxJob, overlays []ports.File) (ports.SandboxResult, error)
}

// VerifyEvidenceDeps are the dependencies of VerifyEvidence.
type VerifyEvidenceDeps struct {
	Source  Source
	Sandbox ports.Sandbox
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

	if err := v.deps.Source.Fetch(ctx, p.Repo, p.HeadSHA); err != nil {
		return failf(err, "verify_evidence %s: fetch head source", p.Ticket)
	}

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
		return v.ingestVerified(ctx, p.Ticket, cause, p)
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

func (v VerifyEvidence) ingestVerified(ctx context.Context, ticket id.ChangeID, cause *change.Event, p change.VerifyEvidencePayload) error {
	payload := change.FindingVerified{
		FindingID: p.FindingID,
		Ref:       p.FindingRef,
		Tier:      finding.TierE1,
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
