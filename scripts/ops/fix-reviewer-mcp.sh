#!/usr/bin/env bash
# Gives the reviewer and resolver agents the same Heimdall MCP server and the same locked-down permissions as the
# test-author, each with its own token. Found live on KAN-19 (Nov 7): the reviewers had no heimdall_* tools and fell
# back to curl. Run by the owner, because the repo guard keeps the agent away from tokens:
#   ssh heimdall-vm 'bash -s' < scripts/ops/fix-reviewer-mcp.sh
# Prints no token. Keeps a backup of every file it changes and restarts the agents it changed.
set -euo pipefail

base=/var/lib/switch-agents/1000
secrets=/etc/heimdall/secrets
model="$base/03/test-author/opencode.json"
stamp=$(date -u +%Y%m%dT%H%M%SZ)

declare -A slot=([resolver]=04 [reviewer-1]=05 [reviewer-2]=06 [reviewer-3]=07)

sudo test -f "$model" || { echo "missing $model" >&2; exit 1; }
sudo test -f "$secrets/agent_token_test-author" || { echo "missing test-author token file" >&2; exit 1; }

changed=()
for agent in reviewer-1 reviewer-2 reviewer-3 resolver; do
  n=${slot[$agent]}
  cfg="$base/$n/$agent/opencode.json"
  tok="$secrets/agent_token_$agent"
  if ! sudo test -d "$base/$n/$agent"; then echo "$agent: no workspace at $base/$n/$agent, skipped"; continue; fi
  if ! sudo test -f "$tok"; then echo "$agent: no token file agent_token_$agent, skipped"; continue; fi
  if sudo test -f "$cfg"; then
    sudo cp -p "$cfg" "$cfg.bak-$stamp"
  else
    # No config yet (found live): start from the test-author's, with the same owner and mode.
    sudo cp -p "$model" "$cfg"
  fi
  sudo python3 - "$model" "$cfg" "$secrets/agent_token_test-author" "$tok" <<'PY'
import copy, json, os, sys
model_path, cfg_path, model_tok_path, tok_path = sys.argv[1:5]
model = json.load(open(model_path))
cfg = json.load(open(cfg_path))
model_tok = open(model_tok_path).read().strip()
tok = open(tok_path).read().strip()

def swap(v):
    if isinstance(v, str):
        return v.replace(model_tok, tok)
    if isinstance(v, dict):
        return {k: swap(x) for k, x in v.items()}
    if isinstance(v, list):
        return [swap(x) for x in v]
    return v

if "heimdall" not in model.get("mcp", {}):
    sys.exit("the test-author config has no mcp.heimdall block to copy")
cfg.setdefault("mcp", {})["heimdall"] = swap(copy.deepcopy(model["mcp"]["heimdall"]))
if "permission" in model:
    cfg["permission"] = copy.deepcopy(model["permission"])
st = os.stat(cfg_path)
tmp = cfg_path + ".new"
with open(tmp, "w") as f:
    json.dump(cfg, f, indent=2)
os.chown(tmp, st.st_uid, st.st_gid)
os.chmod(tmp, st.st_mode)
os.replace(tmp, cfg_path)
PY
  echo "$agent: mcp.heimdall and permissions set (backup $cfg.bak-$stamp)"
  changed+=("$n")
done

for n in "${changed[@]}"; do
  sudo systemctl restart "switch-agent-1000@$n"
  echo "restarted switch-agent-1000@$n: $(systemctl is-active "switch-agent-1000@$n")"
done
