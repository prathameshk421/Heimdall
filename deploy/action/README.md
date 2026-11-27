# Heimdall GitHub Action mode: install in under 10 minutes

Heimdall Action mode runs on GitHub Actions alone: no server and no database. Three workflows check each pull request
and post two check runs (`heimdall/fast`, `heimdall/proof`) and one summary comment. Start in shadow mode. Shadow
mode posts `neutral` checks that say what would block, and blocks nothing. The design is in
`docs/product/design/ACTION_MODE.md`.

## What you need

- Admin rights on the repository. Creating the environment `heimdall` needs them.
- The [`gh` CLI](https://cli.github.com) logged in as that admin, and Go 1.27 to build `heimdall`.
- A local checkout of the repository.
- A model key for any OpenAI-compatible endpoint, in a file. Use a dedicated key with a spend limit at the provider:
  anyone who can push a branch to the repository can read a repository secret (ACTION_MODE 3.5).

On a fork, check that Actions are enabled before you add the workflows. A repository with Actions off accepts the
workflow files and then starts no run:

```sh
gh api -X PUT repos/OWNER/REPO/actions/permissions -F enabled=true -f allowed_actions=all
```

## 1. Build the CLI (1 minute)

Build from a clean clone. The binary then knows its own commit and pins the workflows to it:

```sh
git clone https://github.com/prathameshk421/Heimdall.git && cd Heimdall
CGO_ENABLED=0 go build -o heimdall ./cmd/heimdall
./heimdall version          # prints the full commit SHA the workflows will pin
```

The CLI builds on Linux, macOS and Windows.

## 2. Run init (2 minutes)

First the plan. It runs no command and writes no file:

```sh
./heimdall action init -repo OWNER/REPO -dir PATH/TO/CHECKOUT
```

Then apply it:

```sh
./heimdall action init -repo OWNER/REPO -dir PATH/TO/CHECKOUT -apply -model-key-stdin < PATH/TO/KEY_FILE
```

`init` prints one `done <step>` line per step and ends with `heimdall action init: done`. It does this:

| Step | What happens |
|---|---|
| environment, branch policy | Creates the environment `heimdall`, which only the default branch may deploy to |
| log key | Puts a new random 32-byte key in that environment. An existing key is kept |
| model key | Sets the repository secret `HEIMDALL_MODEL_KEY` from stdin. The key goes to `gh` on stdin and is never printed |
| model settings | Sets `HEIMDALL_MODEL_BASE_URL`, `HEIMDALL_MODEL` and the three `HEIMDALL_MODEL_PRICE_*` variables |
| workflows | Writes `.github/workflows/heimdall-{trigger,proof,gate}.yml` into the checkout, pinned to the full Heimdall SHA |
| policy | Writes a shadow-mode `heimdall.yaml` when the repository has none. An existing file is never changed |

The default model is `deepseek/deepseek-v4-flash` on OpenRouter, at $0.50 / $0.50 / $2.00 per million input, cached
input and output tokens. For another model, pass all of these (the cost cap needs the prices):

```sh
./heimdall action init -repo OWNER/REPO -dir PATH/TO/CHECKOUT -apply -model-key-stdin < KEY_FILE \
  -model MODEL_ID -model-base-url https://your-endpoint/v1 \
  -price-input 0.50 -price-cached-input 0.50 -price-output 2.00
```

Run `init` again at any time. It keeps the log key and leaves files that are up to date. Without admin it stops at the
first step with `gh: Must have admin rights to Repository. (HTTP 403)`. A binary built from a checkout with local
changes does not know its commit: commit or stash them, or pass `-ref <full SHA>`. Pin a full SHA, never a tag or a
branch.

The policy that `init` writes, which you can edit:

```yaml
action:
  mode: shadow
  budget:
    per_pr_usd: 0.50
    monthly_usd: 50
```

Heimdall reads the policy at the merge base, never from the pull request. An absent `action` area means shadow mode with
a $0.50 cap per PR.

## 3. Merge the files (your review)

In the checkout, commit the three workflows and `heimdall.yaml` on a branch and merge them through your usual review.
The gate must be on the default branch: GitHub starts `workflow_run` and `schedule` workflows only from there, and only
default-branch jobs reach the environment `heimdall`.

The gate writes `refs/heimdall/state` and creates that ref on its first run. It needs `contents: write` for it, which
the workflow already asks for. Do not add steps to the proof job: any process that runs as the runner user can read
the model key (ACTION_MODE 3.2 rule 4).

<details>
<summary>Without <code>-dir</code>: copy the workflows by hand</summary>

Copy `heimdall-trigger.yml`, `heimdall-proof.yml` and `heimdall-gate.yml` from `deploy/action/workflows/` into
`.github/workflows/`. In each file, replace the `0000...` in every line marked `# heimdall-pin` with the full Heimdall
commit SHA you install.

</details>

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

### Protecting `refs/heimdall/*` (recommended repository ruleset)

Every event log and head file on `refs/heimdall/state` is authenticated with HMAC-SHA256 (`HEIMDALL_LOG_KEY`), so
tampering with file contents is detected and refused. However, a MAC alone cannot stop someone with repository write
access from force-pushing an older, validly signed commit or deleting the ref (whole-ref rollback).

To prevent whole-ref rollback and deletion, add a GitHub **repository ruleset** targeting `refs/heimdall/*`:

- **Target refs:** include pattern `refs/heimdall/*`
- **Rules:** enable **Restrict creations**, **Restrict updates** (blocks pushes), **Restrict deletions**, and **Block force pushes**
- **Bypass list:** allow **only** the Heimdall GitHub App (installed via `heimdall action init -apply -enforce`) so
  only the privileged gate job can create or advance `refs/heimdall/*`, and no human or third-party token can
  force-push, roll back, or delete state refs.

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
