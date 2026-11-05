#!/usr/bin/env bash
# Installs the agent harness from docs/harness/ into the repository root (T16).
#   bash scripts/harness-install.sh           install or repair
#   CHECK=1 bash scripts/harness-install.sh   change nothing; exit 1 if anything differs from the templates
# AGENTS.md is the source of truth and CLAUDE.md is a symlink to it. MODULE in the lint config is the go.mod module path.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"
src=docs/harness
check=${CHECK:-}
module=$(sed -n 's/^module //p' go.mod | head -1)
[ -n "$module" ] || { echo "harness-install: no module line in go.mod" >&2; exit 2; }
drift=0

report() { echo "harness-install: $1" >&2; drift=1; }

# put <template> <dest> <mode> [sed expression]: write the dest from the template when it differs.
put() {
  local from=$1 dest=$2 mode=$3 expr=${4:-}
  local want; want=$(mktemp)
  if [ -n "$expr" ]; then sed "$expr" "$from" > "$want"; else cat "$from" > "$want"; fi
  if [ ! -f "$dest" ] || [ -L "$dest" ] || ! cmp -s "$want" "$dest"; then
    if [ -n "$check" ]; then report "$dest differs from $from"; else
      mkdir -p "$(dirname "$dest")"; rm -f "$dest"; cp "$want" "$dest"
    fi
  fi
  if [ -z "$check" ]; then chmod "$mode" "$dest"; elif [ -e "$dest" ] && [ "$(stat -c %a "$dest")" != "$mode" ]; then
    report "$dest has mode $(stat -c %a "$dest"), want $mode"
  fi
  rm -f "$want"
}

put "$src/AGENTS.md" AGENTS.md 644
put "$src/claude/settings.json" .claude/settings.json 644
for hook in "$src"/claude/hooks/*.sh; do put "$hook" ".claude/hooks/$(basename "$hook")" 755; done
put "$src/golangci.yml" .golangci.yml 644 "s#MODULE#${module//#/\\#}#g"

if [ ! -L CLAUDE.md ] || [ "$(readlink CLAUDE.md)" != "AGENTS.md" ]; then
  if [ -n "$check" ]; then report "CLAUDE.md is not a symlink to AGENTS.md"; else ln -sfn AGENTS.md CLAUDE.md; fi
fi

if [ -n "$check" ]; then
  [ "$drift" -eq 0 ] && echo "harness-install: installed harness matches docs/harness" || exit 1
else
  echo "harness-install: installed from $src"
fi
