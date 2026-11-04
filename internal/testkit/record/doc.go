// Package record captures real HTTP exchanges for use as contract fixtures under testdata/real (L3).
//
// A Transport wraps a real http.RoundTripper, so the recorded call is a real call to the real system. The request
// is cloned, never changed, and its body is buffered and replayable so retries and redirects resend it. Each
// exchange is scrubbed (Scrub removes secrets and personal data, and credential-looking path segments are replaced)
// and written to dir/<system>/<seq>-<method>-<path>.json. The caller still receives the unmodified response.
//
// Headers are an allowlist: a header not on it is recorded as [REDACTED] under its name, and credential headers
// (Authorization, Cookie, Set-Cookie, API keys) are never written at all. Bodies larger than MaxBodyBytes fail the
// exchange rather than being recorded cut short. New refuses a directory that already holds recordings unless
// Options.Overwrite is set, so an earlier run is never mixed in silently.
//
// Recordings are re-made on purpose with `make record`, and gitleaks scans the history, so a pattern Scrub misses
// is still caught.
//
// Test code only (D121): production code must not import it.
package record
