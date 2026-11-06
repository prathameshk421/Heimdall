package rootlessrunner

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// checkOutput is what a static check printed and the exit status of the tool.
type checkOutput struct {
	Text string
	Exit int
}

// parseCheckOutput splits the runner output at the last checkMarker line. The text before it is what the tool
// printed; the number after it is the tool's exit status. Output without the marker is not a check result: the job was
// killed or never ran the tool.
func parseCheckOutput(out []byte) (checkOutput, bool) {
	i := bytes.LastIndex(out, []byte(checkMarker))
	if i < 0 {
		return checkOutput{}, false
	}
	exit, err := strconv.Atoi(strings.TrimSpace(string(out[i+len(checkMarker):])))
	if err != nil {
		return checkOutput{}, false
	}
	return checkOutput{Text: string(out[:i]), Exit: exit}, true
}

// checkResult stores the log and turns a check output into a sandbox result: exit 0 passes, any other exit fails.
func (c Client) checkResult(ctx context.Context, out []byte) (ports.SandboxResult, error) {
	parsed, ok := parseCheckOutput(out)
	if !ok {
		return ports.SandboxResult{}, fmt.Errorf("runner check output has no exit status")
	}
	logRef, err := c.Blobs.Put(ctx, ports.BlobLog, bytes.NewReader(out))
	if err != nil {
		return ports.SandboxResult{}, fmt.Errorf("store runner log: %w", err)
	}
	text := parsed.Text
	if len(text) > ports.MaxCheckOutput {
		text = text[:ports.MaxCheckOutput]
	}
	res := ports.SandboxResult{LogRef: workflow.BlobRef(logRef), Output: text}
	if parsed.Exit == 0 {
		res.Passed = 1
	} else {
		res.Failed = 1
	}
	return res, nil
}
