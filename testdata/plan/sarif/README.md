# SARIF Pairs and New-Only Projection (`testdata/plan/sarif/`)

This directory contains test pairs of baseline and head SARIF outputs and their expected `New(base, head)` results.
Builders of M3 Review (step 1: `internal/domain/sarif`) must match this behavior and output shape byte for byte.

## Specification Reference

- **`docs/REVIEW_ARCH.md`** section 4.3 ("New in this change only") and section 4.4 ("One shape for everything a tool says").
- **`docs/BUILD_PLAN.md`** section 4 M3 ("Review", step 1).

## Architecture Rules (RV1)

1. **Fingerprint Definition:**
   `Fingerprint = rule_id + ":" + file_path + ":" + sha256(collapsed_whitespace(line_text))`
   Line numbers are not part of the fingerprint, so code that moved without modification does not produce false new results.
2. **`New(base, head []Result) []Result`:**
   A head result is retained if and only if no result in `base` matches its `Finger` fingerprint.
3. **One Normalized Shape:**
   All tool output (Opengrep/Semgrep, Gitleaks, Hadolint, Squawk, oasdiff) is normalized to `sarif.Result`.

## Data Model

```json
{
  "check": "patterns",
  "lens": "security",
  "rule": "sql-concat",
  "level": "error",
  "file": "internal/api/refunds.go",
  "lines": [45, 47],
  "message": "query formatted with string concatenation rather than parameterized query",
  "finger": "sql-concat:internal/api/refunds.go:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
}
```

## Pairs

- `pair1_security/`:
  - `base.sarif`: Contains a pre-existing key alert in legacy code.
  - `head.sarif`: Contains the pre-existing key alert plus a new SQL concatenation defect.
  - `new.json`: Exactly the new SQL concatenation result.
- `pair2_drift/`:
  - `base.sarif`: Clean (0 results).
  - `head.sarif`: OpenAPI spec drift error.
  - `new.json`: Exactly the spec drift result.
- `pair3_clean/`:
  - `base.sarif`: 2 existing linter warnings.
  - `head.sarif`: Identical 2 linter warnings.
  - `new.json`: `[]` (empty list; no new warnings).
