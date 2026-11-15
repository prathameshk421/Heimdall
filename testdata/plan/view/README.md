# Golden Views and Need Projections (`testdata/plan/view/`)

This directory contains golden event logs, folded views, missing needs, anomalies, and status outputs.
Builders of M0 Core (step 3) and M5 Interaction (step 4) must match these outputs byte for byte.

## Specification Reference

- **`docs/AGENT_INTERACTION_ARCH.md`** section 3 F1 ("Facts and views"), section 4.1 ("Who says what, where"), and section 13 H1 ("The fold").
- **`docs/BUILD_PLAN.md`** section 4 M0 ("Core", step 3), section 4 M5 ("Interaction", step 4).

## Architecture Principles

1. **Ingest never refuses a fact (C3, F1):** The event log holds recorded facts. A surprising fact (e.g. merge without approval) produces an anomaly in `anomaly.go`, not a rejection.
2. **`View` has no stored state field:** `View` is a pure projection folded from `[]change.Event` by `Fold(events)`.
3. **`Missing` replaces the state machine:** Computed as `Missing(v View, p policy.Policy) []Need`. A workflow row whose dependencies are satisfied but not completed is a need.
4. **`Render(v View, m []Need)`:** Produces the human-facing board status lines.

## Directory Structure

- `kan-22/`: Full story of ticket KAN-22 (health check endpoint), through five milestone points:
  - `point-1`: Tests written and committed on branch. Waiting for PR.
  - `point-2`: PR #10 opened; slop failed on scope; acceptance CI green.
  - `point-3`: Fixed head pushed; reviewers assigned; reviewer-1 filed F1 (upheld by slack_owner); reviewer-3 filed F2.
  - `point-4`: Push 59effae; approval recorded; re-reviews clean; acceptance CI green; gate evaluated ok: true.
  - `point-5`: PR closed with `merged: true`.
- `kan-23/`: Full story of ticket KAN-23 (refund cap & openapi spec drift), through five milestone points:
  - `point-1`: Tests submitted by test-author, awaiting approval signoff.
  - `point-2`: Tests approved by rekdfr and committed.
  - `point-3`: PR #11 opened; slop passed; reviewers picked and tasks assigned.
  - `point-4`: All 3 reviewers submitted reviews with 8 findings filed.
  - `point-5`: Sandbox verification complete; gate evaluated ok: false with blocking finding F1.
- `nov7-merge-from-ready/`: Live Nov 7 sequence where approval arrived during IN_REVIEW, placing the change in READY_FOR_APPROVAL, and GitHub merged the PR directly.
- `nov7-push-in-review/`: Live Nov 7 sequence where a new commit was pushed while reviewers were actively reviewing the old head.
- `merge-without-approval/`: Sequence where a PR was merged without any recorded approval on the head commit.

## File Formats

- `change.txt`: Single-line ticket ID (e.g. `KAN-22`).
- `script.jsonl`: Human-readable JSON Lines log input (`at`, `actor`, `type`, `payload`).
- `log.jsonl`: Fully formed, validated, hash-chained `change.Event` envelopes built by `internal/testkit/eventlog`.
- `prefix.jsonl`: The slice of `log.jsonl` corresponding to that point.
- `view.json`: Compact or formatted JSON representation of `change.View`.
- `missing.json`: JSON array of `Need`:
  ```json
  [
    {
      "step": "wait_pr",
      "role": "",
      "description": "pull request opened",
      "kind": ""
    }
  ]
  ```
- `anomaly.json`: JSON array of detected anomalies:
  ```json
  [
    {
      "code": "merged_without_approval",
      "event_index": 14,
      "description": "PR closed with merged: true but no approval was recorded for head commit"
    }
  ]
  ```
- `status.txt`: Multi-line plain text board status output.
