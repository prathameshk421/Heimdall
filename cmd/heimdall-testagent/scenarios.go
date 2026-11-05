package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// The tools this agent calls (TECH_SPEC 14.1).
const (
	toolGetTask        = "heimdall_get_task"
	toolAck            = "heimdall_ack"
	toolReadCode       = "heimdall_read_code"
	toolSubmitTests    = "heimdall_submit_tests"
	toolReportProgress = "heimdall_report_progress"
)

// The canned values every scenario shares.
const (
	// validTestFile is the canned test file inside the change's tests directory.
	validTestFile = "refund_over_limit_test.go"
	// validTestName is the test that covers every acceptance criterion in a canned submission.
	validTestName = "TestRefundOverLimitIsRejected"
	// outsideTestsFile is a test path outside tests/heimdall/<KEY>/, which the server must refuse.
	outsideTestsFile = "tests/other/refund_over_limit_test.go"
	// progressText is the one progress note, well under the server's 400 character limit.
	progressText = "The scripted test author is working through the acceptance criteria."
)

// validTest is a compiling Go test that fails on the base commit, because the refund cap is not enforced there. It
// uses only the standard library, so the "valid" scenario does not depend on the fixture repository's internals.
const validTest = `package heimdall_test

import "testing"

// TestRefundOverLimitIsRejected covers the ticket's refund cap: a refund above its payment is refused.
func TestRefundOverLimitIsRejected(t *testing.T) {
	t.Fatal("a refund larger than its payment was accepted; want an error")
}
`

// unbuildableTest is a Go test that does not compile, because it calls a symbol the fixture repository does not have.
// The MCP edge checks only the submission's shape; the seam check is what must reject this file.
const unbuildableTest = `package heimdall_test

import (
	"testing"

	"github.com/aarush323/heimdall-fixture-pay/internal/refunds"
)

// TestRefundOverLimitIsRejected calls a symbol that does not exist, so the test package does not build.
func TestRefundOverLimitIsRejected(t *testing.T) {
	t.Fatal(refunds.NoSuchTestHelper())
}
`

// scenarioRun plays one scenario against the server and writes its outcome lines to out.
type scenarioRun func(ctx context.Context, c *client, out io.Writer) error

// scenario is one canned behaviour the scripted agent plays: a name a run chooses, the failure it exists for, and the
// steps it takes.
type scenario struct {
	name  string
	about string
	run   scenarioRun
}

// allScenarios is the scenario table. Each name is part of the test interface, so a name is never reused for another
// meaning.
func allScenarios() []scenario {
	return []scenario{
		{name: "valid", about: "a correct submission that covers every acceptance criterion", run: runValid},
		{name: "missing-ac", about: "a submission that leaves one acceptance criterion with no test", run: runMissingAC},
		{name: "import-error", about: "a submission whose test file would not compile", run: runImportError},
		{name: "outside-dir", about: "a file written outside tests/heimdall/<KEY>/", run: runOutsideDir},
		{name: "schema-bad", about: "a JSON field the schema does not have", run: runSchemaBad},
		{name: "slow", about: "acknowledge, report progress, then wait for the deadline without submitting", run: runSlow},
		{name: "silent", about: "never acknowledge the task", run: runSilent},
	}
}

// scenarioFor returns the named scenario. The name is exact, so a typo runs nothing instead of the nearest match.
func scenarioFor(name string) (scenario, bool) {
	for _, s := range allScenarios() {
		if s.name == name {
			return s, true
		}
	}
	return scenario{}, false
}

// scenarioNames lists the canned names for a usage message.
func scenarioNames() []string {
	names := make([]string, 0, len(allScenarios()))
	for _, s := range allScenarios() {
		names = append(names, s.name)
	}
	return names
}

// submissionBuilder returns the arguments of heimdall_submit_tests for one scenario. A builder that cannot derive its
// canned submission from this task returns an error instead of sending something else.
type submissionBuilder func(task workflow.TaskEnvelope) (any, error)

// runValid plays the whole test-author flow with the correct submission.
func runValid(ctx context.Context, c *client, out io.Writer) error {
	return submitFlow(ctx, c, out, "valid", buildValid)
}

// runMissingAC submits a submission one acceptance criterion short.
func runMissingAC(ctx context.Context, c *client, out io.Writer) error {
	return submitFlow(ctx, c, out, "missing-ac", buildMissingAC)
}

