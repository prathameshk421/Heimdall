// Package record captures real HTTP exchanges for use as contract fixtures under testdata/real (L3).
//
// A Transport wraps a real http.RoundTripper, so the recorded call is a real call to the real system. Each
// exchange is written to dir/<system>/<seq>-<method>-<path>.json after Scrub has removed secrets and personal
// data, and credentials in headers (Authorization, Cookie, API keys) are never written at all. The caller
// still receives the unmodified response. Recordings are re-made on purpose with `make record`, and
// gitleaks scans the history, so a pattern Scrub misses is still caught.
//
// Test code only (D121): production code must not import it.
package record
