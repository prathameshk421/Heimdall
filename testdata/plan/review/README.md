# Golden Reviews and Submit Validation (`testdata/plan/review/`)

This directory contains valid and refused `Review@2` submissions.
Builders of M3 Review (step 5: `Review@2` and submit rules) must match these validations byte for byte.

## Specification Reference

- **`docs/REVIEW_ARCH.md`** section 5.4 ("The exact mapping, end to end"), section 5.5 ("Answers must be complete and consistent"), and section 12 RV5.
- **`docs/BUILD_PLAN.md`** section 4 M3 ("Review", step 5).

## Architecture Rules (RV5)

1. **Every obligation answered:** Every card listed in the task's `obligations` must have an entry in `rule_checks`.
2. **Every lead answered:** Every check result in `leads` must have an entry in `lead_checks`.
3. **No contradictions:** A card marked `holds` cannot be cited as violated in `findings`, and a card marked `violated` must have a corresponding finding.
4. **Confirmed leads backed by findings:** A lead marked `confirmed` must be cited by an E2 finding in the review. A `dropped` lead must supply a non-empty explanation `reason`.
5. **Consistency with clean flag:** `clean: true` requires an empty `findings` array.

## Test Cases

### Valid Submissions (`valid/`)
- `clean_review.json`: All obligations checked and hold; all leads dropped with valid reasons; `clean: true`; `findings: []`.
- `review_with_findings.json`: Obligations checked (one violated, one holds); lead confirmed with matching E2 finding; `clean: false`.

### Refused Submissions (`refused/`)
- `missing_obligation.json`: Fails because an assigned obligation card was not answered in `rule_checks`.
- `missing_lead.json`: Fails because an assigned tool lead was not answered in `lead_checks`.
- `contradiction_obligation.json`: Fails because card `authz-route.md` was marked `holds` in `rule_checks` while simultaneously cited in a finding.
- `confirmed_lead_no_finding.json`: Fails because a lead was marked `confirmed` in `lead_checks` but no finding in `findings` cites the lead's evidence.
