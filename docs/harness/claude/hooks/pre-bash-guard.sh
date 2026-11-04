#!/usr/bin/env bash
# PreToolUse(Bash): refuse commands that break AGENTS.md rules R8 and R11 or leave the sandbox.
# Exit 2 blocks the call and shows stderr to the agent.
set -u
cmd=$(jq -r '.tool_input.command // empty')
[ -z "$cmd" ] && exit 0

block() { echo "blocked by AGENTS.md $1: $2" >&2; exit 2; }

grep -Eq '(^|[;&|[:space:]])sudo[[:space:]]'                          <<<"$cmd" && block R11 "no sudo"
grep -Eq 'git[[:space:]]+commit.*(--no-verify|-n[[:space:]])'          <<<"$cmd" && block R11 "never skip the commit hook"
grep -Eq 'git[[:space:]]+push.*(--force|-f[[:space:]]|--force-with-lease)' <<<"$cmd" && block R11 "no force push"
grep -Eq 'git[[:space:]]+(reset --hard|clean -[a-z]*f|checkout -- \.|restore \.)' <<<"$cmd" && block R11 "destructive git: ask the human"
grep -Eq '(curl|wget)[^|]*\|[[:space:]]*(sudo[[:space:]]+)?(ba|z)?sh'   <<<"$cmd" && block R8 "no piping downloads into a shell; add the tool through make tools"
grep -Eq 'docker[[:space:]]+run.*(--privileged|docker\.sock|--pid=host|--net(work)?=host)' <<<"$cmd" && block R8 "no privileged containers or the Docker socket"
grep -Eq '(cat|less|more|head|tail|grep|sed|awk|cp|mv|source|\.)[[:space:]].*(\.env|/secrets/|secrets/|\.switch/)' <<<"$cmd" && block R8 "do not read secrets or Switch credentials"
grep -Eq '(printenv|^env$|^env[[:space:]]*\||declare -x|export -p)'    <<<"$cmd" && block R8 "do not dump the environment"
grep -Eq 'gh[[:space:]]+auth[[:space:]]+(token|status -t)'            <<<"$cmd" && block R8 "tokens are used only by the e2e simulator"
grep -Eq 'gh[[:space:]]+repo[[:space:]]+(delete|archive|edit)'         <<<"$cmd" && block R11 "no repo-level changes"
if grep -Eq 'gh[[:space:]]+api.*(-X|--method)[[:space:]]+(DELETE|PUT|PATCH|POST)' <<<"$cmd"; then
  grep -q 'heimdall-test' <<<"$cmd" || block R8 "live writes only on heimdall-test/ branches and labels"
fi
grep -Eq 'rm[[:space:]]+-[a-zA-Z]*r[a-zA-Z]*f?[[:space:]]+(/|~|\$HOME|\.\.|\*)' <<<"$cmd" && block R11 "no recursive delete outside the working tree"
exit 0
