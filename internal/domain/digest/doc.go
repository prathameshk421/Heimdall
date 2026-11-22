// Package digest computes the weekly shadow digest (roadmap A4) and the weekly misses and latency report (roadmap A12)
// of Action mode from the results Heimdall already recorded. It is a pure function from values to a Report and a
// Markdown page: no I/O, no clock, no GitHub call, no logging.
//
// # Purpose
//
// A team that runs the check in shadow mode needs two answers each week, and neither is a per-pull-request view:
//
//   - Would enforcing the check have blocked work? A4 answers it: how many pull requests would have passed, how many
//     would have blocked, why, what the check proved, and what it cost. The page says "would block", never "blocks",
//     because in shadow mode nothing was blocked.
//   - What did Heimdall not look at, what did it not know, and how long did it take? A12 answers it: every clause that
//     was not checked, every candidate that was not checked or was dropped, every proof run that ended unknown, and the
//     p50 and p95 wall time of both checks. Silence is never shown as safety.
//
// This package deliberately does not fold events, read a ref, or post anything. The caller folds each change (package
// change), extracts one PRRecord per pull request (also in the caller, so this package never depends on the shape of a
// change view), and writes the page.
//
// # API
//
//   - Compute(Input) Report: the totals. It never fails. A record it cannot believe is counted in Report.Malformed and
//     adds to nothing else, so one broken record cannot lower a pass rate or move a percentile.
//   - RenderMarkdown(Report) string: the page. It never fails and never returns more than MaxBytes.
//   - Input, PRRecord, FastRecord, ProofRecord, Counts: what the caller fills, one record per pull request.
//   - Report, ShadowDigest, Misses, Cause, CostStats, Latency, TextCount, ReasonCount: the result, with JSON tags, so
//     `heimdall action digest -format json` is the same numbers as the Markdown.
//   - Mode, ModeOf: shadow or enforce. An empty mode reads as shadow, which is the default of an action area that names
//     none; any other value this package does not define reads as enforce, so a typo never sounds softer than the check.
//   - CountsOf(proof.Counts) Counts: the conversion, so a caller does not copy the fields by hand.
//
// # Invariants
//
//   - Pure and deterministic. The same Input gives the same Report and the same page. Every list is sorted by count and
//     then by a fixed key, so the order of the input never shows. Money is whole micro dollars, because a recorded
//     payload carries no fractions.
//   - The means add up. Report.PRs is the records in the period. Outside, Untimed and Malformed count the records that
//     were read and left out, so no record silently disappears: PRs + Outside + Untimed + Malformed is the length of
//     Input.PRs.
//   - A record is malformed, not merely odd, when it cannot be true: a pull request number below 1, a negative cost, a
//     negative break-glass count, a negative wall time, more clauses than the fast check has, or a proof tally whose
//     parts do not add up (a stored tally is validated the same way). A malformed record is placed before its time is
//     read, because its time cannot be trusted either.
//   - A pull request is "checked" when either check reported on it. A pull request with no result at all is "not
//     checked": it is never counted as a pass.
//   - A would-block cause is a fast check clause that is unmet or unknown, or a proof run that blocked or ended
//     unknown. A clause or a run whose status this package does not define is an unknown cause, never a pass. One pull
//     request can have several causes, so the cause counts are at least the would-block count, never less.
//   - A pass rate is WouldPass out of Checked. An empty Checked is 0 basis points, not 100%: nobody checked, so nothing
//     passed.
//   - Percentiles are nearest rank on the sorted measurements: p50 of one value is that value, p50 of two is the lower
//     one, and p95 of twenty is the twentieth. An empty set has 0, and the report says Measured 0 so a reader can tell
//     "no measurement" from "measured zero".
//   - A wall time of 0 milliseconds means not measured. No real run is under a millisecond. A reader of the state ref
//     has the proof wall time (the time from proof.started to proof.reported of one run) and not the fast check's,
//     because no event records when a fast check started. The page then says "not measured", never a zero.
//   - Every untrusted string is cleaned and escaped. Untrusted means the repository name, a clause id, a reason and any
//     text of a reason this package does not define. clean puts the text on one line and removes control and format
//     characters; escape makes no HTML, image, link, mention, issue reference, heading or table cell. A label goes in a
//     code span whose fence is one backtick longer than any run inside. A reason of package proof is shown in words this
//     package holds, never as the raw value of the record.
//   - Bounded output. Each list is capped, every label has a rune limit, and RenderMarkdown cuts the page at MaxBytes on
//     a rune boundary. A cut list says how many entries it left out, so the counts stay whole.
//
// # Failure modes
//
// Compute and RenderMarkdown do not fail. An empty period renders a short page that says nothing was checked, and it
// never claims readiness. A record whose reason is not one of package proof is shown as text and marked Known false in
// the JSON, so a consumer can tell it from a real reason. A proof run of kind unknown that names no reason counts as
// "no result", as package proof reads a run that reported nothing usable.
//
// # Tests
//
// compute_test.go is the table: an empty period, a mixed period with would-blocks and misses, the mode split, the
// malformed and out-of-period records, and the percentile edges (0, 1, 2 and 20 measurements). render_test.go holds the
// golden pages under testdata, an escaping case with a hostile repository name and reason, a determinism check and the
// size bound. The CLI side is cmd/heimdall/action_digest_test.go, which reads a local JSON snapshot in both formats.
//
// What this package does not do: read events or the state ref, build the snapshot, post the digest anywhere, or decide
// a merge. It only adds numbers up and writes them down.
package digest
