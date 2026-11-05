package ports

import (
	"context"
	"io"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// BlobStore keeps content by its SHA-256 (TECH_SPEC 4.3). Consumers: the sandbox logs, the context bundles (T42) and
// the packs. Adapter: internal/adapters/blobfs (first used by T42). Writes are atomic: a reader never sees a part.
type BlobStore interface {
	// Put stores the content of r under its hash and returns the reference. The class sets retention.
	Put(ctx context.Context, class BlobClass, r io.Reader) (workflow.BlobRef, error)
	// Get returns the content. The caller closes it. A missing blob is ErrNotFound.
	Get(ctx context.Context, ref workflow.BlobRef) (io.ReadCloser, error)
}

// BlobDeleter removes content after its final metadata reference is removed.
type BlobDeleter interface {
	Delete(ctx context.Context, class BlobClass, ref workflow.BlobRef) error
}

// KnowledgeFiles persists approved-source provenance and retention metadata outside Graphiti.
type KnowledgeFiles interface {
	Save(context.Context, KnowledgeFile) error
	Get(context.Context, string) (KnowledgeFile, error)
	Expired(context.Context, time.Time) ([]KnowledgeFile, error)
	Delete(context.Context, string) (bool, error)
}
