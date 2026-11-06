# Agent runtime and controller setup

`testdata/agents/agents.json` is the role manifest. `install.sh --dry-run` validates all five templates, role skills, and token file metadata without printing token contents. The real installer writes the role configs under the controller's `/var/lib/switch-agents/1000/agent/<role>/` tree, mode `0600`, and assigns the configured owner.

## Controller enrollment handoff

The Switch enrollment code is issued by an administrator in the Switch gateway's Machines page. Heimdall prompts for it without echo, retains it only for the enrollment call, and discards upstream output. The upstream CLI accepts the one-time code only as an argument, so it is briefly present in that process's argument vector; use a private administrator session and do not paste it into chat, a checked-in file, or a shared shell transcript. Install the pinned Switch controller release using Switch's published release verification, then enroll it:

```sh
controller_uid=$(id -u ubuntu)
controller_data_dir="/var/lib/switch-agent-controller-data/$controller_uid"
heimdall controller enroll --server https://<switch-host> --name heimdall-vm --data-dir "$controller_data_dir"
```

Run this from the machine's private administrator session. The command confirms enrollment without printing the one-time code. Preserve `controller_data_dir` for the installer. Once enrollment succeeds, as root run:

```sh
sudo deploy/agents/install-controller.sh --controller-user ubuntu --controller-data-dir "$controller_data_dir" --secrets-dir /etc/heimdall/secrets --agents-dir /var/lib/switch-agents/1000
```

The controller installer requires Node.js 22.13+, OpenCode 1.19.5, `switch-agent-controller`, systemd, an enrolled controller, and a private `openrouter_api_key` file. It atomically creates `/etc/switch/agents.env` mode `0600` with the key and `SWITCH_CONTROLLER_SELF_UPDATE=off`, then invokes the upstream `doctor` after installing and enabling `switch-agent-controller-<uid>.service`. The upstream installer creates the configured agent-user pool, systemd template, polkit rule, and agent directory. The install command passes separate controller state, agents, and releases directories so none overlap. Re-running it refreshes the env file and reconciles the service. Recovery is `make recover`; it reconciles Compose, starts the controller unit if inactive, and checks the server, authenticated MCP endpoint, and Graphiti health.

`install-controller.sh --dry-run` reports the planned paths only; it does not inspect secret contents, call Switch, write files, or touch systemd. Controller enrollment itself is a manual gateway action, not part of local dry-run verification.
