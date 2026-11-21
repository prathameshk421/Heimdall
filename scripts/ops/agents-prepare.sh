#!/usr/bin/env bash
# Asks the server to reconcile every configured role agent with Switch (POST /admin/agents/prepare, TECH_SPEC host setup
# API). Each definition is left stopped until agents-activate.sh posts its receipt, so run that right after
# install-agent-configs.sh. The handoffs (no token inside, only the token file path and its SHA-256) are kept in a
# root-only file for the activation step.
# Run on the VM as root: ssh heimdall-vm 'sudo bash -s' < scripts/ops/agents-prepare.sh
set -euo pipefail

secrets=/etc/heimdall/secrets
toml=/etc/heimdall/heimdall.toml
out=/root/heimdall-agent-handoffs.json

key_name=$(sed -n '/^\[switch\]/,/^\[/p' "$toml" | sed -n 's/^api_key_secret[[:space:]]*=[[:space:]]*"\(.*\)"/\1/p' | head -1)
[ -n "$key_name" ] || { echo "prepare: switch.api_key_secret not found in $toml" >&2; exit 1; }

umask 077
code=$(KEY="$secrets/$key_name" python3 - "$out" <<'PY'
import json, os, sys, urllib.request
key = open(os.environ["KEY"]).read().strip()
req = urllib.request.Request("http://127.0.0.1:8080/admin/agents/prepare", data=b"{}", method="POST",
                             headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"})
try:
    with urllib.request.urlopen(req, timeout=60) as r:
        body = r.read()
        open(sys.argv[1], "wb").write(body)
        print(r.status)
except urllib.error.HTTPError as e:
    print(e.code)
PY
)
echo "prepare: HTTP $code"
[ "$code" = "200" ] || exit 1
python3 - "$out" <<'PY'
import json, sys
for h in json.load(open(sys.argv[1])):
    print(f"handoff {h['agent']} role={h['role']} desired={h['desired_state']}")
PY
