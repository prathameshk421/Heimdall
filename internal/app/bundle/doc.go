// Package bundle builds deterministic, role-scoped context blobs for agent tasks.
//
// Build uses stable role instructions and change identity before request context,
// serializes compact JSON without HTML escaping, and refuses a bundle whose
// documented chars/4 estimate exceeds either budget. Ticket acceptance criteria
// are projected from the replayable trigger event as ordered IDs and validated
// blob refs; their untrusted source text is never copied into the role bundle.
// Other untrusted inputs are represented only by validated blob references. Store writes the exact bytes
// through ports.BlobStore; the SHA-256 reference is derived from those bytes.
// Production task steps receive this package through engine.BundleBuilder; the engine does not project or hash
// bundles itself. The package is tested with a port fake and has no adapter dependencies.
package bundle
