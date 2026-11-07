#!/usr/bin/env bash
# Tests scripts/run-tagged.sh in a throwaway module (make scripts-test).
set -u
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"
fail=0; total=0
check() { total=$((total + 1)); if [ "$2" -ne 0 ]; then echo "FAIL: $1"; fail=$((fail + 1)); fi; }

work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

# --- deploy guard and dry-run plan: repeatable and never contacts a host
out=$(cd "$root" && DEPLOY_DRY_RUN=1 bash scripts/deploy.sh 2>&1); code=$?
[ "$code" -eq 0 ] && grep -q 'linux/amd64' <<<"$out" && grep -q 'authenticated /mcp' <<<"$out"; check "deploy dry-run reports the release and authenticated MCP checks" $?
out2=$(cd "$root" && DEPLOY_DRY_RUN=1 bash scripts/deploy.sh 2>&1); code=$?
[ "$code" -eq 0 ] && [ "$out" = "$out2" ]; check "deploy dry-run is idempotent" $?
out=$(cd "$root" && DEPLOY_DRY_RUN=0 bash scripts/deploy.sh 2>&1); code=$?
[ "$code" -eq 2 ] && grep -q 'exact release tag' <<<"$out"; check "deploy refuses an untagged live release" $?
out=$(cd "$root" && DEPLOY_DRY_RUN=1 DEPLOY_ARCH=386 bash scripts/deploy.sh 2>&1); code=$?
[ "$code" -eq 2 ] && grep -q 'DEPLOY_ARCH' <<<"$out"; check "deploy refuses an unsupported target architecture" $?

# setup creates private operator files once, then preserves the operator's config.
setup_dir="$work/operator"
out=$(bash "$root/scripts/setup.sh" --dir "$setup_dir" 2>&1); code=$?
[ "$code" -eq 0 ] && [ "$(stat -c '%a' "$setup_dir")" = 700 ] && [ "$(stat -c '%a' "$setup_dir/secrets")" = 700 ] && [ "$(stat -c '%a' "$setup_dir/heimdall.toml")" = 600 ]; check "setup creates private config and secret paths" $?
printf '\n# operator edit\n' >> "$setup_dir/heimdall.toml"
out=$(bash "$root/scripts/setup.sh" --dir "$setup_dir" 2>&1); code=$?
[ "$code" -eq 0 ] && grep -q 'operator edit' "$setup_dir/heimdall.toml"; check "setup is idempotent and preserves the operator config" $?

# restart/recover send only fixed operations and perform health plus authenticated MCP checks remotely.
mkdir -p "$work/bin"
cat > "$work/bin/git" <<'SH'
#!/usr/bin/env bash
case "$1" in
	describe) printf 'v1.0.0\n' ;;
	status) : ;;
	*) exit 2 ;;
