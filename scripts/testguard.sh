#!/usr/bin/env bash
# Sourced by gotest.sh and run-tagged.sh. testguard_check <jsonfile> <exit-code> <strict> decides whether a run counts as green.
# TEST_STRATEGY principle 6: a skipped test, a run with no tests, or a silent package never passes.
#   strict=1: every package that passes must have run at least one test (catches a TestMain that exits 0 early).
#   strict=0: the run was filtered with -run, so a package may legitimately match nothing; at least one test must still pass.

testguard_require_jq() {
  command -v jq >/dev/null 2>&1 || { echo "jq is required to check the test run for skips and empty packages; install it" >&2; return 2; }
}

testguard_check() {
  local json=$1 rc=$2 strict=$3 skipped passed silent
  testguard_require_jq || return 2
  skipped=$(jq -r 'select(.Action == "skip" and .Test != null) | "  \(.Package) \(.Test)"' "$json")
  if [ -n "$skipped" ]; then
    printf 'skipped tests are failures; fix the precondition or delete the test:\n%s\n' "$skipped" >&2
    return 1
  fi
  [ "$rc" -eq 0 ] || return "$rc"
  passed=$(jq -s '[.[] | select(.Action == "pass" and .Test != null)] | length' "$json")
  if [ "$passed" -eq 0 ]; then
    echo "no tests ran: a run with zero passing tests is a failure, not a pass" >&2
    return 1
  fi
  if [ "$strict" -eq 1 ]; then
    silent=$(jq -s -r '
      ([.[] | select(.Test != null) | .Package] | unique) as $ran
      | [.[] | select(.Action == "pass" and .Test == null and .Package != null) | .Package] | unique
      | map(select(. as $p | $ran | index($p) | not))[]' "$json")
    if [ -n "$silent" ]; then
      printf 'these packages passed without running any test (TestMain exiting early?):\n%s\n' "$silent" >&2
      return 1
    fi
  fi
  return 0
}
