// Package mcp is the MCP edge: the streamable HTTP endpoint at /mcp that the VM agents' OpenCode harnesses call, and the
// stdio bridge (`heimdall mcp`) that relays one client's stdio to that endpoint.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck. This package imports
// domain, ports, internal/schema, and the app packages codegraph and reviewbrief for their result types; it never
// imports adapters.
//
// # Purpose
//
// Turn an agent's tool call into a checked request: authenticate the agent token, offer only the tools of the kind
// of task the agent holds, check the call against the agent's one active task, bound what goes back, and hand the
// rest to the application. Tool table: TECH_SPEC 14.1. Wire tool names are bare: the server lists and accepts get_task, not
// heimdall_get_task. OpenCode adds the heimdall_ prefix on the model side, from the server key heimdall.
//
// Tools are offered by the kind table (workflow.Kind.Tools, H7) plus the shared ones (get_task, report_progress,
// propose_command): tests hold read_code, search_code, ask_humans and submit_tests; review holds the code tools,
// run_check, ask_knowledge, read_card, stage_knowledge and submit_review; verdict holds read_code, run_check, read_card,
// ask_knowledge and submit_verdict; profile holds the profile tools. approve_profile, promote_profile,
// approve_knowledge and forget_knowledge are for people only. A tool whose dependency failed the boot self-check is
// withheld (Deps.Withheld, H9): not listed, and a call gets the unknown-tool answer. The ack, get_change and
// read_untrusted tools are deleted (H7).
//
// get_task answers one JSON object: the task, the kind's skill text, every input rendered at call time
// (internal/app/taskinput; untrusted text wrapped once) and the change's needs. Its first call of a task records
// task.acked.
//
// search_code and find_symbol need their dependency (Deps.Graph) and work at the commit of the assignment. A review
// task's inputs include the brief sections (Deps.Briefs): obligations, prior findings, team memory and the record.
// Each section comes inside its own untrusted wrapper, and a failed section becomes a note and never costs the task.
// A test author reads only the base commit. submit_review names the obligation cards that rule_checks left out. This is a nudge and never a
// refusal. The last brief of a task is held in memory, and a partial one is compiled again on the next get_task.
//
// # API
//
//   - Principal{Agent, Role}: who a token belongs to. Tokens are bound to one agent and one role (TECH_SPEC 15.4).
//   - HashToken(token) string: the lowercase hex SHA-256 of a token. Tokens are stored hashed, and only the hash
//     leaves this package (to Tokens.Resolve).
//   - Tokens.Resolve(ctx, hash): the principal of a stored hash, or ports.ErrUnauthorized when unknown or revoked.
//   - Tasks: the application side of the agent's task. Current returns the agent's one active Assignment, or
//     ErrNoTask. Ack, SubmitTests, AskHumans and Progress carry out the effects (internal/app/agenttools).
//   - Reviews: the application side of reviews and verdicts. SubmitReview and SubmitVerdict record the outputs.
//     SubmitReceipt carries per-finding errors for invalid findings so the reviewer can correct them.
//   - Code and Blobs: narrow read-side ports (ReadFile, Get) the edge needs, declared here as consumer interfaces.
//   - Deps and NewServer(Deps): validates that every dependency is set. Server.Handler() returns the http.Handler
//     for /mcp.
//   - BridgeConfig and Bridge: the stdio bridge. It reads JSON-RPC lines from In, posts each to the server with the
//     bearer token, and writes each response line to Out. Logs go to the injected logger, never to Out.
//
// # Invariants
//
//   - Every call to /mcp carries a bearer token. The token is hashed before lookup and is never logged, returned,
//     or put in an error. The logger sees one INFO "mcp request" line per /mcp request (method, tool, role, agent,
//     status, duration_ms), one WARN per refused token (remote address, no token), the tool name, agent, role and
//     outcome of each tool call, never arguments.
//   - A tool is listed and callable only for the kind the role holds. A call to a tool the kind does not hold
//     gets the same answer as an unknown tool (JSON-RPC invalid params), so the list is not an oracle.
//   - Every tool except the protocol methods works on the agent's own active task. The envelope's role must equal
//     the token's role, or the call is refused.
//   - Text that someone outside the system wrote, and repository code (the head can be an outsider's PR), comes back
//     only inside an untrusted wrapper: a fixed preface, a random 128-bit boundary generated per response, and the
//     content between two boundary lines. The output is capped at ports.MaxUntrustedText bytes and says so when cut.
//   - read_code only reads the base SHA for test-author, and base or head for the other roles.
//   - The first get_task of a task records task.acked, once, before the deadline (TECH_SPEC 6.2). A repeat call
//     records nothing.
//   - submit_tests validates with testset.Submission.Validate against the envelope's acceptance criteria and the
//     tests directory tests/heimdall/<change id> (D150). A submission after the deadline is passed on as late.
//   - submit_review validates with finding.Review.Validate, checks head_sha, and passes to Reviews.SubmitReview.
//     Per-finding errors from SubmitReceipt are returned to the agent as a non-fatal refusal so it can fix them.
//   - submit_verdict validates with deliberation.Verdict.Validate, checks that case_id matches the task, and passes
//     to Reviews.SubmitVerdict. A submission after the deadline is passed on as late.
//   - report_progress: at most 400 characters, at most one report per task every 2 minutes.
//   - Every tool error the agent sees is either a refusal it can act on (validation, role, window) or the generic
//     text "the server could not complete this call". The cause of a generic error is logged, not returned.
//
// # Failure modes
//
//   - No or unknown token: 401 with WWW-Authenticate: Bearer. A store that cannot be reached: 503.
//   - Body above 8 MiB: 413. A submission with 32 files of 64 KiB each, JSON-escaped, is the largest legal request.
//   - Malformed JSON-RPC: a JSON-RPC parse error (-32700). A notification gets 202 and no body.
//   - Unknown method or tool: -32601 or -32602. A refused tool call is an isError result, so the agent keeps its
//     budget and can correct the call.
//   - Bridge: the server is unreachable or answers 401: the bridge writes a JSON-RPC error for the request's id, so
//     the client does not hang. It exits only when its input ends or its context is cancelled.
//
// # Choices the spec leaves open (decide and record in PROGRESS.md)
//
//   - Protocol: MCP streamable HTTP with JSON responses (no SSE stream). The server never starts a stream of its
//     own, so every response is a single JSON body. Batch arrays are refused.
//   - No SDK: the JSON-RPC surface is small (initialize, ping, tools/list, tools/call), so it is written here with the
//     standard library and no new dependency.
//   - Progress rate limit is held in memory per process. A restart resets it. Persisting it belongs to the app.
//   - read_code wraps its text, and read_code allows the head SHA for every role except
//     test-author, because the head is the change under review. read_code numbers the lines ("12: text") so an
//     agent can cite file:line. The wrapper preface and the cut note are fixed server text.
//   - Protocol revisions: the server speaks 2025-06-18 and 2025-03-26. An unknown revision gets 2025-06-18.
//   - submit_tests checks the tests directory tests/heimdall/<change id> (D150). Its schema is generated from
//     testset.Submission, so the wire form and the validator cannot drift.
//   - ask_humans takes 1 to 3 questions of at most 400 characters each. The spec gives the count only; the length
//     limit matches report_progress and is recorded here for the lead to confirm.
//   - report_progress reserves the task's window before the call and gives it back when the call fails.
//   - The bridge posts each request with a 30 s deadline, and refuses a line over 8 MiB with -32600 without posting it.
//     A notification that fails is logged and writes nothing, because it has no id to answer.
//   - A tool the role lacks is -32602 with the same text as an unknown tool. An argument the tool refuses is an
//     isError result, so the agent keeps its budget.
//   - The output filter (secret scan, canary scan) of TECH_SPEC 6.5 is not built: the security package is empty.
//     This package caps lengths but does not scan content. Recorded as not verified.
//
// # Tests
//
// server_test.go covers the protocol surface and the role table; tools_test.go each tool's checks against the
// envelope; untrusted_test.go the wrapper; http_test.go authentication, body limit and token hygiene; bridge_test.go
// the stdio relay against a real httptest server; code_tools_test.go the code tools, the brief fields and the nudge.
// All run with fakes for the ports (no network, fixed clock).
package mcp
