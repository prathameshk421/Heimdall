# Golden Test Plans and Validation (`testdata/plan/testplan/`)

This directory contains valid and refused `TestPlan@1` submissions.
Builders of M4 Tests (step 3: `internal/domain/testplan`) must match these validation rules byte for byte.

## Specification Reference

- **`docs/TEST_AGENT_ARCH.md`** section 4.1 ("What it is"), section 4.2 ("Server checks on the plan"), and section 13 TA2.
- **`docs/BUILD_PLAN.md`** section 4 M4 ("Tests", step 3).

## Architecture Rules (TA2)

1. **Negative / boundary requirement:** Every acceptance criterion must have at least one behaviour, and the plan must contain at least one `negative` or `boundary` behaviour.
2. **Oracle enum:** Every behaviour's oracle must be one of:
   `exact`, `property`, `metamorphic`, `differential`, `regression_seed`.
3. **Rule case oracle constraint:** A behaviour with `case: "rule"` must specify an oracle of `property` or `metamorphic`.
4. **Declared kinds only:** Every behaviour's `kind` (e.g. `api`, `property`, `contract`, `ui`) must be explicitly declared in the repository's `.heimdall/profile.yaml`.
5. **Bounded size:** Plan size and counts must remain within schema bounds.

## Test Cases

### Valid Plans (`valid/`)
- `valid_plan_kan31.json`: Canonical example from `TEST_AGENT_ARCH.md` section 4.1 with `exact` and `property` oracles, positive, rule, and negative cases.
- `valid_plan_kan22.json`: Health endpoint plan covering AC1 (positive) and AC2 (negative refusal of invalid methods).

### Refused Plans (`refused/`)
- `no_negative_case.json`: Refused because all behaviours are positive; missing negative/boundary cases.
- `invalid_oracle.json`: Refused because an unknown oracle (`fuzzy`) is used.
- `rule_case_bad_oracle.json`: Refused because `case: "rule"` is paired with oracle `exact` instead of `property`/`metamorphic`.
- `undeclared_kind.json`: Refused because kind `exploit` is not declared in the repository profile.
