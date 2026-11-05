# Agent runtime templates

`testdata/agents/agents.json` is the install manifest. It maps the test-author, three reviewer slots, and resolver to an OpenCode config template and one of the checked-in skills. The referenced skills are `skills/test-quality/SKILL.md`, `skills/reviewer/SKILL.md`, and `skills/resolver/SKILL.md`.

Run `deploy/agents/install.sh --dry-run --repo <checkout> --agents-dir <controller-root> --secrets-dir <secrets-dir>` to validate the manifest and print the five planned installations without reading or printing token contents. For the real install, omit `--dry-run` and pass `--owner <agent-user>` when the controller has already assigned the dedicated Unix user. The script writes under `<agents-dir>/agent/<name>/`, never contacts the VM, and does not install or alter Switch, systemd, or nftables.

Each `testdata/agents/*.opencode.json` is a template, not a runnable config. Its `{{file:secrets/agent_token_<name>}}` marker names the token file that `admin init` must read for that agent. The marker is deliberately not an environment variable and contains no credential. During installation, `admin init` writes the resolved bearer value into that agent's `opencode.json`, with mode `0600` and ownership set to the agent's dedicated user, as required by D142. Keep the source token file private and scoped to that agent; never copy it into this repository or logs.

The controller's shared environment remains separate: `/etc/switch/agents.env` carries the OpenRouter key and `SWITCH_CONTROLLER_SELF_UPDATE=off`, owned by `ubuntu` with mode `0600`. Agent files live under the controller's per-agent directory (`<agents-dir>/<NN>/<agent-name>/` on the host; `/var/lib/switch-agents/1000/agent/<agent-name>/` in the Nov 4 VM setup). Install the selected skill as `.opencode/skills/<role>/SKILL.md`, create `scratch/`, and keep `AGENTS.md` in that agent directory. The installed config must retain the T01-proven `auto_mode: true` and scratch-only edit pattern. T27's live nftables and service installation are deployment work; these templates do not modify the VM.

## Local runtime artifact

`deploy/agents/runtime` is the local, side-effect-free deployment contract for T27. Its `Render` function validates explicit controller, agent, version, port, group-id, and restricted OpenRouter CIDR inputs, then returns deterministic artifacts for an admin layer to write:

- controller service drop-in with the pinned OpenCode and controller versions recorded;
- one private-identity drop-in per controller-created agent unit;
- `/etc/switch/agents.env` content containing only `SWITCH_CONTROLLER_SELF_UPDATE=off`, intended for mode `0600` (the OpenRouter key is supplied separately and is never rendered here);
- an nftables table that drops IMDS access, permits only the configured loopback ports and OpenRouter HTTPS ranges for the agent group, then drops the group's other traffic.

The package does not read secrets, resolve DNS, invoke `systemctl`/`nft`, or contact the VM. Applying the returned artifacts and proving agent boot/reboot, nftables behaviour, and a live OpenCode task remain deployment acceptance work and are currently **not verified**.
