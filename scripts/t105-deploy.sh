#!/usr/bin/env bash
# Build and install a tagged Linux release through the configured SSM-backed SSH alias.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"

: "${DEPLOY_HOST:=heimdall-vm}"
: "${DEPLOY_SECRETS_DIR:=$root/secrets}"
: "${DEPLOY_CONFIG:=$root/deploy/heimdall.example.toml}"
: "${DEPLOY_AGENT_USER:=sa1000-01}"
: "${DEPLOY_VERIFY_KEY:=}"
: "${DEPLOY_SIGNATURE_FILE:=}"
: "${DEPLOY_ARCH:=amd64}"
: "${DEPLOY_OUTPUT_DIR:=$root/.cache/releases}"
dry_run=${DEPLOY_DRY_RUN:-0}
build_only=${DEPLOY_BUILD_ONLY:-0}

if [[ "$dry_run" != 1 && "$build_only" != 1 ]]; then
	tag=$(git describe --tags --exact-match HEAD 2>/dev/null || true)
	if [[ -z "$tag" ]]; then
		echo 'deploy: HEAD must be an exact release tag' >&2
		exit 2
	fi
	if [[ -n "$(git status --porcelain)" ]]; then
		echo 'deploy: working tree must be clean' >&2
		exit 2
	fi
else
	tag=${DEPLOY_TAG:-$([[ "$build_only" == 1 ]] && echo local-build || echo dry-run)}
fi

[[ "$tag" =~ ^[A-Za-z0-9._-]+$ ]] || { echo 'deploy: invalid release tag' >&2; exit 2; }
[[ "$DEPLOY_ARCH" == amd64 || "$DEPLOY_ARCH" == arm64 ]] || { echo 'deploy: DEPLOY_ARCH must be amd64 or arm64' >&2; exit 2; }

artifact=$(mktemp -d)
trap 'rm -rf "$artifact"' EXIT
mkdir -p "$artifact/bin" "$artifact/deploy/graphiti" "$artifact/testdata"

run() {
	if [[ "$dry_run" == 1 ]]; then
		printf 'DRY-RUN'
		for arg in "$@"; do
			arg=${arg//"$artifact"/<artifact>}
			printf ' %q' "$arg"
		done
		printf '\n'
		return 0
	fi
	"$@"
}

echo "deploy: build linux/$DEPLOY_ARCH release $tag"
run env CGO_ENABLED=0 GOOS=linux GOARCH="$DEPLOY_ARCH" go build -trimpath -ldflags='-s -w' -o "$artifact/bin/heimdall-server" ./cmd/heimdall-server
run env CGO_ENABLED=0 GOOS=linux GOARCH="$DEPLOY_ARCH" go build -trimpath -ldflags='-s -w' -o "$artifact/bin/heimdall" ./cmd/heimdall
cp deploy/compose.yaml "$artifact/deploy/compose.yaml"
cp deploy/compose.release.yaml "$artifact/deploy/compose.release.yaml"
cp deploy/Dockerfile.release "$artifact/deploy/Dockerfile.release"
cp -R deploy/agents "$artifact/deploy/"
cp deploy/graphiti/config.yaml "$artifact/deploy/graphiti/config.yaml"
cp "$DEPLOY_CONFIG" "$artifact/deploy/heimdall.toml"
cp -R testdata/agents "$artifact/testdata/"
cp -R skills "$artifact/"
printf '%s\n' "$tag" > "$artifact/RELEASE"

if [[ "$dry_run" == 1 ]]; then
	# Dry-run deliberately does not inspect secrets or connect to the host.
	echo "DRY-RUN would stage the Linux binaries and $DEPLOY_SECRETS_DIR on $DEPLOY_HOST"
	echo "DRY-RUN would install under /opt/heimdall/releases/$tag, start Compose, and verify /healthz and authenticated /mcp"
	exit 0
fi

python3 - "$artifact" "$artifact/release.tgz" <<'PY'
import gzip
import os
import sys
import tarfile

root, output = sys.argv[1:]
with open(output, "wb") as raw:
    with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0, compresslevel=9) as compressed:
        with tarfile.open(fileobj=compressed, mode="w") as archive:
            paths = []
            for top in ("RELEASE", "bin", "deploy", "skills", "testdata"):
                source = os.path.join(root, top)
                if os.path.isfile(source):
                    paths.append(top)
                else:
                    for directory, dirs, files in os.walk(source):
                        dirs.sort()
                        files.sort()
                        paths.extend(os.path.relpath(os.path.join(directory, name), root) for name in files)
            for path in sorted(paths):
                source = os.path.join(root, path)
                if os.path.islink(source):
                    raise SystemExit("deploy: release input contains a symlink")
                info = archive.gettarinfo(source, arcname=path)
                info.uid = 0
                info.gid = 0
                info.uname = ""
                info.gname = ""
                info.mtime = 0
                info.mode = 0o755 if os.stat(source).st_mode & 0o111 else 0o644
                with open(source, "rb") as file:
                    archive.addfile(info, file)
PY
archive_hash=$(shasum -a 256 "$artifact/release.tgz" | awk '{print $1}')

if [[ "$build_only" == 1 ]]; then
	mkdir -p "$DEPLOY_OUTPUT_DIR"
	install -m 0644 "$artifact/release.tgz" "$DEPLOY_OUTPUT_DIR/$tag.tgz"
	printf '%s  %s\n' "$archive_hash" "$tag.tgz" > "$DEPLOY_OUTPUT_DIR/$tag.tgz.sha256"
	echo "deploy: local Linux/$DEPLOY_ARCH release archive: $DEPLOY_OUTPUT_DIR/$tag.tgz (no host or secret files accessed)"
	file "$artifact/bin/heimdall-server" "$artifact/bin/heimdall"
	exit 0
