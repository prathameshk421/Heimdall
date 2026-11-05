#!/usr/bin/env bash
# Operate the already-deployed stack through the configured SSM-backed SSH alias.
set -euo pipefail

action=${1:-}
host=${DEPLOY_HOST:-heimdall-vm}
agent_user=${DEPLOY_AGENT_USER:-sa1000-01}
controller_uid=${SWITCH_CONTROLLER_UID:-1000}
if [[ $# -ne 1 || ! "$action" =~ ^(restart|recover)$ ]]; then
	echo 'usage: scripts/operate.sh restart|recover' >&2
	exit 2
fi

ssh "$host" "sudo -n /bin/sh -s -- '$action' '$agent_user' '$controller_uid'" <<'REMOTE'
set -eu
action=$1
agent_user=$2
controller_uid=$3
current=/opt/heimdall/current
if [ ! -f "$current/deploy/compose.yaml" ]; then
	echo 'operation: no installed release at /opt/heimdall/current; run make deploy first' >&2
	exit 2
fi
cd "$current"
export HEIMDALL_CONFIG="$current/deploy/heimdall.toml"
export HEIMDALL_SECRETS_DIR=/etc/heimdall/secrets
export HEIMDALL_RELEASE=$(cat "$current/RELEASE")
if [ "$action" = restart ]; then
	docker compose -f deploy/compose.yaml -f deploy/compose.release.yaml restart heimdall-server
else
	docker compose -f deploy/compose.yaml -f deploy/compose.release.yaml up -d --wait
	controller_service="switch-agent-controller-$controller_uid.service"
	if ! systemctl list-unit-files "$controller_service" --no-legend | grep -q "$controller_service"; then
		echo "operation: $controller_service is not installed; complete the agent-controller setup first" >&2
		exit 1
	fi
	if ! systemctl is-active --quiet "$controller_service"; then
		systemctl start "$controller_service"
	fi
fi
graphiti_id=$(docker compose -f deploy/compose.yaml -f deploy/compose.release.yaml ps -q graphiti)
if [ -z "$graphiti_id" ] || [ "$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}missing{{end}}' "$graphiti_id")" != healthy ]; then
	echo 'operation: Graphiti is not healthy; inspect docker compose logs graphiti and verify OpenRouter configuration' >&2
	exit 1
fi
if ! curl --fail --silent --show-error http://127.0.0.1:8080/healthz >/dev/null; then
	echo 'operation: /healthz failed; inspect docker compose logs heimdall-server' >&2
	exit 1
fi
token=$(cat /etc/heimdall/secrets/agent_token_test-author)
if ! sudo -u "$agent_user" curl --fail --silent --show-error \
  -H "Authorization: Bearer $token" -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"heimdall-ops-check","version":"1"}}}' \
  http://127.0.0.1:8080/mcp >/dev/null; then
	echo 'operation: authenticated MCP check failed; verify the agent token and server logs' >&2
	exit 1
fi
echo "operation: $action completed; health and authenticated MCP checks passed"
REMOTE
