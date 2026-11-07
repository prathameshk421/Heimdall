package review

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// Refs verifies that a path or citation is visible to the submitting caller for the change.
type Refs interface {
	FileExists(context.Context, workflow.ChangeRef, string) (bool, error)
	LineCount(context.Context, workflow.ChangeRef, string) (int, error)
	RunExists(context.Context, workflow.ChangeRef, id.AgentName, string) (bool, error)
	MemoryExists(context.Context, workflow.ChangeRef, id.AgentName, string) (bool, error)
	PackExists(context.Context, workflow.ChangeRef, id.AgentName, string) (bool, error)
}

// Submission contains the review and the trusted scope in which it was produced.
type Submission struct {
	Change       workflow.ChangeRef
	Caller       id.AgentName
	FilesChanged []string
	Review       finding.Review
	Existing     []FiledFinding
}

// Reporter identifies one reviewer contributing to a merged finding.
type Reporter struct {
	Agent      id.AgentName
	Confidence finding.Confidence
}

// FiledFinding is the application representation ready for persistence.
type FiledFinding struct {
	Input     finding.Input
	Tier      finding.Tier
	Reporters []Reporter
}

// Problem is one correctable semantic error. Message always starts with a payload field path.
type Problem struct{ Message string }

// Error implements error for convenient reporting.
func (p Problem) Error() string { return p.Message }

// Result contains all correctable problems, or the accepted findings when there are none.
type Result struct {
	Findings []FiledFinding
	Problems []Problem
}

// Submit validates a review, verifies its references, and deduplicates it with existing findings.
// Correctable input problems are returned in Result.Problems; reference-store failures are returned as errors.
func Submit(ctx context.Context, refs Refs, sub Submission) (Result, error) {
	if refs == nil {
		return Result{}, errors.New("review references must not be nil")
	}
	if problems := reviewProblems(sub); len(problems) > 0 {
		return Result{Problems: problems}, nil
	}

	accepted := make([]FiledFinding, 0, len(sub.Review.Findings))
	var problems []Problem
	for i, input := range sub.Review.Findings {
		findingProblems, err := findingProblems(ctx, refs, sub, i, input)
		if err != nil {
			return Result{}, err
		}
		if len(findingProblems) > 0 {
			problems = append(problems, findingProblems...)
			continue
		}
		accepted = append(accepted, FiledFinding{
			Input:     input,
			Tier:      tier(input),
			Reporters: []Reporter{{Agent: sub.Caller, Confidence: input.Confidence}},
		})
	}
	if len(problems) > 0 {
		return Result{Problems: problems}, nil
	}
	return Result{Findings: merge(sub.Existing, accepted)}, nil
}

func reviewProblems(sub Submission) []Problem {
	problems := joinedProblems(sub.Review.Validate())
	if sub.Review.HeadSHA != sub.Change.HeadSHA {
		problems = append(problems, Problem{Message: "head_sha must match the change head"})
	}
	return problems
}

func findingProblems(ctx context.Context, refs Refs, sub Submission, index int, input finding.Input) ([]Problem, error) {
	prefix := fmt.Sprintf("findings[%d]", index)
	problems := joinedProblems(input.Validate())
	location, err := checkLocation(ctx, refs, sub.Change, prefix+".location", input.Location)
	if err != nil {
		return nil, err
	}
	problems = append(problems, location...)
	for evidenceIndex, evidence := range input.Evidence {
		at := fmt.Sprintf("%s.evidence[%d]", prefix, evidenceIndex)
		checked, err := checkEvidence(ctx, refs, sub, at, evidence)
		if err != nil {
			return nil, err
		}
		problems = append(problems, checked...)
	}
	contextProblems, err := checkContextRefs(ctx, refs, sub, prefix, input.ContextRefs)
	if err != nil {
		return nil, err
	}
	problems = append(problems, contextProblems...)
	problems = append(problems, diffScopeProblems(sub, prefix, input)...)
	return problems, nil
}

func checkContextRefs(ctx context.Context, refs Refs, sub Submission, prefix string, contextRefs []string) ([]Problem, error) {
	var problems []Problem
	for refIndex, ref := range contextRefs {
		at := fmt.Sprintf("%s.context_refs[%d]", prefix, refIndex)
		if name, isCard := finding.CardRef(ref); isCard {
			if name == "" {
				problems = append(problems, Problem{Message: at + " must be card: followed by a plain file name"})
			}
			continue
		}
		memory, err := refs.MemoryExists(ctx, sub.Change, sub.Caller, ref)
		if err != nil {
			return nil, fmt.Errorf("look up %s: %w", at, err)
		}
		if memory {
			continue
		}
		pack, err := refs.PackExists(ctx, sub.Change, sub.Caller, ref)
		if err != nil {
			return nil, fmt.Errorf("look up %s: %w", at, err)
		}
		if !pack {
			problems = append(problems, Problem{Message: at + " does not exist for this change and caller"})
		}
	}
	return problems, nil
}

