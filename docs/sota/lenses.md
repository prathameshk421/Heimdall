# Lenses and review quality (SOTA Phase 0, Nov 22, 2026)

Scope: whether each lens, seat and rule card is useful and state of the art: precision, recall, novelty, actionability,
cost, overlap between lenses, and what a person does with each result. Sources: `REVIEW_ARCH.md`, `TECH_SPEC.md` 9 to
11, the fixture policy (`heimdall.yaml` on `heimdall-test/demo-base`), and every finding of PR #18 (event log,
Nov 21, 20:22 to 22:05 UTC), the first live run with a promoted profile and a lint check.

## 1. Desired state

- **Each seat earns its seat.** A seat's findings come from its own lenses; two seats overlap only where a defect truly
  belongs to both lenses, and the person sees one finding with several reporters.
- **One defect is one finding.** Across seats and across heads, the same root cause is one finding that follows the
  code: a new head re-checks it (still there, fixed, moved) instead of filing it again.
- **Proof follows the claim.** A defect a tool can show is E2; one a test can show is E1; a concurrency defect is E1
  through a `-race` or stress repro, not E4. E4 is for judgement calls only, and it never blocks alone.
- **High recall on what matters, silence on clean code.** Planted bugs in the diff, out of the diff, and in the cards
  are found; a clean PR gets zero findings.
- **Every finding is actionable.** It names the line, the rule or card, the failing input, and the smallest fix.
- **Cost scales with the change, not with the head count.**

## 2. What PR #18 shows

The change: 20 lines (a duplicate-refund guard whose error `Refund` ignores, planted on purpose), on top of KAN-23's
refund branch, which carries the fixture's seeded defects. Three heads. Seats (fixture policy):
`reviewer-1 {dev: correctness, contract, tests}`, `reviewer-2 {security: security, data}`,
`reviewer-3 {infra: infra, operability}`.

| Root cause | Filings | Seats | Best tier reached | Real? |
|---|---|---|---|---|
| `paymentJSON` renames `merchant_id` to `merchant` against `api/openapi.yaml` | 9 (F1 F5 F10 F12 F19 F25 F31 F36 F44) | all three | E1 verified | yes (seeded) |
| Refund cap checked and recorded under two lock acquisitions (race) | 9 (F2 F6 F11 F14 F23 F26 F33 F39 F48) | all three | E4 only | yes |
| `POST /payments/{id}/refunds` missing from `api/openapi.yaml` | 8 (F3 F9 F17 F22 F27 F34 F42 F49) | all three | E4 only | yes |
| int64 overflow in the cap and in `ValidateNew` | 6 (F4 F7 F16 F28 F35 F41) | 1 and 3 | E1 verified | yes |
| `Refund` ignores `refunds.record`'s error (the planted bug) | 6 (F8 F15 F24 F32 F40 F47) | all three | **E2 verified** (golangci-lint) | yes (planted) |
| Refund ignores `Idempotency-Key` (ADR-004, PAY-142 AC2) | 5 (F13 F21 F29 F38 F46) | all three | E1 verified | yes (seeded, card) |
| Refund response lacks `refund_ref` (INC-031) | 4 (F20 F30 F37 F45) | all three | E1 verified | yes (seeded, memory) |
| KAN-23 tests pin the incomplete wire shape | 3 (F18 F43 F50) | 2 and 3 | E4 only | yes |

- **Recall: 8 of 8** real issues found, including the planted one, a memory-only one (INC-031) and a card-only one
  (ADR-004). Each seat alone found 7 or 8 of them (reviewer-1: 7, reviewer-2: 7, reviewer-3: 8).
- **Precision of claims: no false finding** seen in the 50; the 33 verified are proven true. The E4 ones were read by
  hand and are true too (not independently proven).
- **Duplication: 50 filings for 8 issues (6.25×).** Every seat filed nearly every issue, and every head filed them again.
- **Lens separation: none.** The infra seat filed the overflow and the errcheck bug (correctness); the security seat
  filed the OpenAPI drift (contract). Findings do not record a lens at all.

## 3. What exists

