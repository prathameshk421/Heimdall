// Package pack holds bounded project pack collectors, redaction and pack safety primitives.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
// Markdown and transcript collection are root-relative and injectable through Root. OSRoot uses
// os.Root, while tests use an in-memory fs.FS. Git collection accepts a read-only GitSource and
// retains commit summaries and per-file ownership counts, never patches. Collectors reject unsafe
// paths and fail closed at configured limits. They do not log document contents. Collection
// failures return before a partial result is exposed; redaction and later pack stages own the
// handling of returned documents. Pipeline publication requires an injected trusted owner
// approval verifier and metadata store; reads require a caller authorizer. The provided memory
// blob store stores ciphertext only and is not durable metadata or replay protection. Metadata
// implementations must atomically bind owner/replay key to the canonical input digest and reject
// a different digest for an existing key.
package pack
