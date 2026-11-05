package ports

import "context"

// Memory is team memory (TECH_SPEC 9). Consumer: the use cases that record decisions and answer ask_knowledge. Adapter:
// internal/adapters/graphiti, a client of the Graphiti MCP server (task T49).
type Memory interface {
	// AddEpisode queues one episode. The adapter deduplicates by ep.Key.
	AddEpisode(ctx context.Context, ep Episode) error
	// Search returns facts for q, in the groups the server set on q (TECH_SPEC 9.4).
	Search(ctx context.Context, q MemoryQuery) ([]MemoryItem, error)
}
