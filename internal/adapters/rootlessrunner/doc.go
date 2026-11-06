// Package rootlessrunner contains the secure production sandbox boundary.
//
// Purpose: move execution of untrusted generated tests out of heimdall-server and into a dedicated runner process.
// The server side sends a bounded source archive and fixed job contract through a Unix socket. The runner side uses
// rootless Podman with an immutable allowlisted image and fixed resource, filesystem, and network controls.
//
// Invariants: only profile names configured at runner startup select images; images require sha256 digests; the
// caller cannot select a command; archives and overlays are bounded; the container has no network, runtime socket,
// secrets, writable host mount, or elevated capabilities; logs are capped at 5 MiB; jobs time out at 10 minutes.
//
// Failure modes: invalid jobs, images, paths, oversized payloads, unavailable runner sockets, runtime failures,
// timeouts, and absent test events fail closed. Rootless Podman and subordinate UID/GID mappings are OS prerequisites.
//
// Tests exercise policy argument construction, profile rejection, path traversal attempts, resource limits, API
// request validation, output bounds, and the adapter contract. Runtime escape acceptance remains a live VM check.
package rootlessrunner
