#!/usr/bin/env bash
set -euo pipefail

usage() {
	echo "usage: $0 [--dry-run] [--repo DIR] [--agents-dir DIR] [--secrets-dir DIR] [--owner USER]" >&2
}

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
agents_dir=/var/lib/switch-agents/1000
secrets_dir=/run/secrets
owner=
dry_run=false

while (($#)); do
	case "$1" in
		--dry-run) dry_run=true; shift ;;
		--repo) [[ $# -ge 2 ]] || { usage; exit 2; }; repo=$2; shift 2 ;;
		--agents-dir) [[ $# -ge 2 ]] || { usage; exit 2; }; agents_dir=$2; shift 2 ;;
		--secrets-dir) [[ $# -ge 2 ]] || { usage; exit 2; }; secrets_dir=$2; shift 2 ;;
		--owner) [[ $# -ge 2 ]] || { usage; exit 2; }; owner=$2; shift 2 ;;
		-h|--help) usage; exit 0 ;;
		*) echo "install agents: unknown option" >&2; usage; exit 2 ;;
	esac
done

manifest=$repo/testdata/agents/agents.json
templates=$repo/testdata/agents
skills=$repo/skills
rules=$repo/testdata/agents/AGENTS.md

[[ -f "$manifest" ]] || { echo "install agents: manifest is unavailable" >&2; exit 1; }
[[ -f "$rules" ]] || { echo "install agents: agent rules are unavailable" >&2; exit 1; }

while IFS=$'\t' read -r name template skill; do
	[[ -n "$name" ]] || continue
	template_path=$templates/$template
	skill_path=$skills/$skill/SKILL.md
	token_path=$secrets_dir/agent_token_$name
	destination=$agents_dir/agent/$name

	[[ -f "$template_path" ]] || { echo "install agents: template unavailable for $name" >&2; exit 1; }
	[[ -f "$skill_path" ]] || { echo "install agents: skill unavailable for $name" >&2; exit 1; }
	[[ -f "$token_path" && -r "$token_path" ]] || { echo "install agents: missing token file for $name" >&2; exit 1; }
	grep -Fq '"model": "openrouter/deepseek/deepseek-v4-flash"' "$template_path" || { echo "install agents: wrong model for $name" >&2; exit 1; }
	grep -Fq '"auto_mode": true' "$template_path" || { echo "install agents: auto_mode is not enabled for $name" >&2; exit 1; }
	grep -Fq '"auto_approve": false' "$template_path" || { echo "install agents: auto_approve is enabled for $name" >&2; exit 1; }
	grep -Fq "{{file:secrets/agent_token_$name}}" "$template_path" || { echo "install agents: token reference missing for $name" >&2; exit 1; }

	if $dry_run; then
		echo "plan $name dir=$destination model=openrouter/deepseek/deepseek-v4-flash skill=$skill"
		continue
	fi

	mkdir -p "$destination/.opencode/skills/$skill" "$destination/scratch"
	install -m 0644 "$skill_path" "$destination/.opencode/skills/$skill/SKILL.md"
	install -m 0644 "$rules" "$destination/AGENTS.md"
	python3 - "$template_path" "$token_path" "$destination/opencode.json" <<'PY'
import json
import os
import sys

template_path, token_path, output_path = sys.argv[1:]
with open(template_path, encoding="utf-8") as handle:
    config = json.load(handle)
with open(token_path, encoding="utf-8") as handle:
    token = handle.read().strip()
if not token or any(char in token for char in "\r\n"):
    raise SystemExit("install agents: token file is empty or multiline")
headers = config["mcp"]["heimdall"]["headers"]
authorization = headers["Authorization"]
if not authorization.startswith("Bearer {{file:secrets/") or not authorization.endswith("}}"):
    raise SystemExit("install agents: invalid token reference")
headers["Authorization"] = "Bearer " + token
with open(output_path, "w", encoding="utf-8") as handle:
    json.dump(config, handle, indent=2)
    handle.write("\n")
os.chmod(output_path, 0o600)
PY
	if [[ -n "$owner" ]]; then
		chown -R "$owner" "$destination"
	fi
	echo "installed $name dir=$destination"
done < <(python3 - "$manifest" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    manifest = json.load(handle)
for name in sorted(manifest["agents"]):
    agent = manifest["agents"][name]
    print(name, agent["template"], agent["skill"], sep="\t")
PY
)

if $dry_run; then
	echo "dry-run: no files changed"
fi
