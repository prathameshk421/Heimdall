package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/collaboration"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// Proposer runs propose_command (AGENT_INTERACTION_ARCH 5.3): the command runs as the person who sent the trigger
// message, never as the agent, and a deciding verb waits for that person's yes.
type Proposer interface {
	ProposeCommand(ctx context.Context, agent id.AgentName, ticket id.ChangeID, text string, trigger change.MessageID) (string, error)
}

// commandTools are the tools of the command area. Every agent may propose; only the trigger's sender can authorize.
func (s *Server) commandTools() ([]tool, error) {
	if s.deps.Proposer == nil {
		return nil, nil
	}
	return []tool{{name: "propose_command",
		description: "Ask Heimdall to run a command for the person whose message asked for it, as that person; a deciding command waits for their yes. Use it when a person asked in chat for dismiss, uphold or similar; do not use it for your own decisions. Example: propose_command {\"command\":\"dismiss F2 duplicate of F1\",\"trigger\":\"<message id>\",\"ticket\":\"KAN-5\"}",
		schema:      proposeCommandSchema(),
		handle:      s.proposeCommand}}, nil
}

func proposeCommandSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","minLength":1,"maxLength":500},` +
		`"trigger":{"type":"string","minLength":1,"maxLength":128}},"required":["command","trigger"],"additionalProperties":false}`)
}

func (s *Server) proposeCommand(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var in struct {
		Command string `json:"command"`
		Trigger string `json:"trigger"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if p.Ticket == "" {
		return "", refusalf("ticket: propose_command needs the ticket the person's message is about. Example: %q.", `"ticket": "KAN-22"`)
	}
	text := strings.TrimSpace(in.Command)
	if strings.HasPrefix(strings.ToLower(text), "heimdall ") {
		text = strings.TrimSpace(text[len("heimdall "):])
	}
	out, err := s.deps.Proposer.ProposeCommand(ctx, p.Agent, p.Ticket, text, change.MessageID(in.Trigger))
	if errors.Is(err, collaboration.ErrRefused) {
		return "", refusalf("command: %s", err.Error())
	}
	return out, err
}
