package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// knowledgeQueryAudit records stable references and a one-way question fingerprint without retaining query text.
type knowledgeQueryAudit struct{ logger *slog.Logger }

func (a knowledgeQueryAudit) RecordKnowledgeQuery(ctx context.Context, task id.TaskID, agent id.AgentName, question string, refs []string) error {
	sum := sha256.Sum256([]byte(question))
	a.logger.InfoContext(ctx, "team knowledge queried", "task", task, "agent", agent, "question_sha256", hex.EncodeToString(sum[:]), "memory_refs", refs)
	return nil
}
