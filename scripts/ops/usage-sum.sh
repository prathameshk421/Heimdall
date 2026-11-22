#!/usr/bin/env bash
# Writes today's agent token count (UTC) for the server's daily token cap (P4): input, output and reasoning tokens from
# every agent's OpenCode database, read-only. Cache reads are not counted. The file lands in the server's data volume,
# where the server reads it as /var/lib/heimdall/usage.json.
# Run as root by heimdall-usage.timer (deploy/heimdall-usage.timer), or by hand:
#   ssh heimdall-vm 'sudo bash -s' < scripts/ops/usage-sum.sh
set -euo pipefail

agents=${HEIMDALL_AGENTS_DIR:-/var/lib/switch-agents/1000}
out=${HEIMDALL_USAGE_FILE:-/var/lib/docker/volumes/heimdall_heimdall_data/_data/usage.json}

AGENTS="$agents" OUT="$out" DAY="${HEIMDALL_USAGE_DAY:-}" python3 - <<'PY'
import datetime, json, os, sqlite3, tempfile

today = datetime.date.fromisoformat(os.environ["DAY"]) if os.environ.get("DAY") else datetime.datetime.now(datetime.timezone.utc).date()
start = int(datetime.datetime(today.year, today.month, today.day, tzinfo=datetime.timezone.utc).timestamp() * 1000)
end = start + 86_400_000
# os.walk, not glob: the databases sit under the hidden .local/share/opencode, which glob skips.
dbs = sorted(os.path.join(root, "opencode.db") for root, _, files in os.walk(os.environ["AGENTS"]) if "opencode.db" in files)
total = 0
for db in dbs:
    con = sqlite3.connect("file:" + db + "?mode=ro", uri=True)
    try:
        for (data,) in con.execute("select data from message where time_created >= ? and time_created < ?", (start, end)):
            tokens = (json.loads(data) or {}).get("tokens") or {}
            total += int(tokens.get("input", 0)) + int(tokens.get("output", 0)) + int(tokens.get("reasoning", 0))
    finally:
        con.close()

out = os.environ["OUT"]
directory = os.path.dirname(out)
st = os.stat(directory)
fd, tmp = tempfile.mkstemp(dir=directory, prefix=".usage-")
with os.fdopen(fd, "w") as f:
    json.dump({"date": today.isoformat(), "tokens": total}, f)
os.chown(tmp, st.st_uid, st.st_gid)  # the server user owns the volume
os.chmod(tmp, 0o640)
os.replace(tmp, out)
print(f"usage {today.isoformat()}: {total} tokens from {len(dbs)} agent databases")
PY
