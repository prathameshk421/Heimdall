package mcp

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reviewbrief"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// briefDeadline bounds one compile of a brief. It stays under the 30 s deadline of the stdio bridge, so a slow source
// costs a section and never the get_task call.
const briefDeadline = 26 * time.Second

// maxHeldBriefs bounds the memory the held briefs use. Dropping them costs only a new compile and a missed nudge.
const maxHeldBriefs = 256

// heldBrief is the last brief of a task and the head commit it describes.
type heldBrief struct {
	head  id.SHA
	brief reviewbrief.Brief
}

// taskBrief returns the held brief of the task when it is whole and for the same head, and compiles it otherwise.
func (s *Server) taskBrief(ctx context.Context, p Principal, a Assignment) (reviewbrief.Brief, error) {
	head := a.Task.Change.HeadSHA
	if held, ok := s.heldBrief(a.Task.TaskID, head); ok && !held.Partial {
		return held, nil
	}
	ctx, cancel := context.WithTimeout(ctx, briefDeadline)
	defer cancel()
	brief, err := s.deps.Briefs.Brief(ctx, p, a)
	if err != nil {
		return reviewbrief.Brief{}, fmt.Errorf("compile brief of task %s: %w", a.Task.TaskID, err)
	}
	s.briefMu.Lock()
	defer s.briefMu.Unlock()
	if len(s.briefs) >= maxHeldBriefs {
		clear(s.briefs)
	}
	s.briefs[a.Task.TaskID] = heldBrief{head: head, brief: brief}
	return brief, nil
}

func (s *Server) heldBrief(task id.TaskID, head id.SHA) (reviewbrief.Brief, bool) {
	s.briefMu.Lock()
	defer s.briefMu.Unlock()
	held, ok := s.briefs[task]
	if !ok || held.head != head {
		return reviewbrief.Brief{}, false
	}
	return held.brief, true
}

// obligationNudge names the obligation cards that the review left out of rule_checks. It never refuses the review: a
// card can be wrong, and the outcomes judge the reviewer. A task with no held brief gets no nudge.
func (s *Server) obligationNudge(a Assignment, r finding.Review) string {
	brief, ok := s.heldBrief(a.Task.TaskID, a.Task.Change.HeadSHA)
	if !ok {
		return ""
	}
	var open []string
	for _, card := range brief.ObligationCards {
		answered := slices.ContainsFunc(r.RuleChecks, func(c finding.RuleCheck) bool { return c.Card == card })
		if !answered {
			open = append(open, card)
		}
	}
	if len(open) == 0 {
		return ""
	}
	return " These obligation cards have no rule_checks entry: " + strings.Join(open, ", ") + ". Answer every obligation in your next review."
}
