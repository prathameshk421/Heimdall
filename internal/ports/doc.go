// Package ports declares the interfaces the application needs. Each interface is defined by its consumer, has one to
// five methods (CODING_STANDARDS 1), and is implemented by an adapter that the application never names. The package
// imports the standard library and the domain only (TECH_SPEC 2.2, enforced by internal/archcheck).
//
// Port -> consumer -> adapter -> task (docs/IMPLEMENTATION_PLAN.md):
//
//	EventStore          use cases, outbox drainer        adapters/postgres           T20
//	Outbox              outbox drainer                   adapters/postgres           T20, drainer T22
//	PRReader            poller, slop rule, gate          adapters/github             T24
//	PRComments          outbox drainer                   adapters/github             T24
//	BranchWriter        test-author flow                 adapters/github             T24
//	CheckWriter         gate                             adapters/github             T24
//	CodeReader          test pin, sandbox source         adapters/github             T24
//	AgentRuntime        task engine                      adapters/switchroom         T26 (runtime T27)
//	Sandbox             red-first, repro flows           adapters/docker (runner)    T15, jobs T29
//	Memory              decisions, ask_knowledge         adapters/graphiti           T49
//	Chat                rooms, Switch stream, milestones adapters/switch             T26
//	TicketReader        trigger poll, AC parsing         adapters/jira, adapters/file T25
//	TicketWriter        outbox drainer (policy-gated)    adapters/jira, adapters/file T25
//	BlobStore           sandbox logs, bundles, packs     adapters/blobfs            T42 (first user)
//	Clock               every use case                   internal/platform          T17
//	IDs                 every use case that makes ids    internal/platform          T17
//
// Public API. The value types the ports exchange (PR, ChangedFile, File, Check, AgentRef, WakeResult, Liveness,
// SandboxJob, SandboxResult, TestResult, Episode, MemoryQuery, MemoryItem, Ticket, Criterion, TicketChange,
// TicketDraft, RoomRequest, Message, Cursor, BlobClass) each have a Validate that names every bad field. Validation
// is applied where the value came from outside: a PR and a ticket from GitHub or Jira, a sandbox request built by the
// application, a memory query whose groups the server must set. Sentinel errors: ErrVersionConflict (2.3),
// ErrNotFound and ErrUnavailable (2.5); callers branch with errors.Is.
//
// Invariants. A port takes ctx first if it does I/O. Scope is set by the caller's server-side resolution, never by a
// caller: MemoryQuery.Groups must be set, and a Memory adapter never widens them (TECH_SPEC 9.4). Untrusted text
// (PR bodies, ticket descriptions, room messages) is bounded by MaxUntrustedText and is never placed in an event or
// prompt by a port (TECH_SPEC 6.3).
//
// Deliberate differences from TECH_SPEC 2.3. The spec's VCS interface has seven methods, so it is split by consumer
// into PRReader, PRComments, BranchWriter, CheckWriter and CodeReader; the same methods, in the same signatures
// where the spec gives them. Projections are not a port in this package: the projectors of TECH_SPEC 4.1 are built
// by T20 and T22, and their interface is decided there, when a consumer exists. Secrets are config only (TECH_SPEC
// 2.6) and have no port.
//
// Failure modes. A conflicting Append returns ErrVersionConflict and nothing is written. An unreachable remote returns
// ErrUnavailable and the caller keeps the effect in the outbox. A refused wake is a WakeResult, not an error.
//
// Tests. contract_test.go gives every port a tiny in-test fake, asserts it satisfies the interface at compile time,
// and calls each method through the interface. types_test.go covers every value type's validation, and
// errors_test.go covers the sentinel errors with errors.Is. A reusable contract suite for real adapters is out of
// scope for T11; each adapter task writes its own test against the real system.
package ports
