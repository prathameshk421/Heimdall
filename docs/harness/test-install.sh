#!/usr/bin/env bash
# Tests scripts/harness-install.sh in a throwaway copy of the repo layout: install, check, drift, repair.
set -u
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
fail=0; total=0
check() { total=$((total + 1)); if [ "$2" -ne 0 ]; then echo "FAIL: $1"; fail=$((fail + 1)); fi; }

work=$(mktemp -d); trap 'rm -rf "$work"' EXIT
mkdir -p "$work/docs" "$work/scripts"
cp -R "$repo/docs/harness" "$work/docs/harness"
cp "$repo/scripts/harness-install.sh" "$work/scripts/"
printf 'module example.com/mod\n\ngo 1.22\n' > "$work/go.mod"
printf 'bootstrap\n' > "$work/CLAUDE.md"
git -C "$work" init -q
inst() { (cd "$work" && "$@" bash scripts/harness-install.sh >/dev/null 2>&1); }

inst env; check "install succeeds" $?
[ -L "$work/CLAUDE.md" ] && [ "$(readlink "$work/CLAUDE.md")" = "AGENTS.md" ]; check "CLAUDE.md becomes a symlink to AGENTS.md" $?
cmp -s "$work/AGENTS.md" "$work/docs/harness/AGENTS.md"; check "AGENTS.md is the template" $?
[ -x "$work/.claude/hooks/pre-bash-guard.sh" ] && [ -x "$work/.claude/hooks/stop-gate.sh" ]; check "hooks are installed and executable" $?
cmp -s "$work/.claude/settings.json" "$work/docs/harness/claude/settings.json"; check "settings.json is the template" $?
grep -q 'example.com/mod/internal/domain' "$work/.golangci.yml" && ! grep -q MODULE "$work/.golangci.yml"; check ".golangci.yml has the module path filled in" $?
inst env CHECK=1; check "check passes right after install" $?
inst env; check "a second install changes nothing and succeeds" $?
inst env CHECK=1; check "check still passes after a second install" $?

echo '# loosened' >> "$work/.golangci.yml"
inst env CHECK=1; [ $? -ne 0 ]; check "check fails when .golangci.yml drifts" $?
inst env; inst env CHECK=1; check "install repairs drift" $?
chmod -x "$work/.claude/hooks/post-edit.sh"
inst env CHECK=1; [ $? -ne 0 ]; check "check fails when a hook loses its executable bit" $?
inst env
rm "$work/.claude/hooks/stop-gate.sh"
inst env CHECK=1; [ $? -ne 0 ]; check "check fails when a hook is missing" $?
inst env
rm "$work/CLAUDE.md"; printf 'x' > "$work/CLAUDE.md"
inst env CHECK=1; [ $? -ne 0 ]; check "check fails when CLAUDE.md is a copy instead of the symlink" $?

echo "$total install cases, $fail failed"
[ "$fail" -eq 0 ]
