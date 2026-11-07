#!/bin/sh
# Remote half of scripts/deploy.sh. Kept separate so rollback can be exercised against a disposable root.
set -eu

tag=$1
tmp=$2
agent_user=$3
archive_hash=$4
install_root=$5
secrets_root=$6
release="$install_root/releases/$tag-$archive_hash"
current="$install_root/current"
previous_current=
if [ -L "$current" ]; then
	previous_current=$(readlink -f "$current" 2>/dev/null || true)
fi
case "$previous_current" in
	"$install_root"/releases/*) ;;
	'') ;;
	*) echo 'deploy: current release symlink points outside the managed release directory' >&2; exit 1 ;;
esac
cutover=0
secrets_changed=0
secrets_existed=0
secret_parent=$(dirname "$secrets_root")
secret_base=$(basename "$secrets_root")
secret_backup=
pending_file="$install_root/.deploy-pending"
pending_tmp=
auth_config=

if [ -e "$pending_file" ] || [ -L "$pending_file" ]; then
	echo 'deploy: an interrupted deployment is recorded; run make recover before deploying again' >&2
	exit 1
fi

restore_secrets() {
	if [ "$secrets_existed" = 1 ] && [ -n "$secret_backup" ] && [ -d "$secret_backup" ]; then
		failed="$secret_parent/.$secret_base.failed.$$"
		mv "$secrets_root" "$failed" || return 1
		if mv "$secret_backup" "$secrets_root"; then
			secret_backup=
			rm -rf "$failed" || true
		else
			mv "$failed" "$secrets_root" || true
			return 1
		fi
	else
		rm -rf "$secrets_root" || true
	fi
}

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

finish() {
	status=$1
	rollback_complete=1
	trap - EXIT
	if [ "$status" -ne 0 ] && { [ "$cutover" = 1 ] || [ "$secrets_changed" = 1 ]; }; then
		echo 'deploy: health or authenticated MCP failed; restoring the previous release' >&2
		restore_secrets || { echo 'deploy: could not restore the previous secrets directory automatically' >&2; rollback_complete=0; }
	fi
	if [ "$status" -ne 0 ] && [ "$cutover" = 1 ]; then
		if [ -n "$previous_current" ] && [ -f "$previous_current/deploy/compose.yaml" ]; then
			ln -sfn "$previous_current" "$install_root/current.rollback" && mv -Tf "$install_root/current.rollback" "$current" || rollback_complete=0
			cd "$current" || rollback_complete=0
			export HEIMDALL_CONFIG="$current/deploy/heimdall.toml"
			export HEIMDALL_SECRETS_DIR=$secrets_root
			export HEIMDALL_RELEASE=$(cat "$current/RELEASE" 2>/dev/null || true)
			docker compose -f deploy/compose.yaml -f deploy/compose.release.yaml up -d --wait --timeout 30 || rollback_complete=0
			curl --fail --silent --show-error http://127.0.0.1:8080/healthz >/dev/null || rollback_complete=0
			if [ -r "$secrets_root/agent_token_test-author" ]; then
				if make_mcp_auth_config; then
					sudo -u "$agent_user" curl --config "$auth_config" --fail --silent --show-error -H 'Content-Type: application/json' \
						--data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"heimdall-rollback-check","version":"1"}}}' \
						http://127.0.0.1:8080/mcp >/dev/null || rollback_complete=0
				else
					rollback_complete=0
				fi
			if [ -n "$auth_config" ]; then rm -f "$auth_config" || rollback_complete=0; fi
				auth_config=
			else
				rollback_complete=0
			fi
		else
			rm -f "$current" || rollback_complete=0
			if [ -f "$release/deploy/compose.yaml" ]; then
				cd "$release" || rollback_complete=0
				docker compose -f deploy/compose.yaml -f deploy/compose.release.yaml down --remove-orphans || rollback_complete=0
			fi
		fi
	fi
	if [ "$rollback_complete" = 1 ]; then
		rm -f "$pending_file" || true
		if [ -n "$secret_backup" ]; then rm -rf "$secret_backup" || true; fi
	else
		echo 'deploy: rollback is incomplete; make recover will retry from the durable deployment journal' >&2
	fi
	rm -f "$tmp.tgz" || true
	rm -rf "$tmp-secrets" || true
	if [ -n "$pending_tmp" ]; then rm -f "$pending_tmp" || true; fi
	if [ -n "$auth_config" ]; then rm -f "$auth_config" || true; fi
	exit "$status"
}
trap 'finish $?' EXIT

install -d -m 0755 "$install_root/releases" "$secret_parent"
if [ -d "$secrets_root" ]; then secrets_existed=1; fi
secret_backup=$(mktemp -d "$secret_parent/.$secret_base.backup.XXXXXX")
if [ "$secrets_existed" = 1 ]; then cp -a "$secrets_root/." "$secret_backup/"; fi
install -d -m 0700 "$secrets_root"
pending_tmp="$pending_file.tmp.$$"
umask 077
printf '%s\n%s\n%s\n' "${previous_current:--}" "$secret_backup" "$secrets_existed" > "$pending_tmp"
mv "$pending_tmp" "$pending_file"
pending_tmp=
install -d -m 0755 "$release"
previous_hash=$(cat "$release/.artifact.sha256" 2>/dev/null || true)
if [ "$previous_hash" != "$archive_hash" ]; then
	tar -xzf "$tmp.tgz" -C "$release"
	printf '%s\n' "$archive_hash" > "$release/.artifact.sha256"
fi
# Compose reads its variables (the runner socket group id) from deploy/.env, which the host keeps at the install root.
if [ -f "$install_root/.env" ] && [ ! -f "$release/deploy/.env" ]; then
	install -m 0600 "$install_root/.env" "$release/deploy/.env"
fi
secrets_changed=1
rsync -a --delete --chmod=F600,D700 "$tmp-secrets/" "$secrets_root/"
chmod 0600 "$secrets_root"/*
# Compose mounts each file as it is on the host, and the server runs as 65532, so the files must be that user's.
chown -R 65532:65532 "$secrets_root"
ln -sfn "$release" "$current.new"
mv -Tf "$current.new" "$current"
cutover=1
cd "$current"
export HEIMDALL_CONFIG="$current/deploy/heimdall.toml"
export HEIMDALL_SECRETS_DIR=$secrets_root
export HEIMDALL_RELEASE=$tag
if [ "$previous_hash" != "$archive_hash" ]; then
	# Build from the release's own path, never through the current symlink. BuildKit remembers a build context by its
	# path and treats a file with the same size and mtime as unchanged, and every release binary has both (the archive
	# fixes the mtime), so building through the symlink put the previous release's binary into the new image.
	(cd "$release" && docker compose -f deploy/compose.yaml -f deploy/compose.release.yaml build heimdall-server)
fi
# The Switch sidecar joins the server's network namespace, so the server must be running before compose creates it.
docker compose -f deploy/compose.yaml -f deploy/compose.release.yaml up -d --wait --timeout 60 heimdall-server
docker compose -f deploy/compose.yaml -f deploy/compose.release.yaml up -d --wait --timeout 60
curl --fail --silent --show-error http://127.0.0.1:8080/healthz >/dev/null
make_mcp_auth_config
sudo -u "$agent_user" curl --fail --silent --show-error \
	--config "$auth_config" -H 'Content-Type: application/json' \
	--data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"heimdall-deploy-check","version":"1"}}}' \
	http://127.0.0.1:8080/mcp >/dev/null
rm -f "$auth_config"
auth_config=
rm -rf "$tmp.tgz" "$tmp-secrets"
rm -f "$pending_file"
rm -rf "$secret_backup"
secret_backup=
trap - EXIT
