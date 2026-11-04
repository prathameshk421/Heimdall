#!/usr/bin/env bash
# Installs scripts/pre-commit as .git/hooks/pre-commit. Repeatable.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
hooks=$(git -C "$root" rev-parse --git-path hooks)
case "$hooks" in /*) ;; *) hooks="$root/$hooks" ;; esac
mkdir -p "$hooks"
install -m 755 "$root/scripts/pre-commit" "$hooks/pre-commit"
echo "hooks-install: pre-commit hook installed in $hooks"
