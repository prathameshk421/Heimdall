# Agent harness templates

Not live yet: nothing here is active in this repository. **Fixes are pending:** `docs/STAGE_1_FIXES.md` T101 to T104 change `pre-bash-guard.sh`, `golangci.yml`, `settings.json` and `opencode.json`, so apply those before installing. `make harness-install` (T16, T18) copies it into place at the repo root and replaces `MODULE` with the Go module path.

| Template | Installed as | Purpose |
|---|---|---|
| `AGENTS.md` | `AGENTS.md`, with `CLAUDE.md` symlinked to it | the builder's rules, loop and gotchas |
| `claude/settings.json` | `.claude/settings.json` | permissions and hook wiring for Claude Code |
| `claude/hooks/*.sh` | `.claude/hooks/` | pre-bash and pre-edit guards, post-edit format and vet, stop gate |
| `golangci.yml` | `.golangci.yml` | the lint rules behind standards sections 1 to 7, with rule ids in messages |
| `opencode/opencode.json` | each VM agent's harness directory (spec 6.9) | deny-by-default permissions for test-author, reviewers, resolver, profiler |

Verified Nov 4 without Go installed: the four hook scripts behaved as intended on sample inputs for allow and block cases, the ask path, the stop-loop guard and a missing toolchain (one bug found and fixed in the process), and both JSON files parse. **Not verified:** `golangci.yml` has never been run (Go is not installed yet) and must pass `golangci-lint config verify` and `make lint-selftest` in T16; `opencode.json` has never been run in OpenCode and the MCP tool names are unconfirmed (T03).
