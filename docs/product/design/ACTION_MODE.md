# Action mode: design (A1)

Status: design, Nov 21 2026. No product code. Owner: P-A1. Inputs: `STRATEGY.md` draft 2 (section 3, 7, 9),
`ROADMAP.md` Stage A, `CRITIC-deepseek.md` sections 3 to 6, `TECH_SPEC.md` sections 1 to 5, 8, 11, 12,
`HARNESS_DESIGN.md` Part B, and the `doc.go` of each package named below. Every GitHub fact that no run in this repo
has proven is marked `not verified` and goes to the spike S1 (section 8).

**S1 update (Nov 21 2026).** S1 ran; its results are in `ACTION_MODE_SPIKE.md`. Each place this document changed
because of it is marked `[S1]`. The big one: REST `PATCH /git/refs` is **not** a compare-and-swap on
`refs/heimdall/*`. The append now uses the GraphQL `updateRefs` mutation with `beforeOid` (2.2 to 2.4, WP AM8).

## 0. Summary

Action mode runs Heimdall as a stateless step in the customer's GitHub Actions. It has no database, no VM and no
Switch. The customer brings a model key. It posts two checks and one comment on every pull request:

- `heimdall/fast`: deterministic, no model, under 2 minutes p95. This is the check a team makes required.
- `heimdall/proof`: async, model-driven, capped at 15 minutes and a cost budget. It may say `unknown`.
- One proof summary comment, updated in place.

The core decisions, each argued below:

| # | Decision | Section |
|---|---|---|
| D-A1 | The domain is reused. Fold, the gate clauses, tiers, finding states, slop, testplan, present, sarif, verify and rulecard stay. Three domain additions: a tri-state verdict, an `action` policy area, and action events | 1 |
| D-A2 | The event log lives on one git ref, `refs/heimdall/state`, one JSONL file per change, written with compare-and-swap through the GraphQL `updateRefs` mutation (`beforeOid`) `[S1]`, with an HMAC over each chain head | 2 |
| D-A3 | Three workflows. An unprivileged `pull_request` trigger, an unprivileged `pull_request` proof run, and a privileged `workflow_run` gate that never executes PR code. No `pull_request_target` anywhere | 3 |
| D-A4 | A built-in agent loop over an OpenAI-compatible endpoint, not OpenCode or Claude Code headless. PR code runs only as a separate Linux user with no network | 4 |
| D-A5 | `unknown` is a third verdict. It never maps to `success`. In enforce mode it maps to `action_required` | 5 |
| D-A6 | Third-party review comments are candidate findings at E4. Each one earns E1 or E2, is refuted, or is dropped and counted | 6 |
| D-A7 | A per-PR budget, a monthly repo cap read from the log, and a deterministic trivial-PR skip at $0 | 7 |

## 1. What is reused and what changes

### 1.1 Reused unchanged

| Package | What Action mode uses | Why it fits |
|---|---|---|
| `internal/domain/change` | `Event`, the payload registry, `Fold` and `View`, the hash chain (`hash.go`: `Canonical`, `Chain`, `VerifyChain`), `Effect` and its keys | The log is the same log. Only its store changes (section 2) |
| `internal/domain/gate` | `BlockingSet` (E4 never blocks), the approval rule (head SHA, not the author, an approver of the changed domains), the policy-at-merge-base clause, `report.go` | The fast check's approval and policy clauses are these clauses |
| `internal/domain/finding` | Tiers E1 to E4, the states of TECH_SPEC 11.2, `FindingIn.Validate`, `Review.Validate`, `CheckRefutation` and `Refuted` (`refute.go`) | Prove-or-drop is the S3 verify and S4 refute stages of `REVIEW_ARCH.md` |
| `internal/domain/evidence` | The tier contract in its `doc.go` (the tiers themselves live in `finding/vocab.go`) | Unchanged |
| `internal/domain/slop` | `Evaluate` for size, with `require_ticket: false` in Action mode | The size rule feeds the trivial-PR skip (section 7) |
| `internal/domain/testplan` | `CheckPlan`: the six-line plan the model submits before test code | Refuses a weak plan with no model call |
| `internal/domain/verify` | `ParseBytes` (JUnit), `Judge` (red-first), `Stability` (three runs, flake quarantine) | Proof of a red-first test is this rule. A rerun-pass is never a clean pass |
| `internal/domain/sarif` | `Parse`, `New(base, head)` | Analyzer results that are new in the change |
| `internal/domain/present` | `ScopeOf`, `Arrange` | A pre-existing finding never blocks and never becomes a line comment |
| `internal/domain/rulecard` | `Card`, `CanBlock`, `CanReachE3` | Memory rules for changed paths (E3) |
| `internal/domain/policy` | `Validate`, `MatchGlob`, `Equal`, approvers and domains | Policy is read from the merge base, as today |
| `internal/secretscan` | The embedded gitleaks rules and redaction | The secrets clause and the redaction of every posted text |
| `internal/app/reconcile` | `Run` and `Record` | One `Run` per Action invocation. No `Loop`, no `Sweep` goroutine, no kicks |
| `internal/app/gateuse` | `RepoSource.Outside` | It reads through ports, so it works with a token-based GitHub adapter |
| `internal/adapters/policyyaml`, `profileyaml` | Parse `heimdall.yaml` and `.heimdall/profile.yaml` from the merge base | Unchanged |
| `internal/adapters/localrunner` | Tarball extraction rules, `ParseEvents`, process-group kill, cleaned environment | The inner part of the Action sandbox (4.4) |
| `internal/app/budget` | `Guard.Check`, `StopAfterTwiceEstimate` | Cost cap (section 7) |

The dependency rule (R3) holds. Every new rule is a pure package under `internal/domain`. Every new outside system is
an adapter behind a port. `cmd/heimdall` wires them. The domain gains no clock, no I/O and no randomness (R5): the
run's time and ids come through `ports.Clock` and `ports.IDs`, as in the server.

### 1.2 What must change

