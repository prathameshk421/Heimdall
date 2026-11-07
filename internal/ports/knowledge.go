package ports

import "context"

// KnowledgeState is the durable delivery state for an approved knowledge source.
type KnowledgeState string

const (
	// KnowledgePending means provenance is durable and awaits a successful memory write.
	KnowledgePending KnowledgeState = "pending"
	// KnowledgeComplete means the Graphiti write succeeded and was recorded durably.
	KnowledgeComplete KnowledgeState = "completed"
)

// KnowledgeRecord carries redacted content and source provenance until memory delivery is complete.
type KnowledgeRecord struct {
	Team           string
	SourcePath     string
	SourceDigest   string
	ContentDigest  string
	IdempotencyKey string
	ApprovedBy     string
	Subject        string
	Content        string
	State          KnowledgeState
}

// KnowledgeRepository durably records provenance and delivery state.
type KnowledgeRepository interface {
	Find(context.Context, string, string) (KnowledgeRecord, bool, error)
	Begin(context.Context, KnowledgeRecord) (KnowledgeRecord, error)
	Complete(context.Context, string, string) error
}

// KnowledgeWriter submits already-redacted content to a server-selected team group.
type KnowledgeWriter interface {
	Write(context.Context, string, string, string) error
}

// KnowledgeReader searches only the server-selected team group.
type KnowledgeReader interface {
	Search(context.Context, string, string) ([]string, error)
}
