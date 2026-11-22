#!/usr/bin/env bash
# Rewrites the live OpenCode config of the resolver, the three reviewers and the profiler from the repo template
# (testdata/agents/<name>.opencode.json): bash, web and file reads denied, only heimdall_* tools and scratch edits
# allowed, and the MCP bearer taken from the server's own token file. Found live on KAN-19: the reviewers' Nov 5
# configs got 401 from Heimdall's MCP and allowed bash, so they curled the endpoint and searched for token files.
# Run on the VM as the owner: ssh heimdall-vm 'bash -s' < scripts/ops/install-agent-configs.sh
# It never prints a token: it reports only whether the written token matches the server's file.
set -euo pipefail

secrets=/etc/heimdall/secrets
base=/var/lib/switch-agents/1000
stamp=$(date -u +%Y%m%dT%H%M%SZ)

# The controller gives each agent a numbered slot (unit switch-agent-1000@<slot>, directory <base>/<slot>/<name>); the
# slot is found by name, so an agent added later (the profiler, D175) is covered without editing this list.
for name in resolver reviewer-1 reviewer-2 reviewer-3 profiler; do
  dir=$(sudo find "$base" -mindepth 2 -maxdepth 2 -type d -name "$name" -path "$base/[0-9]*/$name" | sort | head -1)
  if [ -z "$dir" ]; then
    echo "$name: no agent directory under $base yet, skipped"
    continue
  fi
  slot=$(basename "$(dirname "$dir")")
  cfg="$dir/opencode.json"
  tok="$secrets/agent_token_$name"
  if ! sudo test -f "$tok"; then
    echo "$name: no server token file, skipped"
    continue
  fi
  if sudo test -f "$cfg"; then
    owner=$(sudo stat -c '%U:%G' "$cfg")
    mode=$(sudo stat -c '%a' "$cfg")
    sudo cp -p "$cfg" "$cfg.bak-$stamp"
  else
    # A new agent's first config: owned by the slot's own user, readable by that agent only. The unit's ExecStartPre
    # hands group files to sa1000-<slot> but skips files of the controller user (uid 1000), so the directory's owner
    # (ubuntu) would leave the config unreadable to the agent (found live on the profiler, Nov 21).
    owner="sa1000-$slot:$(sudo stat -c '%G' "$dir")"
    mode=600
  fi
  sudo env NAME="$name" TOK="$tok" KEY="$secrets/deploy_api_key" CFG="$cfg" python3 - <<'PY'
import json, os
token = open(os.environ["TOK"]).read().strip()
# The model key is written into the config, like the bearer. {env:DEPLOY_API_KEY} never reaches an agent: the Switch
# 0.31 controller forwards only an allowlist of variables (OPENAI_API_KEY, OPENROUTER_API_KEY), found live in H11 (B4).
key = open(os.environ["KEY"]).read().strip()
if not key or any(c in key for c in "\r\n\0"):
    raise SystemExit("deploy_api_key must hold one non-empty line")
cfg = {
    "$schema": "https://opencode.ai/config.json",
    "autoupdate": False,
    "share": "disabled",
    "auto_mode": True,
    "auto_approve": False,
    "model": "deploy/deepseek-ai/deepseek-v4.1-flash-1017",
    "provider": {
        "deploy": {
            "name": "Deploy",
            "npm": "@ai-sdk/openai-compatible",
            "options": {"baseURL": "https://api.deploy.com/api/v1", "apiKey": key},
            "models": {
                "deepseek-ai/deepseek-v4.1-flash-1017": {
                    "name": "DeepSeek V4.1 Flash",
                    "tool_call": True,
                    # 8000 ended two H11 reviews: the model spent all of it on reasoning and wrote nothing.
                    "limit": {"context": 200000, "output": 32000},
                }
            },
        }
    },
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
