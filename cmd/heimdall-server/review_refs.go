package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agenttools"
	appreview "github.com/prathameshk421/flip_the_switch_hackathon/internal/app/review"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
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

type reviewRefs struct{ code ports.CodeReader }

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

func (reviewRefs) RunExists(context.Context, workflow.ChangeRef, id.AgentName, string) (bool, error) {
	return false, nil
}

func (reviewRefs) MemoryExists(context.Context, workflow.ChangeRef, id.AgentName, string) (bool, error) {
	return false, nil
}

func (reviewRefs) PackExists(context.Context, workflow.ChangeRef, id.AgentName, string) (bool, error) {
	return false, nil
}
