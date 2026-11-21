package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reviewbrief"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/taskinput"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/skills"
)

// taskView is the answer of get_task: the task, the instructions of its kind, every input rendered now, and what the
// change still needs (HARNESS_DESIGN B6). It is the one call an agent makes to start.
type taskView struct {
	Task struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		Change   string `json:"change"`
		Base     string `json:"base"`
		Head     string `json:"head"`
		Deadline string `json:"deadline"`
		Case     string `json:"case,omitempty"`
	} `json:"task"`
	Instructions string              `json:"instructions"`
	Inputs       []taskinput.Section `json:"inputs"`
	Needs        []string            `json:"needs"`
}

// getTask answers with the whole task. The first call of a task also acknowledges it. With no active task it says so in
// plain words: that is an answer, not a refusal.
func (s *Server) getTask(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	if err := noArgs(args); err != nil {
		return "", err
	}
	a, err := s.deps.Tasks.Current(ctx, p)
	switch {
	case errors.Is(err, ErrNoTask):
		// No task is an answer, not a refusal: the agent has nothing to do until a message wakes it.
		return "You have no active task. Wait for a message that names a task, then call get_task again.", nil
	case err != nil:
		return "", fmt.Errorf("current task of agent %s: %w", p.Agent, err)
	case a.Task.Role != p.Role:
		return "", refusal("Your active task is not for your role.")
	}
	kind, ok := workflow.KindFor(a.Task.Role)
	if !ok {
		return "", refusalf("Your role holds no kind of work.")
	}
	now := s.deps.Clock.Now()
	if !a.Acked && (a.Task.Deadline.IsZero() || !now.After(a.Task.Deadline)) {
		if err := s.deps.Tasks.Ack(ctx, a, now); err != nil {
			return "", fmt.Errorf("ack task %s: %w", a.Task.TaskID, err)
		}
	}
	var v taskView
	v.Task.ID, v.Task.Kind, v.Task.Change = string(a.Task.TaskID), kind.Name, string(a.Task.Change.ID)
	v.Task.Base, v.Task.Head, v.Task.Deadline = string(a.Task.Change.BaseSHA), string(a.Task.Change.HeadSHA), a.Task.Deadline.UTC().Format("2006-01-02T15:04:05Z")
	v.Task.Case = string(a.Task.CaseID)
	v.Instructions = "This task is for " + v.Task.Change + ". Work only on " + v.Task.Change + ": earlier work is finished, so ignore what you remember about it. " +
		"Start your messages in the channel with " + v.Task.Change + ".\n\n" + skills.For(kind)
	v.Inputs = taskinput.Render(ctx, kind, taskinput.Source{Task: a.Task, Rejection: a.Rejection, Attempt: a.Attempt, Leads: a.Leads}, s.inputDeps(p, a))
	v.Needs = s.needs(ctx, a)
	return marshalText(v)
}

// inputDeps are what the input renderers read from: the blob store, the file list, the untrusted wrapper and, for the
// reviewer and resolver, the compiled brief.
func (s *Server) inputDeps(p Principal, a Assignment) taskinput.Deps {
	d := taskinput.Deps{Blobs: s.deps.Blobs, Paths: s.deps.Paths, Wrap: wrapUntrusted, Providers: map[string]taskinput.Provider{}}
	switch {
	case s.deps.Briefs != nil && (p.Role == workflow.RoleReviewer || p.Role == workflow.RoleResolver):
		s.addBriefProviders(d, p, a)
	case s.deps.Profiles != nil && p.Role == workflow.RoleProfiler:
		d.Providers["previous_trial"] = func(ctx context.Context, _ taskinput.Source) (string, error) {
			return s.deps.Profiles.PreviousTrial(ctx, a.Task.Change.Repo)
		}
	case s.deps.Cards != nil && p.Role == workflow.RoleReviewer:
		d.Providers["obligations"] = func(ctx context.Context, _ taskinput.Source) (string, error) {
			return s.deps.Cards.ForTask(ctx, p, a)
		}
	}
	return d
}

// addBriefProviders serves obligations, prior findings and team memory from one compiled brief: one get_task compiles
// it once, however many inputs read it.
func (s *Server) addBriefProviders(d taskinput.Deps, p Principal, a Assignment) {
	var once sync.Once
	var compiled reviewbrief.Brief
	var compileErr error
	brief := func(pick func(b briefText) string) taskinput.Provider {
		return func(ctx context.Context, _ taskinput.Source) (string, error) {
			once.Do(func() { compiled, compileErr = s.taskBrief(ctx, p, a) })
			if compileErr != nil {
				return "", compileErr
			}
			return pick(briefText{compiled.Obligations, compiled.PriorFindings, compiled.Memory}), nil
		}
	}
	d.Providers["obligations"] = brief(func(b briefText) string { return b.obligations })
	d.Providers["prior_findings"] = brief(func(b briefText) string { return b.prior })
	d.Providers["team_memory"] = brief(func(b briefText) string { return b.memory })
}

type briefText struct{ obligations, prior, memory string }

// needs is what the change still needs, in plain words, when the server can say; a failure is logged and the task goes on.
func (s *Server) needs(ctx context.Context, a Assignment) []string {
	if s.deps.Needs == nil {
		return []string{}
	}
	lines, err := s.deps.Needs(ctx, a.Task.Change.ID)
	if err != nil {
		s.deps.Logger.WarnContext(ctx, "needs unavailable for task", "change", string(a.Task.Change.ID), "error", err)
		return []string{}
	}
	if lines == nil {
		return []string{}
	}
	return lines
}

// ToolNames are the names of every tool this server registered, withheld ones included, for the check of the kind
// table: a withheld tool is wired, only its dependency is down.
func (s *Server) ToolNames() []string {
	return slices.Clone(s.registered)
}

// SchemaNames are the output schemas whose submit tool is registered.
func (s *Server) SchemaNames() []string {
	bySubmit := map[string]string{"submit_tests": "TestSubmission@1", "submit_review": "Review@2", "submit_verdict": "Verdict@1", "propose_profile": "Profile@1"}
	var out []string
	for _, name := range s.registered {
		if schema, ok := bySubmit[name]; ok {
			out = append(out, schema)
		}
	}
	return out
}
