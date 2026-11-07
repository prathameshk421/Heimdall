package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// TestQA is the step "sandbox.test_quality": it requests test quality checks on test submission
// and emits StepFailedError when tests are rejected.
type TestQA struct {
	checks []string
}

var _ Step = TestQA{}

type testQAWith struct {
	Checks []string `json:"checks"`
}

// NewTestQA creates a new sandbox.test_quality step.
func NewTestQA(with json.RawMessage) (TestQA, error) {
	if len(with) == 0 {
		return TestQA{}, nil
	}
	var w testQAWith
	if err := decodeWith(with, []string{"checks"}, &w); err != nil {
		return TestQA{}, fmt.Errorf("sandbox.test_quality with: %w", err)
	}
	return TestQA{checks: w.Checks}, nil
}

// Type returns the do name of the step.
func (TestQA) Type() string { return "sandbox.test_quality" }

// Done reports whether tests.verified exists in the window.
func (TestQA) Done(c ChangeView) bool {
	return c.Has(change.EventTestsVerified)
}

// Start returns nothing: the quality check is requested when tests.submitted arrives via OnEvent.
func (TestQA) Start(context.Context, ChangeView, json.RawMessage) (Result, error) {
	return Result{}, nil
}

// OnEvent handles incoming events:
// - tests.submitted: returns ONE effect test_quality.
// - tests.rejected: returns StepFailedError with the reasons from the rejection payload.
func (s TestQA) OnEvent(_ context.Context, c ChangeView, e change.Event) (Result, error) {
	if s.Done(c) {
		return Result{}, nil
	}
	if e.Type == change.EventTestsSubmitted {
		trig, err := triggerOf(c)
		if err != nil {
			return Result{}, fmt.Errorf("sandbox.test_quality: %w", err)
		}
		branch, err := branchOf(c)
		if err != nil {
			return Result{}, fmt.Errorf("sandbox.test_quality: %w", err)
		}
		subRef := submissionRefOf(e)

		payload := change.TestQualityPayload{
			Repo:          trig.Repo,
			Branch:        branch,
			Ticket:        c.ID,
			SubmissionRef: subRef,
		}
		if err := payload.Validate(); err != nil {
			return Result{}, fmt.Errorf("sandbox.test_quality: %w", err)
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return Result{}, fmt.Errorf("sandbox.test_quality: encode payload: %w", err)
		}
		key, err := change.IdempotencyKey(c.ID, e.Version, change.EffectTestQuality, "test_qa")
		if err != nil {
			return Result{}, fmt.Errorf("sandbox.test_quality: %w", err)
		}
		return Result{Effects: []change.Effect{{
			Key:     key,
			Kind:    change.EffectTestQuality,
			Target:  string(c.ID),
			Payload: raw,
		}}}, nil
	}

	if e.Type == change.EventTestsRejected {
		p, err := change.Decode(e)
		if err != nil {
			return Result{}, fmt.Errorf("sandbox.test_quality: read %s: %w", e.Type, err)
		}
		tr, ok := p.(change.TestsRejected)
		if !ok {
			return Result{}, fmt.Errorf("sandbox.test_quality: payload is not change.TestsRejected")
		}
		return Result{}, StepFailedError{Reasons: tr.Reasons}
	}

	return Result{}, nil
}

// OnTimeout returns nothing: this step has no deadline timer.
func (TestQA) OnTimeout(context.Context, ChangeView) (Result, error) {
	return Result{}, nil
}

// branchOf returns the branch name from the newest branch.ensured event in the view.
func branchOf(c ChangeView) (string, error) {
	evs := c.history()
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		if e.Type != change.EventBranchEnsured {
			continue
		}
		p, err := change.Decode(e)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", e.Type, err)
		}
		be, ok := p.(change.BranchEnsured)
		if !ok {
			return "", fmt.Errorf("payload of %s is not change.BranchEnsured", e.Type)
		}
		return be.Branch, nil
	}
	return "", errors.New("no branch.ensured in the view")
}

// submissionRefOf returns the submission blob reference from the event.
func submissionRefOf(e change.Event) workflow.BlobRef {
	if len(e.BlobRefs) > 0 {
		return e.BlobRefs[0]
	}
	// Fall back to deterministic hash of the event payload
	sum := sha256.Sum256(e.Payload)
	return workflow.BlobRef("blob:sha256:" + hex.EncodeToString(sum[:]))
}