esac
SH
cat > "$work/bin/go" <<'SH'
#!/usr/bin/env bash
while (($#)); do
	if [[ "$1" == -o ]]; then
		shift
		printf 'fake linux binary\n' > "$1"
		exit 0
	fi
	shift
done
exit 2
SH
for command_name in scp rsync; do
	cat > "$work/bin/$command_name" <<'SH'
#!/usr/bin/env bash
exit 0
SH
done
cat > "$work/bin/cosign" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" > "$COSIGN_CAPTURE"
while (($#)); do
	if [[ "$1" == --output-signature ]]; then
		printf 'stub signature\n' > "$2"
		break
	fi
	shift
done
if [[ "${COSIGN_FAIL:-0}" == 1 ]]; then exit 1; fi
exit 0
SH
cat > "$work/bin/ssh" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" > "$SSH_CAPTURE"
cat >> "$SSH_CAPTURE"
SH
chmod +x "$work/bin/git" "$work/bin/go" "$work/bin/scp" "$work/bin/rsync" "$work/bin/cosign" "$work/bin/ssh"
release_output="$work/release-output"
PATH="$work/bin:$PATH" DEPLOY_BUILD_ONLY=1 DEPLOY_TAG=v1.0.0 DEPLOY_OUTPUT_DIR="$release_output" bash "$root/scripts/deploy.sh" > "$work/build-one" 2>&1; code=$?
[ "$code" -eq 0 ] && [ -s "$release_output/v1.0.0.tgz" ] && [ -s "$release_output/v1.0.0.tgz.sha256" ]; check "local build materializes a tag-named release archive and checksum" $?
first_hash=$(shasum -a 256 "$release_output/v1.0.0.tgz" | awk '{print $1}')
PATH="$work/bin:$PATH" DEPLOY_BUILD_ONLY=1 DEPLOY_TAG=v1.0.0 DEPLOY_OUTPUT_DIR="$release_output" bash "$root/scripts/deploy.sh" > "$work/build-two" 2>&1; code=$?
second_hash=$(shasum -a 256 "$release_output/v1.0.0.tgz" | awk '{print $1}')
[ "$code" -eq 0 ] && [ "$first_hash" = "$second_hash" ]; check "tagged release archive bytes are reproducible across builds" $?
out=$(PATH="$work/bin:$PATH" RELEASE_ARCHIVE="$release_output/v1.0.0.tgz" RELEASE_SIGNATURE="$release_output/v1.0.0.tgz.sig" bash "$root/scripts/sign-release.sh" 2>&1); code=$?
[ "$code" -eq 2 ] && grep -q 'RELEASE_SIGNING_KEY' <<<"$out"; check "release signing refuses to run without a signing key reference" $?
printf '%s\n' 'operator key reference' > "$work/signing-key"
COSIGN_CAPTURE="$work/sign-args" PATH="$work/bin:$PATH" RELEASE_ARCHIVE="$release_output/v1.0.0.tgz" RELEASE_SIGNATURE="$release_output/v1.0.0.tgz.sig" RELEASE_SIGNING_KEY="$work/signing-key" bash "$root/scripts/sign-release.sh" > "$work/sign-output" 2>&1; code=$?
[ "$code" -eq 0 ] && grep -q 'sign-blob.*--key' "$work/sign-args" && [ -s "$release_output/v1.0.0.tgz.sig" ]; check "release signer invokes cosign on the built artifact using the key reference" $?
deploy_secrets="$work/deploy-secrets"
mkdir -p "$deploy_secrets"
for name in database_url postgres_password openrouter_api_key jira_email jira_api_token github_app_id github_app_installation_id github_app_private_key switch_api_key switch_registration_token agent_token_test-author agent_token_reviewer-1 agent_token_reviewer-2 agent_token_reviewer-3 agent_token_resolver; do
	: > "$deploy_secrets/$name"
	chmod 0600 "$deploy_secrets/$name"
done
printf '%s\n' 'test public key' > "$work/release.pub"
signature_file="$release_output/v1.0.0.tgz.sig"
SSH_CAPTURE="$work/ssh-input" COSIGN_CAPTURE="$work/cosign-args" PATH="$work/bin:$PATH" DEPLOY_HOST=stub-host DEPLOY_TAG=v1.0.0 DEPLOY_OUTPUT_DIR="$release_output" DEPLOY_SECRETS_DIR="$deploy_secrets" DEPLOY_CONFIG="$root/deploy/heimdall.example.toml" DEPLOY_VERIFY_KEY="$work/release.pub" DEPLOY_SIGNATURE_FILE="$signature_file" bash "$root/scripts/deploy.sh" > "$work/deploy-output" 2>&1; code=$?
if [ "$code" -ne 0 ]; then cat "$work/deploy-output"; fi
[ "$code" -eq 0 ] && grep -q 'stub-host' "$work/ssh-input" && grep -q 'docker compose.*up -d --wait --timeout 30' "$work/ssh-input" && grep -q 'Authorization: Bearer' "$work/ssh-input" && grep -q 'release v1.0.0 is healthy' "$work/deploy-output" && grep -q 'verify-blob.*--key' "$work/cosign-args"; check "tagged deploy verifies the configured signature, then stages and probes through SSH" $?
[ -f "$work/ssh-input" ] && grep -q 'previous_hash.*archive_hash' "$work/ssh-input" && grep -q 'build heimdall-server' "$work/ssh-input"; check "deploy skips release extraction and image build when artifact hash is unchanged" $?
[ -f "$work/ssh-input" ] && grep -q 'restoring the previous release' "$work/ssh-input" && grep -q 'previous_current' "$work/ssh-input" && grep -q 'docker compose.*up -d --wait --timeout 30' "$work/ssh-input"; check "deploy restores the previous release when new health or MCP probes fail" $?
COSIGN_CAPTURE="$work/cosign-fail-args" SSH_CAPTURE="$work/ssh-fail-input" COSIGN_FAIL=1 PATH="$work/bin:$PATH" DEPLOY_HOST=stub-host DEPLOY_TAG=v1.0.0 DEPLOY_OUTPUT_DIR="$release_output" DEPLOY_SECRETS_DIR="$deploy_secrets" DEPLOY_CONFIG="$root/deploy/heimdall.example.toml" DEPLOY_VERIFY_KEY="$work/release.pub" DEPLOY_SIGNATURE_FILE="$signature_file" bash "$root/scripts/deploy.sh" > "$work/deploy-fail-output" 2>&1; code=$?
[ "$code" -eq 1 ] && grep -q 'signature verification failed' "$work/deploy-fail-output" && [ ! -f "$work/ssh-fail-input" ]; check "deploy refuses a failed signature before contacting the host" $?

# Exercise the exact remote activation script against disposable install and secret roots.
mkdir -p "$work/remote-bin" "$work/release-payload/deploy" "$work/staged-secrets"
printf 'v2.0.0\n' > "$work/release-payload/RELEASE"
: > "$work/release-payload/deploy/compose.yaml"
: > "$work/release-payload/deploy/compose.release.yaml"
: > "$work/release-payload/deploy/heimdall.toml"
printf 'test-only-token\n' > "$work/staged-secrets/agent_token_test-author"
chmod 0600 "$work/staged-secrets/agent_token_test-author"
tar -czf "$work/remote-release.tgz" -C "$work/release-payload" .
remote_hash=$(shasum -a 256 "$work/remote-release.tgz" | awk '{print $1}')
cat > "$work/remote-bin/rsync" <<'SH'
#!/usr/bin/env bash
source_path=${@: -2:1}
destination=${@: -1}
mkdir -p "$destination"
cp -a "$source_path". "$destination"
SH
cat > "$work/remote-bin/docker" <<'SH'
#!/usr/bin/env bash
printf '%s|%s|cwd=%s\n' "$(pwd -P)" "$*" "$PWD" >> "$REMOTE_DOCKER_CAPTURE"
SH
cat > "$work/remote-bin/curl" <<'SH'
#!/usr/bin/env bash
args_text=$*
printf '%s\n' "$args_text" >> "$REMOTE_CURL_CAPTURE"
if [[ "$args_text" == *'/mcp' ]]; then
	config_file=
	while (($#)); do
		if [[ "$1" == --config ]]; then config_file=$2; shift 2; else shift; fi
	done
	[[ -n "$config_file" && "$(stat -c '%a' "$config_file")" == 600 ]] || exit 23
	grep -q '^header = "Authorization: Bearer ' "$config_file" || exit 24
fi
if [[ "${REMOTE_CURL_FAIL_HEALTH:-0}" == 1 && "$(pwd -P)" == *'/releases/v2.0.0-'* && "$args_text" == *'/healthz' ]]; then exit 22; fi
SH
cat > "$work/remote-bin/sudo" <<'SH'
#!/usr/bin/env bash
if [[ "${1:-}" == -u ]]; then shift 2; fi
exec "$@"
SH
cat > "$work/remote-bin/chown" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${REMOTE_CHOWN_CAPTURE:-/dev/null}"
SH
chmod +x "$work/remote-bin/rsync" "$work/remote-bin/docker" "$work/remote-bin/curl" "$work/remote-bin/sudo" "$work/remote-bin/chown"
remote_success_root="$work/remote-success"
mkdir -p "$remote_success_root"
cp "$work/remote-release.tgz" "$work/remote-success-stage.tgz"
cp -a "$work/staged-secrets" "$work/remote-success-stage-secrets"
REMOTE_CHOWN_CAPTURE="$work/remote-success-chown" REMOTE_DOCKER_CAPTURE="$work/remote-success-docker" REMOTE_CURL_CAPTURE="$work/remote-success-curl" PATH="$work/remote-bin:$PATH" sh "$root/scripts/deploy-remote.sh" v2.0.0 "$work/remote-success-stage" "$(id -un)" "$remote_hash" "$remote_success_root/install" "$remote_success_root/secrets" >/dev/null 2>&1; code=$?
[ "$code" -eq 0 ] && [ "$(readlink "$remote_success_root/install/current")" = "$remote_success_root/install/releases/v2.0.0-$remote_hash" ] && [ ! -e "$work/remote-success-stage.tgz" ] && grep -q '/healthz' "$work/remote-success-curl" && grep -q '/mcp' "$work/remote-success-curl" && ! grep -q 'test-only-token' "$work/remote-success-curl" && grep -qx -- "-R 65532:65532 $remote_success_root/secrets" "$work/remote-success-chown" && grep -q "build heimdall-server|cwd=$remote_success_root/install/releases/v2.0.0-$remote_hash$" "$work/remote-success-docker"; check "remote activation advances after health and MCP using a private curl config file" $?
remote_rollback_root="$work/remote-rollback"
mkdir -p "$remote_rollback_root/install/releases/v1.0.0/deploy" "$remote_rollback_root/install/releases/v1.0.0/deploy" "$remote_rollback_root/secrets"
: > "$remote_rollback_root/install/releases/v1.0.0/deploy/compose.yaml"
: > "$remote_rollback_root/install/releases/v1.0.0/deploy/compose.release.yaml"
printf 'v1.0.0\n' > "$remote_rollback_root/install/releases/v1.0.0/RELEASE"
printf 'previous-test-token\n' > "$remote_rollback_root/secrets/agent_token_test-author"
chmod 0600 "$remote_rollback_root/secrets/agent_token_test-author"
ln -s "$remote_rollback_root/install/releases/v1.0.0" "$remote_rollback_root/install/current"
cp "$work/remote-release.tgz" "$work/remote-fail-stage.tgz"
cp -a "$work/staged-secrets" "$work/remote-fail-stage-secrets"
REMOTE_DOCKER_CAPTURE="$work/remote-rollback-docker" REMOTE_CURL_CAPTURE="$work/remote-rollback-curl" REMOTE_CURL_FAIL_HEALTH=1 PATH="$work/remote-bin:$PATH" sh "$root/scripts/deploy-remote.sh" v2.0.0 "$work/remote-fail-stage" "$(id -un)" "$remote_hash" "$remote_rollback_root/install" "$remote_rollback_root/secrets" > "$work/remote-rollback-output" 2>&1; code=$?
[ "$code" -ne 0 ] && [ "$(readlink "$remote_rollback_root/install/current")" = "$remote_rollback_root/install/releases/v1.0.0" ] && grep -qx 'previous-test-token' "$remote_rollback_root/secrets/agent_token_test-author" && grep -q 'restoring the previous release' "$work/remote-rollback-output" && grep -q "$remote_rollback_root/install/releases/v1.0.0|compose.*up -d --wait" "$work/remote-rollback-docker" && [ ! -e "$remote_rollback_root/install/.deploy-pending" ] && [ ! -e "$work/remote-fail-stage.tgz" ]; check "failed health probe restores prior release and secret directory, restarts it, and cleans staging" $?

SSH_CAPTURE="$work/ssh-input" PATH="$work/bin:$PATH" DEPLOY_HOST=stub-host bash "$root/scripts/operate.sh" recover >/dev/null 2>&1; code=$?
[ "$code" -eq 0 ] && grep -q 'stub-host' "$work/ssh-input" && grep -q '/healthz' "$work/ssh-input" && grep -q 'Authorization: Bearer' "$work/ssh-input"; check "recover uses SSH and verifies health plus authenticated MCP" $?
[ -f "$work/ssh-input" ] && grep -q 'docker inspect --format.*State.Health.Status' "$work/ssh-input" && grep -q 'Graphiti is not healthy' "$work/ssh-input"; check "recovery fails closed unless Graphiti Compose health is healthy" $?
SSH_CAPTURE="$work/ssh-input" PATH="$work/bin:$PATH" DEPLOY_HOST=stub-host bash "$root/scripts/operate.sh" restart >/dev/null 2>&1; code=$?
[ "$code" -eq 0 ] && grep -q 'restart heimdall-server' "$work/ssh-input" && grep -q 'systemctl start' "$work/ssh-input" && grep -q 'systemctl is-active --quiet' "$work/ssh-input"; check "restart ensures the controller service is active before health probes" $?
out=$(PATH="$work/bin:$PATH" DEPLOY_HOST=stub-host bash "$root/scripts/operate.sh" unknown 2>&1); code=$?
[ "$code" -eq 2 ] && grep -q 'usage:' <<<"$out"; check "operation command refuses unknown actions" $?

# Run the actual remote recovery script with fake Compose, systemd and HTTP endpoints.
mkdir -p "$work/ops-bin" "$work/ops-root/install/releases/v1.0.0/deploy" "$work/ops-root/secrets"
: > "$work/ops-root/install/releases/v1.0.0/deploy/compose.yaml"
: > "$work/ops-root/install/releases/v1.0.0/deploy/compose.release.yaml"
: > "$work/ops-root/install/releases/v1.0.0/deploy/heimdall.toml"
printf 'v1.0.0\n' > "$work/ops-root/install/releases/v1.0.0/RELEASE"
ln -s "$work/ops-root/install/releases/v1.0.0" "$work/ops-root/install/current"
printf 'test-only-ops-token\n' > "$work/ops-root/secrets/agent_token_test-author"
cat > "$work/ops-bin/docker" <<'SH'
#!/usr/bin/env bash
printf '%s|%s\n' "$(pwd -P)" "$*" >> "$OPS_DOCKER_CAPTURE"
if [[ "$*" == *'ps -q graphiti'* ]]; then echo graphiti-container; fi
if [[ "$1" == inspect ]]; then
	if [[ "${OPS_GRAPHITI_UNHEALTHY:-0}" == 1 ]]; then echo unhealthy; else echo healthy; fi
fi
SH
cat > "$work/ops-bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$OPS_SYSTEMCTL_CAPTURE"
case "$1" in
  list-unit-files) [[ "${OPS_CONTROLLER_MISSING:-0}" == 1 ]] || echo "$2 enabled" ;;
  is-active) [[ -f "$OPS_CONTROLLER_ACTIVE" ]] ;;
  start) touch "$OPS_CONTROLLER_ACTIVE" ;;
esac
SH
cat > "$work/ops-bin/curl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$OPS_CURL_CAPTURE"
if [[ "${OPS_HEALTH_FAIL:-0}" == 1 && "$*" == *'/healthz' ]]; then exit 22; fi
if [[ "${OPS_MCP_FAIL:-0}" == 1 && "$*" == *'/mcp' ]]; then exit 22; fi
SH
cat > "$work/ops-bin/sudo" <<'SH'
#!/usr/bin/env bash
if [[ "${1:-}" == -u ]]; then shift 2; fi
exec "$@"
SH
chmod +x "$work/ops-bin/docker" "$work/ops-bin/systemctl" "$work/ops-bin/curl" "$work/ops-bin/sudo"
OPS_DOCKER_CAPTURE="$work/ops-docker" OPS_SYSTEMCTL_CAPTURE="$work/ops-systemctl" OPS_CURL_CAPTURE="$work/ops-curl" OPS_CONTROLLER_ACTIVE="$work/controller-active" PATH="$work/ops-bin:$PATH" sh "$root/scripts/operate-remote.sh" recover "$(id -un)" 1000 "$work/ops-root/install" "$work/ops-root/secrets" > "$work/ops-output" 2>&1; code=$?
[ "$code" -eq 0 ] && grep -q 'recover completed' "$work/ops-output" && grep -q 'start switch-agent-controller-1000.service' "$work/ops-systemctl" && grep -q '/healthz' "$work/ops-curl" && grep -q '/mcp' "$work/ops-curl" && ! grep -q 'test-only-ops-token' "$work/ops-output"; check "recovery starts controller and verifies Graphiti, health, authenticated MCP without printing token" $?
OPS_DOCKER_CAPTURE="$work/ops-docker-bad" OPS_SYSTEMCTL_CAPTURE="$work/ops-systemctl-bad" OPS_CURL_CAPTURE="$work/ops-curl-bad" OPS_CONTROLLER_ACTIVE="$work/controller-active" OPS_GRAPHITI_UNHEALTHY=1 PATH="$work/ops-bin:$PATH" sh "$root/scripts/operate-remote.sh" recover "$(id -un)" 1000 "$work/ops-root/install" "$work/ops-root/secrets" > "$work/ops-graphiti-output" 2>&1; code=$?
[ "$code" -eq 1 ] && grep -q 'Graphiti is not healthy' "$work/ops-graphiti-output" && [ ! -e "$work/ops-curl-bad" ]; check "recovery stops before application probes when Graphiti is unhealthy" $?
mkdir -p "$work/ops-root/install/releases/v2.0.0/deploy" "$work/ops-root/.secrets.backup.recovery"
: > "$work/ops-root/install/releases/v2.0.0/deploy/compose.yaml"
: > "$work/ops-root/install/releases/v2.0.0/deploy/compose.release.yaml"
: > "$work/ops-root/install/releases/v2.0.0/deploy/heimdall.toml"
printf 'v2.0.0\n' > "$work/ops-root/install/releases/v2.0.0/RELEASE"
printf 'new-test-token\n' > "$work/ops-root/secrets/agent_token_test-author"
printf 'previous-recovery-token\n' > "$work/ops-root/.secrets.backup.recovery/agent_token_test-author"
chmod 0600 "$work/ops-root/secrets/agent_token_test-author" "$work/ops-root/.secrets.backup.recovery/agent_token_test-author"
ln -sfn "$work/ops-root/install/releases/v2.0.0" "$work/ops-root/install/current"
printf '%s\n%s\n1\n' "$work/ops-root/install/releases/v1.0.0" "$work/ops-root/.secrets.backup.recovery" > "$work/ops-root/install/.deploy-pending"
OPS_DOCKER_CAPTURE="$work/ops-journal-docker" OPS_SYSTEMCTL_CAPTURE="$work/ops-journal-systemctl" OPS_CURL_CAPTURE="$work/ops-journal-curl" OPS_CONTROLLER_ACTIVE="$work/controller-active" PATH="$work/ops-bin:$PATH" sh "$root/scripts/operate-remote.sh" recover "$(id -un)" 1000 "$work/ops-root/install" "$work/ops-root/secrets" > "$work/ops-journal-output" 2>&1; code=$?
[ "$code" -eq 0 ] && [ "$(readlink "$work/ops-root/install/current")" = "$work/ops-root/install/releases/v1.0.0" ] && grep -qx 'previous-recovery-token' "$work/ops-root/secrets/agent_token_test-author" && [ ! -e "$work/ops-root/install/.deploy-pending" ] && [ ! -e "$work/ops-root/.secrets.backup.recovery" ] && grep -q 'interrupted deployment rolled back' "$work/ops-journal-output"; check "recovery resumes a durable interrupted-deployment rollback before health checks" $?

# Controller setup can be planned locally without enrollment, secret contents, or system changes.
cat > "$work/bin/node" <<'SH'
#!/usr/bin/env bash
echo v22.13.0
SH
for command_name in opencode switch-agent-controller systemctl; do
	cat > "$work/bin/$command_name" <<'SH'
#!/usr/bin/env bash
exit 0
SH
done
cat > "$work/bin/switch-agent-controller" <<'SH'
#!/usr/bin/env bash
if [[ "${1:-}" == --version ]]; then echo 0.31.0; fi
SH
cat > "$work/bin/opencode" <<'SH'
#!/usr/bin/env bash
echo 1.19.5
SH
chmod +x "$work/bin/node" "$work/bin/opencode" "$work/bin/switch-agent-controller" "$work/bin/systemctl"
controller_secrets="$work/controller-secrets"
mkdir -m 0700 "$controller_secrets"
: > "$controller_secrets/openrouter_api_key"
chmod 0600 "$controller_secrets/openrouter_api_key"
out=$(PATH="$work/bin:$PATH" bash "$root/deploy/agents/install-controller.sh" --dry-run --controller-user "$(id -un)" --controller-data-dir "$work/controller-state" --secrets-dir "$controller_secrets" --agents-dir "$work/agents" --env-file "$work/agents.env" 2>&1); code=$?
[ "$code" -eq 0 ] && grep -q "service=switch-agent-controller-$(id -u "$(id -un)").service" <<<"$out" && grep -q "data_dir=$work/controller-state" <<<"$out" && grep -q 'dry-run: no files or services changed' <<<"$out" && [ ! -e "$work/agents.env" ]; check "controller dry-run reports service configuration without writing or enrolling" $?
cat > "$work/bin/node" <<'SH'
#!/usr/bin/env bash
echo v22.12.0
SH
out=$(PATH="$work/bin:$PATH" bash "$root/deploy/agents/install-controller.sh" --dry-run --controller-user "$(id -un)" --secrets-dir "$controller_secrets" --agents-dir "$work/agents" --env-file "$work/agents.env" 2>&1); code=$?
[ "$code" -eq 1 ] && grep -q 'Node.js 22.13 or newer' <<<"$out"; check "controller installer refuses an unsupported Node.js version" $?
cat > "$work/bin/node" <<'SH'
#!/usr/bin/env bash
echo v22.13.0
SH
cat > "$work/bin/opencode" <<'SH'
#!/usr/bin/env bash
echo 1.19.4
SH
out=$(PATH="$work/bin:$PATH" bash "$root/deploy/agents/install-controller.sh" --dry-run --controller-user "$(id -un)" --secrets-dir "$controller_secrets" --agents-dir "$work/agents" --env-file "$work/agents.env" 2>&1); code=$?
[ "$code" -eq 1 ] && grep -q 'OpenCode 1.19.5 is required' <<<"$out"; check "controller installer refuses an unsupported OpenCode version" $?
cat > "$work/bin/opencode" <<'SH'
#!/usr/bin/env bash
echo 1.19.5
SH
cat > "$work/bin/switch-agent-controller" <<'SH'
#!/usr/bin/env bash
if [[ "${1:-}" == --version ]]; then echo 0.30.0; fi
SH
out=$(PATH="$work/bin:$PATH" bash "$root/deploy/agents/install-controller.sh" --dry-run --controller-user "$(id -un)" --secrets-dir "$controller_secrets" --agents-dir "$work/agents" --env-file "$work/agents.env" 2>&1); code=$?
[ "$code" -eq 1 ] && grep -q 'switch-agent-controller 0.31.0 is required' <<<"$out"; check "controller installer refuses an unsupported Switch controller version" $?
cat > "$work/bin/switch-agent-controller" <<'SH'
#!/usr/bin/env bash
if [[ "${1:-}" == --version ]]; then echo 0.31.0; fi
SH
out=$(PATH="$work/bin:$PATH" bash "$root/deploy/agents/install-controller.sh" --dry-run --controller-user "$(id -un)" --secrets-dir "$work" --agents-dir "$work/agents" --env-file "$work/agents.env" 2>&1); code=$?
[ "$code" -eq 1 ] && grep -q 'private regular secret file is required' <<<"$out"; check "controller installer fails closed when OpenRouter secret file is absent" $?

# Full local controller flow with disposable credentials and fake Switch/systemd commands.
for name in openrouter_api_key agent_token_test-author agent_token_reviewer-1 agent_token_reviewer-2 agent_token_reviewer-3 agent_token_resolver; do
	printf 'stub-%s\n' "$name" > "$controller_secrets/$name"
	chmod 0600 "$controller_secrets/$name"
done
cat > "$work/bin/switch-agent-controller" <<'SH'
#!/usr/bin/env bash
if [[ "${1:-}" == --version ]]; then echo 0.31.0; exit 0; fi
printf '%s\n' "$*" >> "$CONTROLLER_CAPTURE"
if [[ "${1:-}" == doctor && "${SWITCH_DOCTOR_FAIL:-0}" == 1 ]]; then exit 1; fi
exit 0
SH
cat > "$work/bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$SYSTEMCTL_CAPTURE"
exit 0
SH
chmod +x "$work/bin/switch-agent-controller" "$work/bin/systemctl"
controller_agents="$work/installed-agents"
controller_env="$work/controller.env"
CONTROLLER_CAPTURE="$work/controller-calls" SYSTEMCTL_CAPTURE="$work/systemctl-calls" PATH="$work/bin:$PATH" bash "$root/deploy/agents/install-controller.sh" --controller-user "$(id -un)" --controller-data-dir "$work/controller-state" --secrets-dir "$controller_secrets" --agents-dir "$controller_agents" --env-file "$controller_env" > "$work/controller-output" 2>&1; code=$?
if [ "$code" -ne 0 ]; then cat "$work/controller-output"; fi
[ "$code" -eq 0 ] && grep -q 'install-service --separate-users' "$work/controller-calls" && grep -q -- '--data-dir /.*controller-state' "$work/controller-calls" && grep -q -- '--agents-dir' "$work/controller-calls" && grep -q -- '--agent-users 16' "$work/controller-calls" && grep -q 'switch-agent-controller-' "$work/systemctl-calls" && [ "$(stat -c '%a' "$controller_env")" = 600 ] && [ "$(stat -c '%a' "$controller_agents/agent/test-author/opencode.json")" = 600 ] && grep -q 'installed test-author' "$work/controller-output"; check "controller installation configures private env, service, and five role agent runtimes" $?
CONTROLLER_CAPTURE="$work/controller-calls" SYSTEMCTL_CAPTURE="$work/systemctl-calls" PATH="$work/bin:$PATH" bash "$root/deploy/agents/install-controller.sh" --controller-user "$(id -un)" --controller-data-dir "$work/controller-state" --secrets-dir "$controller_secrets" --agents-dir "$controller_agents" --env-file "$controller_env" > "$work/controller-retry-output" 2>&1; code=$?
[ "$code" -eq 0 ] && grep -q 'service active; agent runtime is ready' "$work/controller-retry-output" && [ "$(grep -c '^install-service --separate-users' "$work/controller-calls")" -eq 2 ] && [ "$(stat -c '%a' "$controller_env")" = 600 ]; check "controller setup and role config installation are repeatable" $?

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
[ "$code" -ne 0 ] && grep -q 'a real failure' <<<"$out"; check "a matching failing test fails and reports its assertion" $?
out=$(run drift 'Drift' "drift"); code=$?
[ "$code" -ne 0 ] && grep -q 'no test matches' <<<"$out"; check "a tag with no tests at all fails" $?
printf '//go:build e2e\n\npackage e2e\n\nfunc broken( {\n' > "$work/e2e/b_test.go"
out=$(run e2e '^TestTask_T24$' "e2e T24"); code=$?
[ "$code" -ne 0 ] && grep -q "expected ')', found '{'" <<<"$out"; check "a package that does not compile fails and reports the build error" $?
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

# --- the run guard: zero tests and silent packages are failures
out=$(gotest ./skips/ -run 'NoSuchTest$'); code=$?
[ "$code" -ne 0 ] && grep -qi 'no tests ran' <<<"$out"; check "gotest.sh fails when no test ran" $?
mkdir -p "$work/silent"
printf 'package silent\n\nimport "testing"\n\nfunc TestNever(t *testing.T) {}\n' > "$work/silent/n_test.go"
printf 'package silent\n\nimport (\n\t"os"\n\t"testing"\n)\n\nfunc TestMain(m *testing.M) { os.Exit(0) }\n' > "$work/silent/main_test.go"
out=$(gotest ./silent/ ./skips/ -run 'TestPasses$|TestNever$'); code=$?
[ "$code" -eq 0 ]; check "a -run filter may leave a package without tests" $?
out=$(gotest ./silent/); code=$?
[ "$code" -ne 0 ] && grep -qi 'silent' <<<"$out"; check "gotest.sh fails a package whose TestMain exits 0 before any test" $?
nojq=$(mktemp -d); for t in bash go dirname mktemp rm cat grep sed awk env; do ln -s "$(command -v $t)" "$nojq/$t" 2>/dev/null; done
out=$(cd "$work" && PATH="$nojq" HOME="$HOME" ROOT="$root" "$(command -v bash)" "$root/scripts/gotest.sh" ./skips/ -run 'TestPasses$' 2>&1); code=$?
[ "$code" -ne 0 ] && grep -q 'jq' <<<"$out"; check "gotest.sh fails and says so when jq is missing" $?
rm -rf "$nojq"
cat > "$work/e2e/c_test.go" <<'GO'
//go:build e2e

package e2e

import "testing"

func TestTask_T30(t *testing.T) { t.Skip("no token") }
GO
out=$(run e2e '^TestTask_T30$' "e2e T30"); code=$?
[ "$code" -ne 0 ] && grep -qi 'skipped tests are failures' <<<"$out"; check "run-tagged.sh fails a skipped real-system test" $?
rm "$work/e2e/c_test.go"

# --- hooks-install.sh: the pre-commit hook runs make fast and blocks a red commit
repo="$work/repo"; mkdir -p "$repo/scripts"
git -C "$repo" init -q && git -C "$repo" config user.email t@t && git -C "$repo" config user.name t
cp "$root/scripts/pre-commit" "$root/scripts/hooks-install.sh" "$repo/scripts/"
printf 'secrets-staged:\n\t@true\nfast:\n\t@echo fast-ran >> ran.log; exit 1\n' > "$repo/Makefile"
(cd "$repo" && bash scripts/hooks-install.sh >/dev/null 2>&1); check "hooks-install succeeds" $?
[ -x "$repo/.git/hooks/pre-commit" ]; check "the pre-commit hook is installed and executable" $?
printf 'x' > "$repo/a.txt"; git -C "$repo" add -A
out=$(git -C "$repo" commit -q -m red 2>&1); code=$?
[ "$code" -ne 0 ] && [ "$(git -C "$repo" rev-list --count HEAD 2>/dev/null || echo 0)" = "0" ] && grep -q fast-ran "$repo/ran.log"; check "a red make fast blocks the commit" $?
printf 'secrets-staged:\n\t@true\nfast:\n\t@true\n' > "$repo/Makefile"; git -C "$repo" add -A
git -C "$repo" commit -q -m green >/dev/null 2>&1; check "a green make fast lets the commit through" $?
(cd "$repo" && bash scripts/hooks-install.sh >/dev/null 2>&1); check "hooks-install is repeatable" $?

# the pre-commit hook stops a commit whose staged diff holds a secret, before make fast even runs
printf 'secrets-staged:\n\t@echo secret-found >> ran.log; exit 1\nfast:\n\t@echo fast-ran-2 >> ran.log\n' > "$repo/Makefile"
printf 'y' > "$repo/b.txt"; git -C "$repo" add -A
git -C "$repo" commit -q -m leak >/dev/null 2>&1; code=$?
[ "$code" -ne 0 ] && grep -q secret-found "$repo/ran.log" && ! grep -q fast-ran-2 "$repo/ran.log"; check "a secret in the staged diff blocks the commit before make fast" $?

out=$(bash "$root/deploy/tests/deploy-contract.sh" 2>&1); code=$?
[ "$code" -eq 0 ] && grep -q 'deploy contract passed' <<<"$out"; check "deploy runner contract passes" $?

echo "$total script cases, $fail failed"
[ "$fail" -eq 0 ]
