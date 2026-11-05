package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// outsidePath is a file the change does not touch. The bad-path scenario names it in its first review.
const outsidePath = "internal/refunds/ledger.go"

// reviewClaim is the one finding the scripted reviewer files. go vet reports it: a printf verb that does not fit its
// argument. The finding names a changed file and a line range from that file.
const reviewClaim = "The over-payment error formats an int with %s, so its text reads %!s(int=100) and the caller cannot read the amount."

// reFindingIndex finds the index of each finding a refusal names, such as "finding 0:".
var reFindingIndex = regexp.MustCompile(`finding (\d+):`)

// reNumbered matches one numbered line of a read_code answer, such as "12: text".
var reNumbered = regexp.MustCompile(`^\d+: `)

// reviewBuild returns the first review of a scenario. lines holds the number of lines of each changed file at head.
type reviewBuild func(task workflow.TaskEnvelope, lines map[string]int) (finding.Review, error)

// reviewCorrect returns the corrected review after the server refused prev. refusal is the text of the refusal.
type reviewCorrect func(prev finding.Review, refusal string, task workflow.TaskEnvelope, lines map[string]int) (finding.Review, error)

// runReviewClean submits a review with no findings.
func runReviewClean(ctx context.Context, c *client, out io.Writer) error {
	return reviewFlow(ctx, c, out, "review-clean", cleanReview, nil)
}

// runReviewFinding submits one finding on the first changed file, with a line range from that file.
func runReviewFinding(ctx context.Context, c *client, out io.Writer) error {
	return reviewFlow(ctx, c, out, "review-finding", findingReview, nil)
}

// runReviewBadPath submits a finding outside the diff, and when the server refuses it, submits the corrected review.
func runReviewBadPath(ctx context.Context, c *client, out io.Writer) error {
	return reviewFlow(ctx, c, out, "review-bad-path", badPathReview, correctReview)
}

// runReviewSilent reads the reviewer task and stops without acknowledging it.
func runReviewSilent(ctx context.Context, c *client, out io.Writer) error {
	return silentFlow(ctx, c, out, "review-silent")
}

// reviewFlow plays the reviewer flow: read the task, acknowledge it, read the diff, read every changed file at head,
// then submit the review. A refusal is printed as the outcome. When correct is set, a refused review is corrected and
// submitted once more. A refused get_task is the outcome of the run, because the token does not hold this role's task.
func reviewFlow(ctx context.Context, c *client, out io.Writer, name string, build reviewBuild, correct reviewCorrect) error {
	task, started, err := startTask(ctx, c, out, name)
	if err != nil || !started {
		return err
	}
	if _, err := setupCall(ctx, c, toolReadUntrusted, map[string]any{"ref": string(task.Inputs.Diff)}); err != nil {
		return err
	}
	lines, err := readChangedFiles(ctx, c, task)
	if err != nil {
		return err
	}
	first, err := build(task, lines)
	if err != nil {
		return fmt.Errorf("build the %s review: %w", name, err)
	}
	res, err := c.callTool(ctx, toolSubmitReview, first)
	if err != nil {
		return err
	}
	if err := reportOutcome(out, name, res); err != nil {
		return err
	}
	if !res.IsError || correct == nil {
		return nil
	}
	fixed, err := correct(first, res.text(), task, lines)
	if err != nil {
		return fmt.Errorf("correct the %s review: %w", name, err)
	}
	res, err = c.callTool(ctx, toolSubmitReview, fixed)
	if err != nil {
		return err
	}
	return reportOutcome(out, name, res)
}

// startTask reads the agent's task and acknowledges it. The bool is false when the server refused the task: that refusal
// is written as the outcome line and the run ends without an error. Any other refusal is a setup failure.
func startTask(ctx context.Context, c *client, out io.Writer, name string) (workflow.TaskEnvelope, bool, error) {
	res, err := c.callTool(ctx, toolGetTask, map[string]any{})
	if err != nil {
		return workflow.TaskEnvelope{}, false, err
	}
	if res.IsError {
		return workflow.TaskEnvelope{}, false, reportOutcome(out, name, res)
	}
	var task workflow.TaskEnvelope
	if err := json.Unmarshal([]byte(res.text()), &task); err != nil {
		return workflow.TaskEnvelope{}, false, fmt.Errorf("decode the task envelope: %w", err)
	}
	if _, err := setupCall(ctx, c, toolAck, map[string]any{}); err != nil {
		return workflow.TaskEnvelope{}, false, err
	}
	return task, true, nil
}