| Part | Behaviour | Evidence |
|---|---|---|
| Seats and lenses | Policy names each seat's role and lenses; the brief and cards are per seat | fixture `heimdall.yaml` lines 20-24; `internal/app/reviewbrief` |
| Leads | Profile check results reach reviewers as leads with lens and rule | `internal/domain/finding/submit.go:9-13`; live PR #18 (errcheck lead) |
| Tiers | E1 repro test, E2 tool result, E3 memory, E4 judgement; verification re-runs E1 and E2 | `TECH_SPEC.md` 11; events `finding.verified` 685-698 |
| Finding record | `fingerprint`, `category`, `location`, `card_refs`; **no lens** | `internal/domain/change/events_review.go:150-170` |
| Cross-seat merge | Specified (overlapping lines and claim Jaccard ≥ 0.6 merge into one finding with several reporters); not seen working on PR #18 | `TECH_SPEC.md` 566; F32, F40, F47 verified separately |

## 4. Gaps

| # | Gap | Severity | Evidence | Smallest correct fix | Test that would have caught it |
|---|---|---|---|---|---|
| L1 | **Fixed Nov 22.** `buildReviewEvents` matches incoming findings against existing live findings across heads by fingerprint or overlapping file+lines + claim Jaccard ≥ 0.6, reusing the original `F<n>` ref instead of re-filing. Live multi-head run not verified yet. | done | `internal/app/agenttools/dedup.go`, `reviews.go` | — | `TestCrossSeatAndCrossHeadDuplicateMergesIntoOneFinding` |
| L2 | **Fixed Nov 22.** Cross-seat duplicates merge into the existing finding ref on submit; `View` tracks additional `Reporters` and `gate_facts.go` counts them toward consensus. Live multi-seat run not verified yet. | done | `internal/app/agenttools/dedup.go`, `internal/domain/change/view.go`, `gate_facts.go` | — | `TestCrossSeatAndCrossHeadDuplicateMergesIntoOneFinding` |
| L3 | **Lenses do not steer.** Every seat reviews everything; the infra seat files correctness bugs. Findings carry no lens, so overlap cannot even be measured. | high | seats in fixture policy vs filings above; `FindingRecord` has no lens | Record the lens on every finding (required, one of the seat's lenses); the skill tells a seat to leave out-of-lens issues to their owner seat unless no seat holds that lens | Submit validation test: a finding with a lens the seat does not hold is refused with the owner seat's name |
| L4 | **Concurrency defects stay E4.** The race was filed 9 times and never proven, although a `go test -race` repro could make it E1. | medium | 9 race filings, best tier E4 | The reviewer skill and `get_task` say how to prove a race (a repro test under `-race` or a stress loop); the runner runs repro tests with `-race` when the claim's category is concurrency | Live: Phase 2 planted race must reach E1 |
| L5 | **Precision on a clean PR is unmeasured.** No run has offered a clean change; silence is not known to hold. | high | not verified | Phase 2 suite: a clean PR must get zero findings from every seat | Phase 2 |
| L6 | **Fixed Nov 22.** `dashboard.ScorecardFromEvents` now reports `ByCard` and `ByCategory` (`Filed`, `Verified`, `Upheld`, `Dismissed`) on `GET /admin/ops`. | done | `internal/app/dashboard/scorecard.go` | — | `TestScorecardCountsByCardAndCategory` |
| L7 | **The `vet` check writes text, not SARIF**, so it can never produce a lead; it costs runner time on every head. | low | promoted profile `49423b50` (`vet` check, `go vet ./...`); D192 notes | Drop `vet` (golangci-lint runs govet) or convert its output | Profile validation warns on a check without a SARIF writer |

## 5. Order of work

1. L1 and L2 together: they remove most of the 6.25× duplication and most review cost (`performance.md` P1, P2).
2. L3: record and enforce lenses, then measure overlap.
3. L5 and L4 in the Phase 2 suite (clean PR, planted race).
4. L6, L7.

## Live update (Nov 22, PR #20)

L1 and L2 were re-tested live after Phase 1. Recall held (both planted defects found, F1 verified E1 and F2 verified E2),
and three gaps in the merge rule were fixed with tests built from the live claims (`aff303a`, `7758670`): the 0.6 claim
threshold (now 0.34, same category), code moved by a later head (3-line slack), and E4 findings that were never merge
candidates. Remaining: same-defect paraphrases scoring 0.29 to 0.33 by word overlap, which overlaps the highest
different-defect score on PR #18 (0.29). Word overlap is not enough; a code-identifier or embedding signal is the next step.
