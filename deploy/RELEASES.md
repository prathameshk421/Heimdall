# Heimdall release signing and deployment verification

Release publishing and deployment use a detached Cosign blob signature over the exact deterministic `.tgz` archive. The deployment code does not contain a trust root. Each operator supplies the public verification key file through `DEPLOY_VERIFY_KEY`; it must be a readable, regular, non-symlink file controlled by that operator. The key is never downloaded or replaced by deployment code.

A release operator builds the exact tagged tree and signs the resulting archive:

```sh
DEPLOY_TAG=vX.Y.Z make deploy-build
make release-sign TAG=vX.Y.Z RELEASE_SIGNING_KEY=/secure/path/to/private-key
```

`RELEASE_SIGNING_KEY` may be a Cosign private-key path or a Cosign KMS key reference. The signer writes `.cache/releases/vX.Y.Z.tgz.sig` next to the archive (or `<RELEASE_ARCHIVE>.sig` when using an explicit archive path). Keep private signing material in the signing system; this command does not create or retain it.

An operator provisions the corresponding public key out-of-band, then deploys with:

```sh
DEPLOY_TAG=vX.Y.Z DEPLOY_VERIFY_KEY=/etc/heimdall/trust/release.pub make deploy
```

Before SSH or secret-file metadata checks, deploy rebuilds the same tagged archive, requires its bytes to match the local archive, and runs `cosign verify-blob --key <operator-key> --signature <archive>.sig <archive>`. A missing key, signature, Cosign binary, mismatched archive, or invalid signature stops deployment. `make deploy-build`, setup, and dry-run remain local and do not require a key or signature.

The signature authenticates archive bytes. It does not establish that the operator's chosen public key is the organization's intended trust root; key distribution and rotation remain an operator responsibility. No signing key, public key, or signed release is committed as a default.
