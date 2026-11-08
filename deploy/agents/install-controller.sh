#!/usr/bin/env bash
# Install the enrolled Switch agent controller and its shared, private environment file.
set -euo pipefail

usage() { echo 'usage: install-controller.sh [--dry-run] [--controller-user USER] [--controller-data-dir DIR] [--secrets-dir DIR] [--agents-dir DIR]' >&2; }
dry_run=false
controller_user=${SWITCH_CONTROLLER_USER:-ubuntu}
secrets_dir=${HEIMDALL_SECRETS_DIR:-/etc/heimdall/secrets}
agents_dir=${SWITCH_AGENTS_DIR:-}
controller_data_dir=${SWITCH_CONTROLLER_DATA_DIR:-}
env_file=${SWITCH_CONTROLLER_ENV_FILE:-/etc/switch/agents.env}
agent_users=${SWITCH_AGENT_USERS:-16}
while (($#)); do
  case "$1" in
    --dry-run) dry_run=true; shift ;;
    --controller-user) [[ $# -ge 2 ]] || { usage; exit 2; }; controller_user=$2; shift 2 ;;
    --secrets-dir) [[ $# -ge 2 ]] || { usage; exit 2; }; secrets_dir=$2; shift 2 ;;
    --agents-dir) [[ $# -ge 2 ]] || { usage; exit 2; }; agents_dir=$2; shift 2 ;;
    --controller-data-dir) [[ $# -ge 2 ]] || { usage; exit 2; }; controller_data_dir=$2; shift 2 ;;
    --env-file) [[ $# -ge 2 ]] || { usage; exit 2; }; env_file=$2; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo 'controller install: unknown option' >&2; usage; exit 2 ;;
  esac
done

if ! id "$controller_user" >/dev/null 2>&1; then echo "controller install: controller user $controller_user does not exist" >&2; exit 1; fi
controller_uid=$(id -u "$controller_user")
if [[ -z "$agents_dir" ]]; then agents_dir="/var/lib/switch-agents/$controller_uid"; fi
if [[ -z "$controller_data_dir" ]]; then controller_data_dir="/var/lib/switch-agent-controller-data/$controller_uid"; fi
case "$controller_data_dir" in /*) ;; *) echo 'controller install: controller data directory must be absolute' >&2; exit 2 ;; esac
case "$controller_data_dir" in *$'\n'*|*$'\r'*) echo 'controller install: controller data directory must be one line' >&2; exit 2 ;; esac
for command_name in node opencode switch-agent-controller systemctl install python3; do
  command -v "$command_name" >/dev/null 2>&1 || { echo "controller install: required command unavailable: $command_name" >&2; exit 1; }
done
node_version=$(node --version 2>/dev/null || true)
node_version=${node_version#v}
IFS=. read -r node_major node_minor _ <<< "$node_version"
if [[ ! "$node_major" =~ ^[0-9]+$ || ! "$node_minor" =~ ^[0-9]+$ ]] || (( node_major < 22 || (node_major == 22 && node_minor < 13) )); then
  echo 'controller install: Switch requires Node.js 22.13 or newer; install the pinned runtime first' >&2
  exit 1
fi
if [[ ! "$agent_users" =~ ^[0-9]+$ ]] || (( agent_users < 1 || agent_users > 99 )); then
  echo 'controller install: SWITCH_AGENT_USERS must be between 1 and 99' >&2
  exit 1
fi
for runtime in node opencode switch-agent-controller; do
  runtime_path=$(command -v "$runtime")
  case "$runtime_path" in
    /home/*|/root/*|/run/user/*) echo "controller install: $runtime must be installed outside home directories for isolated agents" >&2; exit 1 ;;
  esac
done
opencode_version=$(opencode --version 2>/dev/null || true)
if [[ "$opencode_version" != 1.19.5 ]]; then
	echo 'controller install: OpenCode 1.19.5 is required; install the pinned runtime first' >&2
	exit 1
fi
switch_version=$(switch-agent-controller --version 2>/dev/null || true)
if [[ "$switch_version" != 0.31.0 ]]; then
	echo 'controller install: switch-agent-controller 0.31.0 is required; install the pinned verified release first' >&2
	exit 1
fi
if [[ ! -r "$secrets_dir/deploy_api_key" || ! -f "$secrets_dir/deploy_api_key" || -L "$secrets_dir/deploy_api_key" ]]; then
  echo "controller install: private regular secret file is required: $secrets_dir/deploy_api_key" >&2
  exit 1
fi
mode=$(stat -c '%a' "$secrets_dir/deploy_api_key")
if (( (8#$mode & 077) != 0 )); then echo 'controller install: Deploy key file must be private (mode 0600 or stricter)' >&2; exit 1; fi
if $dry_run; then
	printf 'plan controller user=%s uid=%s data_dir=%s agents_dir=%s env_file=%s agent_users=%s service=switch-agent-controller-%s.service\n' "$controller_user" "$controller_uid" "$controller_data_dir" "$agents_dir" "$env_file" "$agent_users" "$controller_uid"
  echo 'plan shared env contains Deploy key from a private file and SWITCH_CONTROLLER_SELF_UPDATE=off'
  echo 'dry-run: no files or services changed'
  exit 0
fi

install -d -m 0750 "$(dirname "$env_file")" "$agents_dir"
tmp_env=$(mktemp "$(dirname "$env_file")/.agents.env.XXXXXX")
trap 'rm -f "$tmp_env"' EXIT
# Python copies one validated line into the env file without sending its contents to stdout or argv.
python3 - "$secrets_dir/deploy_api_key" "$tmp_env" <<'PY'
import json
import os
import sys
source, destination = sys.argv[1:]
with open(source, "r", encoding="utf-8") as handle:
    value = handle.read().strip()
if not value or any(char in value for char in "\r\n\0"):
    raise SystemExit("controller install: Deploy key file must contain one non-empty line")
with open(destination, "w", encoding="utf-8") as handle:
    handle.write("DEPLOY_API_KEY=" + json.dumps(value) + "\n")
    handle.write("SWITCH_CONTROLLER_SELF_UPDATE=off\n")
os.chmod(destination, 0o600)
PY
chown "$controller_user" "$tmp_env"
mv -f "$tmp_env" "$env_file"
chmod 0600 "$env_file"
controller_uid=$(id -u "$controller_user")
releases_dir=${SWITCH_CONTROLLER_RELEASES_DIR:-/var/lib/switch-agent-controller/$controller_uid/releases}
switch-agent-controller install-service --separate-users --user "$controller_user" --data-dir "$controller_data_dir" --agents-dir "$agents_dir" --agent-users "$agent_users" --releases-dir "$releases_dir" --env-file "$env_file"
systemctl enable --now "switch-agent-controller-$controller_uid.service"
systemctl is-active --quiet "switch-agent-controller-$controller_uid.service" || { echo 'controller install: systemd service did not become active' >&2; exit 1; }
if ! switch-agent-controller doctor --data-dir "$controller_data_dir" --env-file "$env_file"; then
	echo 'controller install: upstream doctor failed; inspect enrollment, provider setup, and controller service before retrying' >&2
	exit 1
fi
script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$script_dir/../.." && pwd)
bash "$script_dir/install.sh" --repo "$repo" --agents-dir "$agents_dir" --secrets-dir "$secrets_dir" --owner "$controller_user"
echo 'controller install: service active; agent runtime is ready'
