#!/usr/bin/env bash
# Deletes the 10 old per-ticket Switch rooms and retires their Discord channels (owner decision, Nov 7).
# Run on the VM: ssh heimdall-vm 'bash -s' < scripts/ops/delete-old-discord-rooms.sh
# It signs in with the gateway admin from Switch's .env and never prints a credential.
set -euo pipefail

env_file=/opt/heimdall/switch/.env
jar=$(mktemp)
trap 'rm -f "$jar"' EXIT

# A value may carry a CR (a file saved on Windows), quotes, or an "export " prefix; the last assignment wins, as in a
# shell.
read_env() {
  sudo awk -v key="$1" '
    { sub(/\r$/, ""); sub(/^export[ \t]+/, "") }
    index($0, key "=") == 1 { v = substr($0, length(key) + 2) }
    END {
      if (v ~ /^".*"$/ || v ~ /^\047.*\047$/) v = substr(v, 2, length(v) - 2)
      printf "%s", v
    }' "$env_file"
}
email=$(read_env GATEWAY_ADMIN_EMAIL)
password=$(read_env GATEWAY_ADMIN_PASSWORD)
body=$(EMAIL="$email" PASSWORD="$password" python3 -c 'import json,os; print(json.dumps({"email": os.environ["EMAIL"], "password": os.environ["PASSWORD"]}))')

code=$(curl -s -o /dev/null -w '%{http_code}' -c "$jar" -H 'Content-Type: application/json' \
  --data-binary @- http://127.0.0.1:8000/auth/login <<<"$body")
unset body
if [ "$code" != 200 ]; then
  # Diagnostics without values: lengths, and whether Switch has a user with that email.
  known=$(sudo docker exec -i switch-postgres-1 psql -U postgres -d switch -Atv email="$email" \
    -c "select count(*) from users where lower(email) = lower(:'email')" 2>/dev/null || echo "?")
  echo "login failed: HTTP $code (email length ${#email}, password length ${#password}, users with that email: $known)" >&2
  exit 1
fi
unset password
echo "login ok"

rooms=(
  f2722140-de06-470d-b6ff-f9078838b1c3 # Demo: validate upload file type
  8cedf4ee-7e71-47f1-b5da-479fb479c31f # Demo: add a health check endpoint
  01f9b695-40b0-4785-9f75-0aa6444632af # Demo: reject empty search queries
  e88f1258-b84a-49e0-8870-7ceb8bc0ce6a # Add refunds to the Pay API
  901a75f3-543f-478c-b46b-d40577b729db # Add refunds to the Pay API ffrfr
  69b35b33-4553-488c-b84a-476be33d18bb # add refunds to the pay api!
  b674ce26-aecf-4bc4-afe1-07d012985d8b # Add partial refunds to the Pay API
  284b55d2-858a-4e31-99bb-4ef2aa773558 # KAN-12 Add a refund lookup to the Pay API
  dca06cf8-40c7-4ba4-b967-2e74f440ab6b # KAN-13 Add a refund list to the Pay API
  c5541c81-5a95-4e7e-add3-7b3b68aaebcb # KAN-14 Add a refund count to the Pay API
)
for id in "${rooms[@]}"; do
  out=$(curl -s -w ' HTTP %{http_code}' -b "$jar" -X DELETE "http://127.0.0.1:8000/rooms/$id?retire_channel=true")
  echo "$id $out"
done
