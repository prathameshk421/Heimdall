package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// VerifyEvidence is the step "sandbox.verify_evidence": it requests evidence verification
// for findings of tier E1.
type VerifyEvidence struct{}

var _ Step = VerifyEvidence{}

// NewVerifyEvidence creates a new sandbox.verify_evidence step.
func NewVerifyEvidence(_ json.RawMessage) (VerifyEvidence, error) {
	return VerifyEvidence{}, nil
}

// Type returns the do name of the step.
func (VerifyEvidence) Type() string { return "sandbox.verify_evidence" }

// Done reports whether no E1 finding in the window is waiting for verification.
func (VerifyEvidence) Done(c ChangeView) bool {
	waiting := waitingProvableFindings(c)
	return len(waiting) == 0
}

// Start returns nothing: evidence verification is requested via OnEvent.
func (VerifyEvidence) Start(context.Context, ChangeView, json.RawMessage) (Result, error) {
	return Result{}, nil
}

// OnEvent handles incoming events:
// - finding.filed: requests verification if the finding is E1 and not yet verified in the window.
// - pr.pushed: re-requests verification for every open E1 finding.
func (s VerifyEvidence) OnEvent(_ context.Context, c ChangeView, e change.Event) (Result, error) {
	if e.Type == change.EventFindingFiled {
		return s.onFindingFiled(c, e)
	}
	if e.Type == change.EventPRPushed {
		return s.onPRPushed(c, e)
	}
	if e.Type == change.EventReviewSubmitted {
		return s.onReviewSubmitted(c, e)
	}
	return Result{}, nil
}

// onReviewSubmitted requests verification for every open finding that is not yet verified. The step opens after the
// review step, so this is the point where the filed findings are first verified (A2).
func (s VerifyEvidence) onReviewSubmitted(c ChangeView, e change.Event) (Result, error) {
	trig, err := triggerOf(c)
	if err != nil {
		return Result{}, fmt.Errorf("sandbox.verify_evidence: %w", err)
	}
	headSHA := headSHAOf(c)
	if headSHA == "" {
		return Result{}, errors.New("sandbox.verify_evidence: no head SHA in the view")
	}
	var effects []change.Effect
	for _, f := range findOpenProvableFindings(c) {
		if isFindingVerifiedInWindow(c, f.id) {
			continue
		}
		payload := change.VerifyEvidencePayload{
			Repo:       trig.Repo,
			Ticket:     c.ID,
			FindingID:  f.id,
			FindingRef: f.ref,
			Severity:   f.severity,
			HeadSHA:    headSHA,
		}
		if err := attachProof(&payload, f.filedEvent); err != nil {
			return Result{}, fmt.Errorf("sandbox.verify_evidence: finding %s (%s): %w", f.id, f.ref, err)
		}
		fx, err := buildVerifyEffect(c.ID, e.Version, payload)
		if err != nil {
			return Result{}, err
		}
		effects = append(effects, fx)
	}
	return Result{Effects: effects}, nil
}

func (VerifyEvidence) onFindingFiled(c ChangeView, e change.Event) (Result, error) {
	record, err := parseFindingRecord(e)
	if err != nil {
		return Result{}, fmt.Errorf("sandbox.verify_evidence: read %s: %w", e.Type, err)
	}
	if !provableTier(record.Tier) || isFindingVerifiedInWindow(c, record.FindingID) {
		return Result{}, nil
	}

	trig, err := triggerOf(c)
	if err != nil {
		return Result{}, fmt.Errorf("sandbox.verify_evidence: %w", err)
	}
	headSHA := headSHAOf(c)
	if headSHA == "" {
		return Result{}, errors.New("sandbox.verify_evidence: no head SHA in the view")
	}

	payload := change.VerifyEvidencePayload{
		Repo:       trig.Repo,
		Ticket:     c.ID,
		FindingID:  record.FindingID,
		FindingRef: record.Ref,
		Severity:   record.Severity,
		HeadSHA:    headSHA,
	}
	if err := attachProof(&payload, e); err != nil {
		return Result{}, fmt.Errorf("sandbox.verify_evidence: finding %s (%s): %w", record.FindingID, record.Ref, err)
	}
	fx, err := buildVerifyEffect(c.ID, e.Version, payload)
	if err != nil {
		return Result{}, err
	}
	return Result{Effects: []change.Effect{fx}}, nil
}

