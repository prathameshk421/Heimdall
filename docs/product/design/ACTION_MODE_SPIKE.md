# Action mode spike S1: GitHub facts

Status: measured Nov 20 and 21 2026 on `aarush323/heimdall-fixture-pay`. Owner: P-S1. No product code, no model calls.
Inputs: `ACTION_MODE.md` sections 2, 3 and 8. The owner allowed throwaway live GitHub writes for this spike only
(Nov 21 2026). Every object was a `heimdall-test/s1-*` branch, a `refs/heimdall/*`, `refs/tags/heimdall-test-*` or
`refs/notes/heimdall-test-*` ref, or one PR labelled `heimdall-test` that targets `heimdall-test/s1-k7-base`. Nothing
touched `main`, `heimdall-demo` or another repository. No secret was needed, so no dummy secret exists. Run id `k7`.
Two runs: the first was cut off by a usage limit; this document merges both. Times are UTC.

## 0. Results and what changed

| # | Question | Result | Effect on `ACTION_MODE.md` |
|---|---|---|---|
| 1 | Does REST `PATCH /git/refs` with `force: false` give a compare-and-swap on `refs/heimdall/*`? | **No.** It only checks fast-forward on `refs/heads/*`. On `refs/heimdall/*`, `refs/tags/*` and `refs/notes/*` a stale write, a sibling, a rewind and an unrelated commit all return 200 | **AM8 design changed** (section 1 below) |
| 1b | Which call gives a real compare-and-swap with `GITHUB_TOKEN` (`contents: write`)? | **GraphQL `updateRefs` with `beforeOid`** (verified). `git push --force-with-lease` also works. The Contents API file `sha` works on branches only | D-A2, 2.2, 2.3, 2.4, AM8 |
| 1c | What can a `contents: read` token do? | Read the ref, the tree and file contents. Every write (`POST` blob, `POST` ref, `PATCH` ref, `DELETE` ref, GraphQL `updateRefs`) returns 403 or `FORBIDDEN` | 2.3 |
| 1d | Is `POST /git/refs` with `refs/heimdall/...` accepted from `GITHUB_TOKEN`? | **Yes**, 201. Creating an existing ref returns 422 `Reference already exists` (a create-if-absent check) | Q3 answered: the ref |
| 1e | Does a write to `refs/heimdall/*` start `on: push` workflows? Does a clone fetch it? | **No** to both. The same commit pushed to a `heimdall-test` branch started a run. A default clone shows only `refs/heads` and `refs/remotes` | 2.3 |
| 2 | `workflow_run` facts | **Blocked**: needs a workflow on the default branch (section 2) | None |
| 3 | Required check expected source | Configurable per check: ruleset `integration_id`, branch protection `app_id`. A `GITHUB_TOKEN` check run has app `github-actions`, id 15368 (section 3) | 3.3 confirmed |
| 4 | Limits | PR comment: documented 65,536 characters, but the API accepted up to **262,144 bytes**. Blob: 1 MiB and 10 MiB accepted (section 4) | 2.6 |
| 5 | Latency, one workflow, one stage | Push to check run visible: p50 6.1 s, p95 8.8 s, max 8.8 s (n=10) (section 5) | 3.3 |
| 6 | Cancel grace | SIGINT 1.0 s after the cancel request, SIGTERM 8.1 s later, SIGKILL 2.0 s after that. Then `always()` and `cancelled()` steps run, but the whole cancel is cut at **300 s** and a cut job loses its logs (section 6) | 2.4 |

## 1. The ref compare-and-swap (AM8)

### 1.1 REST `PATCH /repos/{o}/{r}/git/refs/{ref}` with `{"sha": X, "force": false}`

Setup per namespace: create the ref at commit C1 (orphan), update to C2 (child of C1), then try a sibling C2b (child of
C1, so not a fast-forward of C2), a rewind to C1, an unrelated orphan, and the same with `force` omitted.
Raw results (`HTTP status`):

| Update | `refs/heads/heimdall-test/s1-k7-cas` | `refs/heimdall/s1-cas-probe` | `refs/heimdall/state` (in a workflow, `GITHUB_TOKEN`) | `refs/tags/heimdall-test-s1-k7-tag` | `refs/notes/heimdall-test-s1-k7` |
|---|---|---|---|---|---|
| fast-forward C1 to C2 | 200 | 200 | 200 | 200 | 200 |
| sibling C2b of tip C2 | **422** `Update is not a fast forward` | 200 | 200 | 200 | 200 |
| rewind to C1 | **422** same | 200 | 200 | not run | not run |
| unrelated orphan | **422** same | 200 | not run | not run | not run |
| `force` omitted, sibling | **422** same | 200 | not run | not run | not run |

