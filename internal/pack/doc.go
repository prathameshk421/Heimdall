// Package pack builds, reviews, signs, encrypts, uploads and serves a reviewer's private context pack.
//
// Collection is local to the developer machine. Raw material is redacted before any distiller can see it,
// and distilled output is scanned again before owner approval. The signed manifest covers every section hash;
// only age ciphertext is handed to the uploader. Reads verify signature, age, freshness and caller ownership.
// All environmental actions are supplied through interfaces so the pipeline cannot silently substitute a
// fixture or an unencrypted path.
//
// Invariants: source bytes never reach a distiller before pass one; any pass-two match refuses the build;
// no upload occurs before explicit owner approval; every returned section is for the caller's own pack.
// Failures from collection, signing, encryption, upload and verification stop the operation with context.
// Tests cover secret redaction, provenance, manifest hashes and signatures, approval ordering, encryption,
// caller isolation, stale packs, malformed inputs and canary containment.
package pack
