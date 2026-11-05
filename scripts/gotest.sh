#!/usr/bin/env bash
# Runs go tests through gotestsum and FAILS when any test was skipped (TEST_STRATEGY principle 6:
# a skipped test or a missing fixture fails the run; it never passes silently).
#   gotest.sh <go test args...>      e.g. gotest.sh -race ./...
set -uo pipefail
export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"
root=${ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}
gotestsum=$(cd "$root" && go tool -modfile=tools.mod -n gotestsum) || exit 2
source "$(dirname "${BASH_SOURCE[0]}")/testguard.sh"
testguard_require_jq || exit 2
json=$(mktemp); trap 'rm -f "$json"' EXIT

"$gotestsum" --format testname --jsonfile "$json" -- "$@"
rc=$?
strict=1
for a in "$@"; do case "$a" in -run|-run=*|-test.run|-test.run=*) strict=0 ;; esac; done
testguard_check "$json" "$rc" "$strict"
