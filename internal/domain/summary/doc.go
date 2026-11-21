// Package summary renders the proof summary comment: the one thing a person reads on a pull request in Action mode
// (STRATEGY 3, ACTION_MODE 5, 6 and 7, work package A3).
//
// # Purpose
//
// The fast check, the proof run and the reviewers each produce facts. This package turns them into one GitHub-flavoured
// Markdown page that the adapter writes as a comment and updates in place. It is a pure function from values to a
// string: no I/O, no clock, no logging, no GitHub client. The caller reads the world, fills Input and writes the result.
//
// Why a new package, not an extension of package present. Package present decides what a review shows as line comments
// (scope and a budget of comments). This package writes one page from different inputs (fastcheck.Result,
// proof.Outcome, findings with their proof class, cost). The two share no type and no rule, and a line comment budget
// has no place on a summary. A new package keeps both small.
//
// The page has these parts, always in this order:
//
//  1. A hidden marker line, then one headline: the verdict in plain words, the mode (shadow or enforce) and the short
//     head commit. Example: "Proven: 1 blocking finding · enforce · `a1b2c3d`".
//  2. Needs a person: what proof cannot settle (an unknown run, a run that reproduced nothing, a proven finding that
//     does not block, an unknown clause, an unmet clause only an approver can settle).
//  3. Proven findings: id (F1), place (file:line), claim, severity, tier, whether it blocks, the evidence (failing test,
//     tool and rule id, or team rule card) and who raised it.
//  4. Unproven, not shown: a collapsed details block. The summary line gives the count. Inside are the counts per
//     reviewer with reasons ("CodeRabbit 7: 5 no test reproduced, 2 refuted") and then the findings.
//  5. Fast check: one line per clause, met, unmet, unknown or not checked, with its reason.
//  6. Not checked: what Heimdall did not look at, so nobody reads silence as safety.
//  7. Cost: this run, the month to date if given, tokens and wall time.
//
// # API
//
//   - Render(Input) string: the page. It never fails and it never returns more than MaxBytes (60,000, under GitHub's
//     65,536).
//   - Input: the change id, the write counter, the full head commit, policy.Action (mode and proof_required),
//     fastcheck.Result, proof.Outcome, the Findings and the Usage (month to date and wall time).
//   - Finding: id, reporters, tier, state, severity, location, claim, Evidence and a proof.Finding for the class,
//     the drop reason or the not-checked reason, and the blocking flag. The caller sets Blocking with gate.BlockingSet.
//   - Marker, ParseMarker: the first line of the comment. The adapter finds its comment by this line and the author
//     login. ParseMarker reads the first line only, and it is strict about the form.
//   - Reviewer, ReviewerOf, Evidence, EvidenceKind, Usage: small value types.
//
// # Wording
//
//   - Mode. Shadow mode says "would block because ..." everywhere and never "blocks". Enforce mode says "blocks" when the
//     proof check is required and says "blocking, advisory only" when it is not, as proof.Outcome.Conclusion does.
//     A mode that is not shadow reads as enforce, as package proof reads it, so a typo never sounds softer than the check.
//   - Unproven. A test can prove a bug is real and cannot prove a finding is false. The page says "unproven" and "not
//     shown" and "not checked". It never says "false" or "dropped". A test (TestWordsForUnproven) holds the words.
//   - Headline order: a proven blocking finding, a failed fast check, an unknown proof run, an unknown fast check,
//     proven findings that do not block, a trivial skip, and last "nothing proven against this change".
//
// # Invariants
//
//   - Pure and deterministic. The same Input gives the same page. Findings are sorted (blocking, severity, tier, id for
//     the proven list; reason then id for the unproven list), clauses are in the order of ACTION_MODE 5.1, reviewers are
//     in a fixed order. The order of the input does not matter.
//   - Every untrusted string is cleaned and escaped. Untrusted means every string in Input except the enums and the
//     numbers: paths, claims from models or bots, test names, rule ids, reviewer names, clause reasons, the change id.
//     clean puts the text on one line, removes control and format characters (bidirectional overrides, zero width
//     characters) and cuts it by rune. escape then turns "<", ">" and "&" into entities, puts a backslash before the
//     characters of Markdown, puts a zero width space after "@" and "://" and "www" and before a digit after "#", so the
//     text makes no HTML, image, link, mention, issue reference, heading or marker. Paths, test names and rule ids go
//     in code spans whose fence is one backtick longer than any run inside. Untrusted text is always placed in the
//     middle of a line after a prefix of ours, never at the start of a line.
//   - The marker is one hidden line at the start. The change id and the head go into it only when they match a strict
//     form, and "unknown" otherwise. Text that looks like a marker is escaped, so the page has exactly one. No decision
//     reads the marker (ACTION_MODE 2.7).
//   - Size. The page is at most MaxBytes, counted in bytes. The fast check clauses (at most 16 lines) are never
//     dropped for a list. Render starts with caps on each list (40 proven findings, 30 unproven findings, 12 reviewers,
//     12 lines of needs and of not checked) and halves every cap until the page fits. A list that is cut ends with
//     "and N more ... not listed to keep this comment short", so the count stays whole. A list item, a code span and
//     a details block are written whole or not at all. If nothing fits, the page is the marker, the headline and one
//     line.
//   - Fail closed. Only a proven finding with tier E1, E2 or E3 is shown as proven: a finding marked proven with tier E4
//     or none is shown as unproven ("no test or tool evidence"). A missing fast check result is unknown. A fast check
//     with an unmet clause fails even when its verdict says pass, and a clause with a status nobody defined is unknown.
//     A spend that cannot be true (negative, not a number, infinite) prints "unknown", never "$0.00".
//   - An id that is not "F" and digits (a ULID, an empty string) is printed as "F?". People never see a ULID.
//   - A trivial skip prints "$0.00 this run, no model call", whatever spend the caller passes.
//
// # Decisions
//
// The design leaves these open, so the strict choice was made.
//
//   - The numbers in the page come from Input.Findings, not from proof.Outcome.Counts, so a count and its list cannot
//     disagree. The outcome supplies the kind and the reason.
//   - A finding with several reporters counts once under each in the per-reviewer lines, because each tool's own noise is
//     what the line measures. The total in the summary line counts a finding once.
//   - Mentions in text are broken with a zero width space, not a backslash, because GitHub reads a mention from the
//     text after Markdown has been parsed, so a backslash does not stop it (`not verified` on github.com).
//   - Same-repository references that GitHub makes from text (a bare "GH-12" or a commit id) are not broken. They
//     link inside GitHub only, and they cannot leave it.
//   - The break-glass line (ACTION_MODE 5.4) is not here. Work package A8 adds its section when the command exists.
//
// # Failure modes
//
// Render never fails and never panics. A missing or impossible input shows as unknown or "not recorded" on the page. A
// long or hostile string is cut and escaped, a huge list is cut with a count, and a page that still does not fit
// becomes the short page.
//
// Not verified: how github.com renders the page. The escape rules follow the GitHub-flavoured Markdown spec, and the tests
// check the text, not a browser. That a zero width space stops a mention, an issue reference and an autolink on
// github.com is `not verified`. Work package A7 should post one hostile page to the fixture repository and look.
//
// # Tests
//
// golden_test.go renders seven realistic pages (cases_test.go): a clean pull request with nothing proven, one proven E1
// bug in enforce mode, shadow mode with a would-block, third-party noise only, an unknown run that hit its budget, a
// trivial skip at $0, and a proven finding that does not block with an unmet approval. Run
// go test ./internal/domain/summary -update to rewrite them and read the diff. inject_test.go feeds fifteen hostile
// strings (a mention, an image tag, a link with javascript:, a fake marker, a bare link, a fence that closes a code
// span, a closing details tag, entities, an issue reference, a table, math, bidirectional text) into every place an
// untrusted string can go, and checks that the page has one marker, no tag but its own, no mention, link, image or
// issue reference outside code spans, no new heading and the same number of lines. size_test.go holds the size limit
// for inputs from empty to 20,000 findings, the cut count and the check that a normal page is not cut.
// behavior_test.go holds the wording of shadow, enforce and advisory, the headline table, every stop reason, the
// fail-closed cases, the cost line, the order, the places and the marker. escape_test.go and short_internal_test.go
// hold the escape table, the code span fence and the short page.
//
// What this package does not do: post or find the comment, decide which findings block (gate.BlockingSet does that),
// prove or drop a finding (the proof run does that), or run any rule of the fast check.
package summary
