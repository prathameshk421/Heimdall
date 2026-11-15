# Gold Findings: Held-Out Benchmark (`webhook-notifier`)

- **G1 (Resource leak outside diff):** `internal/delivery/sender.go` removes `defer resp.Body.Close()` on the success path. While error cases close the body, 200 OK responses leak the TCP connection, exhausting `http.Transport` connection pools in long-running services.
- **G2 (Card violation):** `internal/signer/hmac.go` downgrades hashing from `crypto/sha256` to `crypto/sha1`. Eli's card (`webhook-signature.md`) strictly requires HMAC-SHA256.
- **G3 (Renamed CLI flag breaking caller):** `cmd/notifier/main.go` renames `--timeout` to `--max-timeout`. `scripts/drain.sh` (outside diff) still invokes `notifier --timeout 10` and fails under `set -e`.
- **N1 (Noise / False alarm):** `slog.Info` in `sender.go` logging the destination URL is permitted and is not a secret leak.

Clean branch contains documentation comment edits only. Every finding on the clean branch is noise.
