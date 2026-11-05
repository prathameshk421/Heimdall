#!/usr/bin/env bash
# Build Heimdall for Linux and install its loopback-only Compose stack over the SSM SSH alias.
set -euo pipefail

usage() {
	cat <<'USAGE'
Usage: deploy/deploy.sh [--host SSH_ALIAS] [--config FILE] [--secrets-dir DIR]

Builds heimdall-server for Linux, transfers the binary and Compose artifacts,
installs /etc/heimdall/secrets from a local directory, and enables the systemd
unit. SSH must already provide the SSM-backed connection (default: heimdall-vm).
USAGE
}

host=heimdall-vm
config=
secrets_dir=
while (($#)); do
	case "$1" in
		--host) (($# >= 2)) || { usage >&2; exit 2; }; host=$2; shift 2 ;;
		--config) (($# >= 2)) || { usage >&2; exit 2; }; config=$2; shift 2 ;;
		--secrets-dir) (($# >= 2)) || { usage >&2; exit 2; }; secrets_dir=$2; shift 2 ;;
		-h|--help) usage; exit 0 ;;
		*) printf 'deploy: unknown option: %s\n' "$1" >&2; usage >&2; exit 2 ;;
	esac
done

[[ -n "$config" && -f "$config" ]] || { echo 'deploy: --config must name an existing heimdall.toml' >&2; exit 2; }
[[ -n "$secrets_dir" && -d "$secrets_dir" ]] || { echo 'deploy: --secrets-dir must name an existing directory' >&2; exit 2; }
required_secrets=(database_url postgres_password openrouter_api_key jira_email jira_api_token github_app_id github_app_installation_id github_app_private_key switch_api_key switch_registration_token agent_token_test-author agent_token_reviewer-1 agent_token_reviewer-2 agent_token_reviewer-3 agent_token_resolver)
for name in "${required_secrets[@]}"; do
	[[ -f "$secrets_dir/$name" && ! -L "$secrets_dir/$name" ]] || { printf 'deploy: required regular secret file is missing: %s\n' "$name" >&2; exit 2; }
done
while IFS= read -r -d '' entry; do
	[[ -f "$entry" && ! -L "$entry" ]] || { echo 'deploy: secrets directory may contain only regular files' >&2; exit 2; }
done < <(find "$secrets_dir" -mindepth 1 -maxdepth 1 -print0)
command -v go >/dev/null || { echo 'deploy: go is required' >&2; exit 1; }
command -v ssh >/dev/null || { echo 'deploy: ssh is required' >&2; exit 1; }
command -v scp >/dev/null || { echo 'deploy: scp is required' >&2; exit 1; }

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/payload/deploy/graphiti"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o "$work/payload/heimdall-server" "$root/cmd/heimdall-server"
install -m 0644 "$root/deploy/compose.yaml" "$work/payload/deploy/compose.yaml"
install -m 0644 "$root/deploy/Dockerfile.server" "$work/payload/deploy/Dockerfile.server"
install -m 0644 "$root/deploy/Dockerfile.server.dockerignore" "$work/payload/deploy/Dockerfile.server.dockerignore"
install -m 0644 "$root/deploy/graphiti/config.yaml" "$work/payload/deploy/graphiti/config.yaml"
install -m 0644 "$config" "$work/payload/heimdall.toml"
chmod 0600 "$work/payload/heimdall.toml"

ssh "$host" 'sudo install -d -o root -g root -m 0755 /opt/heimdall /etc/heimdall /etc/heimdall/secrets'
scp "$work/payload/heimdall-server" "$host:/tmp/heimdall-server.new"
scp "$work/payload/deploy/compose.yaml" "$work/payload/deploy/Dockerfile.server" "$work/payload/deploy/Dockerfile.server.dockerignore" "$host:/tmp/"
scp "$work/payload/deploy/graphiti/config.yaml" "$host:/tmp/heimdall-graphiti-config.yaml"
scp "$work/payload/heimdall.toml" "$host:/tmp/heimdall.toml.new"

# Content comparisons keep reruns from replacing files or restarting healthy services.
ssh "$host" 'sudo sh -s' <<'REMOTE'
set -eu
install_if_changed() {
	from=$1 to=$2 mode=$3
	if ! cmp -s "$from" "$to"; then
		install -o root -g root -m "$mode" "$from" "$to"
	fi
}
install_if_changed /tmp/heimdall-server.new /usr/local/bin/heimdall-server 0755
install_if_changed /tmp/heimdall-server.new /opt/heimdall/heimdall-server 0644
install_if_changed /tmp/compose.yaml /opt/heimdall/compose.yaml 0644
install_if_changed /tmp/Dockerfile.server /opt/heimdall/Dockerfile.server 0644
install_if_changed /tmp/Dockerfile.server.dockerignore /opt/heimdall/Dockerfile.server.dockerignore 0644
install_if_changed /tmp/heimdall-graphiti-config.yaml /opt/heimdall/graphiti-config.yaml 0644
install_if_changed /tmp/heimdall.toml.new /etc/heimdall/heimdall.toml 0600
rm -f /tmp/heimdall-server.new /tmp/compose.yaml /tmp/Dockerfile.server /tmp/Dockerfile.server.dockerignore /tmp/heimdall-graphiti-config.yaml /tmp/heimdall.toml.new
REMOTE

# Secrets travel only over the established SSH transport. Tar names and modes are
# retained; contents are never printed or expanded by this script.
tar -C "$secrets_dir" -cf - . | ssh "$host" 'sudo tar --no-same-owner -xf - -C /etc/heimdall/secrets && sudo chown -R root:root /etc/heimdall/secrets && sudo find /etc/heimdall/secrets -type d -exec chmod 0700 {} + && sudo find /etc/heimdall/secrets -type f -exec chmod 0600 {} +'

ssh "$host" 'sudo install -d -o root -g root -m 0755 /opt/heimdall/graphiti && sudo install -o root -g root -m 0644 /opt/heimdall/graphiti-config.yaml /opt/heimdall/graphiti/config.yaml && sudo rm -f /opt/heimdall/graphiti-config.yaml && sudo sh -s' <<'REMOTE'
set -eu
cat >/tmp/heimdall-compose.service.new <<'UNIT'
[Unit]
Description=Heimdall Compose stack
Requires=docker.service
After=docker.service network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/opt/heimdall
Environment=HEIMDALL_CONFIG=/etc/heimdall/heimdall.toml
Environment=HEIMDALL_SECRETS_DIR=/etc/heimdall/secrets
ExecStart=/usr/bin/docker compose -f /opt/heimdall/compose.yaml up -d --build --wait
ExecStop=/usr/bin/docker compose -f /opt/heimdall/compose.yaml down
TimeoutStartSec=600

[Install]
WantedBy=multi-user.target
UNIT
if ! cmp -s /tmp/heimdall-compose.service.new /etc/systemd/system/heimdall-compose.service; then
	install -o root -g root -m 0644 /tmp/heimdall-compose.service.new /etc/systemd/system/heimdall-compose.service
	systemctl daemon-reload
fi
rm -f /tmp/heimdall-compose.service.new
systemctl enable --now heimdall-compose.service
REMOTE

echo "Heimdall deploy completed for SSH host $host. VM health and rerun idempotence are not verified by this local invocation."
