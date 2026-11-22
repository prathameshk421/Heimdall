# Heimdall Whole-Product Security Review & VM Performance Hardening Audit

- **Branch audited:** `product-build` (base commit `954b5de` — `AM11b: verify WorkDir is writable during uidsandbox selfCheck`)
- **Audit branch:** `security-perf-audit`
- **Date:** 2026-11-22
- **Toolchain measured:** `go1.27.4` (`darwin/arm64` native & `linux/amd64` cross-compile)
- **Scope:** Full source audit of authentication, UID sandbox, git-ref event store, Markdown/external-comment parsers, Action mode trust boundaries, GitHub Actions workflows, binary sizes, HTTP clients, goroutine/ticker lifecycles, hot-path allocations, and VM resource footprint.

---

## Executive Summary

Heimdall's security architecture is unusually strong for its stage: the codebase consistently fails closed, keeps secrets out of error strings and command arguments, defends against DNS rebinding via `net.Dialer.Control`, uses AES-256-GCM `__Host-` cookies with AAD and constant-time comparisons, pins third-party GitHub Actions by full SHA, and enforces HMAC-SHA256 + GraphQL `updateRefs` CAS on `refs/heimdall/state`.

However, this review identified **1 Critical deployment/supply-chain issue, 4 High findings, 6 Medium findings, and 5 Low findings** across security, plus **6 concrete high-impact performance and VM hardening wins**.

### Summary of Security Findings

| ID | Severity | Area | Summary | Location |
|---|---|---|---|---|
| SEC-01 | **Critical** | Workflows | Placeholder SHA `0000...` in workflow templates risks unpinned `@main` copy-paste | `deploy/action/workflows/heimdall-{gate,proof,trigger}.yml` |
| SEC-02 | **High** | Action Gate / `workflow_run` | `serveProof` downloads & records forged artifacts (and costs) when a PR modifies `heimdall-proof.yml`; Round-1 `PostChecks` races ahead of `ReportProofUnknown` | `cmd/heimdall/action_gate.go:209-246`, `internal/app/action/proof_record.go:56-95`, `internal/app/action/gate_effects.go:27-52` |
| SEC-03 | **High** | `summary` | Deterministic runtime panic (`slice bounds out of range`) in `breakWWW` via Unicode case-folding length expansion (`İ` + `www.`) | `internal/domain/summary/escape.go:94-111` |
| SEC-04 | **High** | `external` | Deterministic runtime panic (`slice bounds out of range [-1:]`) in `flatten` on `#`-only lines in bot comments | `internal/domain/external/claim.go:315-319` |
| SEC-05 | **High** | `uidsandbox` | `Proxy` has no concurrency cap, dial timeout, or header read deadline — host-side FD/goroutine DoS from `PhaseFetch` | `internal/adapters/uidsandbox/proxy.go:197-200,257-274,333-377` |
| SEC-06 | **Medium** | `uidsandbox` | `Stage` does not wipe `/sbx/tmp` or freeze `/sbx/home` between `Exec` jobs (breaks `head-1`/`head-2`/`fixed-1` isolation), has no disk quota, and lacks `SystemCallFilter=` / `PhaseExec` `RestrictAddressFamilies=` | `internal/adapters/uidsandbox/sandbox.go:343-351,376-452` |
| SEC-07 | **Medium** | `uidsandbox` | `publicAddr` omits non-public IPv4/IPv6 special prefixes (`0.0.0.0/8`, `64:ff9b::/96` NAT64, `2002::/16`, `240.0.0.0/4`, etc.) | `internal/adapters/uidsandbox/proxy.go:160-167` |
| SEC-08 | **Medium** | `action` (`os.Root`) | `headFile` uses `root.Stat` + `root.ReadFile`, allowing in-tree symlinks to target `.git/*` and bypass `repoPath` `.git` exclusion | `internal/app/action/proof_review.go:295-324` |
| SEC-09 | **Medium** | `action` (Secrets) | `maskSecret` only masks literal `ModelKey` in transcript/summary; misses `GITHUB_TOKEN`, encoded/escaped forms, and `heimdall-proof.json` | `cmd/heimdall/action_proof.go:98,245-278` |
| SEC-10 | **Medium** | Workflows | `heimdall-gate.yml` passes `HEIMDALL_LOG_KEY` and `GITHUB_TOKEN` into `setup-go` and `go build` (missing `step: install` split) | `deploy/action/workflows/heimdall-gate.yml:41-46`, `deploy/action/action.yml:58-78` |
| SEC-11 | **Medium** | `auth` | Session revocations are in-memory only (lost on server restart) and unbounded; `RedirectURL` and `OIDC.Issuer` do not enforce `https://` | `internal/transport/auth/session.go:185-212`, `internal/transport/auth/auth.go:197-220` |
| SEC-12 | **Medium** | `external` / `action` | Indirect prompt injection surface in `reviewerMessages` (no BiDi stripping in `external`, no untrusted-data framing around claims/paths) | `internal/app/action/proof_review.go:118-140`, `internal/domain/external/claim.go:54-92` |
| SEC-13 | **Low** | `auth` | `safeNext` checks `/auth` prefix before path normalization (`/foo/../auth/start` passes); `githubProvider` allows default redirects | `internal/transport/auth/next.go:14-26`, `internal/transport/auth/provider.go:199` |
| SEC-14 | **Low** | `gitlog` | `Load` and effect-free `Append` do not verify `effects/<change>.jsonl`; whole-ref rollback requires GitHub ruleset; non-CAS `ActionAPI.UpdateRef` still exported | `internal/adapters/gitlog/store.go:115,137`, `internal/adapters/github/gitdata.go:78-94` |
| SEC-15 | **Low** | `github` | Server-mode `Adapter.Comment` and `Adapter.InlineComment` match any `Bot` user instead of Heimdall's own bot login | `internal/adapters/github/checks.go:113-116`, `internal/adapters/github/inline.go:40-44` |
| SEC-16 | **Low** | `uidsandbox` | `ensureUser` appends `heimdall-sbx` non-idempotently to `/etc/cron.deny` and `/etc/at.deny` on every `Setup` | `internal/adapters/uidsandbox/sandbox.go:308-312` |

---

## Part 1 — Whole-Product Security Review

### 1. Authentication & Web Board (`internal/transport/auth/`, `cmd/heimdall-server/board_auth.go`)

