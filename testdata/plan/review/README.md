# Golden reviews and submit rules (`testdata/plan/review/`)

Each case is one file: the seat's **brief** (the input the submit rules check against), the **submission**, and for a
refused case the **expected refusal**. M3 step 5 (`Review@2` and the submit rules) must accept every `valid/` case and
refuse every `refused/` case with exactly the refusal written in it.

Spec: `docs/REVIEW_ARCH.md` 5.4 (steps 4 and 6), 5.5, RV5, section 11 (`Review@2` adds `lead_checks`);
`docs/AGENT_INTERACTION_ARCH.md` 6.2 (a refusal has the field, what is wrong, a minimal valid example) and H5.

## Case file

```
{ "brief":      { "obligations": [card file names], "leads": [Result + lead_id] },
  "submission": a Review@2,
  "expected_refusal": { "field": ..., "problem": ..., "example": ... }   // refused cases only
}
```

- `brief.obligations` are the card file names the seat must answer (5.4 step 4). `brief.leads` are new results (REVIEW_ARCH
  4.4 `Result` fields) of a lens the seat holds. `lead_id` is the key a `lead_checks` entry uses (open question 1).
- `submission` is `Review@1` (`internal/schema/testdata/Review@1.schema.json`) plus `lead_checks`, which is what section 11
  says `Review@2` adds. There is no `title` key: the schema has `additionalProperties: false` and no `title` property, so a
  `"title": "Review@2"` inside a submission would be refused for that and not for the rule under test. The version is
  carried by the schema name, not by the submission. Every submission here validates against `Review@1` once `lead_checks`
  is removed (checked with a JSON Schema validator).
- Match is **exact** on `field`, `problem` and `example`. `example` is the refusal's minimal valid fix (H5, 6.2).

## Rules under test (5.5)

1. Every card in `obligations` has one `rule_checks` entry; every lead has one `lead_checks` entry.
2. `holds` is refused when a finding in the same review cites that card; and the reverse: `violated` needs a finding
   that cites the card.
3. A `confirmed` lead has a finding that cites its run. A `dropped` lead has a reason.
4. `clean` is true only when `findings` is empty (Review@1 schema description and `finding.Review`).

A finding cites a card by putting the card's file name, the same string as `rule_checks[].card`, in `context_refs`.
Each refused case breaks exactly one rule.

## Cases

| File | Brief | What it does |
|---|---|---|
| `valid/clean_review.json` | cards `authz-new-route.md`, `secrets-in-logs.md`; 1 lead | both cards answered, the lead dropped with a reason, `clean: true`, no findings |
| `valid/review_with_findings.json` | cards `spec-drift.md`, `secrets-in-logs.md`; no lead | `spec-drift.md` violated and cited by the one finding; `secrets-in-logs.md` is `not_applicable`. The finding's file `internal/api/health.go` is in `files_reviewed` |
| `refused/missing_obligation.json` | as clean | no entry for `secrets-in-logs.md` (rule 1) |
| `refused/missing_lead.json` | as clean | `lead_checks` is empty (rule 1) |
| `refused/contradiction_holds_but_cited.json` | spec-drift brief | `spec-drift.md` is `holds` and a finding cites it (rule 2) |
| `refused/violated_without_finding.json` | spec-drift brief | `spec-drift.md` is `violated`, `findings` is empty (rule 2, reverse) |
| `refused/confirmed_lead_without_finding.json` | one card, one lead | lead confirmed, no finding cites its run (rule 3). Its `example` is `null` until open question 2 is decided |
| `refused/dropped_lead_without_reason.json` | as clean | lead dropped, no `reason` (rule 3) |
| `refused/clean_with_findings.json` | spec-drift brief | `clean: true` and one finding (rule 4) |

The old `contradiction_obligation.json` README text named the card `authz-route.md`; the file used `spec-drift.md`. The
case now uses `spec-drift.md` and the README says the same.

## Open questions (the docs do not decide these; the case is left out or the value is marked)

1. **Lead identity.** `lead_checks` must name a lead, but REVIEW_ARCH gives a `Result` no id. `lead_id` is a placeholder
   key the brief carries. Candidates: the `Finger`, or a brief-assigned id. Decide in 5.4.
2. **How a finding cites its run.** 5.5 says a confirmed lead "has a finding that cites its run", and 4.5 says the finding
   cites the run, but neither names the field or the run's id. `context_refs` is documented as "pack or memory references".
   Because of this there is **no valid case with a confirmed lead**, and the refused confirmed-lead case has no example.
   Once decided, add the valid case and the example.
3. **Findings with several problems.** Whether `submit_review` returns the first refusal or all (`finding.Review` joins
   its errors today). Each case breaks one rule, so no case depends on it.
4. **A finding outside `files_reviewed`.** The Go type has `Review.Reviewed(file)` but no document says a finding whose
   `location.file` is not in `files_reviewed` is refused. The valid case keeps them consistent; there is no refused case.
5. **Whitespace-only `reason` or `note`.** Not decided; no case.
6. **`Review@2`: "findings carry staged file paths"** (section 11). The shape is not given; no case uses it.
