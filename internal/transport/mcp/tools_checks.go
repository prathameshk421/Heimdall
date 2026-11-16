package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// checksTools are the tools of the checks area: running a deterministic check, when a sandbox is wired.
func (s *Server) checksTools() ([]tool, error) {
	if s.deps.Sandbox == nil {
		return nil, nil
	}
	return []tool{
		{name: "run_check", description: "Run a deterministic check on the head commit through the sandbox and read its located diagnostics. Use it before you cite a tool result in a finding.", schema: runCheckSchema(), roles: []workflow.Role{workflow.RoleReviewer, workflow.RoleResolver}, handle: s.runCheck},
	}, nil
}

// maxChecksPerTask bounds the heimdall_run_check calls of one task; each one is a sandbox job.
const maxChecksPerTask = 5

// maxCheckDiagnostics bounds the diagnostics one check call returns.
const maxCheckDiagnostics = 50

func runCheckSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string","enum":["go_vet"],` +
		`"description":"The check to run on the head commit."}},"required":["kind"],"additionalProperties":false}`)
}

// runCheck runs one fixed check on the head commit through the sandbox. The reviewer chooses only the check name; the
// command, the commit and the limits are fixed by the server. The result is the tool's own output, so it is wrapped
// as untrusted text like any other text from the repository.
func (s *Server) runCheck(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var in struct {
		Kind string `json:"kind"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if in.Kind != ports.CheckGoVet {
		return "", refusalf("Unknown check %q. The check you can run is %q.", in.Kind, ports.CheckGoVet)
	}
	a, err := s.currentTask(ctx, p)
	if err != nil {
		return "", err
	}
	if !s.reserveCheck(a.Task.TaskID) {
		return "", refusalf("You have used your %d checks for this task.", maxChecksPerTask)
	}
	job := ports.SandboxJob{
		Kind: ports.SandboxStatic, Check: ports.CheckGoVet, Repo: change.Repo(a.Task.Change.Repo),
		SHA: a.Task.Change.HeadSHA, Dir: ".", Timeout: 3 * time.Minute,
	}
	res, err := s.deps.Sandbox.Run(ctx, job)
	if err != nil {
		return "", fmt.Errorf("run check: %w", err)
	}
	return wrapUntrusted("go vet at the head commit", []byte(formatCheck(res)))
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

// formatCheck renders a check result: PASS, or the located diagnostics (at most maxCheckDiagnostics).
func formatCheck(res ports.SandboxResult) string {
	if res.Failed == 0 && res.Passed > 0 {
		return "PASS: go vet reported no diagnostics."
	}
	diags := verify.ParseVet([]byte(res.Output))
	var b strings.Builder
	fmt.Fprintf(&b, "FAIL: go vet reported %d diagnostics.\n", len(diags))
	for i, d := range diags {
		if i == maxCheckDiagnostics {
			fmt.Fprintf(&b, "... %d more\n", len(diags)-i)
			break
		}
		fmt.Fprintf(&b, "%s:%d:%d: %s\n", d.File, d.Line, d.Column, d.Message)
	}
	return b.String()
}
