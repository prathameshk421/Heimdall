# AM16 live test: Action mode on `aarush323/heimdall-fixture-pay`

Run on Nov 22 2026 by the AM16 delegate, run id `am16-r1`. Heimdall source: branch `am16-action` at `66a924f`
(AM16 steps 1 to 3 merged with `product-build`). Model: `deepseek-ai/deepseek-v4.1-flash-1017` on
`https://api.deploy.com/api/v1`, prices $0.50 input, $0.50 cached input and $2.00 output per million tokens. Budget:
`action.budget.per_pr_usd: 1.0` in the policy at the merge base.

## Result in one paragraph

The install-to-first-summary time **was not measured**: the gate could not run. `heimdall action init -apply` stopped
at its first step because creating environment `heimdall` needs admin rights on the repository, and the only login on
this machine with access (`prathameshk421`) has write, not admin (`aarush323` is the only admin). Without the
environment there is no log key, and the gate must run on the default branch with that environment. So this run tested
the unprivileged half: the trigger and proof workflows on a pull request, the composite action building heimdall from
a pinned SHA, the sandbox on a hosted runner, and a real model call under the cost cap. All of that ran. The proof
ended `unknown (model_failure)` after the agent used all 40 turns without submitting, at a cost of $0.2603.

## Timeline (UTC)

| Time | Event |
|---|---|
| 04:11:07 | `heimdall action init -repo aarush323/heimdall-fixture-pay -apply -model-key-stdin < ~/heimdall/secrets/deploy_api_key`: exit 1, `step 1 (environment): ... gh: Must have admin rights to Repository. (HTTP 403)` |
| 04:12:16 to 04:12:18 | Repository secret `HEIMDALL_MODEL_KEY` set with `gh secret set` from the same file on stdin (write access is enough for a repository secret). Five variables `HEIMDALL_MODEL*` set |
| 04:13 | Action source pushed: orphan commit `cb626b8ff9ccad34be915bf33393e462325c7538` on `heimdall-test/am16-r1-action` |
| 04:14:15 | Base branch `heimdall-test/am16-r1` pushed (`b46fc18`): the three workflows pinned to `cb626b8`, and `action: { mode: shadow, budget: { per_pr_usd: 1.0 } }` in `heimdall.yaml` |
| 04:14:20 | PR #19 opened from `heimdall-test/am16-r1-pr` (`02ec2eb`) against the base branch |
| 04:14:22 | Runs 44984157813 (Heimdall trigger) and 44984157761 (Heimdall proof) created |
| 04:15:11 | heimdall built in the proof job (44 s; binary sha256 `81c5b3c3dc109bd6bcf2d3d8d10ba7617bf59ebbc6db3fafd18ac29afa5140a5`) |
| 04:15:19 | Trigger run done, success (57 s from creation) |
| 04:15:19 | Proof: `sandbox self check passed` for phase `exec` and phase `fetch` |
| 04:20:03 | Proof: `heimdall action proof: #19: unknown (model_failure); 0 candidates, 0 proven, 0 blocking; $0.2603, 40 turns` |
| 04:20:07 | Proof run done, success (5 min 45 s from creation). Both artifacts uploaded |

From PR open to the proof artifact: 5 min 47 s. The first summary comment from Action mode: none (no gate).

## How the action was reached

The workflows pin `uses: <owner>/<repo>/deploy/action@<sha>`. The `am16-action` commits are not on GitHub, and this
repository is not pushed without the owner asking. So the test pushed a snapshot to the fixture repository instead: an
orphan commit with `go.mod`, `go.sum`, `cmd/`, `internal/` and `deploy/`, without `*_test.go` and `testdata/`.

The first snapshot, with tests, was refused by GitHub push protection: `internal/testkit/record/record_test.go:22` holds a
string that matches an OpenRouter API key. Push protection was not bypassed. That string is probably a test fixture
for the redactor, but whether it is a live key was not checked (R8: it was not read). The workflows then used
`aarush323/heimdall-fixture-pay/deploy/action@cb626b8ff9ccad34be915bf33393e462325c7538`.

## The artifacts

`heimdall-event` (from both runs, identical):

