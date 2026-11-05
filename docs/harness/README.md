# Agent harness templates

Not live yet: nothing here is active in this repository. T101 to T104 (`docs/STAGE_1_FIXES.md`) are applied. `make harness-install` (T16, T18) copies it into place at the repo root and replaces `MODULE` with the Go module path.

| Template | Installed as | Purpose |
|---|---|---|
| `AGENTS.md` | `AGENTS.md`, with `CLAUDE.md` symlinked to it | the builder's rules, loop and gotchas |
| `claude/settings.json` | `.claude/settings.json` | permissions and hook wiring for Claude Code |
| `claude/hooks/*.sh`, `test-hooks.sh` | `.claude/hooks/` (the test script runs from `make harness-test`) | pre-bash and pre-edit guards, post-edit format and vet, stop gate |
| `golangci.yml` | `.golangci.yml` | the lint rules behind standards sections 1 to 7, with rule ids in messages |
| `opencode/opencode.json` | each VM agent's harness directory (spec 6.9) | deny-by-default permissions for test-author, reviewers, resolver, profiler |

Verified Nov 4 without Go installed: the four hook scripts behaved as intended on sample inputs for allow and block cases, the ask path, the stop-loop guard and a missing toolchain (one bug found and fixed in the process), and both JSON files parse. `test-hooks.sh` runs 44 cases against `pre-bash-guard.sh`, all correct (Nov 4). `golangci.yml` passed `golangci-lint v2.5.0 config verify` and a scratch-module run (Nov 4). **Not verified:** `make lint-selftest` on the real module (T16); the attack check re-run at G3. `opencode.json` was verified on the VM under OpenCode 1.19.5 in T03; the MCP tool names are `heimdall_<tool>`.
