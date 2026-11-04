// Package github is the GitHub adapter of the VCS ports (TECH_SPEC 2.3, task T24) and the token source that narrows
// every call to one permission (task T35).
//
// # Purpose
//
// The adapter reads pull requests, comments, reviews and check runs, and writes comments, branches, commits and check
// runs. It implements ports.PRReader, ports.PRComments, ports.BranchWriter, ports.CheckWriter and ports.CodeReader.
// Nothing outside this package sees a GitHub type.
//
// # API
//
//   - New(Config) builds the adapter. It parses the App key once and fails on a bad key or a bad base URL.
//   - Tokens() returns the TokenSource. Reader(ctx) mints a read-only installation token. Writer(ctx, op, repo)
//     mints a token narrowed to one repository and one operation (OpCheckWrite, OpBranchWrite, OpCommentWrite).
//     Each token is cached for 50 minutes on the injected Clock.
//   - RateLimitError is returned when GitHub asks for a wait longer than two minutes, or keeps refusing after
//     three attempts. It wraps ports.ErrUnavailable, so the outbox retries it later.
//
// # Traps
//
//   - Status codes map to ports errors: 401 and 403 (without rate-limit headers) are ports.ErrUnauthorized, which the
//     outbox does not retry; 404 is ports.ErrNotFound; 5xx and transport failures are ports.ErrUnavailable.
//   - Error text never carries a response body, a token or a key. A body can echo a token, so bodies are not logged.
//   - A circuit breaker opens after five consecutive unavailable outcomes and stays open for 30 seconds. Client errors
//     do not count against it.
//   - Rate limits are waited out inside the call only up to two minutes. A longer Retry-After returns at once.
//   - Only the first page of a list can be served from the ETag cache. A list spanning several pages is never cached.
//   - Next-page links must stay on the configured API origin. A link to any other host is an error, not a request.
//   - CodeReader.ReadFile uses the contents API, which returns a directory listing for a directory path and returns
//     at most 1 MB. Larger files are not verified.
//   - SetCheck finds an existing run by its external ID among the first 100 runs named check_name on the commit.
//     Not verified against a live repository (T35 live test is build-tagged and not run here).
package github
