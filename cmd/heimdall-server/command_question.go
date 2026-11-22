package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/collaboration"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// AnswerQuestion answers a mapped person's plain-words @heimdall question from the change's recorded facts (T1, U6).
func (r *commandRuntime) AnswerQuestion(ctx context.Context, target collaboration.Target, _ string, _ string) (string, error) {
	if target.ChangeID == "" {
		return "Ask in a change's room so I can check its status.", nil
	}
	events, err := r.events.Load(ctx, target.ChangeID)
	if err != nil {
		return "", fmt.Errorf("load change %s for question: %w", target.ChangeID, err)
	}
	view, err := change.Fold(events)
	if err != nil {
		return "", fmt.Errorf("fold change %s for question: %w", target.ChangeID, err)
	}
	return formatQuestionAnswer(target.ChangeID.String(), view), nil
}

func formatQuestionAnswer(changeName string, view change.View) string {
	parts := []string{statusSentence(changeName, view)}
	if view.PR != nil && view.PR.Gate != nil && !view.PR.Gate.OK && len(view.PR.Gate.Reasons) > 0 {
		parts = append(parts, fmt.Sprintf("Gate blocked: %s.", strings.Join(view.PR.Gate.Reasons, "; ")))
	}
	if open := openFindingRefs(view); len(open) > 0 {
		parts = append(parts, fmt.Sprintf("Open findings: %s.", strings.Join(open, ", ")))
	}
	if active := activeTaskSummary(view); len(active) > 0 {
		parts = append(parts, fmt.Sprintf("Active tasks: %s.", strings.Join(active, ", ")))
	}
	if view.Trigger != nil && view.Trigger.CriteriaEdited {
		parts = append(parts, "Acceptance criteria were edited; tests must cover all current criteria.")
	}
	return strings.Join(parts, " ")
}

func statusSentence(changeName string, view change.View) string {
	switch {
	case view.Closed != nil && view.Closed.Merged:
		return fmt.Sprintf("%s is merged.", changeName)
	case view.Closed != nil:
		return fmt.Sprintf("%s is closed.", changeName)
	case view.PR != nil:
		return fmt.Sprintf("%s has open PR #%d.", changeName, view.PR.Number)
	default:
		return fmt.Sprintf("%s is active (no PR open yet).", changeName)
	}
}

func openFindingRefs(view change.View) []string {
	var refs []string
	for ref, f := range view.Findings {
		if !f.State.Terminal() {
			refs = append(refs, fmt.Sprintf("%s (%s)", ref, f.State))
		}
	}
	slices.Sort(refs)
	return refs
}

func activeTaskSummary(view change.View) []string {
	var items []string
	for _, t := range view.Tasks {
		if t.Live() {
			items = append(items, fmt.Sprintf("%s (%s)", t.Kind, t.Agent))
		}
	}
	slices.Sort(items)
	return items
}
