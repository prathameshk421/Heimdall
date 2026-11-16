# Test plan goldens (`testdata/plan/testplan/`)

M4 step 3 (`internal/domain/testplan`) must accept every `valid/` case and refuse every `refused/` case with exactly
the refusal written in it.

Spec: `docs/TEST_AGENT_ARCH.md` 4.1 (the plan), 4.2 (server checks), 5.1 (the profile declares kinds), TA2;
`docs/AGENT_INTERACTION_ARCH.md` 6.2 and H5 (every refusal is `{Field, Problem, Example}`); `docs/BUILD_PLAN.md`
M4 done-when ("a plan with no negative case is refused naming the criterion").

## Case file

```
{ "input":            {"criteria": [AC ids of the ticket], "declared_kinds": [kinds the profile declares]},
  "plan":             a TestPlan@1, exactly the 4.1 shape,
  "expected_refusal": {"field": ..., "problem": ..., "example": ...}   // refused cases only
}
```

- `input.criteria` is what the server knows from the ticket (`change.triggered` acceptance criteria).
  `input.declared_kinds` is the kinds of the repository profile. Both cases use the four kinds of the 5.1 profile
  example: `api`, `property`, `contract`, `ui`. The refusal lists the kinds from this input, not a fixed list.
- `plan` has the keys of 4.1 and no others. There is no `title`: 4.1 has none, and the schema name carries the
  version.
- Match is **exact** on `field`, `problem` and `example`. `field` is a path into the plan. `example` is the
  smallest valid replacement for that field (6.2: "a minimal valid example"), as JSON text.

## Rules (4.2), in the order the server applies them

1. Every criterion in `input.criteria` has a behaviour (`behaviours[].ac`).
2. At least one behaviour has `case` `negative` or `boundary`.
3. Every `oracle` is `exact`, `property`, `metamorphic`, `differential` or `regression_seed`.
4. A behaviour with `case` `rule` has oracle `property` or `metamorphic`.
5. Every `kind` is in `input.declared_kinds`; the refusal lists the declared ones.

Each refused case breaks exactly one rule, checked by evaluating all five rules on every case. So no golden depends
on which refusal wins when several apply, or on whether all are returned (open question 2).

Rule 2 is read as plan-wide, as 4.2 words it. Read per criterion it would refuse the 4.1 example itself (AC1 has
only a positive and a rule behaviour). To meet the M4 done-when, the refusal names every criterion it applies to.
See open question 1.

## Cases

| File | Breaks | What it is |
|---|---|---|
| `valid/kan31_from_the_spec.json` | none | the 4.1 example, unchanged |
| `valid/kan22_recorded_ticket.json` | none | the recorded KAN-22 ticket's two criteria (from `testdata/plan/view/kan-22` event 1): a positive and a boundary |
| `refused/criterion_without_behaviour.json` | 1 | AC2 has no behaviour; AC1 has a boundary, so rule 2 holds |
| `refused/no_negative_case.json` | 2 | every criterion covered, every behaviour positive or rule |
| `refused/unknown_oracle.json` | 3 | `behaviours[0].oracle` is `fuzzy` |
| `refused/rule_case_exact_oracle.json` | 4 | the rule behaviour `B2` has oracle `exact` |
| `refused/undeclared_kind.json` | 5 | `behaviours[2].kind` is `exploit` |

## Open questions (the docs do not decide these; no case depends on them)

1. **Negative per plan or per criterion?** 4.2 says "at least one" for the plan; the M4 done-when says the refusal
   names "the criterion". Which criterion, when the rule is plan-wide? The goldens name all of them.
2. **First refusal or all?** When a plan breaks several rules, does the tool return the first (in the order above)
   or every refusal? H5's repeat detection compares refusals, so the answer changes what counts as "the same".
3. **Size bounds.** 4.2 says "sizes are bounded" but no document gives a number (behaviours per plan, text length,
   paths). There is no oversize case until the bounds are set.
4. **A behaviour naming an unknown criterion** (`ac: "AC9"`), and duplicate behaviour ids: not in 4.2; no case.