| Change | Where | Kind |
|---|---|---|
| A tri-state verdict: `pass`, `fail`, `unknown`. A clause is unmet because a fact is false (fail) or because a fact is not known (unknown). Today both are "unmet", which is correct for the merge but loses the reason | `internal/domain/gate/verdict.go` (new). `Decision` keeps `OK` and `Unmet`; `Verdict(Decision)` derives the third state from a new `Clause.Unknown` flag | Additive. `Evaluate` keeps its contract: silence never helps |
| The fast-check clause set. The server's ten clauses assume a ticket, seats and pinned ticket tests. Action mode needs eight other clauses (5.1) | `internal/domain/fastcheck` (new, pure). It calls the exported approval and policy clauses of `gate` | New package |
| The proof outcome: proven, dropped with reason, needs a person, not checked, unknown with reason, cost | `internal/domain/proof` (new, pure) | New package. A3 renders it |
| An `action` area in the policy: mode (shadow or enforce), budgets, review sources, declared inputs, break-glass people | `internal/domain/policy/area_action.go` | Additive. Absent area means shadow mode with defaults |
| Action events: `proof.started`, `proof.reported`, `cost.recorded`, `external.ingested`, `breakglass.recorded`, `breakglass.reviewed`, `state.reset` | `internal/domain/change/events_action.go` | Additive. **`internal/domain/change` is held by the Stage 0 session (ROADMAP Stage 0).** This WP waits for its release or goes through the coordinator |
| The Action row table: `fast`, `proof`, `publish`, `sweep` | `internal/domain/actionflow` (new, pure). It reads `change.View` and returns `change.Action` values | New package. The server rows in `change/rows_*.go` stay as they are |
| A token-based GitHub source. Today `TokenSource` mints App installation tokens only (`internal/adapters/github/token.go`) | `internal/adapters/github/static_token.go` | Additive. The App path stays for enforce mode and the server |
| New GitHub readers: review comments, issue comments, artifacts, code scanning alerts, the Git Data API | `internal/adapters/github/{comments,artifacts,codescan,gitdata}.go` | Additive |
| Chat completions with tool calls. Today the client sends plain chat requests only (`openrouter/room_digest.go`) | `internal/adapters/openrouter/chat.go`, port `internal/ports/model.go` | Additive |
| An event store on a git ref | `internal/adapters/gitlog` (new) implementing `ports.EventStore` and `ports.Outbox` | New adapter |
| A sandbox that isolates by Linux user and network namespace on the runner | `internal/adapters/uidsandbox` (new) implementing `ports.Sandbox` | New adapter |
| The agent loop | `internal/app/agentloop` (new) | New use case |
| The CLI verbs `heimdall action trigger`, `heimdall action proof`, `heimdall action gate`, `heimdall action init` | `cmd/heimdall/action*.go` | Wiring |

What Action mode does not use: Postgres, River, Switch, rooms, seats, the pick row, Graphiti, the MCP transport, the
dashboard, the resident agents, `rootlessrunner`. None of them is deleted. The server keeps them.

## 2. Where state lives with no database

### 2.1 What state exists

Two kinds of fact, as in the server:

1. **Observed facts.** The PR, its head and base, changed files, reviews, comments, check runs, CI results. GitHub
   holds them. Every run reads them again and records what is new (`pr.opened`, `pr.pushed`, `approval.recorded`,
   `ci.reported`), exactly as `intake` and `prsync` do. If the stored log is lost, these facts come back from
   GitHub.
2. **Produced facts.** What Heimdall made: red-first test results, finding tiers, refutations, drops, cost, gate
   evaluations, break-glass records. GitHub does not hold them. A lost produced fact costs money to make again, and a
   forged one is the real threat.

### 2.2 The candidates

| Store | Size | Lifetime | Concurrency | Who can write | Cross-PR reads (monthly cap, digest) |
|---|---|---|---|---|---|
| Check run output | `summary` and `text` at most 65,535 characters each | With the commit | Last write wins, no compare | Any workflow with `checks: write`, including a branch workflow of any writer | One list call per SHA. No repo-wide query |
| Hidden marker in the summary comment | 65,536 characters for the whole body | With the PR | Last write wins | The gate, and any person with write access, who can edit any comment | Search API only |
| Workflow artifacts | Large | 1 to 90 days, then gone | Immutable per upload, no compare | Any workflow run, including fork runs | List by name across the repo |
| A git ref | Blobs through the Git Data API: 10 MiB accepted, larger sizes not measured `[S1]` | Permanent | Compare-and-swap through GraphQL `updateRefs` with `beforeOid`. **Not** REST `PATCH` with `force: false`: that checks fast-forward only on `refs/heads/*` and is a plain set on `refs/heimdall/*` `[S1]` | The gate's token; also any person with write access through `git push` | One tree read |

### 2.3 The pick: one ref, `refs/heimdall/state`

The log is a commit history on `refs/heimdall/state`. Its tree is:

```
changes/pr-<owner>-<repo>-<n>.jsonl      one event per line, versions 1..n, the same Event envelope as Postgres
changes/pr-<owner>-<repo>-<n>.head       {"version": 37, "hash": "<chain head>", "mac": "<hmac>"}
effects/pr-<owner>-<repo>-<n>.jsonl      outbox rows: {"effect": {...}} and {"done": "<key>"}
blobs/sha256/<2>/<2>/<hash>              test files, evidence excerpts (content addressed, as blobfs)
budget/<yyyy-mm>.json                    the month's cost per change, for the cap
```

Why this one:

- **It is the event store.** `ports.EventStore.Append(change, expected, events, effects)` maps to one commit. The
  per-change `expected` version is checked against the `.head` file. The compare-and-swap is the GraphQL
  `updateRefs` mutation with `beforeOid` = the tip the gate read, `afterOid` = the new commit and `force: false`
  `[S1]` (call and raw results in `ACTION_MODE_SPIKE.md` 1.2; verified with `GITHUB_TOKEN` and `contents: write`).
  REST `PATCH /git/refs` with `force: false` is not a compare on this ref: a stale writer got 200 `[S1]`. The same `Fold`, `VerifyChain` and reconciler run on it. No second model of a change exists.
