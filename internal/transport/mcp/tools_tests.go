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
		{name: "submit_tests", description: "Submit your acceptance tests once, as full files with the criteria they cover, and return what was accepted or the reasons to fix. Use it when the tests fail on base for the right reason; do not submit files outside the tests directory. Example: submit_tests {\"files\":[{\"path\":\"tests/heimdall/KAN-5/a_test.go\",\"content\":\"...\"}],\"acceptance_criteria\":{\"AC1\":[\"TestA\"]},\"expected_paths\":[\"internal/**\"],\"notes\":\"...\"}",
			schema: json.RawMessage(submitSchema), handle: s.submitTests},
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

// criterionIDs lists the acceptance criterion ids of a task, such as AC2.
func criterionIDs(in workflow.Inputs) []string {
	ids := make([]string, 0, len(in.AcceptanceCriteria))
	for _, c := range in.AcceptanceCriteria {
		ids = append(ids, c.ID)
	}
	return ids
}
