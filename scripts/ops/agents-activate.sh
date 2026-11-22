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
import glob, hashlib, json, os, shutil, subprocess, sys, time, urllib.request
key = open(os.environ["KEY"]).read().strip()

def carries(path, token_sha):
    try:
        auth = json.load(open(path))["mcp"]["heimdall"]["headers"]["Authorization"]
    except (OSError, ValueError, KeyError):
        return False
    return auth.startswith("Bearer ") and hashlib.sha256(auth[len("Bearer "):].encode()).hexdigest() == token_sha

starting = []  # (name, config path, root-only copy) of agents this run starts
for h in json.load(open(sys.argv[1])):
    name = h["agent"]
    cfgs = sorted(glob.glob(os.path.join(os.environ["BASE"], "[0-9]*", name, "opencode.json")))
    if not cfgs:
        print(f"{name}: no installed config, no receipt")
        continue
    if not carries(cfgs[0], h["token_sha256"]):
        print(f"{name}: installed config does not carry the handoff's token, no receipt")
        continue
    if h.get("desired_state") == "stopped":
        # The controller empties a new agent's folder on its first start (D176); keep a root-only copy to restore.
        keep = f"/root/heimdall-agent-config-{name}.json"
        shutil.copyfile(cfgs[0], keep)
        os.chmod(keep, 0o600)
        starting.append((name, cfgs[0], keep, h["token_sha256"]))
    receipt = {"agent": name, "role": h["role"], "mcp_endpoint": h["mcp"]["url"], "token_sha256": h["token_sha256"],
               "credentials_installed": True}
    req = urllib.request.Request("http://127.0.0.1:8080/admin/agents/activate", data=json.dumps(receipt).encode(),
                                 method="POST", headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            print(f"{name}: activate HTTP {r.status}")
    except urllib.error.HTTPError as e:
        print(f"{name}: activate HTTP {e.code}")

# After the controller's first start, put back any config it emptied, owned by the slot's own user, and restart once.
for name, cfg, keep, token_sha in starting:
    slot = os.path.basename(os.path.dirname(os.path.dirname(cfg)))
    unit = f"switch-agent-1000@{slot}"
    for _ in range(30):
        if subprocess.run(["systemctl", "is-active", "--quiet", unit]).returncode == 0:
            break
        time.sleep(2)
    time.sleep(5)
    if carries(cfg, token_sha):
        print(f"{name}: config intact after first start")
    else:
        st = os.stat(os.path.dirname(cfg))
        shutil.copyfile(keep, cfg)
        os.chown(cfg, st.st_uid, st.st_gid)
        os.chmod(cfg, 0o600)
        subprocess.run(["systemctl", "restart", unit], check=True)
        print(f"{name}: the controller emptied its folder on first start; config restored and {unit} restarted (D176)")
    os.remove(keep)
PY