// runImportError submits a well-formed file that would not compile.
func runImportError(ctx context.Context, c *client, out io.Writer) error {
	return submitFlow(ctx, c, out, "import-error", buildImportError)
}

// runOutsideDir submits a file outside the change's tests directory.
func runOutsideDir(ctx context.Context, c *client, out io.Writer) error {
	return submitFlow(ctx, c, out, "outside-dir", buildOutsideDir)
}

// runSchemaBad submits a document with a field the schema does not have.
func runSchemaBad(ctx context.Context, c *client, out io.Writer) error {
	return submitFlow(ctx, c, out, "schema-bad", buildSchemaBad)
}

// submitFlow plays the test-author flow: read the task, acknowledge it, read the changed code at the base commit,
// submit the canned tests and report progress. The server's answer to the submission is the outcome: a refusal is
// printed as one line and is not an error, because it is what the scenario exists to produce.
func submitFlow(ctx context.Context, c *client, out io.Writer, name string, build submissionBuilder) error {
	task, err := fetchTask(ctx, c)
	if err != nil {
		return err
	}
	if _, err := setupCall(ctx, c, toolAck, map[string]any{}); err != nil {
		return err
	}
	if err := readBaseFile(ctx, c, task); err != nil {
		return err
	}
	args, err := build(task)
	if err != nil {
		return fmt.Errorf("build the %s submission: %w", name, err)
	}
	res, err := c.callTool(ctx, toolSubmitTests, args)
	if err != nil {
		return err
	}
	if err := reportOutcome(out, name, res); err != nil {
		return err
	}
	if res.IsError {
		return nil
	}
	return reportProgress(ctx, c)
}

// runSlow acknowledges, reports progress and then waits for its context to end without submitting, so a deadline test
// can watch the server time the task out. Ending on the context is the scripted behaviour, so it is not an error.
func runSlow(ctx context.Context, c *client, out io.Writer) error {
	if _, err := setupCall(ctx, c, toolAck, map[string]any{}); err != nil {
		return err
	}
	if err := reportProgress(ctx, c); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "slow: waiting: acknowledged and reported progress; no submission will be sent\n"); err != nil {
		return fmt.Errorf("write the slow outcome: %w", err)
	}
	<-ctx.Done()
	return nil
}

// runSilent reads the task and stops without acknowledging, which is the 60 second ack window's failure path.
func runSilent(ctx context.Context, c *client, out io.Writer) error {
	if _, err := fetchTask(ctx, c); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "silent: no ack: read the task and stopped without acknowledging\n"); err != nil {
		return fmt.Errorf("write the silent outcome: %w", err)
	}
	return nil
}

// fetchTask reads the agent's one active task and decodes the envelope with the domain type, so the wire form and the
// agent's reading of it cannot drift.
func fetchTask(ctx context.Context, c *client) (workflow.TaskEnvelope, error) {
	res, err := setupCall(ctx, c, toolGetTask, map[string]any{})
	if err != nil {
		return workflow.TaskEnvelope{}, err
	}
	var task workflow.TaskEnvelope
	if err := json.Unmarshal([]byte(res.text()), &task); err != nil {
		return workflow.TaskEnvelope{}, fmt.Errorf("decode the task envelope: %w", err)
	}
	return task, nil
}

// readBaseFile reads the first changed file at the base commit, which is all a test-author may read.
func readBaseFile(ctx context.Context, c *client, task workflow.TaskEnvelope) error {
	if len(task.Inputs.FilesChanged) == 0 {
		return errors.New("the task names no changed file to read")
	}
	args := map[string]any{"path": task.Inputs.FilesChanged[0], "sha": string(task.Change.BaseSHA)}
	_, err := setupCall(ctx, c, toolReadCode, args)
	return err
}

// reportProgress sends the one progress note the server allows per task every 2 minutes.
func reportProgress(ctx context.Context, c *client) error {
	_, err := setupCall(ctx, c, toolReportProgress, map[string]any{"text": progressText})
	return err
}

// setupCall runs a step whose refusal means the scenario could not run as scripted. Only the submission may be
// refused as an outcome, so everything else turns a refusal into an error with the reason.
func setupCall(ctx context.Context, c *client, name string, args any) (toolResult, error) {
	res, err := c.callTool(ctx, name, args)
	if err != nil {
		return toolResult{}, err
	}
	if res.IsError {
		return toolResult{}, fmt.Errorf("%s was refused: %s", name, oneLine(res.text()))
	}
	return res, nil
}

