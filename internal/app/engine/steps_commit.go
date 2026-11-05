package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// CommitTests is the step "github.commit_tests": it writes verified tests to the change branch.
type CommitTests struct {
	dirTemplate string
}

var _ Step = CommitTests{}

type commitTestsWith struct {
	Dir string `json:"dir"`
}

// NewCommitTests creates a new github.commit_tests step.
func NewCommitTests(with json.RawMessage) (CommitTests, error) {
	template := "tests/heimdall/{ticket}"
	if len(with) > 0 {
		var w commitTestsWith
		if err := decodeWith(with, []string{"dir"}, &w); err != nil {
			return CommitTests{}, fmt.Errorf("github.commit_tests with: %w", err)
		}
		if w.Dir != "" {
			template = w.Dir
		}
	}
	return CommitTests{dirTemplate: template}, nil
}

// Type returns the do name of the step.
func (CommitTests) Type() string { return "github.commit_tests" }

// Done reports whether tests.committed exists in the window.
func (CommitTests) Done(c ChangeView) bool {
	return c.Has(change.EventTestsCommitted)
}

// Start returns nothing: the commit is requested when tests.verified (or tests.approved) arrives via OnEvent.
func (CommitTests) Start(context.Context, ChangeView, json.RawMessage) (Result, error) {
	return Result{}, nil
}

// OnEvent requests the commit on tests.verified (if no signoff needed) or on tests.approved.
func (s CommitTests) OnEvent(_ context.Context, c ChangeView, e change.Event) (Result, error) {
	if s.Done(c) {
		return Result{}, nil
	}
	if e.Type == change.EventTestsVerified {
		p, err := change.Decode(e)
		if err != nil {
			return Result{}, fmt.Errorf("github.commit_tests: read %s: %w", e.Type, err)
		}
		tv, ok := p.(change.TestsVerified)
		if !ok {
			return Result{}, fmt.Errorf("github.commit_tests: payload is not change.TestsVerified")
		}
		if tv.NeedsSignoff {
			return Result{}, nil
		}
		return s.buildCommitEffect(c, e)
	}
	if e.Type == change.EventTestsApproved {
		return s.buildCommitEffect(c, e)
	}

	return Result{}, nil
}

// OnTimeout returns nothing: this step has no timer.
func (CommitTests) OnTimeout(context.Context, ChangeView) (Result, error) {
	return Result{}, nil
}

func (s CommitTests) buildCommitEffect(c ChangeView, e change.Event) (Result, error) {
	trig, err := triggerOf(c)
	if err != nil {
		return Result{}, fmt.Errorf("github.commit_tests: %w", err)
	}
	branch, err := branchOf(c)
	if err != nil {
		return Result{}, fmt.Errorf("github.commit_tests: %w", err)
	}
	dir := strings.ReplaceAll(s.dirTemplate, "{ticket}", string(c.ID))

	subEv, sub, err := latestSubmitted(c)
	if err != nil {
		return Result{}, fmt.Errorf("github.commit_tests: %w", err)
	}

	files := make([]change.CommitFile, 0, len(sub.Refs))
	for i, ref := range sub.Refs {
		blobRef := blobRefFor(subEv, i)
		files = append(files, change.CommitFile{
			Path:    ref,
			BlobRef: blobRef,
		})
	}
	if len(files) == 0 {
		files = append(files, change.CommitFile{
			Path:    dir + "/test.go",
			BlobRef: submissionRefOf(subEv),
		})
	}

	msg := fmt.Sprintf("tests: commit tests for %s", c.ID)
	payload := change.CommitTestsPayload{
		Repo:    trig.Repo,
		Branch:  branch,
		Dir:     dir,
		Files:   files,
		Message: msg,
	}
	if err := payload.Validate(); err != nil {
		return Result{}, fmt.Errorf("github.commit_tests: %w", err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Result{}, fmt.Errorf("github.commit_tests: encode payload: %w", err)
	}

	key, err := change.IdempotencyKey(c.ID, e.Version, change.EffectGitHubCommitTests, "commit:"+string(sub.TreeHash))
	if err != nil {
		return Result{}, fmt.Errorf("github.commit_tests: %w", err)
	}

	return Result{Effects: []change.Effect{{
		Key:     key,
		Kind:    change.EffectGitHubCommitTests,
		Target:  string(trig.Repo),
		Payload: raw,
	}}}, nil
}

func latestSubmitted(c ChangeView) (change.Event, change.TestsSubmitted, error) {
	for i := len(c.Events) - 1; i >= 0; i-- {
		e := c.Events[i]
		if e.Type != change.EventTestsSubmitted {
			continue
		}
		p, err := change.Decode(e)
		if err != nil {
			return change.Event{}, change.TestsSubmitted{}, fmt.Errorf("read %s: %w", e.Type, err)
		}
		sub, ok := p.(change.TestsSubmitted)
		if !ok {
			return change.Event{}, change.TestsSubmitted{}, fmt.Errorf("payload of %s is not change.TestsSubmitted", e.Type)
		}
		return e, sub, nil
	}
	return change.Event{}, change.TestsSubmitted{}, errors.New("no tests.submitted in the view")
}

func blobRefFor(e change.Event, idx int) workflow.BlobRef {
	if idx < len(e.BlobRefs) {
		return e.BlobRefs[idx]
	}
	if len(e.BlobRefs) > 0 {
		return e.BlobRefs[0]
	}
	return submissionRefOf(e)
}
