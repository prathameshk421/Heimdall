package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agenttools"
	appreview "github.com/prathameshk421/flip_the_switch_hackathon/internal/app/review"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ appreview.Refs = reviewRefs{}
var _ agenttools.ReviewValidator = semanticReviewValidator{}

type semanticReviewValidator struct{ refs appreview.Refs }

func (v semanticReviewValidator) Submit(ctx context.Context, sub agenttools.ReviewSubmission) (agenttools.ReviewResult, error) {
	result, err := appreview.Submit(ctx, v.refs, appreview.Submission{
		Change: sub.Change, Caller: sub.Caller, FilesChanged: sub.FilesChanged, Review: sub.Review,
	})
	if err != nil {
		return agenttools.ReviewResult{}, err
	}
	out := agenttools.ReviewResult{}
	for _, problem := range result.Problems {
		out.Problems = append(out.Problems, agenttools.ReviewProblem{Message: problem.Message})
	}
	for _, filed := range result.Findings {
		out.Findings = append(out.Findings, agenttools.ReviewFinding{Input: filed.Input, Tier: filed.Tier})
	}
	return out, nil
}

// memoryRecheck is the part of the knowledge search service the review refs use to check a cited memory reference.
type memoryRecheck interface {
	Recheck(ctx context.Context, agent id.AgentName, repo change.Repo, sha id.SHA, ref string) (verify.MemoryState, error)
}

type reviewRefs struct {
	code   ports.CodeReader
	memory memoryRecheck
}

func (r reviewRefs) FileExists(ctx context.Context, ref workflow.ChangeRef, path string) (bool, error) {
	_, err := r.code.ReadFile(ctx, change.Repo(ref.Repo), ref.HeadSHA, path)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("read review path %s: %w", path, err)
	}
	return true, nil
}

func (r reviewRefs) LineCount(ctx context.Context, ref workflow.ChangeRef, path string) (int, error) {
	content, err := r.code.ReadFile(ctx, change.Repo(ref.Repo), ref.HeadSHA, path)
	if err != nil {
		return 0, fmt.Errorf("read review path %s: %w", path, err)
	}
	if len(content) == 0 {
		return 0, nil
	}
	return bytes.Count(content, []byte{'\n'}) + 1, nil
}

// RunExists reports whether a tool_result names a tool the harness can repeat. The harness re-runs the tool itself
// (TECH_SPEC 11.1, E2), so a finding does not need an earlier run by the caller: it needs a supported tool.
func (reviewRefs) RunExists(_ context.Context, _ workflow.ChangeRef, _ id.AgentName, tool string) (bool, error) {
	return tool == verifiableTool, nil
}

// verifiableTool is the one tool_result tool the sandbox can repeat today (go vet through the runner).
const verifiableTool = "go vet"

// MemoryExists reports whether a cited memory reference is E3 evidence for the caller: found in the groups the caller
// can see, confirmed, and not superseded. A reference that is only observed is refused at the door, so it cannot be
// filed as E3 and then dropped at verification.
func (r reviewRefs) MemoryExists(ctx context.Context, ref workflow.ChangeRef, caller id.AgentName, memoryRef string) (bool, error) {
	if r.memory == nil {
		return false, nil
	}
	state, err := r.memory.Recheck(ctx, caller, change.Repo(ref.Repo), ref.BaseSHA, memoryRef)
	if err != nil {
		return false, fmt.Errorf("look up memory reference %s: %w", memoryRef, err)
	}
	return verify.TierForE3(state) == finding.TierE3, nil
}

func (reviewRefs) PackExists(context.Context, workflow.ChangeRef, id.AgentName, string) (bool, error) {
	return false, nil
}