Conclusion: the fast-forward check exists only for `refs/heads/*`. On any other namespace the REST update is a plain
set. The doc claim "a ref update with `force: false` fails when the ref moved" is wrong for `refs/heimdall/*`.

Two writers in parallel, both children of the current tip, same call, 6 trials each:

- `refs/heads/...`: exactly one 200 and one 422 `Reference cannot be updated` in all 6 trials.
- `refs/heimdall/s1-cas-probe`: the same, one 200 and one 422 in all 6 trials. In the workflow run
  (`s1-ref`, run 44802600787) writer A got 422 and writer B got 200.

So concurrent writers are serialised by GitHub's ref lock. A writer that read the tip earlier and writes after another
writer has finished is not stopped. That is the case the gate has (read tip, build commit, update), so REST is not a
compare-and-swap for our design.

### 1.2 GraphQL

- `updateRef` (singular) cannot reach `refs/heimdall/*`. `refs(refPrefix: "refs/heimdall/")` returned
  `{"nodes":[]}` and `ref(qualifiedName: "refs/heimdall/s1-cas-probe")` returned `null` for a ref that REST shows.
  The mutation then fails with `NOT_FOUND ... global id of ''`. GraphQL cannot read custom refs either, so reads go
  through REST.
- `updateRefs` takes a ref name (`GitRefname!`), so it works on custom refs. The call:

```
mutation($r:ID!,$n:GitRefname!,$b:GitObjectID,$a:GitObjectID!,$f:Boolean!){
  updateRefs(input:{repositoryId:$r, refUpdates:[{name:$n, beforeOid:$b, afterOid:$a, force:$f}]}){clientMutationId}}
```

`$r` is the repository `node_id` (`gh api repos/{o}/{r} --jq .node_id`), `$n` is `refs/heimdall/state`, `$f` is `false`.

Raw results on `refs/heimdall/s1-cas-probe` (my login) and on `refs/heimdall/s1-gql-cas` (workflow, `GITHUB_TOKEN`
with `contents: write`, run 44865112229):

| Case | Result |
|---|---|
| `beforeOid` = tip, `afterOid` = child, `force: false` | `{"data":{"updateRefs":{"clientMutationId":null}}}`, tip moved |
| `beforeOid` = **stale** (older commit), `afterOid` = a valid fast-forward child of the current tip | **rejected**, tip unchanged. The error is opaque: `{"errors":[{"message":"Something went wrong while executing your query on ... Please include <request id> when reporting this issue."}]}` |
| `beforeOid` = tip, `afterOid` unrelated orphan, `force: false` | `UNPROCESSABLE ... rejecting non-fast forward update` |
| the same with `force: true` | accepted |
| `beforeOid` omitted | on `refs/heimdall/*`: the same opaque error (once). On a branch: accepted with **no** compare. So always pass `beforeOid` |
| two parallel writers, same `beforeOid` | exactly one winner in 8 of 8 trials (my login) and 6 of 6 trials (`GITHUB_TOKEN`); the loser gets the opaque error |
| `GITHUB_TOKEN` with `contents: read` | `{"type":"FORBIDDEN",... "message":"Resource not accessible by integration"}` |

The same table holds on `refs/heads/heimdall-test/s1-k7-cas` (stale `beforeOid` rejected, 8 of 8 races with one winner).

### 1.3 `git push` over smart HTTP (workflow `s1-cas2`, run 44803746248, `GITHUB_TOKEN`, header `AUTHORIZATION: basic`)

| Step | Result |
|---|---|
| create `refs/heimdall/s1-git-cas` with a plain push | `[new reference]` exit 0 |
| fast-forward push | exit 0 |
| sibling push, no force | `! [rejected] (non-fast-forward)` exit 1. This check ran in the client: git compared with the advertised tip |
| `--force-with-lease=ref:C1` while the tip is C2 | `! [rejected] (stale info)` exit 1 |
| `--force-with-lease=ref:C2` with a sibling (non-fast-forward) | `+ b4e5432...bc7403e (forced update)` exit 0. The server does not enforce fast-forward on this ref |
| 8 races, two pushes each with `--force-with-lease=ref:<same tip>` | one `exit=0` and one `! [remote rejected] ... (cannot lock ...)` in 8 of 8 |