// reportOutcome writes the one line that says what the server did with the submission.
func reportOutcome(out io.Writer, name string, res toolResult) error {
	verb := "recorded"
	if res.IsError {
		verb = "refused"
	}
	if _, err := fmt.Fprintf(out, "%s: %s: %s\n", name, verb, oneLine(res.text())); err != nil {
		return fmt.Errorf("write the %s outcome: %w", name, err)
	}
	return nil
}

// oneLine flattens text to one line, so a multi-line refusal stays the single outcome line a scripted run prints.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// buildValid sends the correct submission unchanged.
func buildValid(task workflow.TaskEnvelope) (any, error) {
	return correctSubmission(task), nil
}

// buildMissingAC removes the last acceptance criterion from the coverage map, so the server must refuse the
// submission for that criterion.
func buildMissingAC(task workflow.TaskEnvelope) (any, error) {
	ids := criterionIDs(task)
	if len(ids) == 0 {
		return nil, errors.New("the task has no acceptance criterion to leave out")
	}
	sub := correctSubmission(task)
	delete(sub.AcceptanceCriteria, ids[len(ids)-1])
	return sub, nil
}

// buildImportError keeps the shape valid and submits a file that would not compile, which the seam check must reject.
func buildImportError(task workflow.TaskEnvelope) (any, error) {
	sub := correctSubmission(task)
	sub.Files[0].Content = unbuildableTest
	sub.Notes = "This file calls a symbol that does not exist; the compile check must refuse it."
	return sub, nil
}

// buildOutsideDir writes the test file outside the change's tests directory, which the server must refuse.
func buildOutsideDir(task workflow.TaskEnvelope) (any, error) {
	sub := correctSubmission(task)
	sub.Files[0].Path = outsideTestsFile
	return sub, nil
}

// buildSchemaBad adds a top-level field the TestSubmission schema does not have, so the server must refuse the call
// before the validator sees it.
func buildSchemaBad(task workflow.TaskEnvelope) (any, error) {
	doc, err := asDocument(correctSubmission(task))
	if err != nil {
		return nil, err
	}
	doc["ticket_key"] = string(task.Change.ID)
	return doc, nil
}

// correctSubmission returns the submission the server must accept: one compiling test file under the change's tests
// directory, with every acceptance criterion mapped to its test.
func correctSubmission(task workflow.TaskEnvelope) testset.Submission {
	coverage := make(map[string][]string, len(task.Inputs.AcceptanceCriteria))
	for _, c := range task.Inputs.AcceptanceCriteria {
		coverage[c.ID] = []string{validTestName}
	}
	return testset.Submission{
		Files:              []testset.File{{Path: testsDirectory(task) + "/" + validTestFile, Content: validTest}},
		AcceptanceCriteria: coverage,
		ExpectedPaths:      expectedPaths(task),
		Notes:              "The ticket names the refund cap but no test covers it yet; the test asserts the cap the ticket states.",
	}
}

// criterionIDs lists the acceptance criterion ids of the task, such as AC1.
func criterionIDs(task workflow.TaskEnvelope) []string {
	ids := make([]string, 0, len(task.Inputs.AcceptanceCriteria))
	for _, c := range task.Inputs.AcceptanceCriteria {
		ids = append(ids, c.ID)
	}
	return ids
}

// testsDirectory is where the policy puts this change's tests (D150).
func testsDirectory(task workflow.TaskEnvelope) string {
	return "tests/heimdall/" + string(task.Change.ID)
}

// expectedPaths are the changed paths outside the tests directory, which a submission expects the change to touch.
func expectedPaths(task workflow.TaskEnvelope) []string {
	dir := testsDirectory(task) + "/"
	out := make([]string, 0, len(task.Inputs.FilesChanged))
	for _, f := range task.Inputs.FilesChanged {
		if !strings.HasPrefix(f, dir) {
			out = append(out, f)
		}
	}
	return out
}

// asDocument renders a submission as the JSON object the wire carries, so a scenario can add a field.
func asDocument(sub testset.Submission) (map[string]any, error) {
	raw, err := json.Marshal(sub)
	if err != nil {
		return nil, fmt.Errorf("encode the submission: %w", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("read the submission back: %w", err)
	}
	return doc, nil
}
