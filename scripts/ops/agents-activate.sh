#!/usr/bin/env bash
# Posts one setup receipt per prepared agent (POST /admin/agents/activate), after agents-prepare.sh and
# install-agent-configs.sh. A receipt says credentials_installed only when the agent's installed opencode.json carries
# the bearer whose SHA-256 the handoff names; an agent without such a config gets no receipt and stays stopped.
# Run on the VM as root: ssh heimdall-vm 'sudo bash -s' < scripts/ops/agents-activate.sh
# It never prints a token.
set -euo pipefail

secrets=/etc/heimdall/secrets
toml=/etc/heimdall/heimdall.toml
handoffs=/root/heimdall-agent-handoffs.json
base=/var/lib/switch-agents/1000

key_name=$(sed -n '/^\[switch\]/,/^\[/p' "$toml" | sed -n 's/^api_key_secret[[:space:]]*=[[:space:]]*"\(.*\)"/\1/p' | head -1)
[ -n "$key_name" ] || { echo "activate: switch.api_key_secret not found in $toml" >&2; exit 1; }
[ -f "$handoffs" ] || { echo "activate: run agents-prepare.sh first" >&2; exit 1; }

KEY="$secrets/$key_name" BASE="$base" python3 - "$handoffs" <<'PY'
import glob, hashlib, json, os, sys, urllib.request
key = open(os.environ["KEY"]).read().strip()
for h in json.load(open(sys.argv[1])):
    name = h["agent"]
    cfgs = sorted(glob.glob(os.path.join(os.environ["BASE"], "[0-9]*", name, "opencode.json")))
    if not cfgs:
        print(f"{name}: no installed config, no receipt")
        continue
    auth = json.load(open(cfgs[0]))["mcp"]["heimdall"]["headers"]["Authorization"]
    installed = auth.startswith("Bearer ") and hashlib.sha256(auth[len("Bearer "):].encode()).hexdigest() == h["token_sha256"]
    if not installed:
        print(f"{name}: installed config does not carry the handoff's token, no receipt")
        continue
    receipt = {"agent": name, "role": h["role"], "mcp_endpoint": h["mcp"]["url"], "token_sha256": h["token_sha256"],
               "credentials_installed": True}
    req = urllib.request.Request("http://127.0.0.1:8080/admin/agents/activate", data=json.dumps(receipt).encode(),
                                 method="POST", headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            print(f"{name}: activate HTTP {r.status}")
    except urllib.error.HTTPError as e:
        print(f"{name}: activate HTTP {e.code}")
PY