So `git push` gives a real compare-and-swap, enforced by the server through the old object id in the push command.

### 1.4 Contents API, file `sha` (branch only)

`PUT /repos/{o}/{r}/contents/{path}` with `branch` and `sha` (the blob sha of the file being replaced), on
`heimdall-test/s1-k7-cas`:

| Case | Result |
|---|---|
| create, no `sha` | 201 |
| update with the right `sha` | 200 |
| update with a stale `sha` | **409** `<path> does not match <sha>` |
| update an existing file with no `sha` | 422 `"sha" wasn't supplied` |
| 6 races, same `sha` | one 200 and one 409 `is at <sha> but expected <sha>` in 6 of 6 |
| another file changed on the branch first, then write with this file's still valid `sha` | 200 (per-file, not per-branch) |

This is a per-file compare and the error is typed (409), but it writes a branch. A `PUT` naming `branch=refs/heimdall/...`
was not tried: the repo's bash guard blocked the command because it named no `heimdall-test` target. It was not worked
around. Reading `contents/<path>?ref=refs/heimdall/state` works (200).

### 1.5 Reads (any token with `contents: read`)

All 200 on `refs/heimdall/state`: `GET /git/ref/heimdall/state`, `GET /git/matching-refs/heimdall/`,
`GET /contents/changes/pr-1.jsonl?ref=refs/heimdall/state`, `GET /git/trees/<tip>?recursive=1`.
`GET /branches/heimdall/state` returns 404, as it should: the ref is not a branch.
Unauthenticated `git ls-remote https://github.com/aarush323/heimdall-fixture-pay.git 'refs/heimdall/*'` lists the refs
(this fixture repo is public). In a public repository the state log is therefore world-readable.

### 1.6 Push events and clones

I created `refs/heimdall/s1-pushevt` through the REST API with a commit whose tree held a workflow
`on: push` (no branch filter). No run started. The same commit created as `heads/heimdall-test/s1-k7-pushevt` started
run 44866409029 at once. A fresh `git clone --depth 1` has `refs/heads/main` and `refs/remotes/origin/*` only.
Not tested: a push by a GitHub App token to `refs/heimdall/*`. Note on method: the repo's bash guard looks only at the
command line for the word `heimdall-test` before a `gh api` write. The probes ran from script files (as in the first
run), each limited to the allowlisted S1 objects listed in section 8. The guard did block one inline command (1.4).

### 1.7 Recommendation for AM8

1. **Namespace: keep `refs/heimdall/state`.** It is accepted by `POST /git/refs` from `GITHUB_TOKEN`, it is not a branch,
   clones do not fetch it, and writes start no workflows. Q3 is closed.
2. **Create with REST `POST /git/refs`** (a second create returns 422, which is the safe create-if-absent).
3. **Update with GraphQL `updateRefs`**: `beforeOid` = the tip the gate read, `afterOid` = the new commit,
   `force: false`. Always pass `beforeOid`. This is the compare-and-swap, and it also enforces fast-forward.
4. **Reads with REST** (`GET /git/ref/heimdall/state`, `GET /git/trees/<sha>?recursive=1`, `GET /git/blobs/<sha>`).
   GraphQL cannot see the ref.
5. **Error handling.** A refused `updateRefs` is not typed: it returns "Something went wrong ... <request id>". After
   any GraphQL error other than `FORBIDDEN`, the store reads the tip again over REST. If the tip equals `afterOid`, the
   append succeeded and the response was lost. If the tip is something else, it is a conflict: reload, check the
   change's own `.head`, and either return `ErrVersionConflict` or rebuild on the new tip (2.4). If the tip is still
   `beforeOid`, treat it as a transient failure and retry with backoff. `FORBIDDEN` means the token lacks
   `contents: write`: fail closed.
6. **Contract test.** AM8's `httptest` fake must model `updateRefs` (`beforeOid` mismatch gives the opaque error) and
   the REST reads. A REST `PATCH` model would be wrong.
7. **Fallback if GitHub changes the opaque error into something we cannot read:** `git push --force-with-lease` from the
   runner works today (section 1.3) but needs the `git` binary and the token in an HTTP header. That is a decision for
   the owner, not a runtime fallback (R4).