- **It survives.** Artifacts vanish after at most 90 days. Check runs and comments are scattered per SHA and per PR.
- **Cross-PR reads are one tree read.** The monthly cap and the weekly shadow digest need every PR of a month.
- **Normal clones do not fetch it.** The default refspec is `refs/heads/*`. A ref outside `refs/heads` adds nothing
  to a developer's clone. GitHub's REST docs require a ref name that starts with `refs` and has at least two slashes
  ([Git refs API](https://docs.github.com/en/rest/git/refs)). `POST /git/refs` accepts `refs/heimdall/...` from a
  workflow's `GITHUB_TOKEN` (201), and a second create returns 422 `Reference already exists` `[S1]`. A default
  `git clone` fetched no `refs/heimdall/*` `[S1]`. Reads (`GET /git/ref/heimdall/state`, trees, contents with
  `?ref=refs/heimdall/state`) work with a `contents: read` token; GraphQL cannot see the ref, so reads use REST
  `[S1]`. In a public repository the ref is listed without a login `[S1]`, so the log holds only what the public
  summary could show.
- **Pushes by `GITHUB_TOKEN` do not start workflows**, so the writes cause no CI runs
  ([triggering a workflow](https://docs.github.com/en/actions/using-workflows/triggering-a-workflow)). In enforce mode
  the gate writes with the Heimdall App token. A commit with an `on: push` workflow, created on `refs/heimdall/*`
  through the API by a user token, started no run; the same commit on a `heimdall-test` branch did `[S1]`. An App
  token write to `refs/heimdall/*` is `not verified`.

### 2.4 Concurrency

- **Two pushes.** Each push starts a trigger run, and each trigger run starts a gate run. The gate workflow has
  `concurrency: heimdall-gate-<repo>-<head branch>` with `cancel-in-progress: false`. GitHub keeps one running and
  one pending, and drops older pending runs. The pending run always reads GitHub again when it starts.
- **Every run evaluates the current head, not the triggering SHA.** The gate reads the PR's head from the API at the
  start and again before it publishes. If the head moved, it records its facts and exits without posting, and the
  newer run posts.
- **Appends on one ref from many PRs.** A gate run builds the new commit on the tip it read. A refused ref update is
  one of two cases. If the change's own `.head` moved, the store returns `ErrVersionConflict`, and `reconcile.Run`
  loads again and decides again, up to 5 times, as today. If only other files moved, the store rebuilds the same
  commit on the new tip and tries again without a new decision. A gate run touches one change file and the budget
  file, so most conflicts are the second case. `[S1]` A refused `updateRefs` is not typed: GitHub returns "Something
  went wrong ... <request id>". After any such error the store reads the tip over REST: tip equals `afterOid` means
  the append landed and only the reply was lost; any other tip is a conflict, handled as above; the old tip means a
  transient failure, retried with backoff. `FORBIDDEN` (token without `contents: write`) fails closed. Two writers
  with the same `beforeOid` gave exactly one winner in 14 of 14 trials.
- **Proof runs.** `concurrency: heimdall-proof-<pr>` with `cancel-in-progress: true`. A new head cancels the old
  proof run. The binary catches the cancel signal, stops the model, and writes a partial result
  (`unknown: superseded by <sha>`, cost so far). An `if: always()` step uploads it. `[S1]` On cancel the runner sends
  SIGINT, then SIGTERM about 8 s later, then SIGKILL about 2 s after that. The binary must write its partial result
  within a few seconds of SIGINT. The `always()` and `cancelled()` steps then run, but the whole cancel is cut at
  300 s from the request: a job that overruns loses its post steps and its logs. The partial-result step must
  therefore take seconds.

### 2.5 Idempotency

- Job keys name their input, as in `HARNESS_DESIGN.md` B3. `heimdall/fast` is keyed by head and a digest of the
  result. The same input runs once.
- Check runs are found by `external_id`. The summary comment is found by its marker and its author login.
- An observed fact recorded twice folds to the same view (H1). A gate run that crashes after a GitHub write and before
  its append repeats the write once on the next run. Every write is safe to repeat.
- The proof artifact names the head, the base, the policy hash and the model id. The gate ignores an artifact whose
  key is already in the log.

### 2.6 Size limits

| Limit | Value | Source |
|---|---|---|
| One event | 64 KiB, else refused at append | Ours |
| One change file | 4 MiB. Over it, the gate stops proof work for the change and says why | Ours |
| Blob through the Git Data API | 1 MiB and 10 MiB accepted (10 MiB took 57 s); larger sizes not measured. We cap at 64 KiB per test file (`testset.MaxFileBytes`) | Measured `[S1]` |
| Contents API read | 1 MB. We read blobs through the Git Data API instead | `internal/adapters/github/doc.go` traps |
| Check run `summary` and `text` | 65,535 characters each | [Check runs API](https://docs.github.com/en/rest/checks/runs) |
| Comment body | Documented 65,536 characters. `[S1]` The API accepted up to 262,144 bytes, and the 422 message still says 65,536. The renderer budget stays 60,000 with a truncation line | Measured `[S1]`; web UI rendering of a long body not verified |

Estimate: about 100 events of 500 bytes and 20 KiB of test files per PR, so 1,000 PRs add about 70 MB to the ref.
This is an estimate, not a measurement.

### 2.7 Tampering

Any person with write access can push `refs/heimdall/state` with `git push`, edit any comment, and create check runs
from a branch workflow. Forks can do none of this. The defences:

| Channel | Threat | Defence |
|---|---|---|
| `refs/heimdall/state` | A writer forges "proven" or deletes a drop | Each `.head` file holds `HMAC-SHA256(key, chain head hash)`. The key `HEIMDALL_LOG_KEY` is an environment secret of the environment `heimdall`, whose deployment branch rule allows only the default branch. A `workflow_run` job runs on the default branch, so it can use it; a PR workflow evaluates against `refs/pull/<n>/merge` and cannot ([Nov 7 2025 changelog](https://github.blog/changelog/2025-11-07-actions-pull_request_target-and-environment-branch-protections-changes/)). That `workflow_run` jobs pass this rule is `not verified` (S1). On a bad MAC or a broken chain, the gate appends nothing to that log. It starts a new log with `state.reset{reason}`, re-reads every observed fact, marks every produced fact as `unknown: state failed verification`, and says so in the summary. It fails closed |
| Summary comment | A writer edits the marker | The marker is a pointer for display (`change`, `version`, `head`). No decision reads it |
| Proof artifact | A PR changes the proof workflow to upload a fake result | The gate accepts an artifact only from the run that triggered it, and only when: `workflow_run.path` is `.github/workflows/heimdall-proof.yml`; `workflow_run.event` is `pull_request`; `workflow_run.head_sha` is the PR head; the file `.github/workflows/heimdall-proof.yml` at that head has the same blob as at the merge base; the artifact parses under its schema and size limits. Otherwise the proof is `unknown: proof workflow changed in this PR` until a policy owner approves |
| Check runs | A writer's branch workflow posts `heimdall/fast: success` | Shadow mode: the check is advisory, so a forgery changes nothing. Enforce mode: the gate posts as the Heimdall GitHub App, and the ruleset names that App as the expected source of `heimdall/fast`. GitHub then blocks a merge when another source set the check ([Dec 1 2021 changelog](https://github.blog/changelog/2021-12-01-ensure-required-status-checks-provided-by-the-intended-app/)) |
| Commands in comments | Someone posts `/heimdall break-glass` | The author's login must be in the policy at the merge base. An edited command comment is ignored (TECH_SPEC 11.5). `created_at` must equal `updated_at` |
| Third-party bot comments | A writer edits a CodeRabbit comment to inject text | A comment is only a candidate and never evidence. Its text is wrapped as untrusted (`internal/transport/mcp/untrusted.go`). Proof decides |
| Declared inputs (coverage, SARIF artifacts from the customer's CI) | A PR edits the customer's workflow to hide a finding | Each input names its producer workflow in the policy at the merge base. If the PR changes that file, the clause is `unknown` until a policy owner approves |

### 2.8 Migration to the server's Postgres log

The JSONL lines are the `event` rows of TECH_SPEC 4.1: the same envelope, versions and hashes.
`heimdall admin import --git-ref refs/heimdall/state` (WP AM16) reads each change file, checks `VerifyChain` and the
MAC, and inserts the events with their versions and hashes. The server then owns the log. The gate workflow is
removed, and the ruleset's expected source moves from the BYO App to the server's App. Check names do not change, so
the ruleset keeps its required checks. Open effects are not imported; the server's reconciler decides them again.

## 3. Workflows, triggers and permissions

### 3.1 GitHub's guidance that shapes this

- Do not check out or run untrusted PR code in a privileged workflow. Use `pull_request` for untrusted code, and a
  separate `workflow_run` for the privileged part, which treats artifacts as untrusted data
  ([Securely using pull_request_target](https://docs.github.com/en/actions/reference/security/securely-using-pull_request_target),
  [Preventing pwn requests](https://securitylab.github.com/research/github-actions-preventing-pwn-requests)).
- `pull_request_target` is blocked by default in public repositories from December 2 2026, unless an Actions event
  policy allows it ([Nov 2 2026 changelog](https://github.blog/changelog/2026-11-02-pull_request_target-blocked-by-default-december-2)).
  Action mode therefore never uses it.
- `actions/checkout` v7 refuses the fork head in `pull_request_target`, and in `workflow_run` started by a PR event
  ([Jun 18 2026 changelog](https://github.blog/changelog/2026-06-18-safer-pull_request_target-defaults-for-github-actions-checkout)).
  The gate checks out nothing from the PR. It reads PR objects as data through the API.
- Least privilege for `GITHUB_TOKEN`, pinned actions by SHA, no `${{ github.event.* }}` in `run:` scripts
  ([Secure use reference](https://docs.github.com/en/actions/reference/security/secure-use)). The binary reads the
  event from `GITHUB_EVENT_PATH`.
- Fork PRs in `pull_request` get a read-only token and no secrets. This is GitHub's own sandbox for forks.

### 3.2 The three workflows

| Workflow | Trigger | Runs PR code | Secrets | Token permissions | Writes |
|---|---|---|---|---|---|
| `heimdall-trigger.yml` | `pull_request` (`opened`, `synchronize`, `reopened`, `ready_for_review`, `edited`), `pull_request_review` (`submitted`, `dismissed`), `merge_group` | No | None | `contents: read` | One small artifact `heimdall-event` (PR number, head SHA, event name). It exists because `workflow_run.pull_requests` is empty for fork runs (`not verified`, S1) |
| `heimdall-proof.yml` | `pull_request` (`opened`, `synchronize`, `reopened`, `ready_for_review`) | Yes, only as the sandbox user (4.4) | `HEIMDALL_MODEL_KEY` on the proof step only, same-repo PRs only (forks get none by GitHub's rule) | `contents: read`, `pull-requests: read` | The artifact `heimdall-proof` |
| `heimdall-gate.yml` | `workflow_run` of the two above and of the customer's declared input workflows (`completed`); `issue_comment` (`created`) for commands; `schedule` every 30 minutes for the sweep | Never | `HEIMDALL_LOG_KEY` and, in enforce mode, the App key, from environment `heimdall` | Shadow: `contents: write` (state ref only), `checks: write`, `pull-requests: write`, `actions: read`, `security-events: read`. Enforce: the same set as an App installation token, and `GITHUB_TOKEN` drops to `actions: read` | Check runs, the summary comment, the state ref |

Rules that every workflow follows:

1. Every `uses:` is pinned by full SHA. The composite action downloads the binary for a pinned version and checks its
   SHA-256 against a value in `action.yml`.
2. `actions/checkout` runs with `persist-credentials: false`. The gate does not check out the PR at all.
3. The model key is set as `env` on one step, never on the job.
4. The proof job has exactly the steps the composite action defines. The customer adds no steps to it. Dependency
   install runs inside the sandbox user (4.4), never as the runner user, because any PR-controlled process as the
   runner user can read the key from `/proc`.
5. The gate caches only data derived from the merge base (policy, cards, base analyzer results keyed by base SHA). It
   runs in the default branch's cache scope, so it never caches anything derived from PR content.

### 3.3 The required check

- Shadow mode: `heimdall/fast` posts `neutral` always, with "would block because ..." in its summary. A team may
  leave it unrequired.
- Enforce mode: a ruleset on the default branch requires `heimdall/fast` with the Heimdall App as expected source.
  `heimdall/proof` stays advisory until the team asks for it to be required.
- A check from the gate appears 30 to 90 seconds after a push (trigger run, then gate run). Until then GitHub shows
  "Expected: waiting for status", which blocks a merge. That is fail-closed by design. `[S1]` One stage measured: a
  push to a visible check run from `GITHUB_TOKEN` took p50 6.1 s, p95 8.8 s (n=10). The two-stage chain is blocked
  (it needs a `workflow_run` workflow on the default branch), so 30 to 90 seconds is neither confirmed nor refuted.
  The one-stage figure is a floor.

### 3.4 Forks, Dependabot and agent PRs

| Author | Fast check | Proof run |
|---|---|---|
| Same-repo branch, person or agent | Full | Full |
| Fork | Full: the gate is privileged and reads the PR as data | Model parts are `unknown: fork PR, no model key by design`. Model-free parts run: E2 re-runs of third-party findings in the sandbox, flake reruns |
| Dependabot | Full | As a fork, because Dependabot runs get only Dependabot secrets, unless the owner adds the model key there (open question Q6) |
| First-time contributor whose runs need approval | No trigger run, so no check: the merge stays blocked | No run |

### 3.5 Exposure that remains, said plainly

The model key is a repository secret. Any person who can push a branch to the repository can read it with a workflow
of their own. This is true of every repository secret on GitHub. The key must therefore be a dedicated key with a
spend limit at the provider. An agent that can edit workflow files can also read it; GitHub requires the `workflows`
permission for that, and whether each coding agent holds it is `not verified`. The gate's MAC key and App key are
safe from writers because only default-branch jobs reach environment `heimdall`.

We considered running the proof in the privileged `workflow_run` with the key in environment `heimdall`. We refused
it. It runs PR code in a privileged job against GitHub's guidance. Dependencies the PR controls would write into the
default branch's cache scope. A sandbox escape would expose a write token, not only the model key.

## 4. The agent loop in CI

### 4.1 Options

| | Built-in loop (`internal/app/agentloop`) | OpenCode headless | Claude Code headless |
|---|---|---|---|
| Install time in the job | 0 s: it is the binary | Node and an npm install, about 30 to 60 s (`not verified`) | Node and an npm install, similar |
| Endpoints | Any OpenAI-compatible endpoint (P1.7) | Many | Anthropic API and its cloud partners only, so no BYO DeepSeek or local model |
| Tools | Only Heimdall's: `read_code`, `search_code`, `find_symbol`, `run_test`, `run_check`, `submit_plan`, `submit_tests`, `submit_finding_proof`, `submit_refutation` | Its own shell, edit and file tools, plus ours over MCP | Its own, plus ours over MCP |
| Isolation | Every tool that touches PR code runs in the sandbox user. The loop holds the key and runs nothing else | Its shell tool runs as the user that holds the key. The whole agent would need the sandbox, with network to the model | Same as OpenCode |
| Token control | A budget check before every call; output capped per call (the D6 lesson: bounded, structured output) | Coarse | Coarse |
| Prompt size | Our system prompt and tool schemas, about 2 to 4k tokens (`not verified`) | Its harness prompt adds about 10k tokens per turn (`not verified`) | Similar |
| Output | Validated by the domain (`testplan.CheckPlan`, `finding.FindingIn.Validate`), refused with field paths, one retry | Parsed from text | Parsed from text |
| Cost to build | About 600 lines plus tests, and we own it | Glue only | Glue only |

### 4.2 Recommendation: the built-in loop

The loop is small: send messages and tool schemas, receive tool calls, run each through a fixed dispatch table,
append results, stop on a submit tool, a turn cap, the token budget or the deadline. It wins on the two things Action
mode cannot give up: the key never shares a user with PR code, and the budget is checked per call. OpenCode stays the
runtime of the server's resident and job agents (C1) until a bench says otherwise. The same `agentloop` can become a
second `ports.AgentRuntime` for C1 later; that is not part of A1.

The loop reuses: the kind table of `internal/domain/workflow/kinds.go` (inputs, output schema, skill file), the input
renderers of `internal/app/taskinput`, the skills under `skills/`, and `wrapUntrusted` for every PR text. Prompt
layout puts the stable prefix first (skill, tool schemas, policy excerpt, repo map at base) so provider prefix caching
applies.

### 4.3 What the proof run asks of the model

1. **Intent.** From the PR title and body and a linked GitHub issue (`Fixes #n`), the model writes a test plan
   (`TestPlan@1`). `CheckPlan` refuses a weak plan. No plan possible: `unknown: no intent`.
2. **Red-first tests.** The model writes tests for the plan. The sandbox runs them on base (each must fail with
   `<failure>`, not `<error>`) and on head three times (each must pass). `verify.Judge` and `verify.Stability`
   decide. A test that passes on base proves nothing and is dropped.
3. **Findings.** For each candidate (ours from a review pass, and third-party ones, section 6), the model tries to
   prove it: an E1 repro test that fails on head, or an E2 profile check whose new result hits the location. A
   second call tries to refute what stays unproven (S4 of `REVIEW_ARCH.md`). Everything else is dropped and counted.

### 4.4 Where tests run, and the isolation without rootless podman

Tests run on the runner, with the runner's toolchain, set up by pinned `actions/setup-*` steps that read version files
as data. The profile at the merge base (`.heimdall/profile.yaml`) gives the commands.

The sandbox adapter `internal/adapters/uidsandbox` needs `sudo`, which GitHub-hosted Ubuntu runners have:

1. At job start, as root: create user `heimdall-sbx` with no sudo rights and no docker group. Set the runner's home
   and `_temp` to mode 0700.
2. Copy the PR tree (fetched at the exact head SHA, no `.git` credentials) to `/sbx/work`, owned by `heimdall-sbx`.
3. **Fetch phase.** Dependency install runs as `heimdall-sbx` with egress only through a CONNECT proxy in the binary.
   The proxy allows the registries the profile names (the server's fetch-job rule, TECH_SPEC 12.2). An `nftables`
   rule drops every other packet from that uid.
4. **Execution phase.** Tests run as `heimdall-sbx` in a network namespace with no interface but loopback, under
   `systemd-run --scope` with `NoNewPrivileges`, CPU and memory limits and a deadline, with an empty environment.
5. After each job: kill every process of the uid, and parse JUnit with `verify.ParseBytes` limits.

Why this is enough here: the key lives in the loop's memory and in its environment, which another uid cannot read;
the uid has no network in the execution phase, so it has no way out even for what it can read; the runner VM is
thrown away after the job. It is `not verified` until the escape probes of WP AM11 pass on a hosted runner: read the
loop's `/proc/<pid>/environ`, read the runner's home and `_temp`, reach the network, reach the docker socket, keep a
process alive after the job.

A self-hosted runner without `sudo` cannot isolate. The proof run then reports `unknown: runner cannot isolate PR
code` for every part that runs PR code. There is no silent fallback to running as the runner user. A docker backend
for such runners is open question Q5.

## 5. Fast check and proof run

### 5.1 The fast check (`heimdall/fast`)

No model. No PR code runs. Budget 120 s p95 from push to posted check.

| Clause | Facts | Source | Budget |
|---|---|---|---|
| `secrets` | New secrets in added lines | `secretscan` over the diff, read as blobs | 5 s |
| `analyzers` | New SARIF results at or above the policy severity | The customer's analyzers: code scanning alerts for the head (`security-events: read`), or SARIF artifacts from a declared workflow. `sarif.New(base, head)` keeps what is new | 10 s |
| `pinned_tests` | No test file that existed at the merge base under `action.pinned` globs is deleted, renamed or edited, and no skip marker is added to one, unless a policy owner approved on head | Git trees at base and head | 5 s |
| `test_integrity` | The static rules of A6: a test that mocks a file the PR changes, a weakened assertion | Diff, pure rules | 5 s |
| `diff_coverage` | Changed lines covered at or above `action.diff_coverage.min` | The coverage artifact of the declared test workflow on the head (lcov, Cobertura, Go cover profile). The gate runs again when that workflow completes. Not configured: the clause is "not checked", not unknown | 10 s |
| `approval` | An approval on the head SHA by a non-author who approves the changed domains | `gate` approval rule, reviews API, policy at base | 2 s |
| `memory_rules` | Every active, checkable rule card that applies to a changed path holds | `rulecard`, cards at the merge base under `.heimdall/memory/` | 5 s |
| `heimdall_files` | The PR does not change Heimdall's workflows, the action pin, `heimdall.yaml`, `.heimdall/` or a declared input's workflow, unless a policy owner approved on head | Changed files, policy at base | 1 s |

Overhead in the budget: trigger run start 30 s, gate run start 30 s, binary restore 5 s, blob fetch 15 s, writes 5 s.
These are estimates. A2 measures p95 on the bench.

Merge queue: the trigger also runs on `merge_group`. The gate evaluates the group's commit: `secrets`, `analyzers`,
`pinned_tests` and `heimdall_files` on the combined diff, and every PR in the group must have `heimdall/fast` success
or a recorded break-glass on its own head.

### 5.2 The proof run (`heimdall/proof`)

Budget 15 minutes wall time and the cost cap (section 7). Advisory in v1.

| Step | Time budget | Unknown when |
|---|---|---|
| Sandbox setup and dependency fetch | 3 min | The fetch fails or times out |
| Trivial-PR skip decision | 1 s | Never: it decides deterministically |
| Intent and test plan | 1 min | No intent, plan refused twice |
| Red-first tests | 5 min | The budget or time runs out before a verdict |
| Prove or refute each candidate, most severe first | 5 min | Each candidate not reached is "not checked: budget" |
| Upload | 1 min | The upload fails: the gate's sweep turns the run into `unknown: no result` |

### 5.3 Caching

| Cache | Key | Scope and writer |
|---|---|---|
| Heimdall binary | `heimdall-bin-<version>-<os>-<arch>` | Any job; checksum verified on restore too |
| Dependencies | `heimdall-deps-<os>-<profile hash>-<hashFiles(lockfiles)>`, restore keys by prefix | Proof job, PR scope. Written by the sandbox user, so PR-controlled. It never reaches the default branch scope |
| Base test runs | `heimdall-base-<base sha>-<profile hash>` | Proof job, PR scope. Saves the base half of red-first |
| Base analyzer results | `heimdall-sarif-<base sha>` | Gate, default branch scope. Base-derived only |
| Proof results | Not a cache: the log. Same head, base, policy and model means no rerun | State ref |

### 5.4 The `unknown` verdict and how each check treats it

`unknown` means "a fact we need is not known", never "probably fine". Its reason is always named: budget hit,
timeout, superseded, fork without key, runner cannot isolate, input missing, workflow changed, state failed
verification.

| Mode | Check | pass | fail | unknown |
|---|---|---|---|---|
| Shadow | `heimdall/fast` | `neutral` "would pass" | `neutral` "would block because ..." | `neutral` "would block: unknown ..." |
| Shadow | `heimdall/proof` | `neutral` | `neutral` | `neutral` |
| Enforce | `heimdall/fast` | `success` | `failure` | `action_required`, which blocks a merge |
| Enforce | `heimdall/proof` (advisory) | `success` | `neutral` with the blocking findings | `neutral` with "unknown: reason" |
| Enforce, proof required by the team | `heimdall/proof` | `success` | `failure` | `action_required` |

Only a person clears `unknown`: a rerun that ends `pass`, or a break-glass (A8): `/heimdall break-glass <reason of
15 or more characters>` from a login in `action.break_glass` at the merge base. The gate records
`breakglass.recorded`, posts `success` with "bypassed by @login: reason", and the summary shows it as open until
another approver posts `/heimdall break-glass-reviewed`.

The sweep (`schedule`, every 30 minutes) turns a proof run that has not reported 20 minutes after its head into
`unknown: no result`. Scheduled runs can be late under load; the sweep is a backstop, not the timer of record.

## 6. Prove-or-drop for third-party comments

### 6.1 Reading them

The gate reads, for the PR, through `GITHUB_TOKEN` or the App token:

- review comments (`GET /repos/{o}/{r}/pulls/{n}/comments`): `path`, `line`, `start_line`, `side`, `commit_id`,
  `original_line`, `user.login`, `user.type`, `body`, `in_reply_to_id`, `created_at`, `updated_at`;
- review bodies (`GET /pulls/{n}/reviews`);
- issue comments on the PR (`GET /issues/{n}/comments`), where some tools post a summary.

Only authors in `action.sources` at the merge base count, and only with `user.type == "Bot"`. The default list is
CodeRabbit, GitHub Copilot code review, Cursor Bugbot and Greptile. Their exact logins and body formats are
`not verified`; A5 records real comments from each and fixes them in testdata.

### 6.2 Turning a comment into a candidate

Per source, a small pure parser (`internal/domain/external`) does the work:

1. Strip HTML comments, collapsed `<details>` blocks, suggestion blocks and badges. Keep at most 600 characters of
   claim (the `FindingIn` limit).
2. Read a severity when the source writes one in a known pattern. Otherwise severity is `medium`.
3. Location. An inline comment gives `path` and lines on `commit_id`. If `commit_id` is not the head, the lines are
   mapped through the diff from that commit to head. A comment whose lines no longer exist is dropped as
   `outdated`. A summary comment with no file and line is dropped as `no location`, and counted.
4. Category `style` is "not checked: not provable by a test or tool", and counted apart from drops.
5. Duplicates merge with ours by the existing rule: same file, overlapping lines, claim similarity of 0.6 or more
   (TECH_SPEC 11.2b). The merged finding keeps every reporter.

Each candidate is recorded as `external.ingested{source, comment_id, location, claim, severity}` and enters the log
as a finding at E4 with the bot as reporter. Comment ids make ingest idempotent.

### 6.3 Proving or dropping

In the proof run, most severe first, within budget:

| Outcome | Requires | Shown as |
|---|---|---|
| Proven E1 | A repro test that fails on head with `<failure>` and that `verify.Judge` accepts | "proven" with the test and the one-line failure |
| Proven E2 | A profile check whose new result is inside the finding's file and lines | "proven" with the tool line |
| Refuted | A refutation that passes `finding.CheckRefutation`: a location at head that is not the finding's own lines | "dropped: refuted", with the citation |
| Dropped | No proof within the candidate's budget | "dropped: no proof" |
| Not checked | Budget, time, or a fork without a key | "not checked: reason" |

A proven third-party finding enters the blocking set under the normal rule (`gate.BlockingSet`, policy
`blocking.min_severity` and `min_tier`). The summary prints the counts, for example "40 in, 4 proven, 30 dropped (12
refuted, 15 no proof, 3 outdated), 6 not checked". Heimdall does not reply in the bots' threads in v1 (Q7).

## 7. Cost cap and token budget

- **Trivial-PR skip, $0.** A deterministic rule in `internal/domain/proof`: every changed path matches
  `action.trivial` globs (defaults: `**/*.md`, `docs/**`, lockfiles, generated files), or the size under
  `slop.Evaluate` is at most `action.trivial_lines` lines outside tests. The summary says "skipped: trivial (rule)".
  No model call is made. Third-party comments on a trivial PR are listed as "not checked: trivial PR".
- **Per-PR budget.** `action.budget.per_pr_usd` (proposed default $0.50) and a token cap
  (`action.budget.per_pr_tokens`, proposed 800k input, 40k output). Each call has an output cap (proposed 4k). The
  loop checks the budget before each call, with `budget.Guard.Check`, and stops at the cap. Unfinished items become
  `unknown: budget hit`.
- **Monthly repo cap.** `action.budget.monthly_usd` (proposed default $50). The proof job reads `budget/<yyyy-mm>.json`
  from the state ref (read-only) before its first call, and skips with `unknown: monthly cap reached` when the month's
  spend plus the per-PR budget is over the cap. The gate adds each run's cost when it publishes. The proof job cannot
  reserve atomically, so the cap can be passed by at most (concurrent proof runs × per-PR budget). The provider's
  spend limit on the key is the hard stop.
- **Cost measurement.** From the provider's `usage` on each response (OpenRouter returns cost; other endpoints use a
  price table in the policy). Each run records `cost.recorded{model, prompt_tokens, completion_tokens, usd}`. The
  summary prints the cost of every PR, including $0.
- **Expected cost** (to be measured by A0 and A10): a median under $0.30 per reviewed PR (STRATEGY section 7), with
  the flash model for planning and refutation. One partial run cost about $0.24; that number is `not verified`.

## 8. Implementation plan

Each WP is under 2 days, one concern, and names its files and its test. The test is first only where R1 asks for it
(state, gates, parsing, money, security). Roadmap WPs A2 to A10 map onto these.

| WP | What | Files | Test | Needs | Roadmap |
|---|---|---|---|---|---|
| S1 | **Spike: GitHub facts. Done Nov 21 2026 `[S1]`: see `ACTION_MODE_SPIKE.md`; the `workflow_run` and fork items are blocked there.** Custom ref by API with `GITHUB_TOKEN`; environment secret in `workflow_run` with a default-branch rule; App as expected check source; `workflow_run.pull_requests` for forks; cancel grace time; comment and blob size limits; trigger-to-check latency (20 pushes) | `docs/product/design/ACTION_MODE_SPIKE.md`, throwaway workflows on `aarush323/heimdall-fixture-pay` | The recorded results; this doc changed where a fact differs | Owner's yes for live GitHub writes | A1 |
| AM1 | Tri-state verdict | `internal/domain/gate/verdict.go` | `verdict_test.go`: every clause known-false is fail, every clause unknown is unknown, a mix is fail, nothing unmet is pass | – | A2 |
| AM2 | `action` policy area | `internal/domain/policy/area_action.go`, validate rules | `area_action_test.go`: goldens, defaults, refusals with key paths | – | A2, A4, A8, A10 |
| AM3 | Action events | `internal/domain/change/events_action.go` | Payload round trip and validation table; fold tests for each | Stage 0 releases `change` | A3, A8, A10 |
| AM4 | Fast-check clauses | `internal/domain/fastcheck/` | Table test: all-green facts, one flip per clause, exactly that clause unmet | AM1, AM2 | A2 |
| AM5 | Proof outcome and trivial skip | `internal/domain/proof/` | Table test for each outcome and reason; trivial-skip table; $0 on docs-only | AM2 | A3, A10 |
| AM6 | Coverage parsers | `internal/domain/coverage/` (lcov, Cobertura, Go cover) | Goldens per format, diff-coverage arithmetic, `FuzzParse` | – | A2 |
| AM7 | Third-party comment parsers | `internal/domain/external/` | Goldens from recorded comments of each tool, location mapping through a diff, outdated and no-location drops | AM5 | A5 |
| AM8 | Git-ref event store `[S1]` | `internal/adapters/gitlog/` | Contract test of `ports.EventStore` against an `httptest` fake of the Git Data REST reads and the GraphQL `updateRefs` mutation (a `beforeOid` mismatch returns the opaque error; the store re-reads the tip to tell a lost reply, a conflict and a transient failure apart): append, conflict on own change, rebuild on another change, MAC refused, chain broken refused, size limits | – | A1 |
| AM9 | GitHub adapter additions | `internal/adapters/github/{static_token,comments,artifacts,codescan,gitdata}.go` | Recorded-response tests; pagination; error mapping per `doc.go` traps | – | A2, A5 |
| AM10 | Model chat with tools | `internal/ports/model.go`, `internal/adapters/openrouter/chat.go` | Recorded-response tests: tool calls, usage and cost, refusal, 429 | – | A7 |
| AM11 | Isolated sandbox | `internal/adapters/uidsandbox/` | Escape probes (env, `/proc`, home, `_temp`, network, docker socket, a surviving process) must all fail. Build-tagged; runs only on a hosted Linux runner in CI | – | A7 |
| AM12 | Agent loop | `internal/app/agentloop/` | Scripted fake model: tool dispatch, refused submission and one retry, budget stop, deadline stop, turn cap | AM10 | A7 |
| AM13 | Action rows | `internal/domain/actionflow/rows.go` | Row table tests in the style of `change/rows_test.go` | AM3, AM4, AM5 | A2 |
| AM14 | Gate and proof use cases | `internal/app/action/{gate,proof,ingest}.go` | Component test: fake GitHub, `gitlog` on `httptest`, fake sandbox and model; a PR goes from push to fast check to proof to summary; a forged artifact and a bad MAC fail closed | AM8, AM9, AM12, AM13, AM7 | A2, A5 |
| AM15 | CLI wiring | `cmd/heimdall/action.go`, `action_init.go` (creates environment, secrets, App by manifest via the user's own `gh`) | None for wiring (R1). `make fast` | AM14 | A7 |
| AM16 | Composite action and workflows | `deploy/action/action.yml`, `deploy/action/workflows/heimdall-{trigger,proof,gate}.yml`, `actionlint` and `zizmor` in `make fast` | e2e on `aarush323/heimdall-fixture-pay`: install to first summary under 10 minutes, timed | AM15, S1 | A7 |
| AM17 | Import to the server | `cmd/heimdall/admin_import_gitref.go` | Component test: import a recorded ref into Postgres; `VerifyChain` passes; the server's fold equals the Action fold | AM8 | later |

A3 (the summary renderer), A4 (the shadow check text and weekly digest), A6 (test integrity rules) and A8
(break-glass) keep their own roadmap rows and build on AM5, AM4 and AM3.

**Build order and parallel lanes:**

1. S1 first, alone. It can change section 2 or 3.
2. Lane A (pure, in parallel): AM1, AM2, AM6. Then AM4, AM5. Then AM7.
3. Lane B (adapters, in parallel with lane A): AM8, AM9, AM10, AM11.
4. AM3 joins when Stage 0 releases `internal/domain/change`.
5. Then AM12 (needs AM10), AM13 (needs AM3, AM4, AM5).
6. Then AM14, AM15, AM16, in that order. AM17 can wait for Stage C.

## 9. Open questions for the owner

Owner decision, Nov 21 2026: take every recommendation below. Q3 stays open until S1 reports, because S1 found that
REST `force:false` does not refuse a non-fast-forward update on `refs/heimdall/*`.

| # | Question | My recommendation |
|---|---|---|
| Q1 | Enforce mode needs a GitHub App identity so a writer cannot forge `heimdall/fast`. Do we ask each customer to create a BYO App by manifest (`heimdall action init`), or wait for a hosted Heimdall App? | BYO App by manifest now. Shadow mode needs no App |
| Q2 | `HEIMDALL_LOG_KEY` in environment `heimdall` adds one install step. Required even in shadow mode? | Yes. `heimdall action init` sets it, so the cost is one command |
| Q3 | Custom ref `refs/heimdall/state` or branch `heimdall/state`? | Closed `[S1]`: the ref. `POST /git/refs` accepts it, and `updateRefs` with `beforeOid` is a real compare-and-swap on it |
| Q4 | Red-first tests: attach to the summary as a patch for the author to adopt, or commit them to the PR branch? | Attach in v1. A `/heimdall adopt-tests` command for same-repo PRs later |
| Q5 | Self-hosted runners without `sudo`: proof parts are `unknown`. Build a docker backend (`--network none`) for them? | After the first design partner on self-hosted runners asks |
| Q6 | Fork and Dependabot PRs get no model proof. Add a maintainer command `/heimdall prove <sha>` that runs a privileged proof for one exact SHA? | No in v1. It reopens the privileged-execution question of 3.5 |
| Q7 | Reply in each bot thread ("proven", "dropped: refuted") or only in the summary? | Summary only. Thread replies add the noise we sell against |
| Q8 | Intent source: GitHub issues only, or Jira with a BYO token too? | GitHub issues and the PR body in v1 |
| Q9 | Default budgets: $0.50 per PR, $50 per repo per month, 4k output per call? | Yes, until A0 and A10 give real numbers |
| Q10 | A PR that changes the customer's CI workflow which produces coverage or SARIF makes that clause `unknown` until a policy owner approves. Too strict for teams that edit CI often? | Keep it. It is the same rule as policy at the merge base |

## 10. Not verified

Every item marked `not verified` above. The main ones: environment-scoped values in `workflow_run` under a
default-branch rule; `workflow_run.pull_requests` for forks; the latency of the two-stage trigger-and-gate chain; an
App token write to `refs/heimdall/*`; blobs above 10 MiB; how the web UI renders a long comment; the bot logins and
formats; the escape probes of the sandbox; install and prompt sizes of OpenCode and Claude Code; all costs. `[S1]`
settled (see `ACTION_MODE_SPIKE.md`): custom ref creation by API, the compare-and-swap call, one-stage check latency,
the runner's cancel grace, and the size limits of comments and blobs.
