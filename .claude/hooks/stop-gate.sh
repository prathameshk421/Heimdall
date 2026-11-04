#!/usr/bin/env bash
# Stop: the agent may not end a turn that leaves red checks or an unrecorded task (R1, R9).
# stop_hook_active guards against an endless loop: the second stop is allowed through.
set -u
in=$(cat)
[ "$(jq -r '.stop_hook_active // false' <<<"$in")" = "true" ] && exit 0
cd "$CLAUDE_PROJECT_DIR" || exit 0

changed=$(git status --porcelain -uall 2>/dev/null | awk '{print $2}')
grep -q '\.go$' <<<"$changed" || exit 0          # nothing in Go changed: nothing to gate

if [ -f Makefile ] && grep -q '^fast:' Makefile; then
  out=$(make fast 2>&1) || { printf 'make fast is red. Fix it before stopping (AGENTS.md R1):\n%s\n' "$(tail -40 <<<"$out")" >&2; exit 2; }
fi

if ! grep -q 'docs/PROGRESS.md' <<<"$changed"; then
  echo "Go files changed but docs/PROGRESS.md did not. Record the task state, evidence and anything not verified (AGENTS.md R9) before stopping." >&2
  exit 2
fi
exit 0
