---
name: heimdall-setup
description: Install, configure and troubleshoot Heimdall, the self-hosted agent harness that reviews high-volume pull requests, writes the missing CI tests, and lets a finding block a merge only when a failing test proves it. Use when the user asks to set up Heimdall on a GitHub repository (GitHub Action mode) or on their own Linux server (team mode), to connect chat, Jira or a model, or to fix a Heimdall install.
---

# Set up Heimdall

You set up Heimdall for the user from zero, with the real `heimdall` CLI. Show each plan before you apply it, run
every check, and report what you verified.

## Rules

1. **Never see a secret.** Do not print, paste, log or commit a key or token. The user puts each secret in a file, and
   you pass the file on stdin (`-model-key-stdin < file`) or by path. Never put a secret in a command argument.
2. **Plan, then apply.** Run every `heimdall action init` without `-apply` first, and show the user the plan.
3. **The default branch belongs to the user.** Write files on a new branch and open a pull request. Do not push to the
   default branch yourself.
4. **Say what you did not verify.** A check you could not run is `not verified`, with the reason. Never write
   "should work".

## Step 1: pick the mode

Ask the user, unless the request already says.

| Mode | Pick it when | Needs |
|---|---|---|
| **GitHub Action mode** (start here) | One repository on GitHub. Reviews and proofs run in the repo's own CI | Admin on the repo, `gh`, Go 1.27, a model key |
| **Team mode** (self-hosted) | A team wants the full loop: Jira ticket to red tests, independent AI reviewers, chat, a merge gate | A Linux server, Docker, PostgreSQL, a GitHub App, a model key; Jira and chat are optional |

## Step 2: get the CLI

Build it from a clean clone, so the binary knows its own commit and pins the workflows to it:

```bash
git clone https://github.com/prathameshk421/Heimdall.git && cd Heimdall
CGO_ENABLED=0 go build -o heimdall ./cmd/heimdall
./heimdall --help
```

The CLI builds on Linux, macOS and Windows. The proof sandbox and the server run on Linux only.

## Step 3A: GitHub Action mode

### Check the prerequisites

```bash
gh auth status
gh api repos/OWNER/REPO --jq .permissions.admin        # must print true
gh api repos/OWNER/REPO/actions/permissions --jq .enabled   # must print true
```

If Actions are off (common on a fork), the user can turn them on with
`gh api -X PUT repos/OWNER/REPO/actions/permissions -F enabled=true -f allowed_actions=all`. Ask first.

Ask the user for: a local checkout of the target repository, and a file that holds a **dedicated** model key with a
spend limit at the provider. Anyone who can push a branch can read a repository secret, so the key must be one they
can revoke.

### Show the plan

```bash
./heimdall action init -repo OWNER/REPO -dir PATH/TO/CHECKOUT
```

The plan lists every `gh` call (environment, branch policy, log key, model key, five model variables) and the files it
writes (three workflows and `heimdall.yaml`). The default model is `deepseek/deepseek-v4-flash` on OpenRouter. For
another OpenAI-compatible model, add `-model`, `-model-base-url`, `-price-input`, `-price-cached-input` and
`-price-output` (USD per million tokens). The cost cap needs the prices.

### Apply

```bash
./heimdall action init -repo OWNER/REPO -dir PATH/TO/CHECKOUT -apply -model-key-stdin < PATH/TO/KEY_FILE
```

It prints one `done <step>` line per step and ends with `heimdall action init: done`. A second run is safe: it keeps the
log key and changes no file that is up to date.

### Ship the workflows

The gate starts only from the default branch. In the checkout, put the files on a branch and open a pull request:

```bash
git switch -c heimdall-install
git add .github/workflows/heimdall-*.yml heimdall.yaml
git commit -m "Add Heimdall in shadow mode"
gh pr create --fill
```

The user reviews and merges it.

### Verify

Open any small pull request after the merge. Then:

```bash
gh pr checks PR_NUMBER --repo OWNER/REPO
```

Expect `heimdall/fast` and `heimdall/proof`, and one comment titled with the proof summary. In shadow mode the checks
are `neutral` and block nothing. The first run builds Heimdall from source, which takes about a minute.

### Turn on blocking (later)

Shadow mode first. After a week, read the report:

```bash
./heimdall action digest -repo OWNER/REPO
```

If the user agrees with what would have been blocked, set `mode: enforce` in `heimdall.yaml` through a pull request.
For a dedicated GitHub App, run `init` again with `-enforce` and follow its manifest step.

## Step 3B: team mode (self-hosted)

Follow `docs/SETUP.md` in the Heimdall repo. The short path, from the clone:

```bash
make setup                         # private workspace ~/.config/heimdall with a starter heimdall.toml
cp .env.example .env               # the user fills in their own values; never read this file
make secrets-local SECRETS_DIR=$HOME/.config/heimdall/secrets
heimdall admin init --config $HOME/.config/heimdall/heimdall.toml --secrets-dir $HOME/.config/heimdall/secrets
heimdall connect -list --config $HOME/.config/heimdall/heimdall.toml
heimdall connect people --name NAME --sender SWITCH_SENDER_ID --github LOGIN --agent NAME-reviewer --roles member,approver
DEPLOY_CONFIG=$HOME/.config/heimdall/heimdall.toml DEPLOY_SECRETS_DIR=$HOME/.config/heimdall/secrets make deploy
```

`heimdall admin init` lists the secret files that are still missing, by name only. `make secrets-local` writes one
`0600` file per key and prints no value. Chat (Slack, Discord, Telegram, Mattermost, Teams) runs through Switch: see
`docs/SETUP.md` step 6 and `deploy/agents/README.md`.

Verify:

```bash
heimdall connect -check --config $HOME/.config/heimdall/heimdall.toml
heimdall doctor --strict --config $HOME/.config/heimdall/heimdall.toml
curl -sS http://127.0.0.1:8080/healthz          # on the server: {"status":"ok"}
```

Then a live check on each part the user turned on: a test pull request gets the summary comment and the checks, a
mention of the coordinator in each chat room gets a reply, and a test Jira ticket moved to In Progress opens a room.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `gh: Must have admin rights to Repository. (HTTP 403)` | The `gh` login is not an admin of the repo. Use an admin login |
| Workflows added, but no run starts | Actions are off for the repo (often a fork). See the prerequisites |
| The gate never runs | The workflows are not on the default branch yet. `workflow_run` starts only from there |
| The proof step refuses to start | A model price variable is missing. Run `init -apply` again, with the prices for your model |
| `-ref ... is not a full 40-character commit SHA` | Pass a full SHA, never a tag or a branch. Or build `heimdall` from a clean clone |
| `this binary was not built from a clean checkout` | The clone had local changes. Commit or stash them, rebuild, or pass `-ref` |
| A chat command gets no answer | The sender is not mapped. Run `heimdall connect people` (team mode) |

## Report

End with: the mode, each step and its result, the checks you ran with their output, and a `not verified` list with the
reason for each item.