#### Verified Clean Controls
- **GitHub numeric user ID enforcement:** `internal/transport/auth/auth.go:289-299` rejects `Person.Login != ""` and `Person.GitHubID <= 0` when GitHub sign-in is active; `internal/transport/auth/provider.go:209-224` decodes `id` strictly into `int64` (rejecting floats/strings/non-positive values) and stores `githubKey(user.ID)` as `session.Identity`. Renamed GitHub accounts cannot hijack access.
- **OIDC validation:** `internal/transport/auth/provider.go:110-174` enforces PKCE S256 (`oauth2.S256ChallengeOption`), verifies ID token signature/issuer/audience/expiry via `go-oidc`, compares `idt.Nonce` using `subtle.ConstantTimeCompare` (`provider.go:137`), defaults to `sub`, and requires `email_verified` (`true` or `"true"`) when configured for `email`.
- **`__Host-` cookie and AEAD sealing:** `internal/transport/auth/session.go:19-20,64-138` uses `__Host-heimdall-session` and `__Host-heimdall-login` with `Path: "/"`, `HttpOnly: true`, `Secure: true`, `SameSite: SameSiteLaxMode`, and no `Domain`. Values are sealed with AES-256-GCM (12-byte `crypto/rand` nonce, cookie name as AAD to block cross-cookie swapping), decoded with `base64.RawURLEncoding.Strict()` and `dec.DisallowUnknownFields()`. Session TTL is clamped to `[5m, 24h]` with no sliding renewal (`auth.go:200`, `session.go:154-157`).
- **CSRF & cross-origin defense:** `internal/transport/auth/flow.go:59-62,80-93,184-209` requires both `http.CrossOriginProtection.Check(r)` and a constant-time match of the session's 256-bit `CSRF` token (`X-CSRF-Token` header or `csrf` form field capped at 64 KiB). `GET /auth/logout` renders a confirmation form only; logout requires `POST` + CSRF. Dev mode (`internal/transport/auth/dev.go:15-42`) enforces `CheckLoopback` on the resolved `net.TCPAddr` and keeps `CrossOriginProtection` active.
- **Live role re-validation:** `internal/transport/auth/flow.go:54-58` and `auth.go:330-340` call `g.people.current(s.Person, s.Identity)` on every request with `subtle.ConstantTimeCompare`, so config removal or role demotion takes effect immediately upon reload.
- **Security headers:** `internal/transport/auth/flow.go:32-36,48` sets strict CSP (`default-src 'none'; style-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`), `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, and `Cache-Control: no-store`.

#### Findings

##### [SEC-11a] Medium — Session Revocation Is In-Memory Only and Unbounded
- **Location:** `internal/transport/auth/session.go:185-212`, `internal/transport/auth/flow.go:205`
- **Exploit Scenario:** A user signs out on a shared or compromised browser, invoking `POST /auth/logout` which adds `s.ID` to `g.revoked.until` in process RAM. When `heimdall-server` restarts (deployment, OOM, or crash), the in-memory `revocations` map is wiped. An attacker holding the previously captured `__Host-heimdall-session` cookie can replay it and regain authenticated board access for the remainder of `SessionTTL` (up to 24 hours). Additionally, `revocations.add` has no size cap and expired entries are only purged when another logout occurs.
- **Recommended Fix:** Back `revocations` with a small Postgres table (`revoked_sessions(sid TEXT PRIMARY KEY, expires_at TIMESTAMPTZ NOT NULL)`) passed via a port into `auth.Config`, falling back to a bounded in-memory map only in tests. Prune expired rows periodically or on `add()`/`has()`.

##### [SEC-11b] Medium — `RedirectURL` and `OIDC.Issuer` Do Not Enforce `https://`
- **Location:** `internal/transport/auth/auth.go:197-199,218-220`
- **Exploit Scenario:** `checkConfig` checks `!u.IsAbs() || u.Host == "" || u.Path != callbackPath` for `RedirectURL` and only `cfg.OIDC.Issuer == ""` for OIDC. A deployment misconfigured with an `http://` `RedirectURL` transmits OAuth authorization codes in plaintext and breaks `__Host-` `Secure` cookies in browsers; an `http://` `OIDC.Issuer` allows on-path tampering of OIDC discovery metadata and JWKS endpoints.
- **Recommended Fix:** Reject `u.Scheme != "https"` for both `RedirectURL` and `OIDC.Issuer` (requiring `url.Parse` on `OIDC.Issuer` with `u.Scheme == "https" && u.Host != ""`), permitting `http://` only for loopback hosts in explicitly flagged test harnesses if needed.

