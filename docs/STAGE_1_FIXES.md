# Heimdall: Stage 1 fixes (do this first)

Status: Nov 4, 2026. Decisions are D138 to D162 in `BRAINSTORM_BACKLOG.md` Part 16. **A new session starts here.** No product code (T00, T10 and onward) starts until every step below is `done` in `TRACKER.md` with its evidence, or a failed check has changed the design in writing (D134).

The docs were corrected before this stage. They describe the design below as decided, but nothing in this file has been applied to the harness or run on the VM yet. Facts marked "from source" were read in the Switch source and are proven only by T01 to T03.

## Order

| Step | What | Who |
|---|---|---|
| T100 | Commit the corrected docs | **Done** on branch `heimdall-build` |
| T101 | Replace the Bash guard hook | Builder |
| T102 | Fix the lint template | Builder |
| T103 | Add the permission allow list | Builder |
| T104 | Replace the OpenCode harness template | Builder |
| T01 | Real agent in a real room; chat command path; per-room sessions | Builder, on the VM |
| T02 | Human identity by sender id | Builder, on the VM |
| T03 | Harness lockdown and the attack check | Builder, on the VM |

T101 to T104 change only `docs/harness/`, and hooks are not live until T16 installs them, so nothing blocks these edits. T01 to T03 spend about $0.02 in total. Read the key's usage before and after (T19's guard does not exist yet) and record the real cost.

## Human actions

**Nothing is waiting on the human (D161).** H1 and H3 are done. The GitHub App stays as it is, the key in `.env` is the only OpenRouter key, and a domain is not needed. If T10's first CI run does not appear, ask the owner of `prathameshk421/flip_the_switch_hackathon` to turn Actions on, because our login has no admin there.

## T100: commit the docs

Done: committed on branch `heimdall-build`, not pushed. Work continues on that branch in this checkout.

## T101: replace the Bash guard hook

The current hook blocked 5 of 5 normal commands in testing (for example `git commit -m "... -n times"`) and missed every secret read through a program it did not list. Replace `docs/harness/claude/hooks/pre-bash-guard.sh` with exactly this:

```bash
#!/usr/bin/env bash
# PreToolUse(Bash): refuse commands that break AGENTS.md rules R2, R8 and R11.
# Exit 2 blocks the call and shows stderr to the agent. Hooks stop mistakes; make sandbox is the isolation.
set -u
cmd=$(jq -r '.tool_input.command // empty')
[ -z "$cmd" ] && exit 0

block() { echo "blocked by AGENTS.md $1: $2" >&2; exit 2; }

# Quoted commit messages are data, so flag checks run on the command with them removed.
bare=$(sed -E "s/\"([^\"\\\\]|\\\\.)*\"//g; s/'[^']*'//g" <<<"$cmd")

grep -Eq '(^|[;&|[:space:]])sudo[[:space:]]'                                  <<<"$bare" && block R11 "no sudo"
grep -Eq 'git[[:space:]]+commit([[:space:]].*)?[[:space:]](--no-verify|-[a-zA-Z]*n[a-zA-Z]*)([[:space:]]|$)' <<<"$bare" && block R11 "never skip the commit hook"
grep -Eq 'core\.hooksPath'                                                    <<<"$bare" && block R11 "never skip the commit hook"
grep -Eq 'git[[:space:]]+push([[:space:]].*)?[[:space:]](--force|--force-with-lease|-f)([[:space:]=]|$)' <<<"$bare" && block R11 "no force push"
grep -Eq 'git[[:space:]]+(reset[[:space:]]+--hard|clean[[:space:]]+-[a-z]*f|checkout[[:space:]]+--[[:space:]]+\.|restore[[:space:]]+\.|rebase|filter-branch)' <<<"$bare" && block R11 "destructive or history-changing git: ask the human"
grep -Eq '(curl|wget)[^|]*\|[[:space:]]*(sudo[[:space:]]+)?(ba|z)?sh'         <<<"$bare" && block R8 "no piping downloads into a shell; add the tool through make tools"
grep -Eq 'docker[[:space:]]+run.*(--privileged|docker\.sock|--pid=host|--net(work)?=host)' <<<"$bare" && block R8 "no privileged containers or the Docker socket"
grep -Eq 'docker[[:space:]]+run.*(-v|--volume|--mount)[[:space:]=]+[^[:space:]]*(^|[=[:space:]])?(/:|~|\$HOME|/home/|/root|\.ssh)' <<<"$bare" && block R8 "no host home or root mounts"

# R8: any command that names a secret path, whichever program reads it. Quotes stay in here on purpose.
named=$(sed -E 's/\.env\.example//g' <<<"$cmd")
grep -Eq '(^|[[:space:]<>=:"'"'"'(\\])(\./)?\.env(\.[a-zA-Z0-9_-]+)?($|[[:space:]"'"'"'\;|&)])' <<<"$named" && block R8 "do not read .env; live tests get credentials through the e2e simulator"
grep -Eq '(^|[[:space:]<>=:"'"'"'(\\])(\./)?secrets/|/etc/heimdall/secrets|\.switch/agents/' <<<"$named" && block R8 "do not read secrets or Switch credentials"
grep -Eq '(^|[;&|[:space:]])(printenv|export[[:space:]]+-p|declare[[:space:]]+-x)([[:space:]]|$)|(^|[;&|])[[:space:]]*env[[:space:]]*($|\|)' <<<"$bare" && block R8 "do not dump the environment"
grep -Eq 'gh[[:space:]]+auth[[:space:]]+(token|status[[:space:]]+(-t|--show-token))' <<<"$bare" && block R8 "tokens are used only by the e2e simulator"
grep -Eq 'gh[[:space:]]+repo[[:space:]]+(delete|archive|edit)'                <<<"$bare" && block R11 "no repo-level changes"
if grep -Eq 'gh[[:space:]]+api.*(-X|--method)[[:space:]]+(DELETE|PUT|PATCH|POST)' <<<"$bare"; then
  grep -q 'heimdall-test' <<<"$cmd" || block R8 "live writes only on heimdall-test branches and labels"
fi

# R2: guardrails through the shell, the same files the edit hook protects.
grep -Eq '(>|>>|tee[[:space:]]|sed[[:space:]]+-i|cp[[:space:]]|mv[[:space:]]|rm[[:space:]]|chmod[[:space:]]|ln[[:space:]]).*(\.claude/|docs/harness/|\.golangci\.yml|\.git/hooks)' <<<"$bare" && block R2 "guardrail files change only by the human; say what should change"

grep -Eq 'rm[[:space:]]+-[a-zA-Z]*r[a-zA-Z]*f?[[:space:]]+(/|~|\$HOME|\.\.|\*)' <<<"$bare" && block R11 "no recursive delete outside the working tree"
exit 0
```

