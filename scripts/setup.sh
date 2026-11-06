#!/usr/bin/env bash
# Create a private operator workspace without reading or creating credential values.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
setup_dir=${HEIMDALL_HOME:-${XDG_CONFIG_HOME:-$HOME/.config}/heimdall}

if (($#)); then
	if [[ $# -eq 2 && "$1" == --dir ]]; then
		setup_dir=$2
	else
		echo 'usage: make setup [HEIMDALL_HOME=/private/path]' >&2
		exit 2
	fi
fi

if [[ -L "$setup_dir" ]]; then
	echo 'setup: refusing a symlink as the operator directory' >&2
	exit 2
fi
umask 077
mkdir -p "$setup_dir/secrets"
chmod 0700 "$setup_dir" "$setup_dir/secrets"
if [[ ! -e "$setup_dir/heimdall.toml" ]]; then
	install -m 0600 "$root/deploy/heimdall.example.toml" "$setup_dir/heimdall.toml"
fi
if [[ ! -f "$setup_dir/heimdall.toml" ]]; then
	echo 'setup: config path exists and is not a regular file' >&2
	exit 2
fi
chmod 0600 "$setup_dir/heimdall.toml"

cat <<EOF
Heimdall operator workspace is ready: $setup_dir
Config: $setup_dir/heimdall.toml (edit installation-specific endpoints, ids, and project map)
Secrets directory: $setup_dir/secrets (mode 0700; create each file as mode 0600)
Prepare or resume setup (prints missing names only; secret values are never read):
  heimdall admin init --config '$setup_dir/heimdall.toml' --secrets-dir '$setup_dir/secrets'
Store a single-line value using the hidden prompt:
  heimdall admin secret set --config '$setup_dir/heimdall.toml' --secrets-dir '$setup_dir/secrets' --name database_url
Next: configure secret files without putting values in chat or command arguments, then run:
  DEPLOY_CONFIG='$setup_dir/heimdall.toml' DEPLOY_SECRETS_DIR='$setup_dir/secrets' make deploy
Live tagged deployment also requires the matching detached signature and your operator-managed public key path;
see deploy/RELEASES.md. The deployment script refuses to proceed until those match.
For the agent runtime, enroll interactively without echoing the one-time code, then install the
controller with the same persistent data directory (see deploy/agents/README.md):
  heimdall controller enroll --server https://<switch-host> --name heimdall-vm --data-dir /var/lib/switch-agent-controller-data/<uid>
  sudo deploy/agents/install-controller.sh --controller-user ubuntu --controller-data-dir /var/lib/switch-agent-controller-data/<uid>
Resume and check integrations with:
  heimdall admin init --config '$setup_dir/heimdall.toml'
  heimdall connect --check --config '$setup_dir/heimdall.toml'
EOF
