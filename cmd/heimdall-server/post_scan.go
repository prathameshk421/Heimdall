package main

import (
	"context"
	"log/slog"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/collaboration"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/secretscan"
)

// postScanner is the board's scan of agents' room posts (AGENT_INTERACTION_ARCH 10, H11): an agent's post that carries
// a credential, a canary or private card text raises an alert. The post's text is never logged. Canaries and card
// texts come from Private; the deployment configures none today.
type postScanner struct {
	scanner *secretscan.SecretScanner
	runtime *commandRuntime
	private secretscan.Private
	logger  *slog.Logger
}

func newPostScanner(runtime *commandRuntime, logger *slog.Logger) (*postScanner, error) {
	scanner, err := secretscan.NewSecretScanner()
	if err != nil {
		return nil, err
	}
	return &postScanner{scanner: scanner, runtime: runtime, logger: logger}, nil
}

// Watch scans one delivered message when an agent sent it.
func (p *postScanner) Watch(ctx context.Context, m ports.Message) {
	target := collaboration.Target{Room: ports.RoomBinding{RoomID: m.Room}}
	agent, err := p.runtime.IsAgent(ctx, target, commands.SourceChat, string(m.Sender))
	if err != nil || !agent {
		return
	}
	leaks, err := secretscan.ScanPost(m.Text, p.scanner, p.private)
	if err != nil {
		p.logger.ErrorContext(ctx, "post scan failed", "room", m.Room, "message", m.ID, "cause", err)
		return
	}
	for _, leak := range leaks {
		p.logger.ErrorContext(ctx, "ALERT: an agent's room post leaked", "kind", leak, "room", m.Room, "message", m.ID, "sender", m.Sender)
	}
}