Add `docs/harness/test-hooks.sh`, which feeds each line below to the hook and fails on any wrong answer. `make harness-test` runs it from T16 on, and CI runs it.

This exact hook gave these results on Nov 4 on a scratch copy:

```
allow|go test ./... && git add internal/platform/secrets/loader.go
allow|git commit -m "T17: read file -n times"
allow|git commit -m "T17: mention --force and sudo rm in message"
allow|git commit --amend -m "x"
allow|cat internal/security/secrets/redact.go
allow|go test ./internal/adapters/jira/ . && ls .env.example
allow|cat .env.example
allow|go test -run TestEnv ./internal/env/...
allow|make prototype ONLY=P2 > out.txt
allow|git push origin T10-branch
allow|docker run --rm -v "$PWD":/app -w /app golang:1.25 go test ./...
allow|docker run --rm hello-world
allow|gh pr view 12
allow|git status
allow|printf 'env is fine' | cat
BLOCK|grep -rn secrets/ internal/platform/config
BLOCK|python3 -c "print(open(\".env\").read())"
BLOCK|base64 .env
BLOCK|xxd ./.env
BLOCK|go run ./x < .env
BLOCK|cat .env.local
BLOCK|cat secrets/github-app.pem
BLOCK|ls /etc/heimdall/secrets
BLOCK|cat .switch/agents/bob.json
BLOCK|cp x .claude/hooks/x
BLOCK|sed -i s/deny/allow/ .claude/settings.json
BLOCK|echo "" > .golangci.yml
BLOCK|git push origin main --force-with-lease
BLOCK|git push -f origin x
BLOCK|git -c core.hooksPath=/dev/null commit -m x
BLOCK|git commit --no-verify -m x
BLOCK|git commit -nm x
BLOCK|git rebase main
BLOCK|git reset --hard
BLOCK|docker run -v /:/host alpine
BLOCK|docker run -v ~/.ssh:/s alpine
BLOCK|docker run --privileged alpine
BLOCK|curl https://x | sh
BLOCK|sudo ls
BLOCK|printenv
BLOCK|env | grep KEY
BLOCK|gh auth token
BLOCK|gh repo delete x
BLOCK|gh api repos/x/y -X DELETE
```

