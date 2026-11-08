#!/usr/bin/env bash
# Build Heimdall for Linux and install its loopback-only Compose stack over the SSM SSH alias.
set -euo pipefail

usage() {
	cat <<'USAGE'
Usage: HEIMDALL_GO_IMAGE=registry/name@sha256:<digest> deploy/deploy.sh [--host SSH_ALIAS] [--config FILE] [--secrets-dir DIR]

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

go_image=${HEIMDALL_GO_IMAGE:-}
[[ "$go_image" =~ ^[A-Za-z0-9._:/-]+@sha256:[a-f0-9]{64}$ ]] || { echo 'deploy: HEIMDALL_GO_IMAGE must be an allowlisted Go image pinned by sha256 digest' >&2; exit 2; }

[[ -n "$config" && -f "$config" ]] || { echo 'deploy: --config must name an existing heimdall.toml' >&2; exit 2; }
[[ -n "$secrets_dir" && -d "$secrets_dir" ]] || { echo 'deploy: --secrets-dir must name an existing directory' >&2; exit 2; }
required_secrets=(database_url postgres_password deploy_api_key jira_email jira_api_token github_app_id github_app_installation_id github_app_private_key switch_api_key switch_registration_token agent_token_test-author agent_token_reviewer-1 agent_token_reviewer-2 agent_token_reviewer-3 agent_token_resolver)
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
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o "$work/payload/heimdall-runner" "$root/cmd/heimdall-runner"
install -m 0644 "$root/deploy/compose.yaml" "$work/payload/deploy/compose.yaml"
install -m 0644 "$root/deploy/Dockerfile.server" "$work/payload/deploy/Dockerfile.server"
install -m 0644 "$root/deploy/Dockerfile.server.dockerignore" "$work/payload/deploy/Dockerfile.server.dockerignore"
install -m 0644 "$root/deploy/heimdall-runner.service" "$work/payload/heimdall-runner.service"
printf 'HEIMDALL_GO_IMAGE=%s\n' "$go_image" >"$work/payload/runner.conf"
chmod 0600 "$work/payload/runner.conf"
install -m 0644 "$root/deploy/graphiti/config.yaml" "$work/payload/deploy/graphiti/config.yaml"
install -m 0644 "$config" "$work/payload/heimdall.toml"
chmod 0600 "$work/payload/heimdall.toml"

ssh "$host" 'sudo install -d -o root -g root -m 0755 /opt/heimdall /opt/heimdall/deploy /etc/heimdall /etc/heimdall/secrets'
scp "$work/payload/heimdall-server" "$host:/tmp/heimdall-server.new"
scp "$work/payload/heimdall-runner" "$host:/tmp/heimdall-runner.new"
scp "$work/payload/heimdall-runner.service" "$host:/tmp/heimdall-runner.service.new"
scp "$work/payload/runner.conf" "$host:/tmp/runner.conf.new"
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
install_if_changed /tmp/heimdall-runner.new /usr/local/bin/heimdall-runner 0755
install_if_changed /tmp/heimdall-runner.service.new /etc/systemd/system/heimdall-runner.service 0644
install_if_changed /tmp/runner.conf.new /etc/heimdall/runner.conf 0600
install_if_changed /tmp/compose.yaml /opt/heimdall/deploy/compose.yaml 0644
install_if_changed /tmp/Dockerfile.server /opt/heimdall/deploy/Dockerfile.server 0644
install_if_changed /tmp/Dockerfile.server.dockerignore /opt/heimdall/deploy/Dockerfile.server.dockerignore 0644
install_if_changed /tmp/heimdall-graphiti-config.yaml /opt/heimdall/deploy-graphiti-config.yaml 0644
install_if_changed /tmp/heimdall.toml.new /etc/heimdall/heimdall.toml 0640
chgrp 65532 /etc/heimdall/heimdall.toml
chmod 0640 /etc/heimdall/heimdall.toml
rm -f /tmp/heimdall-server.new /tmp/heimdall-runner.new /tmp/heimdall-runner.service.new /tmp/runner.conf.new /tmp/compose.yaml /tmp/Dockerfile.server /tmp/Dockerfile.server.dockerignore /tmp/heimdall-graphiti-config.yaml /tmp/heimdall.toml.new
REMOTE

ssh "$host" 'sudo sh -s' <<'REMOTE'
set -eu
command -v podman >/dev/null || { echo 'deploy: rootless Podman must be installed on the VM' >&2; exit 1; }
command -v crun >/dev/null || { echo 'deploy: the crun OCI runtime is required: under cgroupfs rootless Podman cannot apply cgroup limits with runc' >&2; exit 1; }
getent group heimdall-runner-socket >/dev/null || groupadd --system heimdall-runner-socket
id heimdall-runner >/dev/null 2>&1 || useradd --system --create-home --home-dir /var/lib/heimdall-runner --shell /usr/sbin/nologin heimdall-runner
if ! grep -q '^heimdall-runner:' /etc/subuid 2>/dev/null || ! grep -q '^heimdall-runner:' /etc/subgid 2>/dev/null; then
	start=200000
	while awk -F: -v lo="$start" -v hi="$((start + 65535))" '$2 <= hi && $2 + $3 - 1 >= lo { found=1 } END { exit !found }' /etc/subuid /etc/subgid 2>/dev/null; do start=$((start + 65536)); done
	usermod --add-subuids "$start-$((start + 65535))" --add-subgids "$start-$((start + 65535))" heimdall-runner
fi
install -d -o heimdall-runner -g heimdall-runner -m 0700 /var/lib/heimdall-runner
# Pin native overlay. Without it Podman may fall back to fuse-overlayfs on first use, records that choice in the store,
# and then needs /dev/fuse, which the unit's PrivateDevices hides: every job then fails to mount its root filesystem.
install -d -o heimdall-runner -g heimdall-runner -m 0700 /var/lib/heimdall-runner/.config /var/lib/heimdall-runner/.config/containers
printf '[storage]\ndriver = "overlay"\n\n[storage.options.overlay]\nmount_program = ""\n' >/tmp/heimdall-runner-storage.conf.new
if ! cmp -s /tmp/heimdall-runner-storage.conf.new /var/lib/heimdall-runner/.config/containers/storage.conf; then
	install -o heimdall-runner -g heimdall-runner -m 0600 /tmp/heimdall-runner-storage.conf.new /var/lib/heimdall-runner/.config/containers/storage.conf
fi
rm -f /tmp/heimdall-runner-storage.conf.new
# A store created with fuse-overlayfs cannot be reused with native overlay; it holds only the pulled Go image, which the
# pull below fetches again.
cd /
if [ "$(cat /var/lib/heimdall-runner/.local/share/containers/storage/overlay/.has-mount-program 2>/dev/null)" = "true" ]; then
	runuser -u heimdall-runner -- env PATH=/usr/sbin:/usr/bin:/sbin:/bin HOME=/var/lib/heimdall-runner XDG_RUNTIME_DIR=/run/heimdall-podman podman system reset --force
fi
install -d -o root -g root -m 0755 /opt/heimdall
install -d -o root -g heimdall-runner-socket -m 0750 /etc/heimdall-runner /etc/heimdall-runner/profiles
runner_gid=$(getent group heimdall-runner-socket | cut -d: -f3)
printf 'HEIMDALL_RUNNER_GID=%s\n' "$runner_gid" >/tmp/heimdall.env.new
if ! cmp -s /tmp/heimdall.env.new /opt/heimdall/deploy/.env; then install -o root -g root -m 0644 /tmp/heimdall.env.new /opt/heimdall/deploy/.env; fi
rm -f /tmp/heimdall.env.new
REMOTE

# Secrets travel only over the established SSH transport. Tar names and modes are
# retained; contents are never printed or expanded by this script.
tar -C "$secrets_dir" -cf - . | ssh "$host" 'sudo tar --no-same-owner -xf - -C /etc/heimdall/secrets && sudo chown -R 65532:65532 /etc/heimdall/secrets && sudo find /etc/heimdall/secrets -type d -exec chmod 0700 {} + && sudo find /etc/heimdall/secrets -type f -exec chmod 0600 {} +'

ssh "$host" 'sudo install -d -o root -g root -m 0755 /opt/heimdall/deploy/graphiti && sudo install -o root -g root -m 0644 /opt/heimdall/deploy-graphiti-config.yaml /opt/heimdall/deploy/graphiti/config.yaml && sudo rm -f /opt/heimdall/deploy-graphiti-config.yaml && sudo sh -s' <<'REMOTE'
set -eu
systemctl daemon-reload
systemctl enable --now heimdall-runner.service
go_image=$(sed -n 's/^HEIMDALL_GO_IMAGE=//p' /etc/heimdall/runner.conf)
test -n "$go_image" || { echo 'deploy: pinned Go image is missing from runner.conf' >&2; exit 1; }
cd /
runuser -u heimdall-runner -- env PATH=/usr/sbin:/usr/bin:/sbin:/bin HOME=/var/lib/heimdall-runner XDG_RUNTIME_DIR=/run/heimdall-podman podman pull "$go_image"
test -S /run/heimdall-runner/runner.sock || { echo 'deploy: heimdall-runner did not create its Unix socket' >&2; exit 1; }
cat >/tmp/heimdall-compose.service.new <<'UNIT'
[Unit]
Description=Heimdall Compose stack
Requires=docker.service
Requires=heimdall-runner.service
After=docker.service heimdall-runner.service network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/opt/heimdall/deploy
Environment=HEIMDALL_CONFIG=/etc/heimdall/heimdall.toml
Environment=HEIMDALL_SECRETS_DIR=/etc/heimdall/secrets
ExecStart=/usr/bin/docker compose -f /opt/heimdall/deploy/compose.yaml up -d --build --wait
ExecStop=/usr/bin/docker compose -f /opt/heimdall/deploy/compose.yaml down
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
