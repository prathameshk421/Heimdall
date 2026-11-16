# Mining goldens (`testdata/plan/mine/`)

M3 step 8 (`internal/mine`, `cmd/heimdall-mine`) reproduces `rules.json` from `history.jsonl`, and `sarif.json`
from those rules and `change.json`, byte for byte.

Spec: `docs/REVIEW_ARCH.md` 4.6 (forgotten companions), 4.2 (the catalogue: `heimdall-mine` is a `contract`
check), 4.4 (the Result shape), RV10 ("support, confidence, the 30-file skip, merges ignored").

## Inputs

- `history.jsonl`: 21 synthetic commits, oldest first: `{hash, parents, files}`. Everything in it is authored.
- `change.json`: the change under review, PR #12, which changes only `migrations/0012_backoff.sql`.

## The computation (4.6)

```
transactions  = commits with at most one parent and at most 30 files
support(A,B)  = transactions that changed both A and B
commits(A)    = transactions that changed A
confidence    = support(A,B) / commits(A)
rule A => B   when support >= 5 and confidence >= 0.8
```

Rules are over exact file paths; there are no globs. A rule is directed: `A => B` and `B => A` are separate rules
with their own confidence. The limits (last 2000 commits, 30 files, support 5, confidence 0.8) are the 4.6
defaults. 4.6 says support and confidence come from policy, but no policy key is named (open question 1).

In this history:

- `c010` changes 35 files and is skipped.
- `c021` is a merge (two parents) and is ignored. It changes the migration and the schema. Counted, it would make
  `commits(migration)` 8 and drop `migration => event.go` to 6/8 = 0.75, below 0.8. So a miner that counts merges
  gets a different `rules.json`.
- 19 transactions remain. They give exactly 10 rules:

| Rule | support | commits(A) | confidence |
|---|---|---|---|
| `cmd/heimdall/main.go` <=> `internal/config/config.go` | 6 | 6 | 1 |
| `internal/domain/finding/finding.go` <=> `finding_test.go` | 6 | 6 | 1 |
| `schema.sql` <=> `event.go` | 6 | 6 | 1 |
| `schema.sql` => `migrations/0012_backoff.sql` | 6 | 6 | 1 |
| `event.go` => `migrations/0012_backoff.sql` | 6 | 6 | 1 |
| `migrations/0012_backoff.sql` => `schema.sql` | 6 | 7 | 6/7 |
| `migrations/0012_backoff.sql` => `event.go` | 6 | 7 | 6/7 |

(`c011` changes the migration with `docs/RUNBOOK.md` and nothing else, which is why the migration's rules are 6/7.)

## `rules.json`

A JSON array of `{antecedent, consequent, support, antecedent_commits, confidence}`, sorted by antecedent, then
consequent. `confidence` is the float64 quotient written by Go `encoding/json`: `1` for 1.0, `0.8571428571428571`
for 6/7. There is no rounding anywhere: the SARIF output carries no float (below). Two-space indent, one newline at
the end.

## `sarif.json`

4.6: for each changed `A` with a rule `A => B` where `B` is not in the change, one result on `A`. Results go in the
order of `change.json` files, then by consequent path. PR #12 changes only the migration, so both of its rules fire:
two results, schema then `event.go`.

- `ruleId` `forgotten-companion`, `level` `note` (4.6: "always a lead, never an enforced rule"; 4.4 levels are
  error, warning, note).
- `message.text` is the 4.6 sentence with the numbers filled in.
- The location is the file `A` with **no region**. 4.6 puts the result "on A" and names no line, so the golden
  gives none (the earlier `startLine: 1` came from nowhere). The SARIF reader maps a result with no region to
  `Lines` `[0, 0]` (open question 2).
- No `properties`. 4.4's `Check` and `Lens` are not the tool's to say: the server's reader takes them from the
  profile row that ran the tool (4.1). The 4.2 catalogue gives that row's lens as `contract`. A repo may put it
  under another lens (`data` fits a migration); that is its profile's choice, not the miner's.
- The driver is `{"name": "heimdall-mine"}`. No version is given, because none is defined.

## Open questions

1. **Policy keys** for N, the file cap, minimum support and minimum confidence (4.6 says "from policy").
2. **A result with no region**: does `Result.Lines` become `[0, 0]`, and how does `New` (4.3) fingerprint a
   file-level result with no reported lines? 4.6 does not say whether the mining row has a baseline; no golden here depends on it.
3. **Several antecedents with the same consequent** (two changed files both missing `B`): one result per
   antecedent, as 4.6 literally says, or one per `B`? No case here.
