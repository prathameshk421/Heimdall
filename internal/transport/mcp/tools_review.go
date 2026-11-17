package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/deliberation"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/schema"
)

// reviewTools are the tools of the review area: submitting a review and a verdict, and reading one's own cards.
func (s *Server) reviewTools() ([]tool, error) {
	reviewSchema, err := schema.Schema("Review@1")
	if err != nil {
		return nil, fmt.Errorf("schema of submit_review: %w", err)
	}
	verdictSchema, err := schema.Schema("Verdict@1")
	if err != nil {
		return nil, fmt.Errorf("schema of submit_verdict: %w", err)
	}
	tools := []tool{
		{name: "submit_review", description: "Submit your review of the pull request once, with findings, evidence and rule_checks, and return what was accepted or the errors to fix. Use it when your hypotheses are checked; do not submit a finding you could not tie to an input. Example: submit_review {\"head_sha\":\"<head>\",\"files_reviewed\":[\"a.go\"],\"clean\":true,\"findings\":[],\"rule_checks\":[]}",
			schema: json.RawMessage(reviewSchema), handle: s.submitReview},
		{name: "submit_verdict", description: "Submit your verdict on the disputed finding once, with a citation for every claim. Use it when you have checked the facts; do not cite a ref the case does not contain. Example: submit_verdict {\"case_id\":\"case_...\",\"decision\":\"uphold\",\"rationale\":\"...\",\"citations\":[],\"confidence\":\"high\"}",
			schema: json.RawMessage(verdictSchema), handle: s.submitVerdict},
	}
	if s.deps.Cards != nil {
		tools = append(tools, tool{name: "read_card", description: "List your personal context cards with no file, or return one card by file name. Use it to read the rules your person enforces; a card says where to look and is not evidence. Example: read_card {\"file\":\"refund-cap.md\"}", schema: readCardSchema(), handle: s.readCard})
	}
	return tools, nil
}

func readCardSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string","maxLength":128,"description":"A card file name from the list. Omit to list your cards."}},"additionalProperties":false}`)
}

func (s *Server) readCard(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var input struct {
		File string `json:"file,omitempty"`
	}
	if err := decodeArgs(args, &input); err != nil {
		return "", err
	}
	assignment, err := s.currentTask(ctx, p)
	if err != nil {
		return "", err
	}
	if assignment.Task.Role != workflow.RoleReviewer || p.Role != workflow.RoleReviewer {
		return "", refusal("Only the assigned reviewer can read personal cards.")
	}
	content, err := s.deps.Cards.Read(ctx, p, assignment, input.File)
	if err != nil {
		if errors.Is(err, ErrCardNotFound) {
			return "", refusal("No such card. Call read_card with no file to list your cards.")
		}
		return "", fmt.Errorf("read personal card: %w", err)
	}
	return wrapUntrusted("personal context card", []byte(content))
}

// caseMatchesTask reports whether a verdict's case id matches the assignment's task.
func caseMatchesTask(caseID id.CaseID, t workflow.TaskEnvelope) bool {
	return t.CaseID != "" && t.CaseID == caseID
}

// submitReview validates a review against its schema and task, then passes it to the application.
// A review after the deadline is recorded as late. An invalid finding is returned to the reviewer with errors.
func (s *Server) submitReview(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var r finding.Review
	if err := decodeArgs(args, &r); err != nil {
		return "", err
	}
	a, err := s.currentTask(ctx, p)
	if err != nil {
		return "", err
	}
	if err := r.Validate(); err != nil {
		return "", refusalf("The review is not valid. Fix these problems and submit again:\n%v", err)
	}
	if r.HeadSHA != a.Task.Change.HeadSHA {
		return "", refusal("The head_sha does not match the change's head commit.")
	}
	late := s.deps.Clock.Now().After(a.Task.Deadline)
	receipt, err := s.deps.Reviews.SubmitReview(ctx, a, r, late)
	if err != nil {
		return "", fmt.Errorf("record review of task %s: %w", a.Task.TaskID, err)
	}
	if len(receipt.FindingErrors) > 0 {
		var lines []string
		for _, fe := range receipt.FindingErrors {
			lines = append(lines, fmt.Sprintf("finding %d: %s", fe.Index, fe.Message))
		}
		return "", refusalf("The review has invalid findings. Fix these problems and submit again:\n%s", strings.Join(lines, "\n"))
	}
	result := "The review is recorded."
	if late {
		result = "The review is recorded as late."
	}
	return result + s.obligationNudge(a, r), nil
}

// submitVerdict validates a verdict on a disputed finding and passes it to the application.
// A verdict after the deadline is recorded as late.
func (s *Server) submitVerdict(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var v deliberation.Verdict
	if err := decodeArgs(args, &v); err != nil {
		return "", err
	}
	a, err := s.currentTask(ctx, p)
	if err != nil {
		return "", err
	}
	if err := v.Validate(); err != nil {
		return "", refusalf("The verdict is not valid. Fix these problems and submit again:\n%v", err)
	}
	if !caseMatchesTask(v.CaseID, a.Task) {
		return "", refusal("The case_id does not match your active task.")
	}
	late := s.deps.Clock.Now().After(a.Task.Deadline)
	if err := s.deps.Reviews.SubmitVerdict(ctx, a, v, late); err != nil {
		return "", fmt.Errorf("record verdict of task %s: %w", a.Task.TaskID, err)
	}
	if late {
		return "The verdict is recorded as late.", nil
	}
	return "The verdict is recorded.", nil
}
