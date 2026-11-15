# Co-Change Mining Goldens (`testdata/plan/mine/`)

This directory contains synthetic commit transactions, mined co-change association rules, and emitted SARIF results.
Builders of M3 Review (step 8: `internal/mine` and `cmd/heimdall-mine`) must match these calculations byte for byte.

## Specification Reference

- **`docs/REVIEW_ARCH.md`** section 4.6 ("Mining history: forgotten companions") and section 12 RV10.
- **`docs/BUILD_PLAN.md`** section 4 M3 ("Review", step 8).

## Mining Algorithm

1. **Transaction Ingestion:**
   - Scan the last N commits on the base branch (policy parameter, default 2000).
   - Ignore merge commits (commits with multiple parents).
   - Skip commits that modify more than 30 files (bulk refactors / vendor updates).
2. **Support and Confidence:**
   - `support(A, B)` = number of commits modifying both file `A` and file `B`.
   - `confidence(A => B)` = `support(A, B) / support(A)`.
3. **Thresholds (from policy):**
   - Minimum support: `support(A, B) >= 5`.
   - Minimum confidence: `confidence(A => B) >= 0.8`.
4. **Lead Emission:**
   - When a change edits `A` but does not edit `B`, emit a SARIF lead result on `A`:
     `"<B> changed in <support> of the last <commits_A> commits that changed <A>. <B> is not in this change."`

## Test Artifacts

- `history.jsonl`: 20 sequential commits showing files modified per commit.
  - Pair `migrations/0012_backoff.sql` and `internal/adapters/postgres/schema.sql` change together in 6 out of 7 commits modifying the migration (support = 6, confidence = 6/7 = 0.857).
  - Bulk commit #10 touches 35 files and is skipped.
- `rules.json`: The association rule mined from `history.jsonl`:
  `migrations/*.sql => internal/adapters/postgres/schema.sql`.
- `sarif.json`: The lead emitted when PR #12 edits `migrations/0012_backoff.sql` without touching `internal/adapters/postgres/schema.sql`.
