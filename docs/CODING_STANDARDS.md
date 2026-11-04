# Heimdall: coding standards

Status: v1, Nov 4, 2026 (decision D104). Applies to every Go package in `heimdall`. CI enforces what a tool can check; reviews enforce the rest. If a rule here is wrong for a case, change the rule in a PR instead of working around it.

The goal is code a new teammate can read top to bottom, that fails loudly and early, and that a table test can pin down. Prefer the plain solution over the clever one.

## 1. Structure

- Follow the layout in `TECH_SPEC.md` 2.2. **`domain` imports nothing internal. `app` imports `domain` and `ports`. Adapters implement ports. `transport` calls `app`.** A CI check fails the build on a violation.
- One package, one job. A package name is a short noun (`gate`, `slop`, `finding`), never `util`, `common`, `helpers` or `base`.
- Interfaces are **defined by the consumer** in `ports` and are small (one to five methods). Return concrete types, accept interfaces.
- No package-level mutable state, no `init()` that does work, no global clients. Everything is wired in `cmd/*` and passed in.
- A file stays under about 400 lines and a function under about 50. If a function needs a comment to explain its sections, split it.

## 2. Domain code (pure)

- No I/O, no clock, no randomness, no logging inside `domain`. Time and ids come in through the `Clock` and `IDs` ports.
- The reducer and the merge rule are plain functions of values. Every state and event pair has a table test, and every merge-rule clause has a test that flips it (spec 3.3, 12.4).
- Make illegal states unrepresentable: use distinct types for ids (`ChangeID`, `SHA`, `AgentName`), enums with a `Valid()` check, and constructors that validate.

## 3. Errors

- Return errors, never panic outside `main` and tests. Wrap with context: `fmt.Errorf("load change %s: %w", id, err)`. Lowercase, no trailing punctuation.
- Use sentinel errors (`ErrVersionConflict`, `ErrNotFound`) or typed errors only where a caller branches on them, checked with `errors.Is` and `errors.As`.
- Never swallow an error. If ignoring it is correct, say why in one comment.
- Adapters translate external errors into domain-level ones. Nothing above an adapter sees an HTTP status or a vendor type.

## 4. Context, concurrency and time

- `ctx context.Context` is the first parameter of anything that does I/O or can block. Every outbound call has a deadline (spec 2.5).
- Every goroutine has an owner, a stop condition and an error path. Use `errgroup` or River workers; do not start bare `go func()` in business code.
- Shared state is guarded by the database (advisory locks, optimistic versions), not by in-process mutexes, unless it is a cache.
- No `time.Sleep` in code or tests. Use the clock port and fake clocks.

## 5. Data and side effects

- Every state change is an event appended with an expected version. Every external side effect is an outbox row written in the same transaction (spec 2.4).
- Every outbox effect has a deterministic idempotency key. Handlers must be safe to run twice.
- SQL lives in `adapters/postgres` only, as named queries with parameters. No string-built SQL.
- Migrations are forward-only and tested against a real Postgres.

## 6. Naming and comments

- Names say what a thing is, not how it is built. No stutter (`gate.Gate` is fine, `gate.GateEvaluator` is not). Initialisms follow Go convention (`ID`, `SHA`, `URL`).
- Comments explain **why** and constraints. Never restate the code. Exported names get a one-line doc comment that starts with the name.
- No commented-out code, no TODO without an owner and an id (`TODO(T36)`), no dead flags.

## 7. Configuration, secrets and logging

- Config is validated at boot and the server refuses to start on any error (spec 2.6). Secrets are read from `/etc/heimdall/secrets` files only (D89), never from flags or environment in production.
- Logging uses `log/slog` with fields, never `fmt.Println`. A redaction handler strips tokens, keys and bodies. No secret, token or personal data in logs, rooms, packs or blobs.
- Errors are logged once, at the boundary that handles them.

## 8. Tests

- **Table tests** for pure logic, **real Postgres in Docker** for adapters that touch the database, and **fakes only for ports in unit tests of the application layer** (the end-to-end gate runs against the real systems).
- Tests assert behaviour through the public API of a package. Use `testify/require` for fatal checks and `cmp.Diff` for structures.
- A bug fix starts with a failing test. A new rule starts with the test that flips it.
- Tests are deterministic: no network, no real clock, no shared state, no ordering assumptions. A flaky test is a bug, not a retry.
- Golden files hold real examples (for instance Jira ADF from KAN tickets) and are regenerated only on purpose.

## 9. Dependencies

- Prefer the standard library. Each new dependency needs a one-line reason in the PR. Pinned in `go.sum`, vendored for deploy builds (D102), checked by `govulncheck` in CI.
- No reflection-heavy frameworks and no code generation we cannot read in a diff.

## 10. Tooling that must pass in CI

`gofmt` and `goimports`, `go vet`, `staticcheck`, `golangci-lint` with `errcheck`, `errorlint`, `gocritic`, `revive`, `gosec`, `bodyclose`, `contextcheck`, `nilerr`, `unparam` and `exhaustive`, the dependency-rule check, `go test -race ./...`, `govulncheck`, and `gitleaks`.

## 11. Pull requests and reviews

- A PR does one thing, with the test that proves it and a short description of why.
- Each package gets a short LLD note **before** its code: purpose, public API, invariants, failure modes, how it is tested. The note lives next to the package as `doc.go`.
- Every stage ends with a review pass (`/code-review` and the maintainability review) and the findings are fixed or recorded before the gate is called done.
- Generated or agent-written code is held to the same rules and is reviewed by a person who can explain it.
