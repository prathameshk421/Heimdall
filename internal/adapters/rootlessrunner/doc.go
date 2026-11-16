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
// Checks and test kinds share one job shape: the head tree at /work, an optional base tree at /base, the command of a
// row of the promoted profile run through `sh -c` with its text passed as an environment variable (never spliced into
// the constant wrapper script), and the result file (SARIF or JUnit) read from /out between fixed markers, bounded.
// A request names only a check id or a test kind; a name the profile does not hold, an image the runner does not hold
// and a directory with shell characters are refused before any container starts.
//
// Services: a profile's services run as throwaway containers on a private `--internal` network created for one job and
// shared with nobody else, so the job keeps no egress. They run as a fixed non-root user with no capabilities (an image
// that cannot is not allowlisted); a service's declared data directory is a fresh tmpfs. They are removed when the job
// ends whatever its outcome, and carry their own timeout. The job starts only after every service accepts a TCP
// connection on its declared port (30 s bound): the probe runs `bash /dev/tcp` inside the rootless network namespace,
// because the host cannot reach an --internal network, with the address and port as arguments, never script text. A
// kind with serve and ready starts its app inside the job and waits on a loopback URL; the image needs curl or wget. A
// check job never gets services.
//
// Leftovers: every job container, service container and job network carries the label heimdall.job=1. Runner.Sweep,
// run once at boot before serving, removes everything so labelled that a dead runner left behind.
//
// History mirror: the warm job also keeps a blobless bare mirror of a repository (commits and trees only), fetched with
// git through a loopback CONNECT proxy that reaches only the exact fetch hosts on port 443 and only public addresses.
// The profile must list github.com in fetch_hosts. A check whose row says `history: true` gets the mirror read-only at
// /history; no other job sees it, and a check never fetches. The runner holds no credential of its own: for a private
// repository the request carries a short-lived read-only token for that one repository, which reaches git only through
// its environment and is cut out of any error.
//
// Failure modes: invalid jobs, images, paths, oversized payloads, unavailable runner sockets, runtime failures,
// timeouts, and absent test events fail closed. Rootless Podman and subordinate UID/GID mappings are OS prerequisites.
//
// Tests exercise policy argument construction, profile rejection, path traversal attempts, resource limits, API
// request validation, output bounds, and the adapter contract. Runtime escape acceptance remains a live VM check.
package rootlessrunner
