# Gold Findings: Held-Out Benchmark (`webhook-notifier`)

Trees: `base/` is the code before the change, `head/` is after it (`head.diff`), and `clean/` is the control (`clean.diff`).
Each diff is `diff -ruN base head` with `a/` and `b/` paths and no timestamps. `check_build.sh` builds each tree and checks
that applying each diff to `base/` gives exactly that tree.

- **G1 (resource leak, inside the diff):** `internal/delivery/sender.go` removes `defer resp.Body.Close()` on the success path.
  The reply is read to the end but never closed, so its connection does not return to the pool, and a long-running notifier
  leaks sockets. This finding is inside `head.diff`. An out-of-diff form needs a caller that uses a returned response; that
  caller would have to change with the signature, so it would join the diff. The case therefore keeps G1 inside the diff.
- **G2 (team convention, card only):** `internal/delivery/sender.go` logs the delivery under the key `id` instead of
  `delivery_id`. Eli's card `cards/eli/webhook-log-fields.md` requires the key `delivery_id`, because the on-call search indexes
  on it. A generic reviewer sees an ordinary structured log line; only the card makes it a finding.
- **G3 (renamed CLI flag, caller outside the diff):** `cmd/notifier/main.go` renames `--timeout` to `--max-timeout`.
  `scripts/drain.sh` is not in the diff and still runs `notifier --timeout 10` under `set -euo pipefail`, so the drain fails
  with an unknown-flag error.
- **N1 (noise):** `slog.Info` in `sender.go` logs the destination URL. It is not a secret leak and is permitted.

The clean branch adds one comment line in `cmd/notifier/main.go`. Every finding on the clean branch is noise.
