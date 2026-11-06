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
runner_line=$(grep -n 'runuser -u heimdall-runner' "$script" | head -1 | cut -d: -f1)
cd_line=$(grep -n '^cd /$' "$script" | head -1 | cut -d: -f1 || true)
if [[ -z "$runner_line" || -z "$cd_line" || "$cd_line" -ge "$runner_line" ]]; then
  echo 'ASSERTION FAILED: deploy must leave the SSH home directory before running as heimdall-runner' >&2
  exit 1
fi
grep -q 'install -d -o root -g heimdall-runner-socket .*/etc/heimdall-runner/profiles' "$script" || { echo 'ASSERTION FAILED: deploy must create the approved profile directory the runner reads at start' >&2; exit 1; }
grep -q 'runuser -u heimdall-runner -- env .*PATH=/usr/sbin' "$script" || { echo 'ASSERTION FAILED: runner podman pull must have /usr/sbin on PATH so podman finds runc' >&2; exit 1; }
if grep -Eq '/opt/heimdall/(compose\.yaml|Dockerfile\.server)' "$script"; then
  echo 'ASSERTION FAILED: compose files must be installed under /opt/heimdall/deploy so the relative build context and dockerfile paths resolve' >&2
  exit 1
fi
grep -q 'compose -f /opt/heimdall/deploy/compose.yaml up' "$script" || { echo 'ASSERTION FAILED: the unit must run the compose file from /opt/heimdall/deploy' >&2; exit 1; }
grep -q 'COPY --chmod=0755 heimdall-server ' "$root/deploy/Dockerfile.server" || { echo 'ASSERTION FAILED: the image must make the server binary executable because the deploy stages it as mode 0644' >&2; exit 1; }
grep -q 'chown -R 65532:65532 /etc/heimdall/secrets' "$script" || { echo 'ASSERTION FAILED: secret files must be owned by the server container user 65532 and stay mode 0600 because the server refuses group or other access' >&2; exit 1; }
if grep -q 'chmod 0640 {}' "$script"; then
  echo 'ASSERTION FAILED: the server refuses secret files with group access, so they must not be 0640' >&2
  exit 1
fi
grep -q 'chgrp 65532 /etc/heimdall/heimdall.toml' "$script" || { echo 'ASSERTION FAILED: the config file must be group-readable by the server container user 65532' >&2; exit 1; }
grep -q '^chmod 0640 /etc/heimdall/heimdall.toml' "$script" || { echo 'ASSERTION FAILED: the config mode must be set even when the content is unchanged and the install is skipped' >&2; exit 1; }
if grep -q 'chown -R root:root /etc/heimdall/secrets' "$script"; then
  echo 'ASSERTION FAILED: secrets must not stay root-only, the server container could not read them' >&2
  exit 1
fi
grep -q 'working_dir: /var/lib/heimdall$' "$root/deploy/compose.yaml" || { echo 'ASSERTION FAILED: the server needs a writable working directory because its blob store path is relative' >&2; exit 1; }
grep -q 'heimdall_data:/var/lib/heimdall' "$root/deploy/compose.yaml" || { echo 'ASSERTION FAILED: the blob store directory must be a named volume so evidence survives restarts' >&2; exit 1; }
grep -q 'chown 65532:65532 /var/lib/heimdall' "$root/deploy/Dockerfile.server" || { echo 'ASSERTION FAILED: the image must create the working directory owned by the server user' >&2; exit 1; }
echo 'deploy contract passed'
