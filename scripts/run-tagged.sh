#!/usr/bin/env bash
# Runs the tests of one build tag that match a regex, and FAILS when none match.
#   run-tagged.sh <tag> <test-regex> <label>
# A missing check must never pass silently (TEST_STRATEGY principle 6), and `go test -run X` exits 0
# when X matches nothing, so the tests are listed first. Run from the module under test.
set -euo pipefail
if [ "$#" -ne 3 ]; then echo "usage: run-tagged.sh <tag> <test-regex> <label>" >&2; exit 2; fi
tag=$1 regex=$2 label=$3
export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"
root=${ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}

if ! listed=$(go test -tags "$tag" -list "$regex" ./... 2>&1); then
  echo "$listed" >&2
  echo "$label: the tests with tag $tag do not build." >&2
  exit 1
fi
if [ "$(grep -cE '^(Test|Fuzz)' <<<"$listed" || true)" -eq 0 ]; then
  echo "$label: no test matches $regex with tag $tag. A missing check fails the run; it never passes silently." >&2
  exit 1
fi
gotestsum=$(cd "$root" && go tool -modfile=tools.mod -n gotestsum)
exec "$gotestsum" --format testname -- -tags "$tag" -run "$regex" -count=1 ./...
