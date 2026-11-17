package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// Limits the tools enforce. The schemas of the tools repeat them, and the schema tests check that they agree.
const (
	ackWindow        = 10 * time.Minute
	progressWindow   = 2 * time.Minute
	maxProgressChars = 400
)

// genericFailure is the only text an agent sees for a failure inside the server. The cause is logged, not returned.
const genericFailure = "the server could not complete this call"

// refusal is an answer the agent can act on: the reason a tool did not do what was asked. It is shown to the agent.
type refusal string

func (r refusal) Error() string { return string(r) }

// refusalf returns a refusal with a formatted reason.
func refusalf(format string, args ...any) error {
	return refusal(fmt.Sprintf(format, args...))
}

// toolHandler runs one tool for one principal and returns the text of its result.
type toolHandler func(ctx context.Context, p Principal, args json.RawMessage) (string, error)

// tool is one MCP tool: its listed shape and its handler. Who may call it is not here: the kind table says which tools
// each kind of task holds (workflow.Kind.Tools), and every kind holds the shared ones.
type tool struct {
	name        string
	description string
	schema      json.RawMessage
	humanOnly   bool
	handle      toolHandler
}

// allows reports whether the role's kind holds the tool, or every kind does.
func (t tool) allows(role workflow.Role) bool {
	k, ok := workflow.KindFor(role)
	if !ok {
		return false // a principal with no role (a human) holds no kind, so no task tools
	}
	return slices.Contains(workflow.SharedTools(), t.name) || slices.Contains(k.Tools, t.name)
}

// buildTools returns the tool table. Each area declares its own tools (tools_<area>.go); a submit schema is
// generated from the domain type, so it cannot drift from the validator.
func (s *Server) buildTools() ([]tool, error) {
	var tools []tool
	for _, area := range []func() ([]tool, error){s.coreTools, s.testsTools, s.reviewTools, s.checksTools, s.talkTools, s.commandTools} {
		more, err := area()
		if err != nil {
			return nil, err
		}
		tools = append(tools, more...)
	}
	for i := range tools {
		withTicket, err := withTicketProperty(tools[i].schema)
		if err != nil {
			return nil, fmt.Errorf("schema of %s: %w", tools[i].name, err)
		}
		tools[i].schema = withTicket
	}
	return tools, nil
}

func noArgsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}

// decodeArgs decodes the arguments of a tool and refuses any field the tool does not declare. Missing arguments are
// an empty object. The decoder never echoes the value of an argument in its error.
func decodeArgs(args json.RawMessage, v any) error {
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return refusalf("The arguments are not valid: %v", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return refusal("The arguments hold more than one JSON value.")
	}
	return nil
}

// noArgs checks that a tool that takes no arguments got none.
func noArgs(args json.RawMessage) error {
	return decodeArgs(args, &struct{}{})
}

// marshalText returns v as the JSON text of a tool result.
func marshalText(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode tool result: %w", err)
	}
	return string(b), nil
}
