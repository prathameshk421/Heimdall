#!/usr/bin/env bash
# PostToolUse(Edit|Write|MultiEdit): format and vet the package right after an edit, so the
# agent sees its own mistake while the context is fresh. Fast checks only; the full set runs in the Stop hook.
set -u
# A hook does not run in a login shell, so the toolchain that make tools installs is not on PATH yet.
export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"
f=$(jq -r '.tool_input.file_path // empty')
case "$f" in *.go) ;; *) exit 0 ;; esac
cd "$CLAUDE_PROJECT_DIR" || exit 0

command -v go >/dev/null || { echo "go toolchain missing: run make tools (T00)" >&2; exit 2; }

gofmt -w "$f" 2>&1 || exit 2
pkg=$(dirname "${f#"$CLAUDE_PROJECT_DIR"/}")
out=$(go vet "./$pkg/..." 2>&1) || { printf 'go vet failed after your edit:\n%s\n' "$out" | head -40 >&2; exit 2; }
exit 0
