# SARIF pairs and the new-only projection (`testdata/plan/sarif/`)

Each `pairN/` holds the output of one check run on base and on head, the two source trees the reported lines come
from, and the expected `New(base, head)`. M3 step 1 (`internal/domain/sarif`) must reproduce `new.json` exactly.

Spec: `docs/REVIEW_ARCH.md` 4.1 (a check is a profile row), 4.3 (new in this change only), 4.4 (the `Result`
shape), RV1 tests first. `docs/BUILD_PLAN.md` M3 step 1.

## Inputs

- `profile.yaml`: the profile rows, copied from REVIEW_ARCH 4.1. A check writes one `/out/result.sarif`, so a
  pair is the output of one row. `pairN/input.json` names it (`{"check": "patterns"}`). `Result.Check` is the row's
  `id` and `Result.Lens` is the row's `lens` (4.1: the lens belongs to the check, not to the tool or the file).
  Neither is in any SARIF file.
- `pairN/base/` and `pairN/head/`: the trees at `/base` and `/work`. Every reported region is real text in them.
  Each result's `region.snippet.text` equals those lines, so a reader may take the text from either place
  (see Open questions 1).
- `base.sarif` / `head.sarif`: SARIF 2.1.0. `ruleId` is bare, as the tool emits it. `level` is the SARIF level.

## The fingerprint

REVIEW_ARCH 4.3: rule id + file path + hash of the reported lines' text with whitespace collapsed. The goldens use

```
finger = ruleId + ":" + file + ":" + hex(sha256(collapse(text)))
collapse(text) = every maximal run of whitespace (including newlines) becomes one space, then trim both ends
text           = lines startLine..endLine of the file, joined by "\n"
```

Line numbers are not in it. The hash is over the text, never over the empty string (a placeholder hash such as
`e3b0c442...` is the hash of empty input and would make every result in a file equal).

## Pairs

| Dir | Check | Base | Head | `New` |
|---|---|---|---|---|
| `pair1` | patterns (security) | 1 `sql-concat` in `internal/legacy/report.go` | the same one plus a new `sql-concat` in `internal/api/refunds.go` | the refunds result only |
| `pair2` | api-break (contract, `baseline: false`) | 0 results | 1 `new-required-request-parameter` | that result (nothing to match in base) |
| `pair3` | migrations (data) | 1 warning: `adding-not-nullable-field` | the same warning, same file | `[]` |
| `pair4` | patterns | `sql-concat` at lines 11-12 | same text, now at lines 18-19 (a helper was added above it) | `[]`: moved code is not new |
| `pair5` | patterns | `sql-concat` at lines 11-12 | same lines, text changed (`id` to `paymentID`) | the head result: a changed line is new |
| `pair6` | patterns | 1 `sql-concat` | 0 results (the code was fixed) | `[]`: a base-only result is dropped |

`pair4`, `pair5` and `pair6` are the three cases REVIEW_ARCH RV1 lists under "Tests first" for `New`. `pair1` to
`pair3` keep the original three shapes.

## Open questions (the docs do not decide these; goldens use the choice written here)

1. **Where the reported lines' text comes from.** 4.3 names the text but `Parse` receives SARIF and `New` receives
   `Result` values. Options: `region.snippet.text` (tools differ in whether they emit it) or a read of the tree at
   `/base` and `/work`. The goldens satisfy both; the builder picks and records it in REVIEW_ARCH.
2. **The `Finger` string format and the whitespace rule.** 4.3 gives the three parts, not the separator, whether
   the rule id is bare or `check/rule`, or whether collapsing also trims. The goldens use `rule:file:hex` with the
   bare rule id, and trim. No case depends on trimming (moved code keeps its indentation in `pair4`).
3. **Where `Check` and `Lens` are filled.** From the profile row, by `Parse` (given the row) or by the executor
   after `Parse`. The goldens only say the values.
4. **Rule ids from converters.** `adding-not-nullable-field` and `new-required-request-parameter` are emitted by
   squawk and oasdiff converters that SR1 has not yet verified (REVIEW_ARCH 4.2: "not verified"). The ids are
   plausible tool ids, not verified output. `sql-concat` is a rule Heimdall writes (5.2), so its id is ours. Opengrep
   may prefix a path to `ruleId`; not verified.
5. **Real tool output.** These files are hand-made SARIF in the 2.1.0 shape. None is recorded output of a tool
   (`testdata/real/` holds none yet). Not verified against any tool.