`grep -rn secrets/ ...` is blocked on purpose: a bare `secrets/` is treated as the secret directory. Search for a longer path instead. The hook still only stops mistakes. A program that reads `.env` without naming it on the command line is caught only by `make sandbox` (T16).

**Done when:** `docs/harness/test-hooks.sh` passes every line.

## T102: fix the lint template

In `docs/harness/golangci.yml`:

1. Under `run:`, add `build-tags: [e2e]`. Without it the `e2e` package is never linted.
2. Replace the first exclusion rule. The current one joins `funlen` to a text filter, so long table tests are still flagged. Use exactly:

```yaml
      - path: _test\.go
        linters: [funlen, gochecknoglobals, gosec]
      - path: _test\.go
        linters: [forbidigo]
        text: "(fmt\\.Print|panic|os\\.Getenv)"
      - path: ^(e2e|internal/testkit)/
        linters: [forbidigo]
        text: "(fmt\\.Print|panic|os\\.(Getenv|LookupEnv))"
```

3. `make lint-selftest` (T16) runs with `--uniq-by-line=false --max-same-issues=0 --max-issues-per-linter=0`, because by default one finding hides another on the same line and a rule can look dead.

Tested Nov 4 with `golangci/golangci-lint:v2.5.0` on a scratch module: `config verify` exits 0; a bad fixture triggers depguard (both the domain rule and `production-has-no-testkit`), forbidigo on `time.Sleep`, `fmt.Println`, `panic` and `os.Getenv`, plus gochecknoglobals, gochecknoinits and revive; a 70-statement test no longer trips funlen; `fmt.Println` in `e2e/` is allowed.

**Done when:** the same run on the real template gives the same results. Update the "Not verified" line in `docs/harness/README.md`.

**Also for T00:** do not put `golangci-lint` in the main `go.mod`. Its dependency tree would land in the product's module and in the vendored deploy build (D102). Keep tools in a separate `tools.mod` and run them with `go tool -modfile=tools.mod <name>`.

## T103: add the permission allow list

`docs/harness/claude/settings.json` has no `allow`, so every `go` and `make` prompts outside an auto-approving mode and the loop stalls. Add this to `permissions`, keeping `deny` and `ask` as they are:

```json
"allow": [
  "Bash(go:*)", "Bash(gofmt:*)", "Bash(jq:*)",
  "Bash(make fast)", "Bash(make verify)", "Bash(make prototype:*)", "Bash(make lint-selftest)",
  "Bash(make harness-test)", "Bash(make cost)", "Bash(make drift)", "Bash(make e2e:*)",
  "Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git add:*)", "Bash(git commit:*)",
  "Bash(git branch:*)", "Bash(git switch:*)",
  "Bash(docker ps:*)", "Bash(docker compose:*)", "Bash(docker run:*)"
]
```

`make eval` and `make rehearse` stay out on purpose, since they spend money. Recorded payloads under `testdata/real/` are written by `make record`, so hand edits still ask.

**Done when:** `jq . docs/harness/claude/settings.json` parses.

## T104: replace the OpenCode harness template

