package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/sarif"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// checksTools are the tools of the checks area: running a deterministic check, when a sandbox is wired.
func (s *Server) checksTools() ([]tool, error) {
	if s.deps.Sandbox == nil || s.deps.Checks == nil {
		return nil, nil
	}
	return []tool{
		{name: "run_check", description: "Run one check of the repository profile on the head commit and return the new results as file:line rule: message. The id must be one listed in your leads; at most 5 calls per task. Use it before you cite a tool result; do not use it to explore. Example: run_check {\"check_id\":\"lint\"}", schema: runCheckSchema(), handle: s.runCheck},
	}, nil
}

// maxChecksPerTask bounds the heimdall_run_check calls of one task; each one is a sandbox job.
const maxChecksPerTask = 5

// maxCheckDiagnostics bounds the diagnostics one check call returns.
const maxCheckDiagnostics = 50

func runCheckSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"check_id":{"type":"string","minLength":1,"maxLength":64,` +
		`"description":"The id of a check listed in your leads."}},"required":["check_id"],"additionalProperties":false}`)
}

// CheckRow is one check of the repository's promoted profile, as the tool needs it.
type CheckRow struct {
	ID       string
	Lens     string
	Baseline bool
}

// Checks reads the checks of a repository's promoted profile. A repository with none returns no rows.
type Checks interface {
	ChecksOf(ctx context.Context, repo change.Repo) ([]CheckRow, error)
}

// runCheck runs one check of the profile on the head commit through the runner. The agent names only the id: the
// command, the commit and the limits come from the profile. The result is the tool's own output, so it is wrapped as
// untrusted text like any other text from the repository.
func (s *Server) runCheck(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var in struct {
		CheckID string `json:"check_id"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	a, err := s.currentTask(ctx, p)
	if err != nil {
		return "", err
	}
	repo := change.Repo(a.Task.Change.Repo)
	rows, err := s.deps.Checks.ChecksOf(ctx, repo)
	if err != nil {
		return "", fmt.Errorf("read profile checks: %w", err)
	}
	row, ids := findCheck(rows, in.CheckID)
	if row == nil {
		if len(ids) == 0 {
			return "", refusal("This repository has no promoted profile with checks.")
		}
		return "", refusalf("Unknown check %q. The checks you can run are: %s.", in.CheckID, strings.Join(ids, ", "))
	}
	if !s.reserveCheck(a.Task.TaskID) {
		return "", refusalf("You have used your %d checks for this task.", maxChecksPerTask)
	}
	head, err := s.checkAt(ctx, a, *row, a.Task.Change.HeadSHA, "")
	if err != nil {
		return "", err
	}
	if row.Baseline && a.Task.Change.BaseSHA != "" {
		base, err := s.checkAt(ctx, a, *row, a.Task.Change.BaseSHA, a.Task.Change.HeadSHA)
		if err != nil {
			return "", err
		}
		head = sarif.New(base, head)
	}
	return wrapUntrusted(in.CheckID+" at the head commit", []byte(formatCheck(row.ID, head)))
}

func findCheck(rows []CheckRow, id string) (*CheckRow, []string) {
	var ids []string
	var found *CheckRow
	for i, r := range rows {
		ids = append(ids, r.ID)
		if r.ID == id {
			found = &rows[i]
		}
	}
	return found, ids
}

// checkAt runs the check at one commit and parses its SARIF.
func (s *Server) checkAt(ctx context.Context, a Assignment, row CheckRow, sha, head id.SHA) ([]sarif.Result, error) {
	res, err := s.deps.Sandbox.Run(ctx, ports.SandboxJob{Kind: ports.SandboxCheck, CheckID: row.ID, Repo: change.Repo(a.Task.Change.Repo),
		SHA: sha, Head: head, Change: a.Task.Change.ID, OnDemand: true, Timeout: 3 * time.Minute})
	if err != nil {
		return nil, fmt.Errorf("run check: %w", err)
	}
	results, err := sarif.Parse(res.Sarif, row.ID, row.Lens)
	if err != nil {
		return nil, refusalf("The check %s wrote a result that is not valid SARIF.", row.ID)
	}
	return results, nil
}

// reserveCheck counts one more check for the task and reports whether it is within the budget.
func (s *Server) reserveCheck(task id.TaskID) bool {
	s.checkMu.Lock()
	defer s.checkMu.Unlock()
	if s.checkRuns[task] >= maxChecksPerTask {
		return false
	}
	s.checkRuns[task]++
	return true
}

// formatCheck renders the new results of a check as lines "file:line rule: message", at most maxCheckDiagnostics.
func formatCheck(id string, results []sarif.Result) string {
	if len(results) == 0 {
		return "PASS: " + id + " reported nothing new on the head commit."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s reported %d new result(s).\n", id, len(results))
	for i, r := range results {
		if i == maxCheckDiagnostics {
			fmt.Fprintf(&b, "... %d more\n", len(results)-i)
			break
		}
		fmt.Fprintf(&b, "%s:%d %s: %s\n", r.File, r.Lines[0], r.Rule, r.Message)
	}
	return b.String()
}
