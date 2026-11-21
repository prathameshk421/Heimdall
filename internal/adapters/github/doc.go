// Package github is the GitHub adapter of the VCS ports (TECH_SPEC 2.3, task T24) and the token source that narrows
// every call to one permission (task T35).
//
// # Purpose
//
// The adapter reads pull requests, comments, reviews and check runs, and writes comments, branches, commits and check
// runs. It implements ports.PRReader, ports.PRComments, ports.BranchWriter, ports.CheckWriter, ports.CodeReader,
// ports.RepoReader, ports.ReviewReader and ports.CheckReader. Nothing outside this package sees a GitHub type.
//
// # API
//
//   - New(Config) builds the adapter. It parses the App key once and fails on a bad key or a bad base URL.
//   - Tokens() returns the TokenSource. Reader(ctx) mints a read-only installation token. Writer(ctx, op, repo)
//     mints a token narrowed to one repository and one operation (OpCheckWrite, OpBranchWrite, OpCommentWrite).
//     Each token is cached for 50 minutes on the injected Clock.
//   - Tokens().Installation(ctx) reads the installation's real permissions and repositories (GET
//     /app/installations/{id} with the App JWT). RequiredPermissions() is the set the product needs; doctor compares
//     the two.
//   - DefaultHead (ports.RepoReader) reads the default branch name, then that branch's commit, with the reader token.
//     An empty repository, or a repository with no default branch, is ports.ErrNotFound.
//   - PRReviews (ports.ReviewReader) reads every submitted review on a pull request with the reader token, skipping
//     pending reviews and mapping GitHub states to ports.ReviewState.
//   - CheckRuns (ports.CheckReader) reads check runs on a commit with the reader token across all pages, mapping
//     status and conclusions to change.CIConclusion, and omitting heimdall/gate.
//   - RateLimitError is returned when GitHub asks for a wait longer than two minutes, or keeps refusing after
//     three attempts. It wraps ports.ErrUnavailable, so the outbox retries it later.
//   - Action mode (work package AM9, ACTION_MODE 2, 5.1, 6.1) adds ActionAPI, built by NewAction or Adapter.Action. It
//     takes any Tokens source. StaticToken is the workflow's GITHUB_TOKEN, handed in by the caller from config, never read
//     from the environment here (R7). ActionAPI implements ports.PRComments (the summary comment, found by hidden marker
//     and author login), ports.PRCommentReader, ports.PRReviewCommentReader, ports.ArtifactReader, ports.CodeScanReader,
//     ports.GitRefs, ports.GitObjects, ports.GitReader (GetCommit, recursive GetTree, GetBlob) and ports.GitRefSwapper
//     (SwapRef, GraphQL updateRefs, for the git-ref event store AM8). It shares the client, pagination and error
//     mapping with Adapter.
//   - ActionAPI.Adapter() (AM15) returns an Adapter on the ActionAPI's client and tokens, so the Action gate reads the
//     pull request, files, reviews and code, and posts check runs, with the workflow's static token. Its Tokens() is
//     nil, because it has no App. ActionAPI.UpsertComment is Comment that also returns the comment id, which the gate
//     records in summary.posted; a reply without an id is an error.
//
// # Traps
//
//   - Status codes map to ports errors: 401 and 403 (without rate-limit headers) are ports.ErrUnauthorized, which the
//     outbox does not retry; 404 is ports.ErrNotFound; 5xx and transport failures are ports.ErrUnavailable.
//   - A 409 is ports.ErrNotFound only on a call that sets conflictMissing (the commit lookup of DefaultHead, where GitHub
//     answers 409 for an empty repository). On every other call a 409 stays an ordinary error, so a failed write is
//     never mistaken for a missing repository.
//   - Error text never carries a response body, a token or a key. A body can echo a token, so bodies are not logged.
//   - A circuit breaker opens after five consecutive unavailable outcomes and stays open for 30 seconds. Client errors
//     do not count against it.
//   - Rate limits are waited out inside the call only up to two minutes. A longer Retry-After returns at once.
//   - Only the first page of a list can be served from the ETag cache. A list spanning several pages is never cached.
//   - Every other GET is conditional: the client keeps the last 200 reply per URL and Accept type (at most 256
//     replies of 256 KiB) and sends its ETag. A 304 does not count against the rate limit and is served from that
//     reply. The key has no token in it, so one Heimdall install must not serve two installations with different
//     access to the same URL from one client.
//   - Next-page links must stay on the configured API origin. A link to any other host is an error, not a request.
//   - CodeReader.ReadFile uses the contents API, which returns a directory listing for a directory path and returns
//     at most 1 MB. Larger files are not verified.
//   - SetCheck finds an existing run by its external ID among the first 100 runs named check_name on the commit.
//     Not verified against a live repository (T35 live test is build-tagged and not run here).
//   - A summary comment is edited only when its author login equals ActionConfig.Author and it holds the marker. A person
//     can copy a marker, so a comment of any other author is never edited, and the adapter adds its own. A body over
//     MaxSummaryBodyBytes with its marker is ErrBodyTooLarge and nothing is sent. That limit is to be confirmed by S1.
//   - An artifact is untrusted input. DownloadArtifact refuses at the byte cap while the zip streams, refuses too many
//     entries, a path that is absolute or has "..", ".", an empty segment, a backslash, a NUL or a drive letter, a symlink,
//     a repeated path, and an entry or total larger than the limits after expansion. Each entry is read through a limit,
//     because a zip header can lie about its size. Every refusal is ports.ErrArtifactRefused. The reason names an
//     entry by index and never by path. GitHub redirects the download to a signed URL on another host, and Go drops the
//     Authorization header on a cross-host redirect. Not verified against a live run.
//   - A 422 from GitHub is not typed (the client maps only 401, 403, 404, 409 on conflictMissing and 5xx). CreateRef on a ref
//     that exists and UpdateRef on a branch that is not a fast-forward both return an ordinary error. The caller reads the
//     ref with GetRef to learn why.
//   - UpdateRef is NOT a compare-and-swap. Spike S1 (ACTION_MODE_SPIKE.md 1.1) measured that REST PATCH /git/refs with
//     force false checks fast-forward only on refs/heads/*. On refs/heimdall/* a stale write, a sibling and a rewind all
//     returned 200. A nil error does not prove nobody moved the ref. Use SwapRef instead.
//   - SwapRef is GraphQL updateRefs with beforeOid and force false, the call S1 1.2 verified as a compare-and-swap. It
//     always sends beforeOid. GitHub refuses a stale beforeOid with an untyped "Something went wrong" error and HTTP 200,
//     and a lost reply looks the same, so every outcome but success and FORBIDDEN wraps ports.ErrRefSwapUnconfirmed: the
//     caller reads the ref to learn what happened. FORBIDDEN (a token without contents: write), 401 and 403 are
//     ports.ErrUnauthorized. Errors repeat the GraphQL error type, never its message. The endpoint is /graphql next to
//     the REST root, or /api/graphql for a GitHub Enterprise Server root ending in /api/v3 (not verified live). The
//     repository's node id is read once per repository and kept on the ActionAPI.
//   - GraphQL cannot see refs/heimdall/* (S1 1.2: refs() and ref() return nothing), so GetRef, GetCommit, GetTree and
//     GetBlob use REST. GetTree is recursive in one call and refuses a truncated listing or one over the caller's entry
//     limit with ports.ErrObjectTooLarge. GetBlob checks the stated size against the caller's limit before decoding,
//     and a blob reply must fit the 8 MiB body cap.
//   - A repository without code scanning answers 404 or 403 on the alerts list. They stay ports.ErrNotFound and
//     ports.ErrUnauthorized, so a caller cannot tell "not enabled" from "no permission". Alert descriptions and messages
//     are untrusted text from the analyzer.
package github
