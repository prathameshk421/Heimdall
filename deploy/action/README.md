# Heimdall Action mode: install in under 10 minutes

Heimdall Action mode runs on GitHub Actions alone: no server and no database. Three workflows check each pull request
and post two check runs (`heimdall/fast`, `heimdall/proof`) and one summary comment. Start in shadow mode. Shadow
mode posts `neutral` checks that say what would block, and blocks nothing. The design is in
`docs/product/design/ACTION_MODE.md`.

## What you need

- Admin rights on the repository. Creating the environment `heimdall` needs them.
- The `gh` CLI logged in as that admin, and a `heimdall` binary (`CGO_ENABLED=0 go build ./cmd/heimdall`).
- A model key for any OpenAI-compatible endpoint. Use a dedicated key with a spend limit at the provider: anyone who
  can push a branch to the repository can read a repository secret (ACTION_MODE 3.5).

On a fork, turn Actions on first. A fresh fork has them off, and every run is skipped without a message:

```sh
gh api -X PUT repos/OWNER/REPO/actions/permissions -F enabled=true -f allowed_actions=all
```

The repository also needs the action at the commit you pin. `uses:` takes a path in a repository GitHub can read, so a
pinned SHA must be on a branch of that repository. A fork of the customer's repository is enough.

## 1. Set up the environment and the keys (2 minutes)

```sh
heimdall action init -repo OWNER/REPO            # prints the steps, changes nothing
heimdall action init -repo OWNER/REPO -apply -model-key-stdin < path/to/model-key-file
```

The first run of `heimdall action init` on a repository the login administers takes about 7 seconds. It prints one
`done <step>` line per step and ends with `heimdall action init: done`. Without admin it stops at step 1 with
`gh: Must have admin rights to Repository. (HTTP 403)`.

`init` creates environment `heimdall`, which only the default branch may deploy to. It also puts a new random log key
in that environment and sets the model key as the repository secret `HEIMDALL_MODEL_KEY`. The key goes from the file to
`gh` on stdin and is never printed. Run it again safely: an existing log key is kept.

Then set the model variables (no secret here):

```sh
gh variable set HEIMDALL_MODEL_BASE_URL --repo OWNER/REPO --body https://openrouter.ai/api/v1
gh variable set HEIMDALL_MODEL --repo OWNER/REPO --body deepseek/deepseek-v4-flash
gh variable set HEIMDALL_MODEL_PRICE_INPUT --repo OWNER/REPO --body 0.50          # USD per million tokens
gh variable set HEIMDALL_MODEL_PRICE_CACHED_INPUT --repo OWNER/REPO --body 0.50
gh variable set HEIMDALL_MODEL_PRICE_OUTPUT --repo OWNER/REPO --body 2.00
```

The prices are required. Without them the cost cap cannot see the spend, and the proof step refuses to start.

## 2. Add the policy (1 minute)

Add an `action` area to `heimdall.yaml` at the repository root. Heimdall reads the policy at the merge base, never from
the pull request. An absent area means shadow mode with a $0.50 cap per PR.

```yaml
action: { mode: shadow, budget: { per_pr_usd: 0.50, monthly_usd: 50 } }
```

## 3. Copy the three workflows (3 minutes)

Copy `heimdall-trigger.yml`, `heimdall-proof.yml` and `heimdall-gate.yml` from `deploy/action/workflows/` into
`.github/workflows/` on the default branch. In each file, replace the `0000...` in every line marked `# heimdall-pin`
with the full Heimdall commit SHA you install. Pin a full SHA, never a tag or a branch.

The gate must be on the default branch. GitHub starts `workflow_run` and `schedule` workflows only from there, and only
default-branch jobs reach the environment `heimdall`. Merge the workflows through your usual review.

The gate writes `refs/heimdall/state` and creates that ref on its first run. It needs `contents: write` for it, which
the file already asks for. A token without that permission stops the gate before it posts anything.

Do not add steps to the proof job. Any process that runs as the runner user can read the model key (ACTION_MODE 3.2
rule 4).

## 4. Open a pull request

The trigger and proof runs start on the pull request. The gate starts when each one completes, posts the two checks,
writes the summary comment and records the run in the ref `refs/heimdall/state`. Each run builds heimdall from source
first: 44 seconds on `ubuntu-latest` in the AM16 live test. In the AM16b run on a repository the login administers,
the first summary comment came **3 minutes 10 seconds** after the first `action init` command: 54 seconds of install,
6 seconds of variables, then the trigger run (54 s) and the first gate run (1 min 6 s). The proof took 3 minutes 15
seconds and its own gate run updated the same comment (`docs/product/design/AM16_LIVE.md`).

The proof job ends green whenever its step wrote a result. Green means the job ran, not that the change passed. The
job summary of the proof run shows the outcome, its reason, the turns and the cost, and in shadow mode what enforce
mode would block. The `heimdall-proof` artifact also holds `heimdall-proof-transcript.json`: a bounded record of each
model turn (tool names and sizes, request size, nudges, finish reason, the first 2 KB of text, and the error of any
tool call that failed), so a proof that ends `unknown` explains itself. The gate reads only `heimdall-proof.json`.

There is one summary comment per pull request, not one per run. The gate finds it by its own marker and edits it, so a
push replaces the old text instead of adding a comment.

`analyzers` and `memory_rules` read `unknown` on a repository with no code-scanning alerts and no rule cards at the
merge base. That is expected and never a pass. The monthly budget row also reads unknown: Action mode has no budget
file, and the per-PR cap is what holds.

## Moving to enforce mode

Stay in shadow mode until the summaries match what your reviewers would say. Then set `mode: enforce`, run
`heimdall action init -repo OWNER/REPO -apply -enforce` to create the Heimdall GitHub App, and add a ruleset on the
default branch that requires `heimdall/fast` with that App as the expected source (ACTION_MODE 3.3).

## What to expect

- A fork or Dependabot pull request gets no model key. Its proof says `unknown` with that reason, never `pass`.
- A model endpoint that fails, a run that is cancelled or a proof that is missing makes the check `unknown`. It is
  never a pass.
- A proof that never gets a working test run ends at the turn cap (40 turns) and posts `unknown` with the reason
  `turn_cap`. Its transcript names the tool and the error that failed. That is a failure of the proof, not of the
  change, and it never becomes a pass.
- A change whose every path matches the policy's trivial globs skips the model at $0.

## Removing it

Delete the three workflow files, then remove the rest:

```sh
gh api -X DELETE repos/OWNER/REPO/environments/heimdall
gh secret delete HEIMDALL_MODEL_KEY --repo OWNER/REPO
gh variable delete HEIMDALL_MODEL --repo OWNER/REPO      # and the other HEIMDALL_MODEL_* variables
gh api -X DELETE repos/OWNER/REPO/git/refs/heimdall/state
```