```json
{"version":1,"event":"pull_request","number":19,"head":"02ec2eb02a1d8a79724d763c1f5267a0b9afbebc"}
```

`heimdall-proof`:

```json
{"version":1,"head":"02ec2eb02a1d8a79724d763c1f5267a0b9afbebc","base":"b46fc18963fe11d41930538e5ab0969727bcb9fd",
 "policy_hash":"4e1711f574bc1c414bcb5003a52fe495ee5a8a16996f64abc85d93f77786a07d","workflow_run":44984157761,
 "model":"deepseek-ai/deepseek-v4.1-flash-1017","kind":"unknown","reason":"model_failure",
 "counts":{"in":0,"proven":0,"blocking":0,"needs_person":0,"refuted":0,"no_proof":0,"outdated":0,"no_location":0,"not_checked":0},
 "cost":{"usd_micros":260348,"prompt_tokens":411212,"completion_tokens":27371}}
```

## Check runs on the head commit `02ec2eb`

| Id | Name | App | Conclusion |
|---|---|---|---|
| 137090955591 | Heimdall trigger | github-actions | success |
| 137090955317 | Heimdall proof | github-actions | success |
| 137090986363 | heimdall/gate | tos-risk-analyser | failure |

No `heimdall/fast` or `heimdall/proof` check exists: only the gate posts those. The `heimdall/gate` check and a summary
comment (id 7074060305, 04:14:29) came from **server-mode** Heimdall, whose App `tos-risk-analyser` is installed on the
fixture repository and reacted to the PR within 9 seconds. They are not Action mode output. Its check said `Blocked`:
no ticket linked, the slop rule failed, acceptance tests not verified. Its comment opened with "Heimdall Review ·
pr-aarush323-heimdall-fixture-pay-19 ... Checks are running".

## What the run showed

1. **The composite action works from a pinned SHA on a hosted runner.** It built heimdall in 44 s with
   `CGO_ENABLED=0 GOTOOLCHAIN=local -mod=readonly`, and the later steps of the job reused the binary.
2. **The trigger record is right.** Event, PR number and head match the pull request.
3. **The sandbox set up on `ubuntu-latest`** and passed its self-check in both phases.
4. **The cost cap held.** $0.26 of the $1.00 budget. 411,212 prompt tokens is about half the default input cap of
   800,000, for a two-file change of 25 lines.
5. **A proof that does not finish fails closed.** The agent reached the 40-turn cap with no submission. The artifact
   says `unknown`, never `pass`. The job still ended `success`, because the proof step wrote a valid artifact. The gate
   turns that artifact into an `unknown` check. That last part was not run.
6. **No HTTP 500 from the Deploy endpoint in this run.** The failure was the turn cap.

## What failed and why

- `action init` step 1: needs admin rights to create the environment. Not a Heimdall bug. The README now says admin
  is required.
- The gate: not run, so no `heimdall/fast` or `heimdall/proof` check, no summary comment, no `refs/heimdall/state` write.
- The proof: 40 turns and no submission on a small PR. The reason `model_failure` hides that it was the turn cap
  (`internal/app/action/proof.go:283` maps `AgentStopTurns` to `ReasonModelFailure`), and the log line names only the
  turn count. Why the model did not submit is not known: the transcript is not in the artifact or the log.

## To finish the test

One of these, from the owner:

1. `aarush323` gives `prathameshk421` admin on `aarush323/heimdall-fixture-pay`, or runs `heimdall action init -apply`
   themselves. Then the gate workflow goes on `main` for the length of the run (the repository has no workflow files on
   `main` today, so no other workflow changes), and is removed afterwards.
2. Or the test runs on a repository that `prathameshk421` owns. Deleting that repository afterwards needs the
   `delete_repo` scope, which the current token lacks.

## Cleanup

Created and removed, all on `aarush323/heimdall-fixture-pay`:

- Branches `heimdall-test/am16-r1`, `heimdall-test/am16-r1-pr` and `heimdall-test/am16-r1-action`: deleted.
- Workflow runs 44984157761 and 44984157813, with their artifacts: deleted.
- Repository secret `HEIMDALL_MODEL_KEY`: deleted.
- Variables `HEIMDALL_MODEL_BASE_URL`, `HEIMDALL_MODEL`, `HEIMDALL_MODEL_PRICE_INPUT`,
  `HEIMDALL_MODEL_PRICE_CACHED_INPUT` and `HEIMDALL_MODEL_PRICE_OUTPUT`: deleted.