fi

[[ -d "$DEPLOY_SECRETS_DIR" ]] || { echo 'deploy: secret directory is missing' >&2; exit 2; }
[[ -r "$DEPLOY_CONFIG" ]] || { echo 'deploy: config file is unreadable' >&2; exit 2; }
release_archive="$DEPLOY_OUTPUT_DIR/$tag.tgz"
if [[ ! -f "$release_archive" || -L "$release_archive" ]]; then
	echo "deploy: tagged release archive is missing: $release_archive; build and sign it before deployment" >&2
	exit 2
fi
if ! cmp -s "$artifact/release.tgz" "$release_archive"; then
	echo "deploy: the built archive does not match $release_archive; rebuild and sign the current tagged source" >&2
	exit 2
fi
if [[ -z "$DEPLOY_VERIFY_KEY" ]]; then
	echo 'deploy: set DEPLOY_VERIFY_KEY to the operator-managed cosign public key path (see deploy/RELEASES.md)' >&2
	exit 2
fi
if [[ ! -f "$DEPLOY_VERIFY_KEY" || -L "$DEPLOY_VERIFY_KEY" || ! -r "$DEPLOY_VERIFY_KEY" ]]; then
	echo 'deploy: DEPLOY_VERIFY_KEY must name a readable regular public key file' >&2
	exit 2
fi
if [[ -z "$DEPLOY_SIGNATURE_FILE" ]]; then
	DEPLOY_SIGNATURE_FILE="$release_archive.sig"
fi
if [[ ! -f "$DEPLOY_SIGNATURE_FILE" || -L "$DEPLOY_SIGNATURE_FILE" || ! -r "$DEPLOY_SIGNATURE_FILE" ]]; then
	echo "deploy: signed release file is missing: $DEPLOY_SIGNATURE_FILE" >&2
	exit 2
fi
if ! command -v cosign >/dev/null 2>&1; then
	echo 'deploy: cosign is required to verify the release signature' >&2
	exit 2
fi
if ! cosign verify-blob --key "$DEPLOY_VERIFY_KEY" --signature "$DEPLOY_SIGNATURE_FILE" "$artifact/release.tgz" >/dev/null; then
	echo "deploy: release signature verification failed for $tag" >&2
	exit 1
fi
secret_names=(
	database_url postgres_password openrouter_api_key jira_email jira_api_token
	github_app_id github_app_installation_id github_app_private_key
	switch_api_key switch_registration_token
	agent_token_test-author agent_token_reviewer-1 agent_token_reviewer-2
	agent_token_reviewer-3 agent_token_resolver
)
for name in "${secret_names[@]}"; do
	path="$DEPLOY_SECRETS_DIR/$name"
	if [[ ! -f "$path" || -L "$path" ]]; then
		echo "deploy: required secret file is missing or not regular: $name" >&2
		exit 2
	fi
	mode=$(stat -c '%a' "$path" 2>/dev/null || stat -f '%Lp' "$path")
	if (( (8#$mode & 077) != 0 )); then
		echo "deploy: secret file permissions must exclude group and other access: $name" >&2
		exit 2
	fi
done

remote_tmp="/tmp/heimdall-deploy-$tag"
scp "$artifact/release.tgz" "$DEPLOY_HOST:$remote_tmp.tgz"
rsync -a --delete --chmod=F600,D700 "$DEPLOY_SECRETS_DIR/" "$DEPLOY_HOST:$remote_tmp-secrets/"
ssh "$DEPLOY_HOST" "sudo -n /bin/sh -s -- '$tag' '$remote_tmp' '$DEPLOY_AGENT_USER' '$archive_hash'" <<'REMOTE'
set -eu
tag=$1
tmp=$2
agent_user=$3
archive_hash=$4
release="/opt/heimdall/releases/$tag"
install -d -m 0755 /opt/heimdall/releases /etc/heimdall/secrets
install -d -m 0755 "$release"
previous_hash=$(cat "$release/.artifact.sha256" 2>/dev/null || true)
if [ "$previous_hash" != "$archive_hash" ]; then
	tar -xzf "$tmp.tgz" -C "$release"
	printf '%s\n' "$archive_hash" > "$release/.artifact.sha256"
fi
rsync -a --delete --chmod=F600,D700 "$tmp-secrets/" /etc/heimdall/secrets/
chmod 0600 /etc/heimdall/secrets/*
ln -sfn "$release" /opt/heimdall/current.new
mv -Tf /opt/heimdall/current.new /opt/heimdall/current
cd /opt/heimdall/current
export HEIMDALL_CONFIG=/opt/heimdall/current/deploy/heimdall.toml
export HEIMDALL_SECRETS_DIR=/etc/heimdall/secrets
export HEIMDALL_RELEASE=$tag
if [ "$previous_hash" != "$archive_hash" ]; then
	docker compose -f deploy/compose.yaml -f deploy/compose.release.yaml build heimdall-server
fi
docker compose -f deploy/compose.yaml -f deploy/compose.release.yaml up -d --wait --timeout 30
curl --fail --silent --show-error http://127.0.0.1:8080/healthz >/dev/null
token=$(cat /etc/heimdall/secrets/agent_token_test-author)
sudo -u "$agent_user" curl --fail --silent --show-error \
  -H "Authorization: Bearer $token" -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"heimdall-deploy-check","version":"1"}}}' \
  http://127.0.0.1:8080/mcp >/dev/null
rm -rf "$tmp.tgz" "$tmp-secrets"
REMOTE
echo "deploy: release $tag is healthy and authenticated MCP is reachable"
