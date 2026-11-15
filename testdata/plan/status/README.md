# Board Status Rendering Goldens (`testdata/plan/status/`)

This directory contains golden status board text rendered by `Render(v View, m []Need)`.
Builders of M5 Interaction (step 4: `app/announce`, `board.go`) must match these layouts byte for byte.

## Specification Reference

- **`docs/AGENT_INTERACTION_ARCH.md`** section 4.1 ("Who says what, where") and section 13 H7 ("The board").
- **`docs/BUILD_PLAN.md`** section 4 M5 ("Interaction", step 4).

## Layout Specification

A status message is composed of 6 fixed rows:
1. **Header Row:** Ticket ID · Title [right-aligned summary status, e.g. `blocked: 1 finding, waiting for approval`].
2. **Tests Row:** Tests status, behaviour count, commit SHA.
3. **PR Row:** PR number, author, slop status, CI acceptance status on head SHA.
4. **Reviews Row:** Review status by seat / role with icons (✅ clean, ❌ findings, ⏳ in progress).
5. **Findings Row:** Active findings with severity, status, and evidence tier.
6. **Next Row:** Plain English translation of `Missing(view, policy)` describing what action is required.

## Test Artifacts (`rendered/`)

- `kan22_point1.txt` to `kan22_point5.txt`: Milestone points for KAN-22.
- `kan23_point1.txt` to `kan23_point5.txt`: Milestone points for KAN-23.
- `nov7_merge_from_ready.txt`: Sequence for merge from ready.
- `nov7_push_in_review.txt`: Sequence for push during review.
- `merge_without_approval.txt`: Sequence for merge without approval.
