#!/usr/bin/env bash
# Runs go tests through gotestsum and FAILS when any test was skipped (TEST_STRATEGY principle 6:
# a skipped test or a missing fixture fails the run; it never passes silently).
#   gotest.sh <go test args...>      e.g. gotest.sh -race ./...
set -uo pipefail
export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"
root=${ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}
gotestsum=$(cd "$root" && go tool -modfile=tools.mod -n gotestsum) || exit 2
json=$(mktemp); trap 'rm -f "$json"' EXIT

"$gotestsum" --format testname --jsonfile "$json" -- "$@"
rc=$?
skipped=$(jq -r 'select(.Action == "skip" and .Test != null) | "  \(.Package) \(.Test)"' "$json")
if [ -n "$skipped" ]; then
  printf 'skipped tests are failures; fix the precondition or delete the test:\n%s\n' "$skipped" >&2
  exit 1
fi
exit "$rc"
