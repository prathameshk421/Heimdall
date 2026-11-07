#!/usr/bin/env bash
# Operate the already-deployed stack through the configured SSM-backed SSH alias.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
action=${1:-}
host=${DEPLOY_HOST:-heimdall-vm}
agent_user=${DEPLOY_AGENT_USER:-sa1000-01}
controller_uid=${SWITCH_CONTROLLER_UID:-1000}
if [[ $# -ne 1 || ! "$action" =~ ^(restart|recover)$ ]]; then
	echo 'usage: scripts/operate.sh restart|recover' >&2
	exit 2
fi
[[ "$agent_user" =~ ^[a-z_][a-z0-9_-]*\$?$ ]] || { echo 'operation: DEPLOY_AGENT_USER must be a valid Unix account name' >&2; exit 2; }
[[ "$controller_uid" =~ ^[0-9]+$ ]] || { echo 'operation: SWITCH_CONTROLLER_UID must be numeric' >&2; exit 2; }

ssh "$host" "sudo -n /bin/sh -s -- '$action' '$agent_user' '$controller_uid' /opt/heimdall /etc/heimdall/secrets" < "$root/scripts/operate-remote.sh"
