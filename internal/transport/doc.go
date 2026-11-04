// Package transport holds the inbound edges: webhook, mcp, web (html/template with htmx) and sse. They
// call app and never import adapters.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
//
// # Purpose
//
// The HTTP surface that is not business logic. Today that is the health endpoint only; the webhook, MCP,
// web and SSE edges land with their tasks. The package holds no state between requests.
//
// # API
//
//   - Checker{Name, Check}: one named readiness check. Check receives the request context and returns nil
//     when healthy. Its error is never sent to the client, because it may carry a token or a URL.
//   - NewRouter(logger, checkers...) (http.Handler, error): GET /healthz. The logger is injected; a nil one is refused. It returns 200 with {"status":"ok"} when
//     every checker passes. When any fails it returns 503 with {"status":"unavailable","failing":[names]},
//     in registration order. A checker with an empty or duplicate name, or a nil Check, is refused.
//
// # Invariants
//
//   - Checker names are unique and non-empty, so a failing name identifies one check.
//   - The response body names checks and never repeats an error message or a secret value.
//   - Checks run in registration order, one request at a time, under the request's context. A check that
//     blocks is bounded by the server's own timeouts (the server sets ReadHeaderTimeout and runs shutdown).
//
// # Failure modes
//
//   - A failing check returns 503 with its name. The caller sees which dependency is down, not why.
//   - A method other than GET on /healthz gets 405 from the mux.
//
// # Tests
//
// health_test.go drives the router through httptest: healthy, one and two failing checks (the secret
// text in an error must not appear in the body), the 405 method check, and the refused checker shapes.
// Board: the server answers /healthz in cmd/heimdall-server (run_test.go).
package transport