// readChangedFiles reads every changed file at the head commit and returns how many numbered lines each one has.
func readChangedFiles(ctx context.Context, c *client, task workflow.TaskEnvelope) (map[string]int, error) {
	lines := make(map[string]int, len(task.Inputs.FilesChanged))
	for _, file := range task.Inputs.FilesChanged {
		res, err := setupCall(ctx, c, toolReadCode, map[string]any{"path": file, "sha": string(task.Change.HeadSHA)})
		if err != nil {
			return nil, err
		}
		n := numberedLines(res.text())
		if n == 0 {
			return nil, fmt.Errorf("%s has no numbered lines at the head commit", file)
		}
		lines[file] = n
	}
	return lines, nil
}

// numberedLines counts the lines of a read_code answer, which numbers each line of the file.
func numberedLines(text string) int {
	n := 0
	for _, line := range strings.Split(text, "\n") {
		if reNumbered.MatchString(line) {
			n++
		}
	}
	return n
}

// reviewOf returns a review of the task's change at the head commit. A clean review holds no findings.
func reviewOf(task workflow.TaskEnvelope, clean bool, findings []finding.Input) finding.Review {
	if findings == nil {
		findings = []finding.Input{}
	}
	return finding.Review{
		HeadSHA:       task.Change.HeadSHA,
		FilesReviewed: slices.Clone(task.Inputs.FilesChanged),
		Clean:         clean,
		Findings:      findings,
	}
}

// firstChanged returns the first changed file of the task, which every finding of these scenarios names.
func firstChanged(task workflow.TaskEnvelope) (string, error) {
	if len(task.Inputs.FilesChanged) == 0 {
		return "", errors.New("the task names no changed file")
	}
	return task.Inputs.FilesChanged[0], nil
}

// spanIn returns the line range the scripted finding cites in file: the first two lines, or the file's lines when it
// has fewer. The range exists in the file at head.
func spanIn(lines map[string]int, file string) ([2]int, error) {
	n := lines[file]
	if n == 0 {
		return [2]int{}, fmt.Errorf("%s has no lines to cite", file)
	}
	return [2]int{1, min(2, n)}, nil
}

// findingFor returns the scripted finding on file. Its evidence is E2: a go vet report at the same location.
func findingFor(file string, span [2]int, claim string) finding.Input {
	loc := finding.Location{File: file, Lines: span}
	return finding.Input{
		Claim:      claim,
		Category:   finding.CategoryCorrectness,
		Severity:   finding.SeverityMedium,
		Confidence: finding.ConfidenceHigh,
		Location:   loc,
		Evidence: []finding.Evidence{finding.NewToolResultEvidence(finding.ToolResult{
			Tool:     "go vet",
			Args:     []string{"./internal/refunds/..."},
			Location: loc,
		})},
		ContextRefs: []string{},
	}
}

// cleanReview is the review of a reviewer who files nothing.
func cleanReview(task workflow.TaskEnvelope, _ map[string]int) (finding.Review, error) {
	return reviewOf(task, true, nil), nil
}

// findingReview is the review with one finding on the first changed file.
func findingReview(task workflow.TaskEnvelope, lines map[string]int) (finding.Review, error) {
	file, err := firstChanged(task)
	if err != nil {
		return finding.Review{}, err
	}
	span, err := spanIn(lines, file)
	if err != nil {
		return finding.Review{}, err
	}
	return reviewOf(task, false, []finding.Input{findingFor(file, span, reviewClaim)}), nil
}

// badPathReview is the first review of the bad-path scenario: its one finding names a file outside the diff.
func badPathReview(task workflow.TaskEnvelope, _ map[string]int) (finding.Review, error) {
	return reviewOf(task, false, []finding.Input{findingFor(outsidePath, [2]int{1, 2}, reviewClaim)}), nil
}

// correctReview moves each finding that the refusal names onto the first changed file. Every other part of the review
// stays as it was.
func correctReview(prev finding.Review, refusal string, task workflow.TaskEnvelope, lines map[string]int) (finding.Review, error) {
	matches := reFindingIndex.FindAllStringSubmatch(refusal, -1)
	if len(matches) == 0 {
		return finding.Review{}, errors.New("the refusal names no finding to correct")
	}
	file, err := firstChanged(task)
	if err != nil {
		return finding.Review{}, err
	}
	span, err := spanIn(lines, file)
	if err != nil {
		return finding.Review{}, err
	}
	findings := slices.Clone(prev.Findings)
	for _, m := range matches {
		i, err := strconv.Atoi(m[1])
		if err != nil {
			return finding.Review{}, fmt.Errorf("the refusal names finding %q: %w", m[1], err)
		}
		if i < 0 || i >= len(findings) {
			return finding.Review{}, fmt.Errorf("the refusal names finding %d, and the review has %d", i, len(findings))
		}
		findings[i] = findingFor(file, span, findings[i].Claim)
	}
	return reviewOf(task, false, findings), nil
}