func (VerifyEvidence) onPRPushed(c ChangeView, e change.Event) (Result, error) {
	p, err := change.Decode(e)
	if err != nil {
		return Result{}, fmt.Errorf("sandbox.verify_evidence: read %s: %w", e.Type, err)
	}
	pr, ok := p.(change.PRPushed)
	if !ok {
		return Result{}, fmt.Errorf("sandbox.verify_evidence: payload of %s is not change.PRPushed", e.Type)
	}
	trig, err := triggerOf(c)
	if err != nil {
		return Result{}, fmt.Errorf("sandbox.verify_evidence: %w", err)
	}

	openE1s := findOpenProvableFindings(c)
	effects := make([]change.Effect, 0, len(openE1s))
	for _, f := range openE1s {
		payload := change.VerifyEvidencePayload{
			Repo:       trig.Repo,
			Ticket:     c.ID,
			FindingID:  f.id,
			FindingRef: f.ref,
			Severity:   f.severity,
			HeadSHA:    pr.HeadSHA,
		}
		if err := attachProof(&payload, f.filedEvent); err != nil {
			return Result{}, fmt.Errorf("sandbox.verify_evidence: finding %s (%s): %w", f.id, f.ref, err)
		}
		fx, err := buildVerifyEffect(c.ID, e.Version, payload)
		if err != nil {
			return Result{}, err
		}
		effects = append(effects, fx)
	}
	return Result{Effects: effects}, nil
}

func buildVerifyEffect(ticket id.ChangeID, version int64, payload change.VerifyEvidencePayload) (change.Effect, error) {
	if err := payload.Validate(); err != nil {
		return change.Effect{}, fmt.Errorf("sandbox.verify_evidence: %w", err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return change.Effect{}, fmt.Errorf("sandbox.verify_evidence: encode payload: %w", err)
	}
	disc := string(payload.FindingID) + ":" + string(payload.HeadSHA)
	key, err := change.IdempotencyKey(ticket, version, change.EffectVerifyEvidence, disc)
	if err != nil {
		return change.Effect{}, fmt.Errorf("sandbox.verify_evidence: %w", err)
	}
	return change.Effect{
		Key:     key,
		Kind:    change.EffectVerifyEvidence,
		Target:  string(ticket),
		Payload: raw,
	}, nil
}

// OnTimeout returns nothing: this step has no timer.
func (VerifyEvidence) OnTimeout(context.Context, ChangeView) (Result, error) {
	return Result{}, nil
}

type findingStateInfo struct {
	id         id.FindingID
	ref        id.FindingRef
	tier       finding.Tier
	severity   finding.Severity
	state      finding.State
	filedEvent change.Event
}

func parseFindingRecord(e change.Event) (change.FindingRecord, error) {
	var rec change.FindingRecord
	if err := json.Unmarshal(e.Payload, &rec); err != nil {
		return change.FindingRecord{}, err
	}
	return rec, nil
}

// provableTier reports whether the server can repeat evidence of the tier: E1 (a repro test), E2 (a static check) and
// E3 (a memory re-query).
func provableTier(t finding.Tier) bool {
	return t == finding.TierE1 || t == finding.TierE2 || t == finding.TierE3
}

// checkNames maps the tool a reviewer names in tool_result evidence to the fixed check the runner knows.
func checkNameFor(tool string) (string, bool) {
	if tool == "go vet" {
		return change.CheckGoVet, true
	}
	return "", false
}

// attachProof sets the evidence the sandbox repeats on the payload, read from the finding.filed event: a check for a
// tool result (E2) and a repro test for E1. Each tier needs its own proof; an event without it is refused.
func attachProof(payload *change.VerifyEvidencePayload, filed change.Event) error {
	rec, err := parseFindingRecord(filed)
	if err != nil {
		return err
	}
	if rec.Tier == finding.TierE3 {
		if rec.MemoryRef == nil {
			return errors.New("tier E3 but the event carries no memory reference")
		}
		payload.Memory = &change.VerifyMemory{Ref: *rec.MemoryRef, Agent: string(rec.By)}
		return nil
	}
	if rec.Tier == finding.TierE2 {
		if rec.ToolResult == nil {
			return errors.New("tier E2 but the event carries no tool result")
		}
		name, ok := checkNameFor(rec.ToolResult.Tool)
		if !ok {
			return fmt.Errorf("tool %q is not one the sandbox can repeat", rec.ToolResult.Tool)
		}
		payload.Check = &change.VerifyCheck{Name: name, File: rec.ToolResult.Location.File, Lines: rec.ToolResult.Location.Lines}
		return nil
	}
	testPath, testSource, testName, ok := extractReproTest(filed)
	if !ok {
		return errors.New("tier E1 but the event does not carry repro test source")
	}
	payload.TestPath, payload.TestSource, payload.TestName = testPath, testSource, testName
	return nil
}

func extractReproTest(e change.Event) (testPath, testSource, testName string, ok bool) {
	var shape struct {
		TestPath   string `json:"test_path"`
		TestSource string `json:"test_source"`
		TestName   string `json:"test_name"`
		ReproTest  *struct {
			Path    string `json:"path"`
			Name    string `json:"name"`
			Content string `json:"content"`
		} `json:"repro_test"`
	}
	if err := json.Unmarshal(e.Payload, &shape); err != nil {
		return "", "", "", false
	}
	if shape.ReproTest != nil && shape.ReproTest.Path != "" && shape.ReproTest.Name != "" && shape.ReproTest.Content != "" {
		src := base64.StdEncoding.EncodeToString([]byte(shape.ReproTest.Content))
		return shape.ReproTest.Path, src, shape.ReproTest.Name, true
	}
	if strings.TrimSpace(shape.TestPath) != "" && strings.TrimSpace(shape.TestSource) != "" && strings.TrimSpace(shape.TestName) != "" {
		return shape.TestPath, shape.TestSource, shape.TestName, true
	}
	return "", "", "", false
}

func headSHAOf(c ChangeView) id.SHA {
	evs := c.history()
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		if e.Type == change.EventPRPushed {
			if p, err := change.Decode(e); err == nil {
				if pr, ok := p.(change.PRPushed); ok {
					return pr.HeadSHA
				}
			}
		}
		if e.Type == change.EventPROpened {
			if p, err := change.Decode(e); err == nil {
				if pr, ok := p.(change.PROpened); ok {
					return pr.HeadSHA
				}
			}
		}
	}
	return ""
}

