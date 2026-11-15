#!/usr/bin/env bash
# Runs the pattern engine's own test mode over every lens pack in this folder.
#
# Each rule has one file it fires on and one it must not fire on. The engine's test mode proves the first half: it
# compares the findings with the `ruleid:` annotations in the fixture. A plain scan proves the second half: no
# `*.clean.*` fixture may have a finding.
#
# The engine is a build input of the `checks` image, not a tool of this repository. It comes from $OPENGREP, or from
# PATH. Nothing here reaches the network.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
engine="${OPENGREP:-opengrep}"

if ! command -v "$engine" >/dev/null 2>&1 && [ ! -x "$engine" ]; then
  echo "test-rules: no pattern engine at '$engine'. Set OPENGREP to the pinned opengrep binary." >&2
  exit 2
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

failed=0
packs=0
rules=0

for pack in "$root"/*/; do
  [ -d "$pack/rules" ] || continue
  packs=$((packs + 1))
  name="$(basename "$pack")"
  space="$work/$name"
  mkdir -p "$space"
  cp "$pack"/rules/*.yaml "$space"/
  if [ -d "$pack/rules/testdata" ]; then
    cp "$pack"/rules/testdata/* "$space"/
  fi

  count="$(find "$space" -maxdepth 1 -name '*.yaml' | wc -l)"
  rules=$((rules + count))

  if ! output="$("$engine" scan --test --config "$space" "$space" 2>&1)"; then
    echo "FAIL $name: the engine's test mode failed" >&2
    echo "$output" >&2
    failed=$((failed + 1))
    continue
  fi
  if ! grep -q "All tests passed" <<<"$output"; then
    echo "FAIL $name: the engine's test mode did not pass" >&2
    echo "$output" >&2
    failed=$((failed + 1))
    continue
  fi

  clean="$("$engine" scan --config "$space" "$space" --json 2>/dev/null || true)"
  strays="$(python3 -c '
import json, sys
try:
    report = json.load(sys.stdin)
except ValueError:
    sys.exit(1)
# Every rule has one file it must not fire on: <rule-id>.clean.<ext>. Another rule may fire on that file, that is
# its own business.
for result in report.get("results", []):
    rule = result.get("check_id", "").rsplit(".", 1)[-1]
    name = result.get("path", "").rsplit("/", 1)[-1]
    if name.startswith(rule + ".clean."):
        print(result["path"], result["start"]["line"], rule)
' <<<"$clean" || true)"
  if [ -n "$strays" ]; then
    echo "FAIL $name: a rule fired on a file it must not fire on" >&2
    echo "$strays" >&2
    failed=$((failed + 1))
    continue
  fi

  echo "ok   $name: $count rules, each fires on one fixture and on no clean fixture"
done

if [ "$packs" -eq 0 ]; then
  echo "test-rules: no lens pack has a rules folder" >&2
  exit 2
fi

if [ "$failed" -ne 0 ]; then
  echo "test-rules: $failed of $packs packs failed" >&2
  exit 1
fi

echo "test-rules: $rules rules in $packs packs passed"
