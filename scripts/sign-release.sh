#!/usr/bin/env bash
# Sign one built release artifact with an operator- or CI-managed cosign key reference.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tag=${RELEASE_TAG:-}
output_dir=${DEPLOY_OUTPUT_DIR:-$root/.cache/releases}
key=${RELEASE_SIGNING_KEY:-}
archive=${RELEASE_ARCHIVE:-}
signature=${RELEASE_SIGNATURE:-}

if [[ -z "$tag" && -z "$archive" ]]; then
	echo 'release-sign: set RELEASE_TAG or RELEASE_ARCHIVE' >&2
	exit 2
fi
if [[ -z "$archive" ]]; then
	archive="$output_dir/$tag.tgz"
fi
if [[ -z "$signature" ]]; then
	signature="$archive.sig"
fi
if [[ -z "$key" ]]; then
	echo 'release-sign: set RELEASE_SIGNING_KEY to a cosign private-key path or KMS key reference' >&2
	exit 2
fi
if [[ ! -f "$archive" || -L "$archive" || ! -r "$archive" ]]; then
	echo "release-sign: archive must be a readable regular file: $archive" >&2
	exit 2
fi
if ! command -v cosign >/dev/null 2>&1; then
	echo 'release-sign: cosign is required' >&2
	exit 2
fi

umask 077
temp_signature="$signature.tmp.$$"
trap 'rm -f "$temp_signature"' EXIT
if ! cosign sign-blob --yes --key "$key" --output-signature "$temp_signature" "$archive" >/dev/null; then
	echo 'release-sign: cosign could not sign the release archive' >&2
	exit 1
fi
if [[ ! -s "$temp_signature" ]]; then
	echo 'release-sign: cosign did not produce a detached signature' >&2
	exit 1
fi
mv -f "$temp_signature" "$signature"
trap - EXIT
chmod 0644 "$signature"
echo "release-sign: wrote detached signature $signature"