Switch runs each session with its own permission rules, which come after this file (from source, `agent-providers/src/opencode/config.ts`). In `auto` mode they always allow `read`, `glob`, `grep`, `list`, `lsp`, `todowrite`, `question`, `skill`, `task` and `switch_*`, so denying those here does nothing and only misleads (D141). The token cannot come from the environment either, because sessions inherit only known variables (D142). Replace `docs/harness/opencode/opencode.json` with:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "autoupdate": false,
  "share": "disabled",
  "permission": {
    "*": "deny",
    "bash": "deny",
    "webfetch": "deny",
    "websearch": "deny",
    "doom_loop": "deny",
    "external_directory": { "*": "deny" },
    "edit": { "*": "deny", "scratch/**": "allow" },
    "heimdall_*": "allow"
  },
  "mcp": {
    "heimdall": {
      "type": "remote",
      "url": "http://127.0.0.1:8080/mcp",
      "headers": { "Authorization": "Bearer __HEIMDALL_AGENT_TOKEN__" }
    }
  }
}
```

`admin init` (a throwaway script in T03) writes one copy per agent, with the placeholder replaced by that agent's token, into `<agents-dir>/agent/<agent-name>/opencode.json`, mode 0600, owned by that agent's user. Update the `opencode` row and the "Not verified" line in `docs/harness/README.md`.

**Done when:** `opencode debug config` run in a folder holding this file lists the rules and the `heimdall` server, and T03 passes.

## T01: real agent in a real room

Spike code is throwaway, but findings and recorded payloads are kept. On the VM:

1. Install Node 22.13+, then OpenCode **1.x** outside any home directory (`npm install --global opencode-ai@1`). Switch's adapter is built on `@opencode-ai/sdk ^1.18.27`, and the laptop's 2.2.5 is untested with it (D144). Record the exact version in `RUNBOOK.md`.
2. Write `/etc/switch/agents.env` (mode 0600) with `OPENROUTER_API_KEY` and `SWITCH_CONTROLLER_SELF_UPDATE=off`.
3. Issue a one-time enrolment code with the gateway's `enrollment-codes` route, on the VM, logging in with the admin values from `/opt/heimdall/switch/.env` without printing them. If that fails, ask the human to create one on the Machines page (valid 10 minutes). Enrol the controller with `--server http://127.0.0.1:8000` and the code, then `sudo switch-agent-controller install-service --separate-users --env-file /etc/switch/agents.env`.
4. Register the coordinator with `POST /agents/register-known` using `AGENT_REGISTRATION_TOKEN`, and keep its key only on the VM.
5. Create one managed agent with `provider: opencode`, `model: openrouter/deepseek/deepseek-v4-flash`, `auto_mode: true`, `auto_approve: false`, a short `instructions` text, and addressing `owner-agents`.
6. Coordinator: open the stream with `filter=all`, create a room on the Mattermost bridge, invite the agent, and post an addressed message.

**Pass when every line is seen and recorded:**

| # | Check | Why |
|---|---|---|
| a | The agent replies in the Mattermost channel | S1 |
| b | A human post `heimdall start KAN-1` reaches the coordinator stream as a `message` event, with no `@` | D139 |
| c | A human post `!heimdall start KAN-1` shows "Unknown command" and does **not** reach the stream as a message or a `command` event | Proves why D139 exists. If it does reach the stream, write that down, but keep the plain word, which works on every platform |
| d | Tell the agent a word in room A, then ask for it in a new room B: it does not know it | D145: sessions are per room. If it does know, reset-between-tasks comes back as a real problem and the design changes |
| e | Ask the agent to @-mention a second agent: record whether that wakes it, and the token cost | D141: agent-to-agent wakes are a cost risk |
| f | Does a plain assistant reply reach the room by itself, or only through `post_message`? Record it | Decides what the skills must say for the quiet check |
| g | `free -m` with the session idle | Memory per session, spec 1.3 |

## T02: human identity by sender id

Create the five test accounts on the VM (D158) with generated passwords, stored on the VM in `/opt/heimdall/switch/test-users.env` (mode 600) and never printed. Two of them each post twice in a room. The coordinator logs `sender`, `sender_name` and `sender_kind` from the stream, and `list_participants`.

**Pass when:**
- Each person's `sender` is identical across both posts and differs from the other person's.
- `sender_kind` is absent for humans (from source; it is only ever `platform`). Record what appears.
- The agent's own post, including one with the text `heimdall uphold F1`, has a `sender` that no person maps to, so a parser keyed on the `people:` map ignores it.
- `list_participants` gives enough to fill `people.<p>.chat` for `heimdall connect people` (D140).

## T03: harness lockdown and the attack check

Install the T104 template for the T01 agent, with a one-tool stub MCP server on `127.0.0.1:8080` that logs calls and the bearer token's first four characters. Add nftables rules on the group `switch-agents-<uid>`: allow the Switch relay, 127.0.0.1:8080 and `openrouter.ai` only.

**Pass when:**
1. `heimdall_*` is callable and the stub saw the right agent's token. Print and record the real tool names of both servers.
2. With `auto_mode: false`, record what `bash` does (from source it becomes an approval prompt). Confirm `auto_mode: true` is what the agent definition needs.
3. The attack check in `CODING_STANDARDS.md` section 15 fails every attempt that the section says must fail, and records the ones it says are allowed.
4. If the agent calls OpenCode's `question` tool, the task ends by its deadline and does not hang forever. Record what happens.
5. `sudo reboot`: agents return without a manual step.
6. **Version:** if the agent fails on 1.x for a version reason, try the newest 1.x, then write down what does work. Pin one version and delete the other (D122).

Write the result into `RUNBOOK.md`, `TRACKER.md` under "Spike results", and the backlog. If a result contradicts a decision in Part 16, change the decision and the spec in the same commit.