- PR #19: closed. A closed PR cannot be deleted through the API. It keeps its two comments: server mode's summary
  and the closing note. Commit `02ec2eb` and its three check runs stay reachable through `refs/pull/19/head`.
- No environment, no environment secret and no `refs/heimdall/*` ref was created.

Listing after cleanup: branches with `am16` 0; `refs/heimdall/*` 0; secrets 0; variables 0; environments 0; runs on
`heimdall-test/am16*` 0; workflows `am11-probe` and `CI` only (as before); PR #19 `CLOSED`; open PRs 18, 15 and 11
(as before).

## Not verified

- Install to first summary under 10 minutes (the AM16 acceptance): not run, because the gate is blocked on admin.
- The gate on `workflow_run`, the environment secret on `workflow_run`, the state ref through GraphQL `updateRefs`,
  and the `heimdall/fast` and `heimdall/proof` checks and summary comment.
- The fail-closed path of a Deploy HTTP 500: no 500 happened.
- Why the agent did not submit within 40 turns.

## Run on the prathameshk421 fork

Run on Nov 22 2026 by the AM16b delegate, run id `am16b-r1`. Heimdall source: branch `am16b-live` at `2e4355f`
(the tool-error record, below) over `product-build` at `d3caa1a`. Repository: `prathameshk421/heimdall-fixture-pay`,
a fork of `aarush323/heimdall-fixture-pay` created for this run. Model: `deepseek-ai/deepseek-v4.1-flash-1017` on
`https://api.deploy.com/api/v1`, prices $0.50 input, $0.50 cached input and $2.00 output per million tokens.
Budget: `action: { mode: shadow, budget: { per_pr_usd: 1.0, monthly_usd: 50 } }` in `heimdall.yaml` on the fork's
`main`. The GitHub login `prathameshk421` administers the fork, so `action init -apply` ran.

### Result in one paragraph

The first full install-to-summary run of Action mode **ran**, and the install-to-first-summary time is
**3 min 10 s** from the first install command to the first Heimdall summary comment on the pull request. `action init`
now completes: the environment `heimdall` exists, the log key is in it, and the model key is a repository secret.
All three workflows ran, the gate posted `heimdall/fast` and `heimdall/proof` as `neutral` (shadow mode), wrote the
summary comment and the state ref. The proof ended `unknown (turn_cap)` after 40 turns at $0.20, because `run_test`
never worked in the sandbox: every staging attempt failed with
`sandbox setup step failed: tar exited 2: ... Cannot open: Function not implemented` (ENOSYS). The gate correctly
turned that into an `unknown` check, never a pass. Two attempts were made; both hit the same turn cap.

### Timeline (UTC)