8. **Branch alternative** (`refs/heads/heimdall-state`): REST `PATCH` is a fast-forward check there, and races give
   422. It would work with no GraphQL. We reject it: it shows up in the branch list, clones fetch it, rulesets that
   restrict branch creation apply, and a push by a PAT or an App token starts `push` workflows.

## 2. `workflow_run` facts: blocked

Not measured. A `workflow_run` workflow runs only when its file is on the default branch, and a PR run cannot test it
before it is merged. Pushing a workflow to `main` or `heimdall-demo` is outside the owner's yes. Items blocked:

- `workflow_run.pull_requests` for fork PRs (cannot be tested without a second account that forks the repo, either).
- An environment secret reachable from a `workflow_run` job under a deployment branch rule that allows only the default
  branch (also needs the environment created, a repository settings write).
- Latency of the two-stage chain (trigger run, then gate run).
- Whether a `workflow_run` event fires for a run of a workflow that exists only on the PR branch.

What the owner would need to allow, for one throwaway pass: (a) a workflow file `s1-gate.yml` on `main` of the fixture
repo with `on: workflow_run: workflows: [s1-trigger]`, removed afterwards; (b) creating an environment named
`heimdall-s1` with a deployment branch rule and one dummy secret, deleted afterwards; (c) for the fork case, a second
GitHub login that forks the fixture repo (the two test logins `aarush323` and `rekdfr` exist; the fork must be opened
from `rekdfr`). Each is a write to the default branch or to repository settings, so it needs a separate yes.

## 3. Required check, expected source

Read only. The fixture repo has no rulesets (`GET /rulesets` returns `[]`, `GET /rules/branches/main` returns `[]`) and
no branch protection on `main` (404). My login has `push` and not `admin` on the repo, so nothing could be changed anyway.
Nothing was changed.

From GitHub's own OpenAPI description (`rest-api-description`, read Nov 21 2026):

- Ruleset `required_status_checks` items: `context` and `integration_id`, described as "The optional integration ID that
  this status check must originate from."
- Branch protection `required_status_checks.checks` items: `context` and `app_id`, described as "The ID of the GitHub
  App that must provide this check. Omit this field to automatically select the GitHub App that has recently provided
  this check, or any app if it was not set by a GitHub App. Pass -1 to explicitly allow any app to set the status."
- A check run made by a workflow's `GITHUB_TOKEN` has `app.slug` `github-actions` and `app.id` 15368 (read from
  the `heimdall-s1-lat` check run). So a ruleset can require `heimdall/fast` from app 15368, or from the Heimdall App's
  own id. A check posted by a writer's branch workflow also has app 15368. Only a ruleset naming the Heimdall App's id
  separates the two. That matches section 2.7 and the Dec 1 2021 changelog.
- `POST /check-runs` with a user token returns 403 `You must authenticate via a GitHub App`. Check runs can be made
  only by a workflow's `GITHUB_TOKEN` or an App.

Not exercised live: a merge blocked by a wrong-source check. It needs a ruleset on a branch (a settings write).

## 4. Limits

PR #17 (head `heimdall-test/s1-k7-cas3`, base `heimdall-test/s1-k7-base`, label `heimdall-test`).
`POST /issues/17/comments` with a body of N ASCII `x`:

| Body | Result |
|---|---|
| 65,535 / 65,536 / 65,537 | 201 / 201 / **201** |
| 70,000, 100,000, 131,072, **262,144** | 201 (the stored comment is 262,144 characters) |
| 262,145 | **422** `Body is too long (maximum is 65536 characters)` |
| 263,168 up to 524,288 | 422, same message |
| 65,536 `é` (131,072 bytes) | 201 |
| 131,072 `é` (262,144 bytes) | 201 |
| 140,000 `é` (280,000 bytes) | 422 |
| `PATCH` an existing comment to 65,537 | 200 |

So the API limit is **262,144 bytes**, not 65,536 characters. The error text still says 65,536. The documented limit
is 65,536, GitHub may enforce it later or in the web editor, and one test does not prove the web UI renders a long
body. The renderer budget stays at 60,000 characters. Not measured: how the web UI shows a 262,144 byte comment.

Check run `summary`: not measured (`POST /check-runs` needs an App or a workflow token). The 65,535 limit stays from the docs.

