#!/usr/bin/env bash
# Checks every gold test of the test bench against its implementations.
#
# For each case and language: the gold test must PASS on the reference implementation, and must FAIL
# with an assertion failure on every planted-wrong implementation listed in that case's mutants.json.
# An implementation is put in place in a fresh temp copy of the language folder; nothing in the repo
# is edited. Offline and deterministic: Go runs with GOPROXY=off, GOTOOLCHAIN=local, GOFLAGS=-mod=mod.
#
# Exit 0 when every row is as expected, 1 when a row is wrong, 2 when the bench layout is broken.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmproot="$(mktemp -d)"
trap 'rm -rf "$tmproot"' EXIT

export GOPROXY=off GOTOOLCHAIN=local GOFLAGS=-mod=mod GOTELEMETRY=off
export PYTHONDONTWRITEBYTECODE=1

failures=0
rows=0
last_output=""

die() {
	echo "run_gold.sh: $*" >&2
	exit 2
}

# Prints "id<TAB>path" for each mutant of one language in one case, as mutants.json lists them.
list_mutants() {
	python3 -I -c '
import json, os, sys
case, lang = sys.argv[1], sys.argv[2]
with open(os.path.join(case, "mutants.json"), encoding="utf-8") as fh:
    entries = json.load(fh)
for m in entries:
    if m["file"].startswith(lang + "/mutants/"):
        print(m["id"] + "\t" + os.path.join(case, m["file"]))
' "$1" "$2"
}

# Prints the file name of the one reference implementation in a language folder.
reference_name() {
	local dir="$1" lang="$2" f name found=()
	for f in "$dir"/*; do
		[[ -f "$f" ]] || continue
		name="$(basename "$f")"
		if [[ $lang == go && $name == *.go && $name != *_test.go ]]; then
			found+=("$name")
		elif [[ $lang == py && $name == *.py && $name != test_*.py ]]; then
			found+=("$name")
		fi
	done
	[[ ${#found[@]} -eq 1 ]] || die "$dir: want exactly one reference implementation, found ${#found[@]}"
	printf '%s\n' "${found[0]}"
}

# Runs the gold test in a folder. Sets last_output and returns the test command's exit status.
run_gold_test() {
	local lang="$1" work="$2" rc=0
	if [[ $lang == go ]]; then
		last_output="$(cd "$work" && go test -count=1 . 2>&1)" || rc=$?
	else
		last_output="$(cd "$work" && python3 -m unittest test_gold 2>&1)" || rc=$?
	fi
	return "$rc"
}

# Prints pass, fail (an assertion failure) or error (anything else) for a finished run.
classify() {
	local lang="$1" rc="$2"
	if [[ $rc -eq 0 ]]; then
		echo pass
	elif [[ $lang == go ]]; then
		if grep -q -e '\[build failed\]' -e '\[setup failed\]' -e 'panic:' <<<"$last_output"; then
			echo error
		elif grep -q -e '--- FAIL' <<<"$last_output"; then
			echo fail
		else
			echo error
		fi
	else
		if grep -q '^ERROR: ' <<<"$last_output"; then
			echo error
		elif grep -q '^FAIL: ' <<<"$last_output"; then
			echo fail
		else
			echo error
		fi
	fi
}

# check_row case language implementation expected source_file language_dir reference_name
check_row() {
	local label="$1" lang="$2" impl="$3" expected="$4" src="$5" langdir="$6" refname="$7"
	local work f rc=0 got ok=yes
	work="$(mktemp -d "$tmproot/run.XXXXXX")"
	for f in "$langdir"/*; do
		[[ -f "$f" ]] || continue
		[[ "$(basename "$f")" == "$refname" ]] && continue
		cp "$f" "$work/"
	done
	cp "$src" "$work/$refname"
	run_gold_test "$lang" "$work" || rc=$?
	got="$(classify "$lang" "$rc")"
	if [[ $got != "$expected" ]]; then
		ok=no
		failures=$((failures + 1))
	fi
	rows=$((rows + 1))
	printf '%-26s %-9s %-22s %-9s %-9s %s\n' "$label" "$lang" "$impl" "$expected" "$got" "$ok"
	if [[ $ok == no ]]; then
		printf '%s\n' "$last_output" | tail -n 25 >&2
	fi
}

printf '%-26s %-9s %-22s %-9s %-9s %s\n' case language implementation expected got ok

for case_dir in "$here"/cases/*/ "$here"/heldout/*/; do
	[[ -f "$case_dir/mutants.json" ]] || continue
	case_dir="${case_dir%/}"
	label="$(basename "$case_dir")"
	for lang in go py; do
		langdir="$case_dir/$lang"
		[[ -d "$langdir" ]] || continue
		refname="$(reference_name "$langdir" "$lang")"
		check_row "$label" "$lang" reference pass "$langdir/$refname" "$langdir" "$refname"

		listed="$(list_mutants "$case_dir" "$lang" | cut -f2 | sort)"
		present="$(find "$langdir/mutants" -type f 2>/dev/null | sort)"
		[[ "$listed" == "$present" ]] || die "$label/$lang: mutants.json and $lang/mutants/ do not list the same files"

		while IFS=$'\t' read -r id path; do
			check_row "$label" "$lang" "$id" fail "$path" "$langdir" "$refname"
		done < <(list_mutants "$case_dir" "$lang")
	done
done

[[ $rows -gt 0 ]] || die "no rows ran: no case folders found"
if [[ $failures -ne 0 ]]; then
	echo "run_gold.sh: $failures of $rows rows are wrong" >&2
	exit 1
fi
echo "run_gold.sh: all $rows rows as expected"
