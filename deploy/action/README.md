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

## 1. Set up the environment and the keys (2 minutes)

```sh
heimdall action init -repo OWNER/REPO            # prints the steps, changes nothing
heimdall action init -repo OWNER/REPO -apply -model-key-stdin < path/to/model-key-file
```

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

Do not add steps to the proof job. Any process that runs as the runner user can read the model key (ACTION_MODE 3.2
rule 4).

## 4. Open a pull request

The trigger and proof runs start on the pull request. The gate starts when each one completes, posts the two checks,
writes the summary comment and records the run in the ref `refs/heimdall/state`. Each run builds heimdall from source
first: 44 seconds on `ubuntu-latest` in the AM16 live test. In that test the trigger run took 57 seconds and the proof
run 5 minutes 45 seconds (`docs/product/design/AM16_LIVE.md`). The time from install to the first summary is not
measured yet.

## Moving to enforce mode

Stay in shadow mode until the summaries match what your reviewers would say. Then set `mode: enforce`, run
`heimdall action init -repo OWNER/REPO -apply -enforce` to create the Heimdall GitHub App, and add a ruleset on the
default branch that requires `heimdall/fast` with that App as the expected source (ACTION_MODE 3.3).

## What to expect

- A fork or Dependabot pull request gets no model key. Its proof says `unknown` with that reason, never `pass`.
- A model endpoint that fails, a run that is cancelled or a proof that is missing makes the check `unknown`. It is
  never a pass.
- A change whose every path matches the policy's trivial globs skips the model at $0.

## Removing it

Delete the three workflow files, then remove the rest:

```sh
gh api -X DELETE repos/OWNER/REPO/environments/heimdall
gh secret delete HEIMDALL_MODEL_KEY --repo OWNER/REPO
gh variable delete HEIMDALL_MODEL --repo OWNER/REPO      # and the other HEIMDALL_MODEL_* variables
gh api -X DELETE repos/OWNER/REPO/git/refs/heimdall/state
```
