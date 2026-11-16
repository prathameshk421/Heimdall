#!/usr/bin/env bash
# Cheap real checks for the held-out webhook-notifier case. Offline, in temp copies; the case is not edited.
#  1. base, head and clean each build with go build ./...
#  2. applying head.diff (and clean.diff) to a copy of base gives exactly the head (and clean) tree
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
export GOPROXY=off GOTOOLCHAIN=local GOFLAGS=-mod=mod GOTELEMETRY=off

status=0
for tree in base head clean; do
	cp -R "$here/$tree" "$tmp/$tree"
	if (cd "$tmp/$tree" && go build ./...); then
		echo "build $tree: ok"
	else
		echo "build $tree: FAILED"
		status=1
	fi
done

for tree in head clean; do
	cp -R "$here/base" "$tmp/apply-$tree"
	if (cd "$tmp/apply-$tree" && patch -p1 -s --no-backup-if-mismatch < "$here/$tree.diff") &&
		diff -r "$tmp/apply-$tree" "$here/$tree" >/dev/null; then
		echo "$tree.diff applied to base equals $tree: ok"
	else
		echo "$tree.diff applied to base equals $tree: FAILED"
		status=1
	fi
done
exit "$status"