| Time | Event |
|---|---|
| 05:07:49 | Install start (`date +%s` at the first command) |
| 05:07:56 | `heimdall action init -repo prathameshk421/heimdall-fixture-pay -apply -model-key-stdin < key`: exit 0 in **7 s**. Steps: environment, branch policy, log key, model key |
| 05:08:04 to 05:08:07 | Five `HEIMDALL_MODEL*` variables set (3 s) |
| 05:08:43 | Environment, environment secret `HEIMDALL_LOG_KEY`, repository secret `HEIMDALL_MODEL_KEY` and the `main` branch policy all verified through the API. Install done: **54 s** |
| 05:08:52 | PR #1 opened from `pay-demo/refunds` into `main` |
| 05:09:00 | Runs 44988127253 (trigger) and 44988127330 (proof) created |
| 05:09:54 | Trigger run done, success (54 s) |
| 05:09:58 | Gate run 44988197523 started (from the trigger's `workflow_run`) |
| 05:10:58 | `heimdall/fast` check run 137103537914 posted, `neutral` |
| 05:10:59 | **First summary comment 7074602918** (gate run 44988197523 posted `pr.opened`, `fast.reported`, `summary.posted`). Install start to here: **3 min 10 s** |
| 05:11:04 | Gate run 44988197523 done, success (1 min 6 s) |
| 05:12:15 | Proof run 44988127330 done, success (3 min 15 s): `unknown (turn_cap)`, $0.1995, 40 turns |
| 05:12:16 | Gate run 44988373825 started (from the proof's `workflow_run`) |
| 05:13:27 | `heimdall/proof` check run 137104091007 posted, `neutral`; summary comment updated to version 4 |
| 05:13:34 | Gate run 44988373825 done, success (1 min 18 s) |

Second attempt, after the tool-error fix was pushed (see below):

| Time | Event |
|---|---|
| 05:35:55 | Empty commit `5f3a14f` pushed to `pay-demo/refunds` to re-run on the new action pin |
| 05:35:59 | Runs 44990103462 (trigger) and 44990103442 (proof) created |
| 05:36:54 | Trigger done, success (55 s) |
| 05:36:58 | Gate 44990170006 started; superseded the old head's result and said so |
| 05:38:06 | Gate 44990170006 done, success (1 min 8 s) |
| 05:38:27 | Proof 44990103442 done, success (2 min 28 s): `unknown (turn_cap)`, $0.1987, 40 turns |
| 05:38:29 | Gate 44990279607 started |
| 05:39:24 | Gate 44990279607 done, success (55 s); `heimdall/proof` check 137109772478 `neutral`; comment updated to version 8 |

### Runs

| Run id | Workflow | Event | Conclusion | Duration |
|---|---|---|---|---|
| 44988127253 | Heimdall trigger | pull_request | success | 54 s |
| 44988127330 | Heimdall proof | pull_request | success | 3 min 15 s |
| 44988197523 | Heimdall gate | workflow_run | success | 1 min 6 s |
| 44988373825 | Heimdall gate | workflow_run | success | 1 min 18 s |
| 44990103462 | Heimdall trigger | pull_request | success | 55 s |
| 44990103442 | Heimdall proof | pull_request | success | 2 min 28 s |
| 44990170006 | Heimdall gate | workflow_run | success | 1 min 8 s |
| 44990279607 | Heimdall gate | workflow_run | success | 55 s |

Every run ended `success`, because a proof that wrote a valid artifact is green by design. Green is not a pass: the
gate decides from the artifact, and it posted `heimdall/proof` as `neutral` with the summary `unknown: turn cap
reached`.

### Check runs on the head `5f3a14f`

| Id | Name | App | Conclusion | Summary |
|---|---|---|---|---|
| 137109064738 | Heimdall trigger | github-actions | success | (the workflow run itself) |
| 137109064868 | Heimdall proof | github-actions | success | (the workflow run itself) |
| 137109497637 | heimdall/fast | github-actions | neutral | would block because approval |
| 137109772478 | heimdall/proof | github-actions | neutral | unknown: turn cap reached |

On the first head `f495174` the same two checks were 137103537914 and 137104091007, both `neutral`.

### The summary comment (verbatim, version 8)

```markdown
<!-- heimdall:proof-summary change=pr-prathameshk421-heimdall-fixture-pay-1 version=8 head=5f3a14f0d535d06137fabbb8add75705da878371 -->

## Would block because the fast check failed (approval) · shadow · `5f3a14f`

Shadow mode: this reports only. It does not block a merge.

### Needs a person
- The proof run is unknown (turn cap reached). Only a person clears it: rerun the proof or decide by hand.
- `analyzers` is unknown: the analyzer results for head are not known
- `memory_rules` is unknown: the rule cards at the merge base are not known
- `approval` needs an approver: no approval has been recorded from an approver of the changed domains

### Proven findings
Nothing is proven.

Unproven, not shown: none.

### Fast check
- met `secrets`: no new secret in the added lines
- **unknown** `analyzers`: would block until known: the analyzer results for head are not known
- not checked `pinned_tests`: action.pinned names no test files
- met `test_integrity`: no test integrity rule is broken
- not checked `diff_coverage`: action.diff\_coverage.min is not set
- **unmet** `approval`: would block because no approval has been recorded from an approver of the changed domains
- **unknown** `memory_rules`: would block until known: the rule cards at the merge base are not known
- met `heimdall_files`: the change touches no Heimdall workflow, policy or declared input

### Not checked
- `pinned_tests`: action.pinned names no test files
- `diff_coverage`: action.diff\_coverage.min is not set
- Proof run: did not finish (turn cap reached). What it had not reached was not checked.

Heimdall judges only what is listed on this page. Whatever is not listed was not looked at, so silence is not safety.

Cost: $0.20 this run, 355.2k input and 10.6k output tokens.


<!-- heimdall:effect=action-summary:1 -->
```

One comment, not two: the gate finds its own comment by the effect marker and edits it, which is the design
(ACTION_MODE 2.7). It went version 2, 3, 4, 7, 8 as the two heads and four gate runs replaced it.

### Model cost

| Run | Artifact | Cost | Prompt tokens | Completion tokens |
|---|---|---|---|---|
| 44988127330 (head `f495174`) | `unknown (turn_cap)` | $0.1995 | 354,482 | 11,144 |
| 44990103442 (head `5f3a14f`) | `unknown (turn_cap)` | $0.1987 | 355,179 | 10,567 |

Total on the Deploy endpoint: **$0.3982** of the $1.00 per-PR cap, under the $3 testing cap.
`heimdall-proof.json` for the second run:

```json
{"version":1,"head":"5f3a14f0d535d06137fabbb8add75705da878371","base":"083f66c89c1bdae12564a273d14ddd2f19a57a48",
 "policy_hash":"00263dbb5d9de24e1c52a009e7e04e49da44bed6698c9c3ab44141428bb04e53","workflow_run":44990103442,
 "model":"deepseek-ai/deepseek-v4.1-flash-1017","kind":"unknown","reason":"turn_cap",
 "counts":{"in":0,"proven":0,"blocking":0,"needs_person":0,"refuted":0,"no_proof":0,"outdated":0,"no_location":0,"not_checked":0},
 "cost":{"usd_micros":198724,"prompt_tokens":355179,"completion_tokens":10567}}
```

### What worked

1. **`action init` on a repository the login administers.** Four steps, 7 s, no error. The environment is limited to
   `main`; the log key and the model key both landed. This is the step that blocked the first AM16 run.
2. **The gate on `workflow_run`, with the environment secret.** Both gate runs read `HEIMDALL_LOG_KEY` from the
   `heimdall` environment on a `workflow_run`, wrote `refs/heimdall/state`, and posted the two checks and the comment.
   None of that had run before.
3. **The summary comment as one editable comment.** Two heads, four gate runs, one comment, eight versions.
4. **The state ref.** `refs/heimdall/state` is at `3669195`, written through the API.
5. **The three files a customer copies are enough.** No edit beyond the pin, the policy line and the variables.
6. **The cost cap.** $0.20 a run of the $1.00, about 355k of the 800k input tokens.
7. **The proof ends fail-closed.** A run that proved nothing is `unknown`, never `pass`, and the gate says so.

### What failed and why

**1. `run_test` never worked: tar staging failed with ENOSYS.** Root cause from the transcript of run 44990103442,
which the fix below made visible for the first time:

```
run_test | sandbox: stage the tree: sandbox setup step failed: tar exited 2:
  /usr/bin/tar: .heimdall/profile.yaml: Cannot open: Function not implemented
  /usr/bin/tar: api/openapi.yaml: Cannot open: Function not implemented
  /usr/bin/tar: cmd/pay: Cannot mkdir: Function not implemented
```

`ENOSYS` on `openat`/`mkdirat` inside the service is what a seccomp filter that allows `openat` but not the second
argument set of `openat2`, or a `ProtectSystem=strict` mount that cannot be written, reports. The sandbox self check
passed for both phases, so the service starts and is isolated; only a write into `WorkDir` fails. The self check
script uses shell builtins only and never writes a file, so it cannot catch this. **Not fixed in this run:** the
sandbox adapter needs a write in its self check and a decision on the syscall set. That is the next task.

Because the model got `error: ...` on every `run_test`, it read files for 20 more turns and hit the 40-turn cap. The
proof never proposed a finding, so the planted refund bug was not proven.

**2. A turn cap explained nothing (fixed here).** In run 44988127330 the transcript held only tool names and argument
sizes. The model's own words were the only clue ("The sandbox staging failed. Let me retry."). Nothing in the job log,
the artifact or the job summary named the failing tool or the error. Fix: the agent loop now records a failed tool
call on the turn (`ports.AgentCallBrief.Failed` and `.Error`, 300 bytes) and the proof transcript carries it
(`TranscriptCall.failed` and `.error`). Tests: `TestAFailedToolCallKeepsItsErrorOnTheTurn`,
`TestAFailedToolErrorIsBounded` (loop) and `TestTheTranscriptNamesAFailedToolCall` (action). That fix is what produced
the ENOSYS text above. `make fast`: 5420 tests pass.

**3. `analyzers` and `memory_rules` are always `unknown`.** `cmd/heimdall/action_facts.go` says so: the gate reads no
code-scanning alerts and no rule cards, so a clause that needs them reads `unknown`, never `met`. Expected, not a
defect of this run, but it means a fresh install always shows two `unknown` rows until those readers exist.

**4. The monthly budget says "the budget file could not be read" on every gate run.** `monthlyRow` in
`internal/domain/actionflow/rows_proof.go` returns `unknown` when `MonthKnown` is false, and the Action gate has no
budget file. The row is honest, and the per-PR cap still held, but the line is noise on every single run.

### Install-to-summary in one number

**3 min 10 s**, from the first `action init` command (05:07:49) to the first Heimdall summary comment (05:10:59).
That is inside the AM16 acceptance of 10 minutes, with the proof still running at the time (it finished 1 min 16 s
later). Of that, 54 s was the install and 6 s the variables; the rest was two workflow runs.

### How the action was reached

The workflows pin `uses: prathameshk421/heimdall-fixture-pay/deploy/action@<sha>`. The `am16b-live` commits are not on
any repository the workflows can reach. As in the first run, a snapshot went to the fixture fork instead: an orphan
branch `action-src` with `go.mod`, `go.sum`, `cmd/`, `internal/` and `deploy/`, without `*_test.go`, `testdata/` or
`internal/testkit`. Two SHAs were used:

| Snapshot | Used by | Why |
|---|---|---|
| `cfe048d` | first attempt | `2e4355f` without the tool-error record |
| `2bb305b` | second attempt | includes the tool-error record |

The snapshot with tests would be refused again by push protection for the OpenRouter-shaped string in
`internal/testkit/record/record_test.go:22`, so the snapshot leaves tests out. Push protection was not bypassed.

### What a customer would trip on

Added to `deploy/action/README.md`: the fork must have Actions enabled before the workflows do anything; the gate
needs `contents: write` for the state ref and creates it on the first run; `action init` needs the model key on stdin
and never prompts; and a proof that never calls `run_test` successfully ends at the turn cap with an `unknown` check,
not a failure.

### Cleanup

Nothing was removed: the fork and PR #1 are the owner's demo and stay open. Left in place on
`prathameshk421/heimdall-fixture-pay`: environment `heimdall` with secret `HEIMDALL_LOG_KEY`, repository secret
`HEIMDALL_MODEL_KEY`, five `HEIMDALL_MODEL*` variables, `refs/heimdall/state`, branch `action-src`, the three
workflows on `main`, and PR #1 open with one comment.

### Not verified

- A proof that proves a finding. `run_test` never staged, so no candidate was proposed or proven in either attempt.
- The planted refund bug in the diff: not found, not proven, not refuted. It was not reached.
- The ENOSYS cause: `openat2` against `RestrictNamespaces`/`ProtectSystem`, or the seccomp profile, is not confirmed.
  The next run needs a sandbox self check that writes a file into `WorkDir`.
- `heimdall/fast` and `heimdall/proof` as `failure` or `action_required`: shadow mode posts `neutral` only.
- Enforce mode, the GitHub App, the ruleset and the `proof_required` failure path: not run.
- A fork pull request with no model key: the PR came from the same repository.
- The sweep (`schedule`) and `workflow_dispatch` paths of the gate: not run.
- Break-glass commands (A8): not built.

