// Package knowledge owns team-scoped memory queries and approved knowledge-file intake.
//
// Graphiti receives only server-derived groups. Agents cannot widen a query. Query results are validated and their
// stable refs are audited against the active task. Personal packs are excluded from all ingestion paths. Knowledge
// files enter as untrusted observations until an authorized human approves them; accepted ADRs may be confirmed.
//
// Failures in scope resolution, Graphiti, audit, and approval are returned with context; unavailable memory never
// becomes E3 evidence. Tests cover scope isolation, shared-team visibility, labels, provenance and approval order.
package knowledge
