package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// getTaskView is the answer of get_task: the task, its instructions and its rendered inputs.
type getTaskView struct {
	Task struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Change string `json:"change"`
		Base   string `json:"base"`
		Head   string `json:"head"`
		Case   string `json:"case"`
	} `json:"task"`
	Inputs []struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	} `json:"inputs"`
}

// diffHeader matches the header line of each file in a rendered diff.
var diffHeader = regexp.MustCompile(`(?m)^=== (\S+) \(`)

var criterionLine = regexp.MustCompile(`(?m)^(AC[1-9][0-9]{0,2}):`)

// parseTask reads the answer of get_task into the envelope the scenarios work with, and the text of each input with
// the untrusted wrapper lines taken off.
func parseTask(text string) (workflow.TaskEnvelope, map[string]string, error) {
	var v getTaskView
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return workflow.TaskEnvelope{}, nil, fmt.Errorf("decode the get_task answer: %w", err)
	}
	inputs := make(map[string]string, len(v.Inputs))
	for _, in := range v.Inputs {
		inputs[in.Name] = unwrap(in.Content)
	}
	task := workflow.TaskEnvelope{TaskID: id.TaskID(v.Task.ID), CaseID: id.CaseID(v.Task.Case)}
	task.Change.ID, task.Change.BaseSHA, task.Change.HeadSHA = id.ChangeID(v.Task.Change), id.SHA(v.Task.Base), id.SHA(v.Task.Head)
	for _, m := range criterionLine.FindAllStringSubmatch(inputs["ticket"]+"\n"+inputs["criteria"], -1) {
		task.Inputs.AcceptanceCriteria = append(task.Inputs.AcceptanceCriteria, workflow.CriterionRef{ID: m[1]})
	}
	task.Inputs.FilesChanged = changedFiles(inputs)
	return task, inputs, nil
}

// unwrap returns the text between the two boundary lines of an untrusted wrapper, or the text itself when it has none.
func unwrap(content string) string {
	lines := strings.Split(content, "\n")
	var out []string
	inside := false
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "<<HEIMDALL-UNTRUSTED-BEGIN"):
			inside = true
		case strings.HasPrefix(l, "<<HEIMDALL-UNTRUSTED-END"):
			inside = false
		case inside:
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return content
	}
	return strings.Join(out, "\n")
}

// changedFiles are the files a scenario may read: the pull request's files, else the headers of the diff, else the files
// of the repository map outside the tests directory.
func changedFiles(inputs map[string]string) []string {
	if inputs["files"] != "" {
		return strings.Split(strings.TrimSpace(inputs["files"]), "\n")
	}
	var out []string
	if inputs["repo_map"] == "" {
		for _, m := range diffHeader.FindAllStringSubmatch(inputs["diff"], -1) {
			out = append(out, m[1])
		}
		return out
	}
	for _, line := range strings.Split(inputs["repo_map"], "\n") {
		if p := strings.TrimSpace(line); p != "" && !strings.HasPrefix(p, "tests/") && !strings.Contains(p, " ") {
			out = append(out, p)
		}
	}
	return out
}
