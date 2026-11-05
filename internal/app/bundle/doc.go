// Package bundle builds deterministic, role-scoped context blobs for agent tasks.
//
// Build uses stable role instructions and change identity before request context,
// serializes compact JSON without HTML escaping, and refuses a bundle whose
// documented chars/4 estimate exceeds either budget. Untrusted inputs are
// represented only by validated blob references. Store writes the exact bytes
// through ports.BlobStore; the SHA-256 reference is derived from those bytes.
// The package is tested with a port fake and has no adapter dependencies.
package bundle