##### [SEC-13] Low — `safeNext` Checks `/auth` Exclusion Without Path Normalization; `githubProvider` Follows Redirects
- **Location:** `internal/transport/auth/next.go:14-26`, `internal/transport/auth/provider.go:199`
- **Exploit Scenario:** `safeNext` blocks open redirects to external origins effectively (`localShape` rejects `//`, `/\`, `\`, `#`, control bytes; `url.Parse` rejects scheme/host/user/opaque). However, line 22 checks `u.Path == "/auth" || strings.HasPrefix(u.Path, "/auth/")` without `path.Clean(u.Path)`. A crafted link `/auth/login?next=/x/../auth/start` passes `safeNext` and sends `Location: /x/../auth/start`, which browsers normalize to `/auth/start` (causing a sign-in loop or landing on `/auth/logout`). Separately, `githubProvider.identify` uses `p.client.Do(req)` with default redirect following on `https://api.github.com/user`.
- **Recommended Fix:** In `safeNext`, compute `cleaned := path.Clean(u.Path)` and return `"/"` if `cleaned == "/auth" || strings.HasPrefix(cleaned, "/auth/")` or if `strings.Contains(u.Path, "..")`. In `auth.New`, attach a `CheckRedirect` refusing redirects on `githubUserURL` (or allow 0 redirects for the user API call).

---

### 2. UID Sandbox & Runner Hardening (`internal/adapters/uidsandbox/`, `deploy/heimdall-runner.service`)

#### Verified Clean Controls
- **DNS rebinding prevention:** `internal/adapters/uidsandbox/proxy.go:169-200` attaches `guardControl` to `net.Dialer.Control`, inspecting the resolved `ip:port` immediately prior to `connect(2)`. Because no second DNS lookup occurs between validation and socket connection, classic TOCTOU DNS rebinding is prevented.
- **Proxy allowlist parsing:** `internal/adapters/uidsandbox/proxy.go:45-155` allows `CONNECT` only, matches lower-cased exact hostnames (no wildcards), rejects IPv4/IPv6 literals (including numeric last-label forms like `0x7f.1` and `2130706433`), and rejects disallowed ports and URI delimiters.
- **nftables egress fence:** `internal/adapters/uidsandbox/sandbox.go:354-372` installs `table inet heimdall_sbx` covering both IPv4 and IPv6, permitting only `meta skuid <uid> oifname "lo" ip daddr 127.0.0.1 tcp dport <port> accept` and dropping all other outbound packets (blocking direct DNS, IPv6 loopback, local host ports, and `169.254.169.254`).
- **`/dev/shm` and systemd hardening + fail-closed self-check:** `internal/adapters/uidsandbox/sandbox.go:228-275,413-452` and `selfcheck.go:16-188` mount a fresh tmpfs on `/run` and `/dev/shm:mode=1777`, enable `PrivateTmp=yes`, `PrivateDevices=yes`, `PrivateIPC=yes`, `ProtectProc=invisible`, `ProtectHome=yes`, `ProtectSystem=strict`, `NoNewPrivileges=yes`, empty capability sets, `PrivateNetwork=yes` (`PhaseExec`), and `RestrictAddressFamilies=AF_INET AF_INET6` (`PhaseFetch`). `plantMarkers` + `selfCheck` prove before any PR code runs that host `/tmp`, `/var/tmp`, `/dev/shm`, `/run/{systemd,dbus,user}`, `/proc/1`, `RunnerHome`, and `RunnerTemp` are invisible, `NoNewPrivs=1`, `Seccomp=2`, all capability bitmasks are `0`, `WorkDir` is writable, and `PhaseExec` has only `lo`.
- **`Stage` privilege boundary:** `internal/adapters/uidsandbox/sandbox.go:376-405` runs `tar -x -f - -C /sbx/work --no-same-owner --no-same-permissions --exclude=.git` inside `PhaseExec` as `heimdall-sbx`, and `readAsUser` (`sandbox.go:642-667`) reads JUnit XML strictly as `heimdall-sbx` bounded to `verify.MaxReportBytes+1`.

#### Findings

##### [SEC-05] High — `uidsandbox.Proxy` Has No Concurrency Limit, Dial Timeout, or Header Read Deadline
- **Location:** `internal/adapters/uidsandbox/proxy.go:197-200,257-274,333-377`
- **Exploit Scenario:** `StartProxy` initializes `&net.Dialer{Control: guardControl}` with zero `Timeout` and zero `KeepAlive`. `p.serve` accepts connections on `127.0.0.1:<port>` in an unbounded loop and spawns a goroutine per connection; `p.handle` reads request headers (`readHead(br)`) with no `SetReadDeadline`. Untrusted code executing during `PhaseFetch` can open hundreds/thousands of TCP sockets to `127.0.0.1:<ProxyPort>` and stall after sending 1 byte, exhausting file descriptors or pinning goroutines/memory in the privileged host `heimdall` process (which runs outside `heimdall-sbx`'s `MemoryMax` cgroup).
- **Recommended Fix:**
  1. Set `d := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second, Control: guardControl}` in `StartProxy`.
  2. Guard `p.serve` with a bounded semaphore (e.g., `sem := make(chan struct{}, 64)`), closing excess connections immediately with `503 Service Unavailable`.
  3. Set `c.SetReadDeadline(time.Now().Add(10 * time.Second))` before `readHead(br)` and clear it (`c.SetReadDeadline(time.Time{})`) only after sending `200 Connection established`, plus bound idle tunnel lifetime via `SetDeadline`.

##### [SEC-06] Medium — Cross-Job State Persistence in `/sbx/tmp` and `/sbx/home` Breaks `Prove` Isolation; Missing `SystemCallFilter=` and Disk Quotas
- **Location:** `internal/adapters/uidsandbox/sandbox.go:343-351,376-452`, `internal/app/action/proof_candidates.go:56-57`
- **Exploit Scenario:**
  1. `proof_candidates.go:56-57` relies on the invariant *"Every run is a separate sandbox job, so the two head runs share nothing"*, running `head-1`, `head-2`, and `fixed-1` sequentially to detect flaky tests and confirm the bug disappears on the reverted tree. However, `Stage()` (`sandbox.go:383`) only calls `resetWork(ctx)` (wiping `/sbx/work`) while `serviceProps` (`sandbox.go:421`) grants `ReadWritePaths=/sbx/work /sbx/home /sbx/tmp` to every job and `jobEnv` sets `HOME=/sbx/home`, `TMPDIR=/sbx/tmp`. A hostile PR test can write a counter or marker into `/sbx/tmp` or `/sbx/home` during `head-1` (or tamper with cached Go modules in `/sbx/home/go/pkg/mod`), causing `head-1`/`head-2` to fail and `fixed-1` to pass (or vice versa), manipulating `testintegrity.Prove`.
  2. Neither `Stage` nor `Exec` enforces a disk quota on `/sbx/work`, `/sbx/home`, `/sbx/tmp`, or private `/tmp`, allowing a hostile test (or direct `Stage` tar bomb) to fill the runner's root volume.
  3. `serviceProps` does not specify `SystemCallFilter=`; `Seccomp=2` in `selfCheck` only reflects filters from `RestrictNamespaces`/`LockPersonality`/`SystemCallArchitectures`/`RestrictAddressFamilies`, leaving kernel attack surface (such as `io_uring_setup`) exposed. `PhaseExec` also omits `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`, exposing `AF_NETLINK`, `AF_VSOCK`, and `AF_ALG`.
- **Recommended Fix:**
  1. Reset `/sbx/tmp` inside `resetWork` (or remove `/sbx/tmp` and point `TMPDIR=/tmp`, leveraging systemd's per-unit `PrivateTmp=yes` which is already ephemeral per job).
  2. Mount `/sbx/home` read-only during `PhaseExec` (`ReadWritePaths=/sbx/work` only, or separate `GOMODCACHE` read-only and ephemeral `GOCACHE=/tmp/gocache`) so `PhaseExec` jobs cannot carry state across `head-1`, `head-2`, and `fixed-1` or poison Go module cache.
  3. Add `SystemCallFilter=@system-service` (or `~@privileged @mount @module @raw-io @reboot @swap @clock @obsolete @debug`) and `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6` to `PhaseExec` (verifying with `selfCheck`).
  4. Consider mounting `/sbx/work` as a size-bounded `tmpfs` (e.g., `size=1G`) or loopback quota on runners that support it.

##### [SEC-07] Medium — `publicAddr` Omits Special-Use IPv4/IPv6 Ranges (Including `0.0.0.0/8` and NAT64 `64:ff9b::/96`)
- **Location:** `internal/adapters/uidsandbox/proxy.go:160-167` (and `internal/adapters/rootlessrunner/fetch.go:94-102`)
- **Exploit Scenario:** `publicAddr` checks Go's `IsLoopback()`, `IsPrivate()`, `IsLinkLocal*()`, `IsMulticast()`, `IsUnspecified()`, and `100.64.0.0/10`. However, `IsUnspecified()` only matches `0.0.0.0` and `::`, not `0.0.0.0/8` (on Linux, connecting to `0.1.2.3` can route to localhost). It also permits `64:ff9b::/96` and `64:ff9b:1::/48` (IPv6 NAT64 well-known prefixes that synthesize IPv4 private/metadata addresses such as `64:ff9b::169.254.169.254` on NAT64-enabled hosts), `2002::/16` (6to4), `2001::/32` (Teredo), `192.0.0.0/24`, `198.18.0.0/15`, and `240.0.0.0/4`. In `rootlessrunner/fetch.go:94-102`, `publicAddresses` additionally omits `ip.Unmap()` and `100.64.0.0/10` (CGNAT).
- **Recommended Fix:** Reject all IANA non-globally-reachable prefixes explicitly in both `uidsandbox.publicAddr` and `rootlessrunner.publicAddresses` (`0.0.0.0/8`, `100.64.0.0/10`, `192.0.0.0/24`, `192.0.2.0/24`, `198.18.0.0/15`, `198.51.100.0/24`, `203.0.113.0/24`, `240.0.0.0/4`, `64:ff9b::/96`, `64:ff9b:1::/48`, `100::/64`, `2001::/32`, `2001:db8::/32`, `2002::/16`), always calling `ip.Unmap()` first.

##### [SEC-16] Low — Non-Idempotent Append to `/etc/cron.deny` and `/etc/at.deny`
- **Location:** `internal/adapters/uidsandbox/sandbox.go:308-312`
- **Exploit Scenario:** On persistent self-hosted runners, every `Setup()` appends `heimdall-sbx\n` unconditionally via `tee -a`, growing both files indefinitely. Also, if `/etc/cron.allow` exists, `/etc/cron.deny` is ignored by cron (safe if `heimdall-sbx` is absent from `cron.allow`, but worth verifying).
- **Recommended Fix:** Check whether `heimdall-sbx` is already listed (e.g., `grep -qxF`) before appending.

---

### 3. Git-Ref Event Store (`internal/adapters/gitlog/`, `internal/adapters/github/gitstate.go`)

#### Verified Clean Controls
- **MAC verification:** `internal/adapters/gitlog/logfile.go:33,55-94` computes HMAC-SHA256 over NUL-delimited `(macContext="heimdall-gitlog-v1", changeID, version, hash, effects)` with a minimum 32-byte key (`store.go:89`), validates using constant-time `hmac.Equal` (`logfile.go:90`), and checks the MAC **before** reading or parsing the change file (`state.go:150-162`). `Config` and `Store` implement `String()` and `GoString()` returning redacted placeholders (`store.go:65-68,82-85`).
- **Hash chain & strict JSON:** `internal/adapters/gitlog/state.go:162-170`, `logfile.go:97-138`, and `internal/domain/change/hash.go:143-183` enforce strict JSON (`DisallowUnknownFields`, single JSON value per line, trailing newline required), consecutive versions `1..N`, uniform `ChangeID` across all events, unbroken SHA-256 hash chain, and head match.
- **CAS via GraphQL `updateRefs`:** `internal/adapters/github/gitstate.go:29-30,251-323` sends parameterized GraphQL variables (`beforeOid`, `afterOid`, `force: false`) after validating `repo`, `ref`, `before`, and `after` SHAs — eliminating GraphQL injection and REST PATCH overwrite bugs. `FORBIDDEN`/`401`/`403` fail closed immediately without retry (`publish.go:199-201`). Opaque GraphQL errors re-read the ref tip via REST (`publish.go:205-221`) to distinguish lost replies, transient failures (bounded backoff), and concurrent writers (bounded rebuilds up to `maxRebuilds=10`).
- **Tamper handling:** `internal/app/action/gate.go:220-244` and `cmd/heimdall/action_gate.go:75-79` fail closed (`ErrStateUnverified`) whenever `gitlog.ErrIntegrity` is returned and `Resetter` is nil.

#### Findings

##### [SEC-14] Low — Effects File Verification Deferred on Read-Only/Effect-Free Operations; Whole-Ref Rollback & Exported `UpdateRef`
- **Location:** `internal/adapters/gitlog/store.go:115,137`, `internal/adapters/gitlog/state.go:80-84`, `internal/adapters/github/gitdata.go:78-94`
- **Exploit Scenario:**
  1. `Store.Load` calls `s.read(ctx, changeID, false)` and `Store.Append` calls `s.read(ctx, changeID, len(fx) > 0)`. If an actor with repository write permission modifies `effects/<change>.jsonl` without modifying `.head`, `Load` and effect-free `Append` do not detect the tampering (though the next `Append` with effects will fail with `ErrIntegrity`).
  2. Anyone with repository write access can `git push --force refs/heimdall/state` back to an older validly-MAC'd commit (whole-ref rollback, documented in `gitlog/doc.go:66-69`), replaying older budget/state history.
  3. `github.ActionAPI.UpdateRef` (`gitdata.go:78-94`) still exports the non-CAS REST `PATCH /git/refs/...` helper; while `gitlog.Git` omits it, a future caller could inadvertently invoke it on `refs/heimdall/*`.
- **Recommended Fix:** Document a repository ruleset in `deploy/action/README.md` restricting push/force-push/delete on `refs/heimdall/*` exclusively to the Heimdall App; remove or unexport `ActionAPI.UpdateRef` if it has no legitimate callers; optionally verify `effects/<change>.jsonl` presence/digest whenever it exists in the tree.

---

### 4. Summary Rendering & External Bot Comment Ingestion (`internal/domain/summary/`, `internal/domain/external/`)

#### Verified Clean Controls
- **Markdown/HTML injection & BiDi neutralization in `summary`:** `internal/domain/summary/escape.go:16-82,118-138` strips all `unicode.Cf` (BiDi overrides, ZWSP/ZWNJ), control chars, and invalid UTF-8; encodes `&`, `<`, `>` as HTML entities; backslash-escapes Markdown punctuation; breaks `@mentions`, `#123` references, and `://` URLs with ZWSP; uses adaptive backtick fences in `code()`; and parses `Marker` strictly on line 1 only (`marker.go:32-56`).
- **Bot spoofing prevention in `external`:** `internal/domain/external/tool.go:36-120` requires `strings.EqualFold(c.UserType, "Bot")` (verified populated from GitHub's `user.type` in `internal/adapters/github/comments.go:115`), membership in the fixed 7-tool GitHub App login allowlist, explicit listing in merge-base `action.sources`, `InReplyTo == 0`, and tool-specific markers where present.
- **Bounds & path validation in `external`:** `resolve.go:119-123` rejects absolute paths, `..`, backslashes, and NULs; `types.go:10-25` bounds bodies (64 KiB), comments (1,000), candidates (200), line ranges (500), diff ops (200k), and patches (1 MiB).

#### Findings

##### [SEC-03] High — Deterministic Runtime Panic (DoS) in `summary.breakWWW` on Unicode Case-Folding Length Change
- **Location:** `internal/domain/summary/escape.go:94-111`
- **Exploit Scenario:** `breakWWW` computes `lower := strings.ToLower(s)`, locates `"www."` in `lower`, and slices the original string `s[last:at]` using byte offset `at` computed from `lower`. In UTF-8, `strings.ToLower` changes byte length for certain characters (e.g., `U+0130` `'İ'` is 2 bytes, lowercasing to 3 bytes `"i\u0307"`; `U+212A` `'K'` is 3 bytes, lowercasing to 1 byte `'k'`). Untrusted text reaching `summary.Render` (e.g., a finding claim, path, or fast-check reason containing `"İİİİwww."`) makes `at > len(s)` (`15 > 12`) and triggers a **runtime panic (`slice bounds out of range`)**, crashing `heimdall action gate` and preventing check/summary publication. With `'K'` preceding `"www."`, `at` lands mid-rune, corrupting UTF-8 and failing to neutralize the `"www."` link.
- **Recommended Fix:** Scan `s` directly in-place matching ASCII `'w'|'W'` followed by `'.'` without calling `strings.ToLower(s)`:
  ```go
  func isWWW(s string, i int) bool {
      if i+4 > len(s) || s[i+3] != '.' {
          return false
      }
      return (s[i]|0x20) == 'w' && (s[i+1]|0x20) == 'w' && (s[i+2]|0x20) == 'w'
  }
  ```

##### [SEC-04] High — Deterministic Runtime Panic (DoS) in `external.flatten` on `#`-Only Lines
- **Location:** `internal/domain/external/claim.go:315-319`
- **Exploit Scenario:** In `flatten`:
  ```go
  if strings.HasPrefix(raw, "#") || (strings.HasPrefix(raw, "**") && strings.HasSuffix(raw, "**")) {
      line = strings.TrimSpace(strings.TrimLeft(line, "#"))
      if !strings.ContainsAny(line[len(line)-1:], ".!?:;") {
          line += "."
      }
  }
  ```
  If any recognized bot comment contains a line consisting solely of `#` characters (e.g., `###` or `## ** **`), `strings.TrimSpace(strings.TrimLeft(line, "#"))` yields `""` (`len(line) == 0`), and `line[len(line)-1:]` evaluates `line[-1:]`, triggering a **runtime panic (`slice bounds out of range [-1:]`)** that crashes both `heimdall action gate` and `heimdall action proof`. An attacker can trigger this if any enabled third-party reviewer echoes a `###` line from a PR comment or code block.
- **Recommended Fix:** Immediately after line 316 (`line = strings.TrimSpace(strings.TrimLeft(line, "#"))`), add:
  ```go
  if line == "" {
      continue
  }
  ```

##### [SEC-12] Medium — Indirect Prompt Injection Surface in `reviewerMessages`
- **Location:** `internal/app/action/proof_review.go:109-140`, `internal/domain/external/claim.go:54-92`
- **Exploit Scenario:** `external.parseBody` does not strip Unicode format/BiDi characters (`unicode.Cf`), and `reviewerMessages` interpolates PR file paths (`f.Path`) and third-party bot claims (`c.Claim`) directly as inline prose into the user prompt without structural data delimiters or an explicit prompt-injection instruction. While Heimdall's proof rule prevents an LLM from fabricating a *proven* finding without a real failing repro test, prompt injection embedded in PR diffs, filenames, or bot-echoed claims can still steer the reviewer model to call `finish_review` early (suppressing real bugs into `not_reproduced`) or exhaust the 40-turn / USD budget (`unknown: turn_cap` / `budget` DoS).
- **Recommended Fix:** Strip `unicode.Cf` and control characters in `external.prepBody` and when formatting `f.Path` in `reviewerMessages`; wrap file lists and third-party claims in explicit `<untrusted_pr_data>` delimiters; and add one sentence to `reviewerPrompt` instructing the model to treat code, diffs, filenames, and third-party claims strictly as untrusted data and never as instructions.

##### [SEC-15] Low — Server-Mode `Adapter.Comment` and `Adapter.InlineComment` Match Any `Bot` Login
- **Location:** `internal/adapters/github/checks.go:113-116`, `internal/adapters/github/inline.go:40-44`
- **Exploit Scenario:** Unlike `ActionAPI.upsert` (`internal/adapters/github/comments.go:171-175`), which verifies `strings.EqualFold(c.Login, x.author)` before editing/skipping a marked comment, server-mode `Adapter.Comment` and `Adapter.InlineComment` check only `c.UserType == "Bot" && strings.Contains(c.Body, marker)`. If another installed GitHub App bot echoes Heimdall's `<!-- heimdall:effect=... -->` marker, `InlineComment` falsely treats the finding as already posted and skips it, and `Comment` attempts to `PATCH` the foreign bot's comment (failing with 403).
- **Recommended Fix:** Match the App's own bot login (`strings.EqualFold(c.Login, a.botLogin)`) in `Adapter.Comment` and `Adapter.InlineComment`.

---

### 5. Action Mode App & CLI (`internal/app/action/`, `cmd/heimdall/action*.go`)

#### Verified Clean Controls
- **Zip & artifact validation:** `internal/adapters/github/artifacts.go:104-242` and `internal/app/action/artifact.go:97-262` bound zip bytes, entry counts, per-file expanded bytes, and total expanded bytes using `io.LimitReader` (ignoring untrusted zip headers), reject symlinks, drive letters, `..`, and duplicate paths, enforce `DisallowUnknownFields` + single JSON object, check `Version`/`Head`/`Base`/`PolicyHash`/`WorkflowRun`, reconcile `Counts` against `Candidates`, and recompute every proven candidate's `Blocking` flag against merge-base policy (`proof_record.go:123-147`).
- **`stageTree` symlink safety:** `cmd/heimdall/action_sandbox.go:143-234` walks the checkout through `os.OpenRoot`, writes symlinks as symlinks (`root.Readlink`) rather than dereferencing them, and replaces any symlink at an overlay target with a regular file (`action_sandbox.go:200-204`).
- **Secret redaction in config & `action init`:** `internal/platform/config/action.go:88-97` redacts `ActionEnv` across `String`/`GoString`/`Format`; `cmd/heimdall/action_init.go:35-66,238-282,327-340` delivers `HEIMDALL_LOG_KEY`, `HEIMDALL_MODEL_KEY`, and `HEIMDALL_APP_KEY` strictly via `stdin` to `gh` and redacts `app-manifests/<code>/conversions` in error text.

#### Findings

##### [SEC-02] High — `serveProof` Records Forged Artifacts & Costs When a PR Edits `heimdall-proof.yml`; Round-1 `PostChecks` Races Ahead of `ReportProofUnknown`
- **Location:** `cmd/heimdall/action_gate.go:209-246`, `cmd/heimdall/action_event.go:239-251`, `internal/app/action/proof_record.go:56-95,150-157`, `internal/domain/actionflow/rows_publish.go:54-61`, `internal/app/action/gate_effects.go:27-52`
- **Exploit Scenario:** An external contributor opens a **fork PR** modifying `.github/workflows/heimdall-proof.yml` so the proof workflow skips the sandbox and uploads a forged `heimdall-proof.json` claiming `kind: "proven"`, `counts.blocking: 0`, and an inflated `cost` (e.g., `$999`) matching the PR's `head`, `base`, `policy_hash`, and `workflow_run`.
  1. `Heimdall gate` triggers via `workflow_run` and runs `serveProof` (`action_gate.go:209`).
  2. Line 210 (`w.runOne`) detects `ProofWorkflowChanged && !PolicyOwnerApproved` and appends `proof.reported` with `unknown: workflow_changed` and `WorkflowRun: <gateRunID>`.
  3. Line 222 calls `proofUntrusted(run, rec, pr.HeadSHA)` — which does **not** check `ProofWorkflowChanged` — so line 224 downloads the forged artifact and line 232 calls `w.gate.RecordProof`.
  4. `RecordProof` (`proof_record.go:85`) checks `currentReport(r.view, a)`, sees `<gateRunID> != <proofRunID>`, and **appends the attacker's forged `proof.reported` AND `cost.recorded` events to `refs/heimdall/state`**.
  5. Line 245 calls `w.runOne` again. On Round 1 of `r.rounds`, `actionflow.Decide` evaluates `proofRow()` (emitting `ReportProofUnknown`) AND `publishRow()` (`rows_publish.go:56`), which reads `f.usableProof()` -> **the attacker's forged `proof.reported`** and emits `PostChecks` with the attacker's conclusion (`success` / `neutral`).
  6. `r.apply` (`gate_effects.go:28-37`) executes `PostChecks` **before** `ReportProofUnknown` — posting the forged green `heimdall/proof` check run to GitHub! If the gate fails or times out before Round 2, the forged check persists; even when Round 2 overwrites the check, the forged `cost.recorded` event stays permanently in `refs/heimdall/state`, poisoning monthly spend tracking.
- **Recommended Fix:**
  1. In `Gate.RecordProof` (`proof_record.go:56-95`), read PR files/approvals (or have `serveProof` check `ProofWorkflowChanged && !PolicyOwnerApproved` before calling `artifactFile`) and refuse to download or record any artifact when `heimdall-proof.yml` was modified without policy-owner approval — record `unknown: workflow_changed` with zero cost instead.
  2. In `actionflow/rows_publish.go:56` (`checks`), gate `f.usableProof()` on `!(f.in.ProofWorkflowChanged && !f.in.PolicyOwnerApproved)`.
  3. In `internal/app/action/gate_effects.go:27-52` (`apply`), only post checks when no log-advancing effect (`RunFast`, `IngestExternal`, `SkipProof`, `ReportProofUnknown`) is also pending in the same round, so checks always reflect the settled event log.

##### [SEC-08] Medium — `headFile` Follows In-Tree Symlinks Into `.git/` (Bypassing `repoPath` Exclusion)
- **Location:** `internal/app/action/proof_review.go:295-324`
- **Exploit Scenario:** `repoPath` rejects paths starting with `.git/`, and `os.OpenRoot` prevents symlinks from escaping *outside* `rv.checkout`. However, `os.Root.Stat` and `os.Root.ReadFile` follow symlinks whose target remains *inside* `rv.checkout` — including `.git/config`, `.git/HEAD`, or `.git/objects/*`. If a PR contains a symlink `leak.txt -> .git/config` and a consumer workflow omits `persist-credentials: false` (or `.git/config` holds local remotes/tokens/history), the LLM's `read_file("leak.txt")` tool reads `.git/config` cleanly, bypassing the `.git` guard. (By contrast, `searcher.wanted` at `proof_review.go:394` checks `d.Type().IsRegular()`, which correctly skips symlinks.)
- **Recommended Fix:** Replace `root.Stat` at `proof_review.go:304` with `root.Lstat`, rejecting any non-regular entry (`!info.Mode().IsRegular()` rejects symlinks), and verify every parent path component has no symlink into `.git` (or open via `root.OpenFile` with `O_NOFOLLOW` on Linux/macOS / resolve via `root.Readlink` and reject `.git`).

##### [SEC-09] Medium — Incomplete Secret Redaction in `heimdall-proof-transcript.json` and `heimdall-proof.json`
- **Location:** `cmd/heimdall/action_proof.go:98,245-278`, `internal/app/action/proof_transcript.go:79-148`
- **Exploit Scenario:** `writeTranscript` (`action_proof.go:249`) and `writeSummary` (`action_proof.go:269`) apply `maskSecret(..., s.env.ModelKey)` using exact literal byte replacement only:
  1. `s.env.Token` (`GITHUB_TOKEN`) is **never** masked.
  2. `heimdall-proof.json` (`action_proof.go:98`) is written without `maskSecret`, even though `ArtifactCandidate.Detail` and `Claim` contain strings derived from sandbox/model output.
  3. JSON-escaped, URL-encoded, or base64-encoded representations of `ModelKey` (e.g., echoed by an upstream proxy error or model text) bypass exact `bytes.ReplaceAll`. Because `heimdall-proof` is uploaded as a GitHub Actions artifact (publicly downloadable on public repositories), any leaked token is exposed.
- **Recommended Fix:** Mask both `s.env.ModelKey` and `s.env.Token` (raw, JSON-escaped, and base64 forms) on `artifact`, `transcript`, and `summary`, and run `internal/secretscan` redaction over `TranscriptTurn.Text`, `ProofTranscript.Detail`, and `TranscriptCall.Error` before writing.

---

### 6. GitHub Actions Workflows & Composite Action (`deploy/action/action.yml`, `deploy/action/workflows/*.yml`)

#### Verified Clean Controls
- **Third-party action pinning:** `actions/checkout@3d3c42e5...`, `actions/setup-go@b7ad1dad...`, and `actions/upload-artifact@cf430e03...` are all pinned to full 40-char commit SHAs.
- **No `pull_request_target` or script injection:** All `${{ inputs.* }}` pass through `env:` in `action.yml:83-89`; event payloads are parsed strictly by Go from `GITHUB_EVENT_PATH`.
- **Least-privilege permissions & `persist-credentials: false`:** `heimdall-gate.yml` sets top-level `permissions: {}` and scopes job permissions; `heimdall-proof.yml` uses `contents: read`, `pull-requests: read`, and `persist-credentials: false`; `heimdall-trigger.yml` uses `contents: read`.
- **Build-before-PR-toolchain ordering in `heimdall-proof.yml`:** `step: install` builds `heimdall` before `actions/setup-go` reads the PR's `go.mod` and before `HEIMDALL_MODEL_KEY` enters `env:`.

#### Findings

##### [SEC-01] Critical — Unpinned Placeholder SHA `0000000000000000000000000000000000000000` in All Workflow Templates
- **Location:** `deploy/action/workflows/heimdall-gate.yml:41`, `deploy/action/workflows/heimdall-proof.yml:31,40,43`, `deploy/action/workflows/heimdall-trigger.yml:28`
- **Exploit Scenario:** Every workflow template ships with `uses: prathameshk421/Heimdall/deploy/action@0000000000000000000000000000000000000000 # heimdall-pin`. While literal zeros fail safely, requiring manual search-and-replace during onboarding leads operators to substitute mutable refs (`@main` or `@v1`), breaking supply-chain immutability for the privileged `heimdall-gate.yml` job that holds `HEIMDALL_LOG_KEY` and `contents: write`.
- **Recommended Fix:** Add a `heimdall action init` (or `heimdall action workflows --pin <40-hex-sha>`) generator that writes the three workflow files with a validated commit SHA (refusing non-40-hex refs), and enforce a release-gate check verifying released workflow artifacts carry the release commit SHA.

##### [SEC-10] Medium — `heimdall-gate.yml` Exposes `HEIMDALL_LOG_KEY` and `GITHUB_TOKEN` to `actions/setup-go` and `go build`
- **Location:** `deploy/action/workflows/heimdall-gate.yml:41-46`, `deploy/action/action.yml:58-78`
- **Exploit Scenario:** Unlike `heimdall-proof.yml:31-33` (which invokes `step: install` with no secrets before `step: proof`), `heimdall-gate.yml` runs a single `step: gate` with `GITHUB_TOKEN` and `HEIMDALL_LOG_KEY` attached to the composite action's `env:`. Consequently, `actions/setup-go` and `go build` (including network fetches from `proxy.golang.org` to compile `heimdall`) execute with both privileged secrets in their process environment.
- **Recommended Fix:** Split `heimdall-gate.yml` into two steps — `step: install` (no `env:` secrets) followed by `step: gate` (with `GITHUB_TOKEN` and `HEIMDALL_LOG_KEY`) — and ultimately replace on-runner `go build` in `action.yml` with a SHA-256 checksum-verified prebuilt release binary (cutting ~44s per run and removing the build toolchain from the privileged job entirely).

##### [SEC-02b] High (Cross-Reference) — `workflow_run` Trust Boundary & Concurrency Group Collision
- **Location:** `deploy/action/workflows/heimdall-gate.yml:15-17,24-26`
- **Notes:** Including `head_repository.full_name` in the concurrency group properly isolates fork branch names from same-repo branches. However, when `schedule` or `workflow_dispatch` runs, the fallback group `heimdall-gate-<repo>-sweep` collides with any same-repo branch named `sweep`. Use `heimdall-gate-sweep` when `github.event_name != 'workflow_run'`, and apply the SEC-02 fix so `workflow_run` from a PR that modified `heimdall-proof.yml` cannot inject events or checks.

---

## Part 2 — VM Usage & Performance Hardening Audit

### 1. Binary Size Measurements (`go1.27.4`)

Measured directly by building each command with default flags vs. `CGO_ENABLED=0 go build -trimpath -ldflags='-s -w'`:

| Binary | Target | Default Build | Hardened (`CGO_ENABLED=0 -trimpath -ldflags='-s -w'`) | Delta |
|---|---|---:|---:|---:|
| `cmd/heimdall` | `darwin/arm64` | 33,146,226 B (31.61 MiB) | 22,446,578 B (21.40 MiB) | **-10.20 MiB (-32.3%)** |
| `cmd/heimdall-server` | `darwin/arm64` | 38,115,490 B (36.34 MiB) | 26,248,530 B (25.03 MiB) | **-11.32 MiB (-31.1%)** |
| `cmd/heimdall-runner` | `darwin/arm64` | 11,798,386 B (11.25 MiB) | 7,926,642 B (7.55 MiB) | **-3.69 MiB (-32.8%)** |
| `cmd/heimdall` | `linux/amd64` (`CGO_ENABLED=0`) | 35,086,248 B (33.46 MiB) | 24,035,488 B (22.92 MiB) | **-10.54 MiB (-31.5%)** |
| `cmd/heimdall-server` | `linux/amd64` (`CGO_ENABLED=0`) | 39,643,914 B (37.81 MiB) | 27,607,200 B (26.33 MiB) | **-11.48 MiB (-30.4%)** |
| `cmd/heimdall-runner` | `linux/amd64` (`CGO_ENABLED=0`) | 12,403,835 B (11.83 MiB) | 8,392,864 B (8.00 MiB) | **-3.83 MiB (-32.3%)** |

#### Actionable Wins
1. **`deploy/action/action.yml:76`** currently runs `go build -trimpath -buildvcs=false` **without `-ldflags='-s -w'`**, shipping 10.54 MiB of unnecessary DWARF/symbol data on every CI run. Add `-ldflags='-s -w'` immediately.
2. **`deploy/deploy.sh:48-49`** already uses `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w'` (verified clean).
3. **`Makefile:159`** (`heimdall-bin`) uses plain `go build`; acceptable for local dev, while release builds (`deploy/deploy.sh`, `scripts/deploy.sh`) strip symbols.

---

### 2. HTTP Clients & Transport Hardening Audit

| Component | Location | Timeout | Transport / Pooling | Body Drain / Cap | Issues Found |
|---|---|---|---|---|---|
| `heimdall-server` shared client | `cmd/heimdall-server/app.go:294` | **None (`&http.Client{}`)** | `DefaultTransport` (`MaxIdleConnsPerHost: 2`) | Adapter-dependent | **High impact:** No transport tuning (`MaxIdleConnsPerHost: 2` causes TLS churn under 3 drainers + sync + Jira + Switch + Graphiti); no `ResponseHeaderTimeout`. |
| `heimdall-server` HTTP server | `cmd/heimdall-server/main.go:84` | `ReadHeaderTimeout: 5s`; **`IdleTimeout: 0`** | N/A | N/A | **Medium:** `IdleTimeout: 0` leaves idle keep-alive sockets open indefinitely; `MaxHeaderBytes` unset (defaults to 1 MiB). |
| `heimdall` Action client | `cmd/heimdall/action.go:53` | `Timeout: 2m` | `DefaultTransport` (`MaxIdleConnsPerHost: 2`) | Adapter-dependent | **Medium:** Hard `2m` client timeout silently truncates `DownloadArtifact`'s `5m` context timeout (`artifacts.go:130`) and `Tarball` streaming (`client.go:177`). |
| `github.client` | `internal/adapters/github/client.go:154,163,418-420` | `30s` per `callOnce`; `5m` artifact | Uses injected `HTTPClient` | Cap `8 MiB`; **`closeBody` does not drain!** | **High impact:** `closeBody` calls `resp.Body.Close()` without draining on errors/rate-limits/oversize (`client.go:162,217,224`), breaking HTTP/1.1 keep-alive reuse. Also `getCache` stops accepting new entries forever once 256 URLs are cached (`client.go:146-148`). |
| `openrouter.ChatClient` | `internal/adapters/openrouter/chat.go:105,116,141,277` | `2m` ctx + fallback `&http.Client{Timeout: 2m}` | Injected or `DefaultTransport` | Cap `4 MiB`; error body read, oversize not drained | **Low:** Fallback allocates new `http.Client` struct per call; oversize error closes without draining. |
| `openrouter.Client` (Balance) | `internal/adapters/openrouter/balance.go:35,55,62,70` | `10s` ctx + fallback `10s` | Injected or `DefaultTransport` | **No `LimitReader` cap!** Error body not drained | **Medium:** `json.NewDecoder(resp.Body).Decode(&body)` has no response size cap; non-2xx closes without draining. |
| `openrouter.Prober` | `internal/adapters/openrouter/probe.go:50,59,66` | `20s` ctx + fallback `20s` | `cmd/heimdall-server/model_provider.go:40` passes `HTTP: nil`! | Drains `64 KiB` (good!) | **Low:** `proberFor` leaves `HTTP: nil`, instantiating a new `http.Client` on every liveness probe instead of sharing the tuned transport. |
| `openrouter.RoomDigestClient` | `internal/adapters/openrouter/room_digest.go:75,82` | Fallback `30s`; no `ctx` timeout if `HTTP` set | Injected or `DefaultTransport` | Cap `1 MiB`; error body read | **Low:** If `c.HTTP` is injected with `Timeout: 0`, `post` does not wrap `ctx` with a deadline. |
| `auth` OAuth/OIDC client | `internal/transport/auth/provider.go:199-220`, `board_auth.go:81` | `10s` | `DefaultTransport` | Cap `1 MiB`; non-200 / trailing bytes not drained | **Low:** Non-200 `GET /user` and `json.NewDecoder` trailing bytes are not drained before `Close()`. |

#### Recommended Shared Transport (`internal/platform/httpx` or in `cmd/heimdall-server` & `cmd/heimdall`)
Replace bare `&http.Client{}` in `cmd/heimdall-server/app.go:294` and `cmd/heimdall/action.go:53` with a tuned `*http.Transport` (keeping `http.Client.Timeout = 0` where streaming/per-call contexts govern deadlines, and setting `ResponseHeaderTimeout` on non-SSE clients):
```go
tr := &http.Transport{
    DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
    ForceAttemptHTTP2:     true,
    MaxIdleConns:          100,
    MaxIdleConnsPerHost:   16,
    IdleConnTimeout:       90 * time.Second,
    TLSHandshakeTimeout:   10 * time.Second,
    ExpectContinueTimeout: 1 * time.Second,
}
```
And update `closeBody` in `internal/adapters/github/client.go:418-420`:
```go
func closeBody(resp *http.Response) {
    _, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
    _ = resp.Body.Close()
}
```

---

### 3. Goroutine Lifecycles, Ticker Cleanup, Polling Cadence & Hot-Path Allocations

#### Verified Clean Lifecycles
- `internal/platform/clock/doc.go:45-60`: `clock.Wait` creates `time.NewTimer(d)` with `defer timer.Stop()` — no timer leaks.
- `internal/transport/dashboard/dashboard.go:343-346`: `follow` stops both poll and keep-alive tickers via `defer stopPoll()` / `defer stopBeat()` on disconnect.
- `cmd/heimdall-server/loops.go:14-68`: `startLoops` ties every background goroutine to a single child context and `sync.WaitGroup`, draining cleanly on `SIGINT`/`SIGTERM` before `pool.Close()`.

#### Performance & Memory Hotspots

1. **P1 — `stageTree` buffers up to 256 MiB tar archive in RAM on every sandbox run** (`cmd/heimdall/action_sandbox.go:78,156-168`):
   - Every `RunOverlay` call (`run_test` + 3 proof runs per proposal = up to 30+ calls per PR) materializes the entire repository tar stream into a `bytes.Buffer` (up to 256 MiB, with slice growth spikes up to ~512 MiB).
   - **Fix:** Stream the tar archive through `io.Pipe()` directly into `b.sbx.Stage(ctx, pr, 0)` (writing in a goroutine with error propagation via `pw.CloseWithError`), cutting peak RAM from ~256–512 MiB to ~2–4 MiB.

2. **P2 — O(N²) redundant full-conversation JSON marshalling in `agentloop`** (`internal/app/agentloop/context.go:50-75,117-123`, `loop.go:184,293`):
   - Per turn: `fitContext()` calls `json.Marshal(r.request())`, then `trimToBudget` re-marshals the entire conversation after **every single trimmed tool message**, then `overBudget()` (`loop.go:293`) marshals the entire request **again**, and `openrouter.ChatClient.send` (`chat.go:101`) encodes it **again**.
   - **Fix:** Inside `trimToBudget`, subtract the exact byte savings (`len(oldContent) - len(stub)`) incrementally and run `requestBytes()` once at the end; reuse `r.ctxw.sentBytes` in `overBudget()` instead of re-marshalling `r.request()`.

3. **P3 — Unbounded in-memory caches and lock maps in long-running `heimdall-server`** (`internal/app/reconcile/inputs.go:20-49`, `internal/app/reconcile/reconcile.go:64,146-155`, `internal/adapters/github/client.go:140-150`, `internal/adapters/github/token.go:61,152-154`):
   - `reconcile.inputCache.items` keys include `<changeID>/<headSHA>/<approvalCount>` and are **never evicted** on expiry — grows monotonically over server uptime.
   - `Reconciler.perRun` allocates one `*sync.Mutex` per `ChangeID` and never deletes finished changes.
   - `github.client.getCache` freezes permanently at 256 entries (0% hit rate for all newer PRs after the first 256 unique URLs).
   - `github.TokenSource.cache` never prunes expired per-repo writer tokens.
   - **Fix:** Add bounded LRU/FIFO eviction (e.g., cap 1,024 entries + sweep expired on `put`) to `inputCache`, `getCache`, and `TokenSource.cache`; delete `perRun[c]` when `v.Over()` completes or use a fixed sharded mutex array (`[64]sync.Mutex` indexed by FNV-1a hash of `ChangeID`) for O(1) zero-leak per-change serialization.

4. **P4 — `gitlog` fetches full recursive tree (up to 50k entries) and re-uploads full 4 MiB JSONL blobs on every append** (`internal/adapters/gitlog/state.go:89-102`, `logfile.go:141-155`, `publish.go:41,90`):
   - Every `Load`/`Append` (and each CAS retry) calls `GetTree(..., recursive=1)` over the entire state ref; `decodeEvents` appends without preallocation (`bytes.Count(data, []byte{'\n'})`) and constructs a new `json.NewDecoder` per line.
   - **Fix:** Preallocate `out` in `decodeEvents`/`decodeEffects` using newline count; for large repos (>1,000 PRs on `refs/heimdall/state`), switch from recursive root tree fetch to non-recursive lookup of `changes/` and `effects/` (or shard by prefix `changes/<prefix>/<change>.jsonl`) and add a state pruning/compaction cadence for merged PRs older than 90 days.

5. **P5 — High-frequency idle polling in `heimdall-server` and SSE dashboard** (`cmd/heimdall-server/app.go:43`, `loops.go:84-111`, `internal/transport/dashboard/dashboard.go:60,342-372`):
   - `narratorLoop` and `episodeDeliveryLoop` poll Postgres every **2 seconds** even when completely idle (unlike `drainLoop`, which uses a `wake` channel).
   - Every open dashboard SSE tab runs a 2-second ticker doing a full `svc.Detail(ctx, cid)` (loading and folding the entire change event stream in Postgres).
   - **Fix:** Wire `narratorLoop` and `episodeDeliveryLoop` to wake channels with a 15–30s fallback tick; in `dashboard`, add a lightweight `LatestVersion(ctx, cid)` query before full `Detail` load (or broadcast version bumps via an in-process pub/sub hub) and cap concurrent SSE streams per user/server.

6. **P6 — Minor hot-path allocations** (`internal/domain/summary/escape.go:58,85-87`, `internal/app/action/proof_review.go:299,412`):
   - `summary.escape` calls `replacements()` creating a fresh `map[rune]string` per call -> replace with a `switch r` statement.
   - `proof_review.headFile` opens/closes `os.OpenRoot` per call and `searcher.scan` allocates a `string(b)` copy + `strings.Split` slice for up to 1 MiB files -> reuse one `*os.Root` per `reviewer` and scan with `bytes.SplitSeq` / `bytes.Contains`.

---

### 4. VM Resource Footprint Recommendations & Mode Comparison

#### Hardened VM Resource Budget (`deploy/compose.yaml` & `deploy/heimdall-runner.service`)

Neither `deploy/heimdall-runner.service` nor `deploy/compose.yaml` currently sets container/unit memory limits or Go runtime GC tunings (`GOMEMLIMIT`, `GOGC`). Recommended baseline for a **4 vCPU / 8 GiB RAM VM** (minimum viable: **2 vCPU / 4 GiB RAM** with sequential sandboxes):

| Service / Unit | Current Limit | Recommended Limit | Go / Runtime Tuning | Expected Steady-State RSS |
|---|---|---|---|---|
| `heimdall-server` (`compose.yaml`) | None | `mem_limit: 512m`, `cpus: 1.0`, `pids_limit: 256` | `GOMEMLIMIT=400MiB`, `GOGC=100` | 40–90 MiB |
| `heimdall-runner.service` (`systemd`) | None | `MemoryHigh=3G`, `MemoryMax=4G`, `TasksMax=512` | `Environment=GOMEMLIMIT=128MiB` (daemon); Podman jobs capped per-container | 15–30 MiB (daemon) + 0.5–2 GiB/job |
| `postgres` (`compose.yaml`) | None | `mem_limit: 512m`, `cpus: 1.0` | `shared_buffers=128MB`, `max_connections=50` | 80–200 MiB |
| `falkordb` (`compose.yaml`) | None | `mem_limit: 512m`, `cpus: 0.5` | `--maxmemory 384mb` | 60–250 MiB |
| `graphiti` (`compose.yaml`) | None | `mem_limit: 512m`, `cpus: 1.0` | Python/uv standalone | 180–350 MiB |
| `embeddings` (`compose.yaml`) | None (`RAYON_NUM_THREADS=2`) | `mem_limit: 1536m`, `cpus: 2.0` | `RAYON_NUM_THREADS=2` (already set) | 400–900 MiB |
| `switch-forward` (`compose.yaml`) | None | `mem_limit: 32m`, `cpus: 0.1` | `socat` | ~4 MiB |
| **Action mode `heimdall` (`ubuntu-latest`)** | `sandboxLimits`: 4 GiB / 200% CPU / 1024 tasks | Keep `sandboxLimits`; set `GOMEMLIMIT=1GiB` on `heimdall` CLI | Add `-ldflags='-s -w'` + streaming `stageTree` (`io.Pipe`) | 30–80 MiB (host) + sandbox job |

> **Why `GOMEMLIMIT` + `GOGC=100` (not `GOGC=off`):** Setting `GOMEMLIMIT` to ~80% of the container/cgroup `MemoryMax` gives Go's GC a soft ceiling that paces collections aggressively before OOM-kill while keeping `GOGC=100` for predictable CPU/heap balance at low utilization.

#### Action Mode vs. Resident Server Mode vs. Market Competitors

*(Competitor pricing and feature claims cross-referenced with `docs/product/research/01-competitors.md` as of 2026-11-20.)*

| Dimension | Heimdall Action Mode | Heimdall Resident Server Mode | SaaS Reviewers (CodeRabbit, Bugbot, Greptile, Claude Review) | OSS Self-Hosted (PR-Agent, Kodus) |
|---|---|---|---|---|
| **Runtime Footprint** | **Single 22.9 MiB static Go binary** (0 VMs, 0 DBs; runs on ephemeral GitHub Actions runners) | 2 Go binaries (26.3 + 8.0 MiB) + Postgres + FalkorDB + Graphiti + TEI + Podman (~2.5–3.5 GiB baseline on 4–8 GiB VM) | Vendor-hosted SaaS (self-host gated to Enterprise, e.g., CodeRabbit 500+ seats) | Python / Node + Postgres + Redis/MQ containers (~1.5–4 GiB VM) |
| **Idle Infrastructure Cost** | **$0/month** (pay only CI minutes + BYO LLM tokens, hard-capped per PR) | ~$20–$40/month cloud VM + BYO LLM tokens | $24–$90/dev/mo or $0.95–$25/PR | ~$20–$40/month VM + BYO LLM tokens |
| **Proof & Execution Isolation** | Executes real Go repro tests in systemd + nftables + CONNECT-proxy UID sandbox; 2× head red + 1× reverted green | Executes multi-language tests in rootless Podman (`--network=none`, `crun` cgroups) | Mostly read-only/advisory (Greptile Enterprise claims sandbox validation; Claude/Bugbot/Copilot do not execute code to gate) | Comment-only; no isolated proof execution or merge gate |
| **Merge Gate Determinism** | Native `heimdall/fast` + `heimdall/proof` checks, HMAC-signed git-ref log, shadow→enforce path | Full event-sourced gate, Jira/Switch integration, E3 team memory, SSE dashboard with OIDC/GitHub auth | Advisory by default; Claude/Copilot checks never fail natively | DIY gating via CI |
| **Cold-Start Latency** | Currently ~44s `go build` + proof (~3m15s total); **drops to ~2s startup** once prebuilt SHA-verified binaries replace source builds | Instant (resident process) | ~1–4 min (SaaS queue) | ~30s–2m |

---

## Prioritized Remediation Roadmap

1. **Immediate (before next customer install):**
   - Fix **SEC-03** (`escape.go:94-111` `breakWWW` panic) and **SEC-04** (`claim.go:315-319` `flatten` panic) — two 5-line fixes eliminating deterministic DoS panics.
   - Fix **SEC-02** (`serveProof`/`RecordProof`/`rows_publish`/`gate_effects`) so PRs modifying `heimdall-proof.yml` never write forged artifacts/costs or flash a green check.
   - Fix **SEC-05** (`uidsandbox/proxy.go` dial timeout, read deadline, connection cap) and **SEC-06** (wipe `/sbx/tmp`, freeze `/sbx/home` in `PhaseExec`).
   - Add `-ldflags='-s -w'` to `deploy/action/action.yml:76` and split `step: install` before `step: gate` in `deploy/action/workflows/heimdall-gate.yml` (**SEC-10**).
2. **Short-term (next sprint):**
   - Fix **SEC-08** (`headFile` `Lstat` symlink rejection), **SEC-09** (mask `GITHUB_TOKEN` + encoded forms + `heimdall-proof.json`), **SEC-07** (complete IANA non-public IP prefixes), and **SEC-01** (`heimdall action init` workflow pin generator + prebuilt checksum-verified Action binaries).
   - Stream `stageTree` via `io.Pipe()` (**P1**), eliminate O(N²) `agentloop` JSON marshalling (**P2**), drain HTTP bodies in `github.client.closeBody` + cap `openrouter.Balance` + tune `http.Transport` (`MaxIdleConnsPerHost: 16`) (**HTTP audit**).
3. **Production hardening (resident server):**
   - Persist `auth` session revocations in Postgres and enforce `https://` on `RedirectURL`/`OIDC.Issuer` (**SEC-11**).
   - Add bounded eviction to `reconcile.inputCache`, `reconcile.perRun`, `github.getCache`, and `TokenSource.cache` (**P3**).
   - Apply container/systemd `MemoryMax`, `GOMEMLIMIT`, and `IdleTimeout` limits in `deploy/compose.yaml`, `deploy/heimdall-runner.service`, and `cmd/heimdall-server/main.go`.
