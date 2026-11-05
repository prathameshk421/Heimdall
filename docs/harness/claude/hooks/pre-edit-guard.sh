#!/usr/bin/env bash
# PreToolUse(Edit|Write|MultiEdit): guardrails the agent must not rewrite alone (R2).
# deny = exit 2. ask = the human is prompted, and an unattended run stops there.
set -u
in=$(cat)
f=$(jq -r '.tool_input.file_path // empty' <<<"$in")
[ -z "$f" ] && exit 0
# Normalise first: ./ and ../ segments must not step around the patterns below.
f=$(realpath -m -- "$f")
rel=${f#"$(realpath -m -- "$CLAUDE_PROJECT_DIR")"/}

case "$rel" in
  .claude/*|docs/harness/*|.git/*|secrets/*|.env|.env.*|/etc/*|"$HOME"/.ssh/*)
    echo "blocked by AGENTS.md R2/R8: $rel is a guardrail or secret; only the human edits it" >&2
    exit 2 ;;
  .golangci.yml|AGENTS.md|CLAUDE.md|e2e/scenarios/*|e2e/stories/*|testdata/real/*|.github/workflows/*|Makefile|tools.mod|tools.sum|.gitleaks.toml|.gitleaksignore|scripts/gotest.sh|scripts/run-tagged.sh|scripts/testguard.sh|scripts/pre-commit|scripts/test-scripts.sh|internal/archcheck/*|internal/lintselftest/*|testdata/lint/*)
    jq -n --arg r "AGENTS.md R2: $rel defines what 'passing' means. Weakening it needs a human yes. Say why in the commit." \
      '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"ask",permissionDecisionReason:$r}}'
    exit 0 ;;
esac

# R4: production code may not mention the test kit.
case "$rel" in
  *_test.go|internal/testkit/*|cmd/heimdall-testagent/*|testdata/*) ;;
  *.go)
    if jq -r '.tool_input.content // .tool_input.new_string // empty' <<<"$in" | grep -q 'internal/testkit'; then
      echo "blocked by AGENTS.md R4: production code must not import internal/testkit" >&2
      exit 2
    fi ;;
esac
exit 0