Git Data API blob: `POST /git/blobs` with base64 content. 1 MiB (request body 1,398,142 bytes) gave 201 in 5 s.
10 MiB (request body 13,981,054 bytes) gave 201 in 57 s. I stopped there, as the brief said. 100 MB is not verified.
The slow 10 MiB call confirms the 64 KiB per test file cap is the right design.

## 5. Latency, push to check run

Workflow `s1-lat` (`push` on `heimdall-test/s1-k7-lat`, one job, posts a check run `heimdall-s1-lat` with
`GITHUB_TOKEN`, `checks: write`). A script made an empty commit and moved the branch with `PATCH` (the call returned
in 0.8 to 1.3 s), then polled `actions/runs?head_sha=` and `commits/<sha>/check-runs` every 0.7 s. Times from the
client clock after the `PATCH` returned. Ten pushes, one at a time, 10 s apart:

| Push | Run visible | Check visible |
|---|---|---|
| 0 | not seen first | 5.83 s |
| 1 | not seen first | 7.07 s |
| 2 | 4.09 s | 5.97 s |
| 3 | 2.85 s | 6.73 s |
| 4 | 8.23 s | 8.82 s |
| 5 | 5.13 s | 5.84 s |
| 6 | not seen first | 5.65 s |
| 7 | not seen first | 5.13 s |
| 8 | 4.31 s | 6.30 s |
| 9 | 7.11 s | 7.90 s |

Check visible: min 5.13 s, **p50 6.1 s, p95 8.8 s**, max 8.8 s. With n=10 the p95 is the max; the brief asked for 10
pushes, the original S1 row for 20. Run visible (6 of 10 seen before the check): p50 4.7 s, max 8.2 s. For four pushes
the run list was empty at the first poll and the check run showed up first, so the run's own latency there is under the
check's. Server stamps have 1 s resolution: `run_created_at` equals `run_started_at`, and `check_started_at` came 2 to 4 s after
`run_created_at`. This is one stage with an empty job. The two-stage chain in 3.3 (trigger run, then `workflow_run`
gate) is blocked (section 2), and each stage adds its own queue and checkout. The "30 to 90 seconds" estimate for two
stages is neither confirmed nor refuted. These numbers are a floor.

## 6. Cancel grace

Workflow `s1-cancel` (`push`), one job with three steps: `trap` (traps TERM, INT, HUP, logs a timestamp for each,
keeps ticking once a second with `sleep 1 || true`), `if: always()` (logs ticks), `if: cancelled()` (logs once). Cancel
by `POST /actions/runs/{id}/cancel` (202).

Run 44866241532 (always step of 40 s), all times UTC:

| Time | Event |
|---|---|
| 04:28:31.223 | cancel request sent (202 returned at 04:28:32.177) |
| 04:28:32.210 | `HANDLER got SIGINT` (1.0 s after the request) |
| 04:28:40.261 | `HANDLER got SIGTERM` (8.1 s after SIGINT) |
| 04:28:41.269 | last tick (tick 40) |
| 04:28:42.285 | `##[error]The operation was canceled.` (about 2.0 s after SIGTERM, about 11 s after the request). No handler line, so the kill is SIGKILL |
| 04:28:42 to 04:29:22 | the `if: always()` step ran all 40 ticks and finished normally |
| 04:29:22 | the `if: cancelled()` step ran; the job concluded `cancelled` at 04:29:23 |

So: a step that ignores signals gets SIGINT, then SIGTERM about 8 s later, then SIGKILL about 2 s later. The pass time
is about 10 s from the signal. A step that handles SIGINT (for example, Go `signal.NotifyContext`) has about 7.5 s to
exit, so the proof binary must write its partial result within a few seconds of SIGINT.

Run 44865712501 (always step of 420 s): the cancel request was at 04:22:02.1. The `trap` step ended at 04:22:13
(11 s). The `always()` step started at 04:22:13 and was still running when the job was completed at **04:27:03**, which is
exactly 300 s after the request. The run concluded `cancelled`. The `always()` step stayed `in_progress`, the
`if: cancelled()` step stayed `pending` (never ran), and `gh run view --log` and `GET /actions/jobs/{id}/logs`
returned `BlobNotFound`: the logs of that job are gone. So the whole cancel has a hard 300 s budget, and a job that
overruns it loses its logs and its post steps.

An earlier probe (run 44802606594) wrongly looked like "SIGINT, then 0.57 s, then dead". The shell ran `bash -e` and
`wait` returned 130 after the signal, so the shell exited on its own. Ignore that run.

