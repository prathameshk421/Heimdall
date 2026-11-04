#!/usr/bin/env bash
# Tests scripts/run-tagged.sh in a throwaway module (make scripts-test).
set -u
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"
fail=0; total=0
check() { total=$((total + 1)); if [ "$2" -ne 0 ]; then echo "FAIL: $1"; fail=$((fail + 1)); fi; }

work=$(mktemp -d); trap 'rm -rf "$work"' EXIT
mkdir -p "$work/e2e"
printf 'package m\n' > "$work/m.go"
printf 'module example.com/m\n\ngo 1.22\n' > "$work/go.mod"
run() { (cd "$work" && ROOT="$root" bash "$root/scripts/run-tagged.sh" "$@" 2>&1); }

cat > "$work/e2e/a_test.go" <<'GO'
//go:build e2e

package e2e

import "testing"

func TestTask_T24(t *testing.T) {}
func TestTask_T25(t *testing.T) { t.Fatal("a real failure") }
GO
out=$(run e2e '^TestTask_T24$' "e2e T24"); code=$?
[ "$code" -eq 0 ]; check "a matching passing test passes" $?
out=$(run e2e '^TestTask_T99$' "e2e T99"); code=$?
[ "$code" -ne 0 ] && grep -q 'no test matches' <<<"$out"; check "a task with no test fails and says why" $?
out=$(run e2e '^TestTask_T25$' "e2e T25"); code=$?
[ "$code" -ne 0 ]; check "a matching failing test fails" $?
out=$(run drift 'Drift' "drift"); code=$?
[ "$code" -ne 0 ] && grep -q 'no test matches' <<<"$out"; check "a tag with no tests at all fails" $?
printf '//go:build e2e\n\npackage e2e\n\nfunc broken( {\n' > "$work/e2e/b_test.go"
out=$(run e2e '^TestTask_T24$' "e2e T24"); code=$?
[ "$code" -ne 0 ]; check "a package that does not compile fails" $?
rm "$work/e2e/b_test.go"
out=$(cd "$work" && ROOT="$root" bash "$root/scripts/run-tagged.sh" 2>&1); code=$?
[ "$code" -ne 0 ]; check "missing arguments fail" $?

# --- gotest.sh: a skipped test is a failure
mkdir -p "$work/skips"
cat > "$work/skips/s_test.go" <<'GO'
package skips

import "testing"

func TestPasses(t *testing.T) {}
func TestSkipped(t *testing.T) { t.Skip("needs docker") }
GO
gotest() { (cd "$work" && ROOT="$root" bash "$root/scripts/gotest.sh" "$@" 2>&1); }
out=$(gotest ./skips/ -run 'TestPasses$'); code=$?
[ "$code" -eq 0 ]; check "gotest.sh passes when nothing is skipped" $?
out=$(gotest ./skips/); code=$?
[ "$code" -ne 0 ] && grep -q 'TestSkipped' <<<"$out" && grep -qi 'skipped tests are failures' <<<"$out"; check "gotest.sh fails and names a skipped test" $?
printf 'package skips\n\nimport "testing"\n\nfunc TestBroken(t *testing.T) { t.Fatal("no") }\n' > "$work/skips/b_test.go"
out=$(gotest ./skips/ -run 'TestBroken$'); code=$?
[ "$code" -ne 0 ]; check "gotest.sh fails when a test fails" $?

# --- hooks-install.sh: the pre-commit hook runs make fast and blocks a red commit
repo="$work/repo"; mkdir -p "$repo/scripts"
git -C "$repo" init -q && git -C "$repo" config user.email t@t && git -C "$repo" config user.name t
cp "$root/scripts/pre-commit" "$root/scripts/hooks-install.sh" "$repo/scripts/"
printf 'fast:\n\t@echo fast-ran >> ran.log; exit 1\n' > "$repo/Makefile"
(cd "$repo" && bash scripts/hooks-install.sh >/dev/null 2>&1); check "hooks-install succeeds" $?
[ -x "$repo/.git/hooks/pre-commit" ]; check "the pre-commit hook is installed and executable" $?
printf 'x' > "$repo/a.txt"; git -C "$repo" add -A
out=$(git -C "$repo" commit -q -m red 2>&1); code=$?
[ "$code" -ne 0 ] && [ "$(git -C "$repo" rev-list --count HEAD 2>/dev/null || echo 0)" = "0" ] && grep -q fast-ran "$repo/ran.log"; check "a red make fast blocks the commit" $?
printf 'fast:\n\t@true\n' > "$repo/Makefile"; git -C "$repo" add -A
git -C "$repo" commit -q -m green >/dev/null 2>&1; check "a green make fast lets the commit through" $?
(cd "$repo" && bash scripts/hooks-install.sh >/dev/null 2>&1); check "hooks-install is repeatable" $?

echo "$total script cases, $fail failed"
[ "$fail" -eq 0 ]
