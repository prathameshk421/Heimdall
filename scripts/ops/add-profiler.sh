#!/usr/bin/env bash
# Adds the profiler seat (D175) on the VM before the server that knows it is deployed: a fresh MCP token for the
# profiler, owned and moded like the resolver's, and `profiler = "profiler"` in [agents] of the server config.
# Run on the VM as root: ssh heimdall-vm 'sudo bash -s' < scripts/ops/add-profiler.sh
# It never prints a token. Both steps are idempotent; a second run changes nothing.
set -euo pipefail

secrets=/etc/heimdall/secrets
toml=/etc/heimdall/heimdall.toml
stamp=$(date -u +%Y%m%dT%H%M%SZ)
tok="$secrets/agent_token_profiler"
ref="$secrets/agent_token_resolver"

if [ -f "$tok" ]; then
  echo "profiler token: already present"
else
  umask 077
  python3 -c 'import secrets, sys; sys.stdout.write(secrets.token_hex(32) + "\n")' > "$tok.new"
  chown --reference="$ref" "$tok.new"
  chmod --reference="$ref" "$tok.new"
  mv "$tok.new" "$tok"
  echo "profiler token: created, owner and mode copied from the resolver's (value not printed)"
fi

if grep -Eq '^profiler[[:space:]]*=' "$toml"; then
  echo "heimdall.toml: profiler already set"
else
  cp -p "$toml" "$toml.bak-$stamp"
  python3 - "$toml" <<'PY'
import sys
path = sys.argv[1]
lines = open(path).read().split("\n")
section, done = "", False
out = []
for line in lines:
    stripped = line.strip()
    if stripped.startswith("[") and stripped.endswith("]"):
        section = stripped
    out.append(line)
    if not done and section == "[agents]" and stripped.startswith("resolver") and "=" in stripped:
        out.append('profiler = "profiler"')
        done = True
if not done:
    raise SystemExit("heimdall.toml: no resolver line in [agents]; nothing changed")
open(path, "w").write("\n".join(out))
PY
  echo "heimdall.toml: profiler added under [agents], backup $toml.bak-$stamp"
fi
sed -n '/^\[agents\]/,/^\[\[/p' "$toml" | grep -v '^\[\['
