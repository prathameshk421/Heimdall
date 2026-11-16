# Review Benchmark Suite (`testdata/reviewbench/`)

This suite evaluates reviewer agent precision and recall across planted defects, personal cards, and code-graph reachability.

## Evaluation sets

- `settle/`: Development set with 3 planted bugs (G1 nil deref out-of-diff, G2 renamed flag out-of-diff, G3 card-only batch limit) and clean control PR.
- `heldout/`: Held-out evaluation set (`webhook-notifier`), written before model evaluation to prevent overfitting prompt/skill instructions. Three planted defects and a clean control branch; the answer key is `heldout/GOLD.md`.

## Arms (heldout)

The arms are the ablation of `docs/REVIEW_ARCH.md` A6, run on the production model: "one thing at a time: person cards,
lens cards, stack skills, checks, refute, and talk between seats". Each arm switches one component off from the full
pipeline, and the full pipeline is the reference. (The one-off-from-full reading is ours; A6 does not spell out the arm list.)

| Arm | Component switched off |
|---|---|
| full | none (reference) |
| no-person-cards | person cards |
| no-lens-cards | lens cards |
| no-stack-skills | stack skills |
| no-checks | checks |
| no-refute | refute stage |
| no-talk | talk between seats |

Results go in a table in `docs/REVIEW_MOAT.md` with recall, precision, noise on the clean change, and tokens for every
arm, including the ones that did not help. Nothing has been run yet for this table.

## Held-out construction

- No answer hint is in `heldout/head/`, the two diffs, or the card: no `BUG` comments, no finding names. `GOLD.md` is the only key.
- Each diff is generated with `diff -ruN base head` (and `base clean`), then the `---`/`+++` labels are rewritten to `a/` and `b/`
  and the timestamps removed, to match `settle/`.
- `check_build.sh` checks that `base`, `head` and `clean` build with `go build ./...` and that the diffs apply to `base`
  to give `head` and `clean` exactly. Run it with `bash testdata/reviewbench/heldout/check_build.sh`.
