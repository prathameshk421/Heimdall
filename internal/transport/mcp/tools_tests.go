package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/schema"
)

// testsTools are the tools of the tests area: submitting the acceptance tests.
func (s *Server) testsTools() ([]tool, error) {
	submitSchema, err := schema.Schema("TestSubmission@1")
	if err != nil {
		return nil, fmt.Errorf("schema of submit_tests: %w", err)
	}
	return []tool{
		{name: "submit_tests", description: "Submit the test files that cover your acceptance criteria.",
			schema: json.RawMessage(submitSchema), roles: []workflow.Role{workflow.RoleTestAuthor}, handle: s.submitTests},
	}, nil
}

// submitTests checks a test submission against the task and records it. A submission after the deadline is recorded
// as late.
func (s *Server) submitTests(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var sub testset.Submission
	if err := decodeArgs(args, &sub); err != nil {
		return "", err
	}
	a, err := s.currentTask(ctx, p)
	if err != nil {
		return "", err
	}
	expect := testset.Expect{TestsDir: "tests/heimdall/" + string(a.Task.Change.ID), ACIDs: criterionIDs(a.Task.Inputs)}
	if err := sub.Validate(expect); err != nil {
		return "", refusalf("The submission is not valid. Fix these problems and submit again:\n%v", err)
	}
	late := s.deps.Clock.Now().After(a.Task.Deadline)
	if err := s.deps.Tasks.SubmitTests(ctx, a, sub, late); err != nil {
		if errors.Is(err, ErrInvalidSubmission) {
			return "", refusalf("The submission is not valid. Fix these problems and submit again:\n%v", err)
		}
		return "", fmt.Errorf("record test submission of task %s: %w", a.Task.TaskID, err)
	}
	if late {
		return "The submission is recorded as late.", nil
	}
	return "The submission is recorded.", nil
}
