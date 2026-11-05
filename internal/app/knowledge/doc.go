// Package knowledge owns the authenticated knowledge-file intake and scoped read contract.
//
// Service.Intake authenticates the caller, validates the explicit team and bounded source
// path/content, verifies owner approval, redacts before calling MemoryWriter, then records
// provenance, digest and idempotency metadata through Repository. Service.Read derives the
// only permitted group from identity; request data cannot widen scope. Repository adapters
// must enforce uniqueness on (team, idempotency key), and memory adapters must enforce the
// supplied team group. Neither guarantee is complete until concrete adapters are integrated.
// Redaction and all persistence/serving are injected; this package never calls Graphiti.
package knowledge
