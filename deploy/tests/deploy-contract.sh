#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
script="$root/deploy/deploy.sh"
if [[ ! -x "$script" ]]; then
  echo 'ASSERTION FAILED: deploy runner exists and is executable' >&2
  exit 1
fi
help=$("$script" --help)
grep -q -- '--host' <<<"$help" || { echo 'ASSERTION FAILED: --host is documented' >&2; exit 1; }
grep -q -- '--secrets-dir' <<<"$help" || { echo 'ASSERTION FAILED: --secrets-dir is documented' >&2; exit 1; }
if grep -Eq '^[[:space:]]+-[[:space:]]+"?(0\.0\.0\.0|\*:|8080:8080)' "$root/deploy/compose.yaml"; then
  echo 'ASSERTION FAILED: compose publishes outside loopback' >&2
  exit 1
fi
grep -q '127\.0\.0\.1:8080:8080' "$root/deploy/compose.yaml" || { echo 'ASSERTION FAILED: service port is loopback-only' >&2; exit 1; }
dry_run=$(make -C "$root" -n deploy CONFIG='/tmp/heimdall config.toml' SECRETS_DIR='/tmp/heimdall secrets')
grep -Fq 'DEPLOY_CONFIG="/tmp/heimdall config.toml" DEPLOY_SECRETS_DIR="/tmp/heimdall secrets" bash scripts/deploy.sh' <<<"$dry_run" || { echo 'ASSERTION FAILED: make deploy must pass CONFIG and SECRETS_DIR to the signed deploy runner' >&2; exit 1; }
if "$script" --host test-host >/dev/null 2>&1; then
  echo 'ASSERTION FAILED: missing deploy inputs must be rejected' >&2
  exit 1
fi
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir "$tmp/secrets"
for name in database_url postgres_password openrouter_api_key jira_email jira_api_token github_app_id github_app_installation_id github_app_private_key switch_api_key switch_registration_token agent_token_test-author agent_token_reviewer-1 agent_token_reviewer-2 agent_token_reviewer-3 agent_token_resolver; do
  : >"$tmp/secrets/$name"
done
ln -s "$tmp/secrets/database_url" "$tmp/secrets/unexpected-link"
if "$script" --config "$root/deploy/heimdall.example.toml" --secrets-dir "$tmp/secrets" >/dev/null 2>&1; then
  echo 'ASSERTION FAILED: symlinked secret inputs must be refused' >&2
  exit 1
fi
echo 'deploy contract passed'