func diffScopeProblems(sub Submission, prefix string, input finding.Input) []Problem {
	if slices.Contains(sub.FilesChanged, input.Location.File) {
		return nil
	}
	var problems []Problem
	if !outsideDiffCategory(input.Category) {
		problems = append(problems, Problem{Message: prefix + ".category must be compat, ops, or security outside the diff"})
	}
	if !hasReasoning(input.Evidence) {
		problems = append(problems, Problem{Message: prefix + ".evidence needs reasoning outside the diff"})
	}
	return problems
}

func checkEvidence(ctx context.Context, refs Refs, sub Submission, at string, evidence finding.Evidence) ([]Problem, error) {
	switch evidence.Type {
	case finding.EvidenceToolResult:
		return checkToolResult(ctx, refs, sub, at, evidence.ToolResult)
	case finding.EvidenceMemory:
		return checkMemoryRef(ctx, refs, sub, at, evidence.MemoryRef)
	case finding.EvidencePackRef:
		return checkPackRef(ctx, refs, sub, at, evidence.PackRef)
	case finding.EvidenceReproTest, finding.EvidenceReasoning:
	}
	return nil, nil
}

func checkToolResult(ctx context.Context, refs Refs, sub Submission, at string, result *finding.ToolResult) ([]Problem, error) {
	if result == nil {
		return nil, nil
	}
	problems, err := checkLocation(ctx, refs, sub.Change, at+".tool_result.location", result.Location)
	if err != nil {
		return nil, err
	}
	exists, err := refs.RunExists(ctx, sub.Change, sub.Caller, result.Tool)
	if err != nil {
		return nil, fmt.Errorf("look up %s.tool_result.tool: %w", at, err)
	}
	if !exists {
		problems = append(problems, Problem{Message: at + ".tool_result.tool does not exist for this change and caller"})
	}
	return problems, nil
}

func checkMemoryRef(ctx context.Context, refs Refs, sub Submission, at string, ref *string) ([]Problem, error) {
	if ref == nil {
		return nil, nil
	}
	exists, err := refs.MemoryExists(ctx, sub.Change, sub.Caller, *ref)
	if err != nil {
		return nil, fmt.Errorf("look up %s.memory_ref: %w", at, err)
	}
	if !exists {
		return []Problem{{Message: at + ".memory_ref does not exist for this change and caller"}}, nil
	}
	return nil, nil
}

func checkPackRef(ctx context.Context, refs Refs, sub Submission, at string, ref *string) ([]Problem, error) {
	if ref == nil {
		return nil, nil
	}
	exists, err := refs.PackExists(ctx, sub.Change, sub.Caller, *ref)
	if err != nil {
		return nil, fmt.Errorf("look up %s.pack_ref: %w", at, err)
	}
	if !exists {
		return []Problem{{Message: at + ".pack_ref does not exist for this change and caller"}}, nil
	}
	return nil, nil
}

func checkLocation(ctx context.Context, refs Refs, changeRef workflow.ChangeRef, at string, location finding.Location) ([]Problem, error) {
	exists, err := refs.FileExists(ctx, changeRef, location.File)
	if err != nil {
		return nil, fmt.Errorf("look up %s.file: %w", at, err)
	}
	lines, err := refs.LineCount(ctx, changeRef, location.File)
	if err != nil {
		return nil, fmt.Errorf("look up %s.lines: %w", at, err)
	}
	var problems []Problem
	if !exists {
		problems = append(problems, Problem{Message: at + ".file does not exist at the change head"})
	}
	first, last := location.Lines[0], location.Lines[1]
	if first < 1 || last < first || last > lines {
		problems = append(problems, Problem{Message: fmt.Sprintf("%s.lines [%d, %d] is outside the file", at, first, last)})
	}
	return problems, nil
}

func outsideDiffCategory(category finding.Category) bool {
	return category == finding.CategoryCompat || category == finding.CategoryOps || category == finding.CategorySecurity
}

func hasReasoning(evidence []finding.Evidence) bool {
	for _, item := range evidence {
		if item.Type == finding.EvidenceReasoning && item.Reasoning != nil && strings.TrimSpace(*item.Reasoning) != "" {
			return true
		}
	}
	return false
}

func tier(input finding.Input) finding.Tier {
	best := finding.TierE4
	for _, evidence := range input.Evidence {
		candidate := finding.TierE4
		switch evidence.Type {
		case finding.EvidenceReproTest:
			candidate = finding.TierE1
		case finding.EvidenceToolResult:
			candidate = finding.TierE2
		case finding.EvidenceMemory:
			candidate = finding.TierE3
		case finding.EvidencePackRef, finding.EvidenceReasoning:
		}
		if candidate.AtLeast(best) {
			best = candidate
		}
	}
	return best
}

func joinedProblems(err error) []Problem {
	if err == nil {
		return nil
	}
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		var out []Problem
		for _, child := range joined.Unwrap() {
			out = append(out, joinedProblems(child)...)
		}
		return out
	}
	return []Problem{{Message: err.Error()}}
}
