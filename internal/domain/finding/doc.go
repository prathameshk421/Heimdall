// Package finding models what reviewers submit and how a finding is judged: the agent-facing input schemas
// (FindingIn, Review), the evidence that backs a finding and the tier that evidence reaches, and the vocabulary of
// severities, categories and lifecycle states.
//
// # Purpose
//
// A finding is only as good as its evidence, so this package decides what counts as evidence before anything is
// stored. It does not run tests, call tools or change state; the harness and the event store do that.
//
// # API
//
//   - FindingIn, Location, Evidence (a tagged union: Type names the one payload set), ReproTest, ToolResult and the
//     New*Evidence constructors. FindingIn.Validate and Review.Validate return every problem joined, each naming its
//     field path such as findings[2].evidence[0].repro_test.path.
//   - Review (input of heimdall_submit_review, Review@1) and Review.Reviewed.
//   - CheckRepoPath, the one file-path rule that findings, reviews and test sets share.
//   - Enums with Valid-style parsers (ParseTier, ParseSeverity, ParseConfidence, ParseCategory, ParseState,
//     ParseEvidenceType). Their UnmarshalText refuses a value outside the set, so JSON cannot put one in the domain.
//   - Tier, State and their AtLeast, CanBlock, Terminal and Weight helpers, which encode TECH_SPEC 11.1 and 11.2.
//
// # Invariants
//
//   - Claim and reasoning are 10 to 600 characters after trimming, counted in runes, so padding cannot satisfy a
//     minimum. Refs (memory, pack, context) are not blank and at most 256 bytes.
//   - A finding has 1 to 8 evidence entries and at most 8 context refs. A review has at most 50 findings.
//   - Every file path is clean, relative, forward-slash, at most 512 bytes, with no control characters.
//   - Lines are 1-based and the first is at most the last.
//   - Evidence has exactly one payload, and that payload is the one its Type names.
//   - A review is either clean with no findings, or has at least one finding.
//   - The JSON wire format is frozen by the golden files in testdata/. A change to a field name fails the tests.
//
// # Failure modes
//
//   - An unknown enum value or evidence type fails decoding. The decoder refuses the whole payload, so the agent sees
//     one decode error, not a validation list. Missing fields decode and are reported by Validate.
//   - Validation reports every problem it finds, so a retry can fix them all at once.
//   - The package has no I/O, so it cannot fail on a dependency. The server maps a validation error to an agent
//     retry, and no finding is stored unvalidated.
//
// # Tests
//
//   - finding_test.go: the golden round trip, the validation table, unknown enums and fields, and the evidence
//     round trip over 2000 generated values.
//   - review_test.go: review goldens, the review validation table, a bad head SHA, Reviewed.
//   - rules_test.go: path edge cases, character counting, reasoning and ref limits, blank payload fields, and the
//     inclusive boundaries (8 evidence, single-line location), each added to kill a surviving mutant.
//   - vocab_test.go: every enum parses its own values and refuses others; severity and tier ordering.
//   - FuzzDecodeFindingIn: any input that decodes and validates re-encodes to an equal, valid value.
//     Run with go test -run=^$ -fuzz=FuzzDecodeFindingIn -fuzztime=20s.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
package finding
