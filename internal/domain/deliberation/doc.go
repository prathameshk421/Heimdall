// Package deliberation models the resolver's output: the verdict a case receives, the decision it recommends and the
// citations that back it.
//
// # Purpose
//
// A verdict is a recommendation on a decision card, never a state change (D42). This package says what a verdict
// must contain before the server stores it or shows it to a person. Challenge rounds and consensus holds are not
// modelled yet (TECH_SPEC 11.2b, task T60).
//
// # API
//
//   - Verdict (the input of heimdall_submit_verdict) and its Validate, which returns every problem joined, each
//     naming its field path such as citations[0].ref.
//   - Decision (uphold, dismiss, amend, spec_change, needs_human) and CitationKind (run, memory, decision, finding),
//     with ParseDecision and ParseCitationKind. Both refuse a value outside their set while decoding.
//   - Citation: a kind, a ref and the quoted span the verdict relies on.
//
// # Invariants
//
//   - The case id is a valid case_ id, and the confidence is one of finding.Confidence.
//   - The rationale is 10 to 1000 characters after trimming, counted in runes.
//   - A verdict has 1 to 16 citations. Each citation's ref has the form its kind names: a run id, a finding name such
//     as F3, or a non-blank ref of at most 256 bytes for memory and decision refs, whose form the server cannot check
//     here.
//   - Each citation quotes 1 to 300 characters.
//
// # Failure modes
//
//   - An unknown decision, kind or confidence fails decoding, so the verdict never enters the domain in that form.
//   - A citation that names a run or memory the server cannot find is not caught here. The server re-verifies every
//     citation in the same transaction (TECH_SPEC 6.5, 11.4); this package only checks shape.
//
// # Tests
//
//   - verdict_test.go: the golden round trip, the validation table with field paths, the every-problem-at-once case,
//     unknown enums on decode, the decision set, and the inclusive limits (10 and 1000 characters, 16 citations,
//     256-byte refs, 300-character quotes).
//   - FuzzDecodeVerdict: any input that decodes and validates re-encodes to an equal, valid value.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
package deliberation
