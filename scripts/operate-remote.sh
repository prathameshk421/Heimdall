#!/bin/sh
# Remote half of scripts/operate.sh. Kept isolated so reconciliation can be exercised with local fakes.
set -eu

action=$1
agent_user=$2
controller_uid=$3
install_root=$4
secrets_root=$5
current="$install_root/current"
pending_file="$install_root/.deploy-pending"
auth_config=
cleanup() {
	if [ -n "$auth_config" ]; then rm -f "$auth_config" || true; fi
}
trap cleanup EXIT
make_mcp_auth_config() {
	auth_config=$(mktemp /tmp/heimdall-mcp-auth.XXXXXX)
	python3 - "$secrets_root/agent_token_test-author" "$auth_config" <<'PY'
import json
import os
import sys
source, destination = sys.argv[1:]
with open(source, "r", encoding="utf-8") as handle:
    token = handle.read().strip()
if not token or any(char in token for char in "\r\n\0"):
    raise SystemExit("invalid agent token file")
with open(destination, "w", encoding="utf-8") as handle:
    handle.write("header = " + json.dumps("Authorization: Bearer " + token) + "\n")
os.chmod(destination, 0o600)
PY
	python_status=$?
	[ "$python_status" -eq 0 ] || return "$python_status"
	chown "$agent_user" "$auth_config" || return 1
	chmod 0600 "$auth_config" || return 1
}
if [ -e "$pending_file" ] || [ -L "$pending_file" ]; then
	if [ "$action" != recover ] || [ -L "$pending_file" ] || [ ! -f "$pending_file" ]; then
		echo 'operation: interrupted deployment journal found; run make recover to reconcile it' >&2
		exit 1
	fi
	{
		IFS= read -r previous_current || exit 1
		IFS= read -r secret_backup || exit 1
		IFS= read -r secrets_existed || exit 1
	} < "$pending_file"
	secret_parent=$(dirname "$secrets_root")
	secret_base=$(basename "$secrets_root")
	secret_parent_real=$(readlink -f "$secret_parent")
	secret_backup_real=$(readlink -f "$secret_backup" 2>/dev/null || true)
	case "$secret_backup_real" in "$secret_parent_real/.$secret_base.backup."*) ;; *) echo 'operation: deployment journal has an invalid secret backup path' >&2; exit 1 ;; esac
	secret_backup=$secret_backup_real
	case "$secrets_existed" in 0|1) ;; *) echo 'operation: deployment journal has an invalid secret state' >&2; exit 1 ;; esac
	if [ "$secrets_existed" = 1 ]; then
		if [ ! -d "$secret_backup" ] || [ -L "$secret_backup" ]; then
			echo 'operation: previous secret backup is missing; deployment journal is preserved for inspection' >&2
			exit 1
		fi
		install -d -m 0700 "$secrets_root"
		rsync -a --delete --chmod=F600,D700 "$secret_backup/" "$secrets_root/"
		chmod 0600 "$secrets_root"/*
	else
		rm -rf "$secrets_root"
	fi
	if [ "$previous_current" = - ]; then
		failed_release=$(readlink -f "$current" 2>/dev/null || true)
		case "$failed_release" in "$install_root"/releases/*) ;; '') ;; *) echo 'operation: interrupted first deployment points outside managed releases' >&2; exit 1 ;; esac
		rm -f "$current"
		if [ -n "$failed_release" ] && [ -f "$failed_release/deploy/compose.yaml" ]; then
			cd "$failed_release"
			docker compose -f deploy/compose.yaml -f deploy/compose.release.yaml down --remove-orphans
		fi
		rm -f "$pending_file"
		rm -rf "$secret_backup"
		echo 'operation: interrupted first deployment was cleaned up; no previous release existed'
		exit 0
	fi
	previous_current_real=$(readlink -f "$previous_current" 2>/dev/null || true)
	case "$previous_current_real" in "$install_root"/releases/*) ;; *) echo 'operation: deployment journal points outside the managed release directory' >&2; exit 1 ;; esac
	previous_current=$previous_current_real
	if [ ! -f "$previous_current/deploy/compose.yaml" ]; then
		echo 'operation: previous release from deployment journal is missing; journal is preserved' >&2
		exit 1
	fi
	ln -sfn "$previous_current" "$install_root/current.recovery"
	mv -Tf "$install_root/current.recovery" "$current"
	cd "$current"
	export HEIMDALL_CONFIG="$current/deploy/heimdall.toml"
	export HEIMDALL_SECRETS_DIR=$secrets_root
	export HEIMDALL_RELEASE=$(cat "$current/RELEASE")
	docker compose -f deploy/compose.yaml -f deploy/compose.release.yaml up -d --wait
fi
current_real=$(readlink -f "$current" 2>/dev/null || true)
case "$current_real" in "$install_root"/releases/*) ;; *) echo 'operation: current release points outside the managed release directory' >&2; exit 1 ;; esac
if [ ! -f "$current/deploy/compose.yaml" ]; then
	echo 'operation: no installed release; run make deploy first' >&2
	exit 2
fi
cd "$current"
export HEIMDALL_CONFIG="$current/deploy/heimdall.toml"
export HEIMDALL_SECRETS_DIR=$secrets_root
export HEIMDALL_RELEASE=$(cat "$current/RELEASE")
if [ "$action" = restart ]; then
	docker compose -f deploy/compose.yaml -f deploy/compose.release.yaml restart heimdall-server
else
	docker compose -f deploy/compose.yaml -f deploy/compose.release.yaml up -d --wait
fi
controller_service="switch-agent-controller-$controller_uid.service"
if ! systemctl list-unit-files "$controller_service" --no-legend | grep -q "$controller_service"; then
	echo "operation: $controller_service is not installed; complete the agent-controller setup first" >&2
	exit 1
fi
if ! systemctl is-active --quiet "$controller_service"; then
	systemctl start "$controller_service"
fi
systemctl is-active --quiet "$controller_service" || { echo "operation: $controller_service did not become active" >&2; exit 1; }
graphiti_id=$(docker compose -f deploy/compose.yaml -f deploy/compose.release.yaml ps -q graphiti)
if [ -z "$graphiti_id" ] || [ "$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}missing{{end}}' "$graphiti_id")" != healthy ]; then
	echo 'operation: Graphiti is not healthy; inspect docker compose logs graphiti and verify the Deploy key and the embeddings service' >&2
	exit 1
fi
if ! curl --fail --silent --show-error http://127.0.0.1:8080/healthz >/dev/null; then
	echo 'operation: /healthz failed; inspect docker compose logs heimdall-server' >&2
	exit 1
fi
make_mcp_auth_config
if ! sudo -u "$agent_user" curl --fail --silent --show-error \
	--config "$auth_config" -H 'Content-Type: application/json' \
	--data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"heimdall-ops-check","version":"1"}}}' \
	http://127.0.0.1:8080/mcp >/dev/null; then
	echo 'operation: authenticated MCP check failed; verify the agent token and server logs' >&2
	exit 1
fi
rm -f "$auth_config"
auth_config=
if [ -e "$pending_file" ]; then
	rm -f "$pending_file"
	rm -rf "$secret_backup"
	echo 'operation: interrupted deployment rolled back to the previous healthy release'
fi
echo "operation: $action completed; health and authenticated MCP checks passed"
