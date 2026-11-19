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
	code     ports.CodeReader
	memory   memoryRecheck
	profiles ports.ProfileStore
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

// ProfileChecks returns the checks of the repository's promoted profile. A tool_result must cite one of them that runs
// on the change, because verify_evidence re-runs that check on the head (HARNESS_DESIGN B7). Without a profile store, or
// without a promoted profile, there are none, so no finding can claim E2.
func (r reviewRefs) ProfileChecks(ctx context.Context, ref workflow.ChangeRef) ([]change.ProfileCheck, error) {
	if r.profiles == nil {
		return nil, nil
	}
	prof, ok, err := r.profiles.ActiveProfile(ctx, ref.Repo)
	if err != nil {
		return nil, fmt.Errorf("read the profile of %s: %w", ref.Repo, err)
	}
	if !ok {
		return nil, nil
	}
	out := make([]change.ProfileCheck, 0, len(prof.Spec.Checks))
	for _, c := range prof.Spec.Checks {
		out = append(out, change.ProfileCheck{ID: c.ID, When: c.When})
	}
	return out, nil
}

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
