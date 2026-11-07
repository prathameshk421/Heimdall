#!/usr/bin/env bash
# Rewrites the live OpenCode config of the resolver and the three reviewers from the repo template
# (testdata/agents/<name>.opencode.json): bash, web and file reads denied, only heimdall_* tools and scratch edits
# allowed, and the MCP bearer taken from the server's own token file. Found live on KAN-19: the reviewers' Nov 5
# configs got 401 from Heimdall's MCP and allowed bash, so they curled the endpoint and searched for token files.
# Run on the VM as the owner: ssh heimdall-vm 'bash -s' < scripts/ops/install-agent-configs.sh
# It never prints a token: it reports only whether the written token matches the server's file.
set -euo pipefail

secrets=/etc/heimdall/secrets
base=/var/lib/switch-agents/1000
stamp=$(date -u +%Y%m%dT%H%M%SZ)

# slot:name for each agent unit (switch-agent-1000@<slot>).
for pair in 04:resolver 05:reviewer-1 06:reviewer-2 07:reviewer-3; do
  slot=${pair%%:*}
  name=${pair#*:}
  cfg="$base/$slot/$name/opencode.json"
  tok="$secrets/agent_token_$name"
  if ! sudo test -f "$tok"; then
    echo "$name: no server token file, skipped"
    continue
  fi
  if ! sudo test -f "$cfg"; then
    echo "$name: no config at $cfg, skipped"
    continue
  fi
  owner=$(sudo stat -c '%U:%G' "$cfg")
  mode=$(sudo stat -c '%a' "$cfg")
  sudo cp -p "$cfg" "$cfg.bak-$stamp"
  sudo env NAME="$name" TOK="$tok" CFG="$cfg" python3 - <<'PY'
import json, os
token = open(os.environ["TOK"]).read().strip()
cfg = {
    "$schema": "https://opencode.ai/config.json",
    "autoupdate": False,
    "share": "disabled",
    "auto_mode": True,
    "auto_approve": False,
    "model": "openrouter/deepseek/deepseek-v4-flash",
    "permission": {
        "*": "deny",
        "bash": "deny",
        "webfetch": "deny",
        "websearch": "deny",
        "doom_loop": "deny",
        "external_directory": {"*": "deny"},
        "edit": {"*": "deny", "**/scratch/**": "allow"},
        "heimdall_*": "allow",
    },
    "mcp": {
        "heimdall": {
            "type": "remote",
            "url": "http://127.0.0.1:8080/mcp",
            "headers": {"Authorization": "Bearer " + token},
        }
    },
}
with open(os.environ["CFG"], "w") as f:
    json.dump(cfg, f, indent=2)
    f.write("\n")
PY
  sudo chown "$owner" "$cfg"
  sudo chmod "$mode" "$cfg"
  match=$(sudo env TOK="$tok" CFG="$cfg" python3 -c 'import json,os; c=json.load(open(os.environ["CFG"])); t=open(os.environ["TOK"]).read().strip(); print("match" if c["mcp"]["heimdall"]["headers"]["Authorization"]=="Bearer "+t else "NO MATCH")')
  sudo systemctl restart "switch-agent-1000@$slot"
  echo "$name: config rewritten ($match), backup $cfg.bak-$stamp, agent restarted: $(systemctl is-active switch-agent-1000@$slot)"
done
