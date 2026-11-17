#!/usr/bin/env bash
# check-owned.sh: fails when a module's branch changes a path outside that module's row (BUILD_PLAN section 2).
#
#   scripts/check-owned.sh <module> [base]          the diff from the merge base with base (default heimdall-build)
#   scripts/check-owned.sh <module> --paths FILE    the paths listed in FILE, one per line (used by the tests)
#
# The rows below are the checkable form of BUILD_PLAN section 2, written as full paths. When that table changes,
# this file changes with it. Each module has allow globs and deny globs; a path is inside when it matches an allow
# glob and no deny glob. In a glob, * also matches /. docs/PROGRESS.md (every module writes "Noticed" there) and
# docs/TECH_SPEC.md (a new tool adds its 14.1 row in the same step, BUILD_PLAN 5.3) are open to every module.
# Renames count as a delete of the old path and an add of the new one, so moving a file out of another module's
# folder is caught too.
#
# Exit: 0 every path is inside; 1 a path is outside (each is printed); 2 usage or git error.
set -euo pipefail

usage() { echo "usage: $0 <M0..M6> [base | --paths FILE]" >&2; exit 2; }

shared=(docs/PROGRESS.md docs/TECH_SPEC.md)

allow_M0=(
	'internal/archcheck/*' scripts/check-owned.sh e2e/task_m0_test.go
	'internal/domain/change/*' 'internal/domain/workflow/*' 'internal/domain/policy/*'
	'internal/app/bundle/*' 'internal/app/people/*' 'internal/app/intake/*' 'internal/app/trigger/*'
	'internal/app/agenttools/*' 'internal/app/dashboard/*' 'internal/app/engine/*' 'internal/adapters/postgres/*'
	'internal/transport/mcp/tools*.go' cmd/heimdall-server/app.go 'cmd/heimdall-server/wire_*.go'
)
deny_M0=('internal/app/engine/steps_review*' 'internal/app/engine/steps_tests*' internal/app/engine/steps_commit.go
	'internal/transport/mcp/*_test.go')

allow_M1=(
	'internal/adapters/rootlessrunner/*' 'internal/adapters/localrunner/*' 'internal/adapters/profileyaml/*'
	'internal/ports/types_profile*.go' internal/ports/sandbox.go internal/ports/types_work.go
	deploy/heimdall-runner.service e2e/task_m1_test.go
)
deny_M1=()

allow_M2=('deploy/lenses/*' 'deploy/checks/*' 'skills/*')
deny_M2=('skills/*.go')

allow_M3=(
	'internal/domain/sarif/*' 'internal/domain/rulecard/*' 'internal/domain/finding/*' 'internal/domain/present/*'
	'internal/app/review/*' 'internal/app/reviewbrief/*' 'internal/app/reviewlearn/*' 'internal/app/codegraph/*'
	'internal/adapters/lensfs/*' 'internal/mine/*' 'cmd/heimdall-mine/*'
	'internal/app/engine/steps_review*.go' 'internal/app/effects/check*.go' internal/app/effects/verify_evidence.go
	internal/transport/mcp/tools_review.go internal/transport/mcp/tools_checks.go
	internal/domain/change/events_review.go internal/domain/change/events_checks.go
	internal/domain/workflow/rows_review.go cmd/heimdall-server/wire_review.go
	'cmd/heimdall-testagent/scenarios_review*.go' 'cmd/heimdall-testagent/scenarios_verdict*.go' e2e/task_m3_test.go
)
deny_M3=()

allow_M4=(
	'internal/domain/testplan/*' 'internal/domain/testset/*' 'internal/domain/verify/*' 'internal/domain/gate/*'
	'internal/app/engine/steps_tests*.go' internal/app/engine/steps_commit.go
	'internal/app/effects/test*.go' internal/app/effects/acceptance.go
	internal/transport/mcp/tools_tests.go internal/transport/mcp/tools_files.go internal/transport/mcp/tools_preview.go
	internal/domain/change/events_tests.go internal/domain/workflow/rows_tests.go cmd/heimdall-server/wire_tests.go
	cmd/heimdall-testagent/scenarios.go cmd/heimdall-testagent/scenarios_test.go e2e/task_m4_test.go
)
deny_M4=()

allow_M5=(
	'internal/app/announce/*' 'internal/app/collaboration/*' 'internal/app/roomstream/*' 'internal/domain/liveness/*'
	'internal/app/toolguard/*' 'internal/app/record/*' 'internal/domain/questions/*' 'internal/domain/commands/*'
	'internal/adapters/switch/*' internal/ports/chat.go 'internal/transport/skills/*' 'internal/pack/*'
	'deploy/agents/*' 'testdata/agents/*' skills/embed.go
	internal/transport/mcp/tools_commands.go internal/transport/mcp/list.go internal/domain/change/events_talk.go
	cmd/heimdall-server/wire_board.go cmd/heimdall-server/wire_liveness.go cmd/heimdall-server/nudge.go
	cmd/heimdall-server/command_consumer.go 'cmd/heimdall-testagent/scenarios_talk*.go' e2e/task_m5_test.go
)
deny_M5=()

allow_M6=(
	'testdata/plan/*' 'testdata/real/*' 'testdata/repos/*' 'testdata/reviewbench/*' 'testdata/testbench/*'
	'internal/testkit/*' 'e2e/bench/*' 'e2e/scenarios/*' 'e2e/stories/*'
)
deny_M6=()

[ $# -ge 1 ] || usage
module=$1
case "$module" in M[0-6]) ;; *) usage ;; esac
declare -n allow="allow_$module" deny="deny_$module"

if [ "${2:-}" = --paths ]; then
	[ $# -eq 3 ] && [ -r "$3" ] || usage
	paths=$(grep -v '^[[:space:]]*$' "$3" || true)
else
	[ $# -le 2 ] || usage
	base=${2:-heimdall-build}
	mb=$(git merge-base "$base" HEAD) || { echo "check-owned: no merge base with $base" >&2; exit 2; }
	paths=$(git diff --name-only --no-renames "$mb" HEAD) || exit 2
fi

# inside reports whether path p is in the module's row (or shared) and not denied.
inside() {
	local p=$1 g
	for g in "${deny[@]}"; do
		# shellcheck disable=SC2053 # the pattern is a glob on purpose
		[[ $p == $g ]] && return 1
	done
	for g in "${shared[@]}" "${allow[@]}"; do
		# shellcheck disable=SC2053
		[[ $p == $g ]] && return 0
	done
	return 1
}

total=0; outside=0
while IFS= read -r p; do
	[ -n "$p" ] || continue
	total=$((total + 1))
	if ! inside "$p"; then
		echo "outside $module: $p"
		outside=$((outside + 1))
	fi
done <<<"$paths"

if [ "$outside" -gt 0 ]; then
	echo "check-owned: $outside of $total changed paths are outside $module's row (BUILD_PLAN section 2)" >&2
	exit 1
fi
echo "check-owned: $total changed paths, all inside $module's row"
