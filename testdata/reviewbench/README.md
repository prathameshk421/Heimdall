# Review Benchmark Suite (`testdata/reviewbench/`)

This suite evaluates reviewer agent precision and recall across planted defects, personal cards, and code-graph reachability.

## Evaluation Framework

- **Arms:**
  - `Arm A`: Base model without personal cards or code graph.
  - `Arm B`: Base model with personal rule cards only.
  - `Arm D`: Base model with code graph navigation only.
  - `Arm C`: Base model with both personal rule cards and code graph.
- **Evaluation Sets:**
  - `settle/`: Development set with 3 planted bugs (G1 nil deref out-of-diff, G2 renamed flag out-of-diff, G3 card-only batch limit) and clean control PR.
  - `heldout/`: Held-out evaluation set written before model evaluation to prevent overfitting prompt/skill instructions.

## Held-Out Case: `webhook-notifier`

A delivery daemon that signs and posts webhooks with retries.
Contains 3 planted defects and a clean control branch.
