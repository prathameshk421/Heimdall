G1 Lookup returns nil,nil: nil deref at internal/payout/runner.go lineFor, and statements/monthly.go loses its ErrNotFound branch and derefs. Files not in the diff.
G2 --dry-run flag renamed to --preview; scripts/nightly.sh still passes --dry-run, so the nightly job fails under set -e. File not in the diff.
G3 maxBatch 2000: bank silently drops above 500. Only the priya card says so.
N1 slog of the IBAN: false alarm for priya (card allows). Generic reviewers may flag it. Counted as noise if filed by a reviewer holding the card; otherwise counted separately.
Clean branch: comment edits and the same log line. Every finding other than the log line is noise.

Run: heimdall cards preview --base base --head head --diff head.diff --cards cards --person priya
Results and limits: docs/REVIEW_MOAT.md section 4.
