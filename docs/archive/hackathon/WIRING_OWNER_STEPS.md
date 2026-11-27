# Owner steps (root and credentials)

Only these steps need the owner. Each one runs in the Terminal panel, where the owner accepts the command. The implementer does not run them. Read each block before accepting it.

## 1. Rootless Podman on the VM (root)

Purpose: the sandbox runs test jobs in rootless Podman (TECH_SPEC 12.2).

```bash
sudo apt-get install -y podman uidmap
```

## 2. Runner user and subordinate ID range (root)

Purpose: the runner runs as its own user, so rootless containers get a UID range.

```bash
sudo useradd --system --home /var/lib/heimdall-runner --shell /usr/sbin/nologin heimdall-runner
```

```bash
sudo usermod --add-subuids 100000-165535 --add-subgids 100000-165535 heimdall-runner
```

```bash
sudo groupadd --system heimdall-runner-socket && sudo usermod -aG heimdall-runner-socket heimdall-runner
```

## 3. Go image digest (no root, no secret)

Purpose: the runner allows only digest-pinned images, and `deploy/deploy.sh` refuses a tag. The implementer resolves the digest; the owner confirms it:

```bash
docker buildx imagetools inspect golang:1.27 --format '{{json .Manifest.Digest}}'
```

Put the result into the deploy config as `HEIMDALL_GO_IMAGE=golang@sha256:<digest>`. The owner confirms the digest before deploy.

## 4. Secret import from `.env` (the owner runs it; the implementer never reads `.env`)

Purpose: the server secret files come from the owner's `.env`.

```bash
.cache/heimdall secrets-sync --env .env --out ~/heimdall/secrets --require
```

Then the owner checks that the secrets directory is mode 0700 and its files are 0600.

## 5. Release signing (the owner types the key password once)

Purpose: deploy accepts only a signed release (`DEPLOY_VERIFY_KEY`). The owner runs signing in the Terminal panel. The implementer never asks for or stores the password.

```bash
make release-sign
```

## 6. Harness drift (guarded files)

Purpose: `make verify` stops on file-mode drift in the harness. The owner restores it.

```bash
make harness-install && make harness-check
```

## Deploy checklist

Before `make deploy`, the owner confirms: steps 1 to 6 are done; the live config `~/heimdall/heimdall.toml` exists (the implementer writes it without secret values); `HEIMDALL_GO_IMAGE` is digest-pinned; the tag is clean and exact.

## Done markers

The owner says "done" for each step. The implementer then checks the result without printing secrets: `systemctl is-active` for the runner, and `stat -c %a` for the secrets directory.
