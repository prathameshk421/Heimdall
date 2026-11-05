// Package jira is the Jira Cloud tracker adapter (ports.TicketReader, TECH_SPEC 13, D50, D97, V52, V55).
//
// # Purpose
//
// It polls Jira for tickets that moved to In Progress with a human assignee, and reads one ticket with its
// acceptance criteria. It is one of the two tracker adapters (the other is tracker/file). It does not decide what
// a trigger means: the application does that from the TicketChange values.
//
// # API
//
//   - New(Config, Secrets, ports.Clock, Waiter, Random, *http.Client) (*Client, error): validates the config and
//     reads the email and API token by name through Secrets (config.Config satisfies it). Nothing is printed or
//     stored in Config. Waiter and Random make retry delays deterministic in tests.
//   - Client.Changes(ctx, since): one JQL search from since minus poll.Overlap (10 minutes). Each changelog entry
//     in the window is a TicketChange keyed by (ticket key, changelog id). The returned cursor is this poll's
//     clock time.
//   - Client.Ticket(ctx, key): summary, status, assignee and the acceptance criteria parsed from the ADF description.
//
// # Invariants
//
//   - The token travels only in the Authorization header. It never reaches a URL, an error or a log.
//   - The base URL is https; http is allowed only for a loopback host (a local test server).
//   - A ticket key is checked against ^[A-Z][A-Z0-9_]*-[0-9]+$ before any request, so a key cannot add a path.
//   - An app account or an unassigned issue never becomes a change.
//   - Every (key, changelog id) pair is reported at most once while it is inside the window (poll.Seen).
//   - No time.Now: every instant comes from the injected clock (R5).
//   - Unavailable calls make at most eight attempts with capped exponential backoff (one-second base, factor two,
//     ten-minute ceiling) and injected full jitter. A 429 honours Retry-After seconds or HTTP dates.
//
// # Failure modes
//
//   - 401 and 403 wrap ports.ErrUnauthorized; 404 wraps ports.ErrNotFound; 429, 5xx and transport failures wrap
//     ports.ErrUnavailable. The outbox retries only the last kind.
//   - An unparseable changelog time or a non-ticket issue key fails the whole poll, so no change is silently lost.
//   - A description that is not ADF (REST v2 wiki markup) is refused, not guessed.
//
// # Not verified
//
// No call was made to a live Jira. The search endpoint (/rest/api/3/search/jql), its expand=changelog parameter,
// the JQL minute resolution and the account time zone all follow the documentation and must be confirmed on
// recorded payloads in T36 (V52, V55, S20). Scrubbed live KAN recordings, including KAN-1 to KAN-3 ADF payloads,
// have not been captured.
//
// # Tests
//
// jira_test.go runs every path against an httptest server: the JQL window, the overlap dedupe with a late-indexed
// change, pagination, app and unassigned skips, the key guard (no request made), the status-to-error mapping, the
// token never appearing in an error, config refusals and a transport failure. resilience_test.go covers retries,
// Retry-After forms and the breaker. adf_test.go covers the ADF walker.
package jira