func isFindingVerifiedInWindow(c ChangeView, fndID id.FindingID) bool {
	for _, e := range c.Events {
		if e.Type == change.EventFindingVerified {
			if rec, err := parseFindingRecord(e); err == nil && rec.FindingID == fndID {
				return true
			}
		}
	}
	return false
}

func updateFindingMap(findings map[id.FindingID]*findingStateInfo, e change.Event) {
	rec, err := parseFindingRecord(e)
	if err != nil {
		return
	}
	if e.Type == change.EventFindingFiled {
		if provableTier(rec.Tier) {
			findings[rec.FindingID] = &findingStateInfo{
				id:         rec.FindingID,
				ref:        rec.Ref,
				tier:       rec.Tier,
				severity:   rec.Severity,
				state:      rec.State,
				filedEvent: e,
			}
		}
		return
	}
	if e.Type == change.EventFindingStateChanged {
		if f, ok := findings[rec.FindingID]; ok {
			f.state = rec.State
			f.tier = rec.Tier
		}
		return
	}
	if e.Type == change.EventFindingVerified {
		if f, ok := findings[rec.FindingID]; ok {
			f.tier = rec.Tier
			if rec.State != "" {
				f.state = rec.State
			}
		}
		return
	}
}

func findOpenProvableFindings(c ChangeView) []findingStateInfo {
	findings := make(map[id.FindingID]*findingStateInfo)
	var order []id.FindingID

	for _, e := range c.Events {
		if e.Type == change.EventFindingFiled {
			if rec, err := parseFindingRecord(e); err == nil && provableTier(rec.Tier) {
				order = append(order, rec.FindingID)
			}
		}
		updateFindingMap(findings, e)
	}

	var openE1s []findingStateInfo
	for _, fID := range order {
		f := findings[fID]
		if f != nil && provableTier(f.tier) && f.state == finding.StateOpen {
			openE1s = append(openE1s, *f)
		}
	}
	return openE1s
}

func waitingProvableFindings(c ChangeView) []id.FindingID {
	openE1s := findOpenProvableFindings(c)
	if len(openE1s) == 0 {
		return nil
	}

	lastPushIdx := -1
	for i := len(c.Events) - 1; i >= 0; i-- {
		if c.Events[i].Type == change.EventPRPushed {
			lastPushIdx = i
			break
		}
	}

	var waiting []id.FindingID
	for _, f := range openE1s {
		if !isVerifiedSince(c.Events, f.id, lastPushIdx) {
			waiting = append(waiting, f.id)
		}
	}
	return waiting
}

func isVerifiedSince(events []change.Event, fndID id.FindingID, afterIdx int) bool {
	for i := len(events) - 1; i >= 0; i-- {
		if afterIdx >= 0 && i < afterIdx {
			break
		}
		e := events[i]
		if e.Type == change.EventFindingVerified {
			if rec, err := parseFindingRecord(e); err == nil && rec.FindingID == fndID {
				return true
			}
		}
	}
	return false
}
