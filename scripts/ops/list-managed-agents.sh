#!/usr/bin/env bash
# Lists the Switch agents the coordinator manages, with the fields agents-prepare compares (machine, description,
# provider, auto mode, directory), so a 409 from prepare can be traced to its agent and field. Prints no credential.
# Run on the VM as root: ssh heimdall-vm 'sudo bash -s' < scripts/ops/list-managed-agents.sh
set -euo pipefail

secrets=/etc/heimdall/secrets
toml=/etc/heimdall/heimdall.toml
field() { sed -n '/^\[switch\]/,/^\[/p' "$toml" | sed -n "s/^$1[[:space:]]*=[[:space:]]*\"\(.*\)\"/\1/p" | head -1; }
base=$(field base_url)
agent=$(field agent_id)
key_name=$(field api_key_secret)

KEY="$secrets/$key_name" URL="$base/agents/$agent/ops/list_managed_agents" python3 - <<'PY'
import json, os, urllib.request
key = open(os.environ["KEY"]).read().strip()
req = urllib.request.Request(os.environ["URL"], data=b"{}", method="POST",
                             headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"})
with urllib.request.urlopen(req, timeout=30) as r:
    agents = json.load(r)["result"]
for a in agents:
    m = a.get("machine") or {}
    print(f"{a.get('name')}: description={a.get('description')!r} provider={a.get('provider')} "
          f"auto_mode={a.get('auto_mode')} auto_approve={a.get('auto_approve')} machine={m.get('name')} "
          f"directory={a.get('directory')!r}")
PY
