// Package runtime renders the local deployment contract for Switch's agent
// controller. It does not install, enable, reload, or activate any service.
//
// # Purpose
//
// Render produces deterministic systemd drop-ins, a controller environment
// file, and an nftables ruleset for the already-installed controller and
// per-agent units described by TECH_SPEC.md section 6.9.
//
// # Invariants
//
// Paths, users, slots, versions, ports, and network ranges are validated
// before anything is rendered. The metadata address is always dropped before
// the agent-group default drop. The controller environment artifact contains
// no credential; the caller supplies its secret separately with mode 0600.
//
// # Failure modes
//
// Invalid or incomplete configuration returns an error and no artifacts. The
// package never reads environment variables, secret files, or system state.
//
// # Tests
//
// runtime_test.go checks deterministic rendering, private file modes, the
// per-agent identity drop-ins, metadata blocking, and fail-closed validation.
package runtime
