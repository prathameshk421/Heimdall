#!/bin/sh
# Hot-installs a new heimdall-server and heimdall-runner on the VM (HARNESS_DESIGN H11). Run it as root on the VM after
# copying the two Linux binaries (and, when it changed, deploy/compose.yaml) to /tmp:
#   scp heimdall-server heimdall-vm:/tmp/heimdall-server.new
#   scp heimdall-runner heimdall-vm:/tmp/heimdall-runner.new
#   scp deploy/compose.yaml heimdall-vm:/tmp/heimdall-compose.yaml.new      (optional)
#   ssh heimdall-vm 'sudo sh -s' < scripts/ops/hot-deploy.sh
# Rollback (puts the .prev copies back and restarts):
#   ssh heimdall-vm 'sudo sh -s rollback' < scripts/ops/hot-deploy.sh
# It backs up the database first, keeps a .prev copy of every file it replaces, restarts the runner and the compose
# stack together (a server restart alone leaves the socat sidecar on a dead network), then prints the binary hashes
# inside the container and the server's /healthz. It reads no secret and prints no credential.
set -eu

server=/opt/heimdall/heimdall-server
runner=/usr/local/bin/heimdall-runner
compose=/opt/heimdall/deploy/compose.yaml
backups=/opt/heimdall/backups
container=heimdall-heimdall-server-1
mode=${1:-deploy}

restart_and_check() {
	systemctl restart heimdall-runner heimdall-compose
	i=0
	until curl -fsS --max-time 5 http://127.0.0.1:8080/healthz >/dev/null 2>&1; do
		i=$((i + 1))
		[ "$i" -lt 60 ] || { echo "hot-deploy: /healthz did not answer within 120 s" >&2; return 1; }
		sleep 2
	done
	echo "== hashes (host runner, server inside the container)"
	sha256sum "$runner"
	docker exec "$container" sha256sum /usr/local/bin/heimdall-server
	echo "== healthz"
	curl -sS --max-time 5 http://127.0.0.1:8080/healthz
	echo
}

case "$mode" in
rollback)
	for f in "$server" "$runner" "$compose"; do
		[ -f "$f.prev" ] && cp -p "$f.prev" "$f" && echo "restored $f"
	done
	restart_and_check
	exit 0
	;;
deploy) ;;
*)
	echo "hot-deploy: unknown mode $mode (deploy or rollback)" >&2
	exit 2
	;;
esac

[ -f /tmp/heimdall-server.new ] || { echo "hot-deploy: /tmp/heimdall-server.new is missing" >&2; exit 1; }
[ -f /tmp/heimdall-runner.new ] || { echo "hot-deploy: /tmp/heimdall-runner.new is missing" >&2; exit 1; }

stamp=$(date -u +%Y%m%dT%H%M%S)
install -d -o root -g root -m 0755 "$backups"
docker exec heimdall-postgres-1 pg_dump -U heimdall heimdall | gzip >"$backups/heimdall-$stamp.sql.gz"
echo "backup: $backups/heimdall-$stamp.sql.gz ($(stat -c %s "$backups/heimdall-$stamp.sql.gz") bytes)"

# replace keeps the installed file as .prev only when the new one differs, so a second run of the same build never
# overwrites the last good version with itself (found live in H11: a re-run lost the pre-H11 binaries).
replace() {
	new=$1 target=$2 mode=$3
	if cmp -s "$new" "$target"; then
		echo "unchanged: $target"
	else
		cp -p "$target" "$target.prev"
		install -o root -g root -m "$mode" "$new" "$target"
		echo "replaced: $target (previous kept as $target.prev)"
	fi
	rm -f "$new"
}
replace /tmp/heimdall-server.new "$server" 0755
replace /tmp/heimdall-runner.new "$runner" 0755
[ -f /tmp/heimdall-compose.yaml.new ] && replace /tmp/heimdall-compose.yaml.new "$compose" 0644

echo "== installed"
sha256sum "$server" "$runner"
restart_and_check
