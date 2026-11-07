package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// ticketDescription tells the agent what the optional ticket argument is for.
const ticketDescription = "The ticket this call is about, for example KAN-16. Always pass the ticket named in the message that woke you. Without it you get your oldest open task, which may be a different ticket."

// withTicketProperty adds the optional ticket argument to a tool's input schema.
func withTicketProperty(schema json.RawMessage) (json.RawMessage, error) {
	var doc map[string]any
	if err := json.Unmarshal(schema, &doc); err != nil {
		return nil, fmt.Errorf("read tool schema: %w", err)
	}
	props, _ := doc["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
	}
	props["ticket"] = map[string]any{"type": "string", "description": ticketDescription}
	doc["properties"] = props
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("write tool schema: %w", err)
	}
	return out, nil
}

// withTicket takes the optional ticket argument out of a call. The tools decode their own arguments strictly, so the
// ticket must not reach them; it travels on the principal and picks which task the call is about.
func withTicket(p Principal, args json.RawMessage) (Principal, json.RawMessage, error) {
	if len(args) == 0 {
		return p, args, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return p, args, refusal("the arguments must be a JSON object.")
	}
	raw, ok := fields["ticket"]
	if !ok {
		return p, args, nil
	}
	delete(fields, "ticket")
	var ticket string
	if err := json.Unmarshal(raw, &ticket); err != nil {
		return p, args, refusal("ticket must be text, for example KAN-16.")
	}
	if ticket != "" {
		cid, err := id.ParseChangeID(ticket)
		if err != nil {
			return p, args, refusal("ticket must look like KAN-16.")
		}
		p.Ticket = cid
	}
	rest, err := json.Marshal(fields)
	if err != nil {
		return p, args, fmt.Errorf("rewrite tool arguments: %w", err)
	}
	return p, rest, nil
}