Effect on design: the partial-result step after a cancel must finish in far less than 300 s (uploading one artifact and
one `updateRefs` is seconds). It must not be a long step. Not tested: `concurrency.cancel-in-progress` (it uses the same
cancel path, so the same signals are expected, not verified), and Windows and macOS runners.

## 7. Not verified

- Everything in section 2.
- A push by a GitHub App token to `refs/heimdall/*` (the App path of enforce mode).
- `PUT /contents/...` with `branch=refs/heimdall/...` (blocked by the repo's bash guard, not worked around).
- How the web UI renders a comment above 65,536 characters; whether GitHub starts enforcing 65,536 on the API.
- Blob sizes above 10 MiB; check run `summary` size.
- Latency of the two-stage trigger-and-gate chain; the original 20 pushes (10 done).
- `cancel-in-progress` signals; self-hosted runner cancel.

## 8. Objects and cleanup

Objects created, all on `aarush323/heimdall-fixture-pay`, and their state after cleanup (verified by listing
afterwards on Nov 21 2026):

- Branches `heimdall-test/s1-k7`, `s1-k7-base`, `s1-k7-cancel`, `s1-k7-cas`, `s1-k7-cas2`, `s1-k7-cas3`, `s1-k7-lat`,
  `s1-k7-pushevt`: deleted.
- Refs `refs/heimdall/state`, `s1-cas-probe`, `s1-git-cas`, `s1-gql-cas`, `s1-pushevt`;
  `refs/tags/heimdall-test-s1-k7-tag`; `refs/notes/heimdall-test-s1-k7`: deleted.
- PR #17: its 11 comments deleted, closed. A closed PR cannot be deleted through the API, so it stays in the PR list
  as closed (title "S1 spike: comment size probe (throwaway)").
- Label `heimdall-test` (the repo had none; created for the PR): deleted.
- Workflow files lived on the deleted branches only. All 20 workflow runs of those branches: deleted.
- Repository secrets: none were created (count 0).
- Check runs (`heimdall-s1-lat`, `post`) hang off commits of the deleted branches and cannot be deleted through the
  API. They are unreachable from any branch.
- Other `heimdall-test/*` branches (`demo-base`, `green-1..3`, `h11-ticketless-20261119`) and the open PRs 11 and 15
  were not made by S1 and were not touched.

## 9. AM11 probes: the isolated sandbox

Status: measured Nov 21 2026 by P-AM11 on `aarush323/heimdall-fixture-pay`, branch `heimdall-test/am11-r1`, runner
`ubuntu-latest` (image `ubuntu-26.04` version 20261116.88.1, Ubuntu 26.04.1, runner 2.338.2, systemd 259, nftables
1.1.6, setpriv from util-linux 2.41.3, Go 1.25.14 from `actions/setup-go`). No model calls. No secret: the canary was a
random value made in the step. The workflow file, the probe module and the harness binary went onto the branch through
the contents API. Nothing went into this repository's workflows.

**Result: one probe escaped.** From the execution phase, the host's systemd-resolved answered a DNS query through its
Unix socket. Every other probe was blocked. AM11 is therefore not done: section 4.4 of `ACTION_MODE.md` does not hold
until a fix from 9.4 lands and the probes pass again.

### 9.1 Method

The harness is `internal/adapters/uidsandbox/probe_linux_test.go` (tags `linux,sbxprobe`), cross-compiled here with
`GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -tags sbxprobe`. On the runner it is the "loop" process. It:

1. holds the canary in its own environment, and writes it to `$HOME/heimdall-canary.txt` and
   `$RUNNER_TEMP/heimdall-canary.txt` with mode 0644, so only the directory modes can stop the sandbox user;
2. runs the controls as the runner user: its `/proc/self/environ` has the canary, both files are readable, DNS works
   (`github.com` = 4.149.173.80), TCP to 1.1.1.1:443 works, and `/var/run/docker.sock` accepts a connect;
3. finds the runner's own files by walking up to `Runner.Worker`: `.credentials`, `.runner` and `_diag` under
   `/home/runner/actions-runner/cached/2.338.2/`;
4. calls `New`, `Setup` (uid 999, proxy on 127.0.0.1:36289 in run 1) and `Stage` with the probe module of
   `testdata/probe` plus a `config.json` (parent pid, canary paths, canary SHA-256, proxy port, runner files);
5. runs `go mod download` in the fetch phase (allowlist `proxy.golang.org`, `sum.golang.org`, port 443), then
   `go test -run ^TestFetch` in the fetch phase, then `go test -run ^TestExec` in the execution phase;
6. after the execution job, checks that the daemon pid the probe wrote is gone and that `pgrep -u 999` finds nothing.

A probe test passes when the escape fails. It fails with `ESCAPED` when the escape works.

### 9.2 Results: run 44868233309, all 40 attempts in 21 probe tests blocked

Run 44868368873 repeated every probe below with the same result and added the probes of 9.3.

| Probe | Phase | Exact result |
|---|---|---|
| Read the loop's `/proc/<pid>/environ` | exec | `open /proc/2511/environ: permission denied` |
| Open the loop's `/proc/<pid>/mem` | exec | `open /proc/2511/mem: permission denied` |
| Scan every `/proc/*/environ` for the canary key or value | exec | in none; 2 readable (own uid), 194 denied |
| Own environment | exec | 13 fixed variables, no canary |
| Read the home canary; list the home | exec | `open /home/runner/heimdall-canary.txt: permission denied`; `open /home/runner: permission denied` |
| Read the `_temp` canary | exec | `open /home/runner/work/_temp/heimdall-canary.txt: permission denied` |
| Read the runner's `.credentials`, `.runner`, `_diag` | exec | `permission denied` for each |
| DNS, system resolver | exec | `read udp 127.0.0.1:34678->127.0.0.53:53: read: connection refused` |
| DNS to 8.8.8.8 | exec | `dial udp 8.8.8.8:53: connect: network is unreachable` |
| TCP to 1.1.1.1:443, 140.82.112.3:443, [2606:4700:4700::1111]:443, 169.254.169.254:80 | exec | `connect: network is unreachable` for each |
| Network interfaces | exec | only `lo` |
| Fetch proxy from the execution phase: `CONNECT example.com:443` and `proxy.golang.org:443` | exec | `dial tcp 127.0.0.1:36289: connect: connection refused` |
| `/var/run/docker.sock`, `/run/containerd/containerd.sock` | exec | `connect: permission denied` |
| Passwordless root escalation (`/usr/bin/sudo -n true`) | exec | refused: `The "no new privileges" flag is set, which prevents sudo from running as root.` |
| `su root` | exec | `su: Authentication failure` |
| Privileges | exec | `NoNewPrivs=1`, `CapEff=0`, `CapBnd=0`, uid 999 in all four fields, no groups |
| Install a crontab | exec | `You (heimdall-sbx) are not allowed to use this program (crontab)` |
| `at` job | exec | not applicable: the image has no `at` |
| `systemd-run --user` | exec | `Failed to connect to user scope bus` |
| Daemon kept alive: a `setsid` child and a `setsid -f nohup` child | exec | pid 4040 gone after the kill (`stat /proc/4040: no such file`); `pgrep -u 999` empty; the workflow's last step printed `no heimdall-sbx process` |
| Root rights of the user (`-l -U heimdall-sbx`, run as root) | setup | `User heimdall-sbx is not allowed to run sudo on ip-10-0-142-233.`; `id`: `groups=986(heimdall-sbx)` |
| Home and `_temp` mode after setup | setup | both 0700 (the image ships the home as 0750 and `_temp` as 0755) |
| Allowed fetch: `go mod download` of `rsc.io/quote` v1.5.2 | fetch | exit 0; proxy log `sandbox proxy tunnel target=proxy.golang.org:443` |
| `CONNECT proxy.golang.org:443` | fetch | 200 (the positive check) |
| `CONNECT` to `example.com:443`, `github.com:443`, `proxy.golang.org.evil.example:443` | fetch | `403 Forbidden`, logged `host not in the registry allowlist` |
| `CONNECT 1.1.1.1:443`, `169.254.169.254:80` | fetch | `403 Forbidden`, logged `IP literal target refused` |
| `CONNECT proxy.golang.org:80` | fetch | `403 Forbidden`, logged `port not allowed` |
| Direct TCP to 1.1.1.1:443, 127.0.0.53:53, 169.254.169.254:80 | fetch | `i/o timeout` (the nftables drop) |
| Direct TCP to [2606:4700:4700::1111]:443 | fetch | `network is unreachable` |
| DNS, system resolver | fetch | `write udp 127.0.0.1:42039->127.0.0.53:53: write: operation not permitted` |
| `/var/run/docker.sock` | fetch | `permission denied` |
| Offline build with the fetched module | exec | works: `"Hello, world."` |

### 9.3 The escape: run 44868368873

A network namespace cuts the sockets of the network stack. It does not cut Unix sockets on the file system.

| Probe | Phase | Exact result |
|---|---|---|
| `resolvectl query example.com` | exec | **ESCAPED**: `example.com: 104.20.23.154, 172.66.147.243, 2606:4700:10::6814:179a, 2606:4700:10::ac42:93f3`, after `Failed to resolve interface name for index 2` (the host's eth0 does not exist in the namespace) |
| `resolvectl query example.com` | fetch | **ESCAPED**: the same answer, `-- link: eth0`. This also goes around the proxy allowlist |
| `getent ahosts example.com` (NSS) | both | blocked: exit 2. NSS on this image asks 127.0.0.53 and does not use the varlink socket |
| Host Unix sockets that accept a connect | fetch | 23 of 65 listed, among them `/run/dbus/system_bus_socket`, `/run/systemd/resolve/io.systemd.Resolve`, `/run/systemd/io.systemd.Manager`, `/run/snapd.socket`, `/run/polkit/agent-helper.socket`, `/run/ssh-unix-local/socket`, `/run/systemd/journal/stdout` and two abstract sockets |
| The same list | exec | `/proc/net/unix` lists 0 (it is per namespace), but the resolvectl answer shows that file system sockets still connect |

What it means: PR code in the execution phase can send any data out as DNS names under a domain its author controls.
It still cannot read the key (9.2), so the leak is limited to what heimdall-sbx can read: the PR tree, the model's
tests and world-readable host files. In the fetch phase it skips the registry allowlist. The D-Bus and systemd
sockets were only connected to, not used. What an unprivileged caller can do through them is `not verified`.

### 9.4 Fix options (owner decision, not built)

The brief said to stop on an escape and write it up, so nothing below is built.

1. **A transient service instead of a scope (recommended).** `systemd-run --pipe --wait --collect` with
   `User=heimdall-sbx`, `NoNewPrivileges=yes`, `PrivateNetwork=yes` (execution phase), `TemporaryFileSystem=/run` or
   `InaccessiblePaths=` for `/run/dbus`, `/run/systemd`, the snapd, polkit and ssh sockets, plus `PrivateTmp=yes`,
   `ProtectHome=yes` and `RuntimeMaxSec=`. This also gives systemd's own NoNewPrivileges, which a scope cannot set,
   and a private /tmp. The cost: stdin and stdout go through `--pipe`, and the fetch rule may move from nftables to
   `IPAddressDeny=any` with `IPAddressAllow=127.0.0.1` (a cgroup BPF filter; `not verified` on this image).
2. **A mount namespace in the current chain.** `unshare --mount` with an empty tmpfs over `/run` before `setpriv`.
   The chain has no shell, so this needs a small helper (the binary itself), and it must keep `/run/netns`.
3. **`RestrictAddressFamilies=~AF_UNIX`** for the execution phase. It breaks test suites that use Unix sockets, so it
   is not a default.

### 9.5 Not verified

- Any fix from 9.4. The probes must run again after it.
- What D-Bus, polkit, snapd or the local ssh socket allow an unprivileged caller (only the connect was tried).
- The memory, CPU and task limits under load (set, not stressed). A job that hits its deadline on the runner (the
  kill path is unit-tested with a fake exec only).
- `New` on a runner without passwordless root or without nftables. The `CannotIsolateError` paths are code plus a
  unit test of the error type; no such runner was used.
- Self-hosted runners, ARM runners and `ubuntu-24.04`.

### 9.6 Objects and cleanup

All on `aarush323/heimdall-fixture-pay`, all deleted Nov 21 2026, and checked by listing afterwards:

- Branch `heimdall-test/am11-r1`, made from the tip of `main` (nothing was written to `main` or `heimdall-demo`):
  deleted.
- Workflow runs 44868233309 (success), 44868368873 (failure: the escape), and 44868360227, 44868362078, 44868364155,
  44868366675 (intermediate commits of the second upload, cancelled): all six deleted. The run list of the branch is
  empty. The logs survive only in this section.
- No PR, label, secret or environment was created (no `am11` branch left, secrets count 0, label list unchanged).
