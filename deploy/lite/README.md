# Heimdall Lite (Self-Hosted VM)

Minimal 2-container deployment for teams running Heimdall on a small company VM (1 vCPU, 1 GiB RAM, ~$4-$6/mo).

## Footprint

| Container | Memory Cap | Tuning | Security |
|---|---|---|---|
| `heimdall-server` | `128m` | `GOMEMLIMIT=96MiB`, `GOGC=50`, `cpus: 0.50`, `pids_limit: 128` | `read_only`, `cap_drop: [ALL]`, `no-new-privileges`, `127.0.0.1:8080` only |
| `postgres:16.15-alpine` | `128m` | `shared_buffers=32MB`, `work_mem=2MB`, `max_connections=25` | `read_only`, `no-new-privileges`, no host port published |

Total reserved memory is **256 MiB**, leaving the rest of a 1 GiB VM for the host OS and ephemeral test runs via `heimdall-runner.service`.

## Quick Start

1. Install the runner socket (`deploy/heimdall-runner.service`) on the VM.
2. Create your config (`deploy/heimdall.example.toml`) and secret files in `$HEIMDALL_SECRETS_DIR` (`database_url`, `postgres_password`, `deploy_api_key`, `github_app_id`, `github_app_installation_id`, `github_app_private_key`).
3. Start the stack from the repository root:

```sh
export HEIMDALL_CONFIG=/etc/heimdall/heimdall.toml
export HEIMDALL_SECRETS_DIR=/etc/heimdall/secrets
export HEIMDALL_RUNNER_GID=$(stat -c '%g' /run/heimdall-runner/runner.sock)
docker compose -f deploy/lite/compose.yaml up -d --build --wait
```

4. Check health: `curl -sf http://127.0.0.1:8080/healthz`.

> Note: When you do not need a resident VM at all, use **Heimdall Action mode** (`deploy/action/README.md`) — zero VMs, zero databases, runs directly in GitHub Actions with `refs/heimdall/state`.
