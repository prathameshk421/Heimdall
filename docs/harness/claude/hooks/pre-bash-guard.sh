#!/usr/bin/env bash
# PreToolUse(Bash): refuse commands that break AGENTS.md rules R2, R8 and R11.
# Exit 2 blocks the call and shows stderr to the agent. Hooks stop mistakes; make sandbox is the isolation.
set -u
cmd=$(jq -r '.tool_input.command // empty')
[ -z "$cmd" ] && exit 0

block() { echo "blocked by AGENTS.md $1: $2" >&2; exit 2; }

# Quoted commit messages are data, so flag checks run on the command with them removed.
bare=$(sed -E "s/\"([^\"\\\\]|\\\\.)*\"//g; s/'[^']*'//g" <<<"$cmd")

grep -Eq '(^|[;&|[:space:]])sudo[[:space:]]'                                  <<<"$bare" && block R11 "no sudo"
grep -Eq 'git[[:space:]]+commit([[:space:]].*)?[[:space:]](--no-verify|-[a-zA-Z]*n[a-zA-Z]*)([[:space:]]|$)' <<<"$bare" && block R11 "never skip the commit hook"
grep -Eq 'core\.hooksPath'                                                    <<<"$bare" && block R11 "never skip the commit hook"
grep -Eq 'git[[:space:]]+push([[:space:]].*)?[[:space:]](--force|--force-with-lease|-f)([[:space:]=]|$)' <<<"$bare" && block R11 "no force push"
grep -Eq 'git[[:space:]]+(reset[[:space:]]+--hard|clean[[:space:]]+-[a-z]*f|checkout[[:space:]]+--[[:space:]]+\.|restore[[:space:]]+\.|rebase|filter-branch)' <<<"$bare" && block R11 "destructive or history-changing git: ask the human"
grep -Eq '(curl|wget)[^|]*\|[[:space:]]*(sudo[[:space:]]+)?(ba|z)?sh'         <<<"$bare" && block R8 "no piping downloads into a shell; add the tool through make tools"
grep -Eq 'docker[[:space:]]+run.*(--privileged|docker\.sock|--pid=host|--net(work)?=host)' <<<"$bare" && block R8 "no privileged containers or the Docker socket"
grep -Eq 'docker[[:space:]]+run.*(-v|--volume|--mount)[[:space:]=]+[^[:space:]]*(^|[=[:space:]])?(/:|~|\$HOME|/home/|/root|\.ssh)' <<<"$bare" && block R8 "no host home or root mounts"

# R8: any command that names a secret path, whichever program reads it. Quotes stay in here on purpose.
named=$(sed -E 's/\.env\.example//g' <<<"$cmd")
grep -Eq '(^|[[:space:]<>=:"'"'"'(\\])(\./)?\.env(\.[a-zA-Z0-9_-]+)?($|[[:space:]"'"'"'\;|&)])' <<<"$named" && block R8 "do not read .env; live tests get credentials through the e2e simulator"
grep -Eq '(^|[[:space:]<>=:"'"'"'(\\])(\./)?secrets/|/etc/heimdall/secrets|\.switch/agents/' <<<"$named" && block R8 "do not read secrets or Switch credentials"
grep -Eq '(^|[;&|[:space:]])(printenv|export[[:space:]]+-p|declare[[:space:]]+-x)([[:space:]]|$)|(^|[;&|])[[:space:]]*env[[:space:]]*($|\|)' <<<"$bare" && block R8 "do not dump the environment"
grep -Eq 'gh[[:space:]]+auth[[:space:]]+(token|status[[:space:]]+(-t|--show-token))' <<<"$bare" && block R8 "tokens are used only by the e2e simulator"
grep -Eq 'gh[[:space:]]+repo[[:space:]]+(delete|archive|edit)'                <<<"$bare" && block R11 "no repo-level changes"
if grep -Eq 'gh[[:space:]]+api.*(-X|--method)[[:space:]]+(DELETE|PUT|PATCH|POST)' <<<"$bare"; then
  grep -q 'heimdall-test' <<<"$cmd" || block R8 "live writes only on heimdall-test branches and labels"
fi

# R2: guardrails through the shell, the same files the edit hook protects.
grep -Eq '(>|>>|tee[[:space:]]|sed[[:space:]]+-i|cp[[:space:]]|mv[[:space:]]|rm[[:space:]]|chmod[[:space:]]|ln[[:space:]]).*(\.claude/|docs/harness/|\.golangci\.yml|\.git/hooks)' <<<"$bare" && block R2 "guardrail files change only by the human; say what should change"

grep -Eq 'rm[[:space:]]+-[a-zA-Z]*r[a-zA-Z]*f?[[:space:]]+(/|~|\$HOME|\.\.|\*)' <<<"$bare" && block R11 "no recursive delete outside the working tree"
exit 0
