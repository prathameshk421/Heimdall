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
