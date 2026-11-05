#!/usr/bin/env bash
# Real run of deploy/compose.yaml with a throwaway config and throwaway secret files in a temp directory.
# No real credential is read or written. The temp directory is removed on exit, and so is the stack.
#
# Usage: deploy/verify-compose.sh            (from anywhere; needs docker and curl)
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
compose_file="$here/compose.yaml"
work="$(mktemp -d)"

compose() {
	docker compose -f "$compose_file" "$@"
}

cleanup() {
	compose down -v --remove-orphans >/dev/null 2>&1 || true
	rm -rf "$work"
}
trap cleanup EXIT

mkdir -p "$work/secrets"
chmod 700 "$work/secrets"
printf '%s' 'test-only-postgres-password' >"$work/secrets/postgres_password"
printf '%s' 'test-only-not-a-real-key' >"$work/secrets/openrouter_api_key"
chmod 600 "$work/secrets/"*
cp "$here/heimdall.example.toml" "$work/heimdall.toml"

export HEIMDALL_CONFIG="$work/heimdall.toml"
export HEIMDALL_SECRETS_DIR="$work/secrets"

echo "== compose config -q"
compose config -q
echo "exit=$?"

echo "== compose up -d --build --wait"
compose up -d --build --wait --wait-timeout 420
echo "exit=$?"

echo "== compose ps -a"
compose ps -a

echo "== healthz on 127.0.0.1:8080"
curl -sS --max-time 10 http://127.0.0.1:8080/healthz
echo
echo "exit=$?"

echo "== published ports"
compose ps --format '{{.Service}} {{.Ports}}'

echo "== compose down -v"
compose down -v --remove-orphans
echo "exit=$?"
