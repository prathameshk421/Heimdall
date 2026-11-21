// Package external turns the review comments that third-party tools leave on a pull request into candidate findings
// (ACTION_MODE 6.1 and 6.2).
//
// # Purpose
//
// The proof run may prove or drop a claim made by CodeRabbit, GitHub Copilot code review, Cursor Bugbot or Greptile.
// This package does the step before that: it reads one comment as the GitHub API gives it, decides whether it is one of
// these tools, cleans the text into a claim, reads the severity the tool states, and puts the lines on the head commit.
// It does no I/O, reads no clock, logs nothing and has no GitHub client. The caller fetches the comments and the diffs
// and passes them in. This package does not prove, refute, merge duplicates with our findings or reply in the threads.
//
// # API
//
//   - Comment: one comment as a plain struct (body, path, line, start_line, original_line, side, commit_id, position,
//     outdated flag, in_reply_to_id, user login and type). A null from the API is 0.
//   - PR{Head, Sources, Diffs}: what is known about the PR. Sources is action.sources at the merge base. Diffs maps an
//     older commit to the diff from it to head, as []FileDiff of Hunk. ParsePatch reads the "patch" text GitHub gives
//     for one file into hunks.
//   - PR.Ingest(Comment) Result: one comment. The outcome is a Candidate, a Drop with a proof.DropReason and a Cause, or
//     Ignored with an IgnoreReason. It never fails.
//   - PR.IngestAll([]Comment) Batch: many comments, with the Counts the summary prints ("3 outdated", "2 no location").
//
// # Tools and how each is recognised
//
// A comment is read only when all of these hold, in this order: user.type is Bot; the login is one of the logins below,
// compared without case and without the [bot] suffix; the login is in PR.Sources; the comment is not a reply
// (in_reply_to_id is 0); and the body carries a mark of the tool. Any other author is ignored with a typed reason and is
// not an error.
//
// The formats below were checked against 411 real comments from pingdotgg/t3code (testdata/real, and the corpus test;
// docs/product/research/13-t3code-bot-comments.md has the counts). Where the documentation and the data disagreed, the
// data won.
//
//   - CodeRabbit (coderabbitai): either header, the word CodeRabbit, or a fingerprinting comment. The older header is
//     "_category_ | _severity_"; from November 2026 it is "**<picture>…</picture> Minor** · **Category** · **Quick
//     win**", and the whole line is removed from the claim. Severity: Critical is critical, Major is high, Minor is
//     medium, Trivial and Nitpick are low. A Nitpick is style.
//   - GitHub Copilot code review (copilot-pull-request-reviewer, Copilot): the login and the Bot type only. The real
//     login on a public PR is "Copilot"; either login in PR.Sources admits either. Copilot writes plain prose with no
//     fixed mark and states no severity.
//   - Cursor Bugbot (cursor): BUGBOT_BUG_ID, the DESCRIPTION START comment, or a "**High Severity**" line. Severity
//     from that line: Critical, High, Medium, Low.
//   - Greptile (greptile-apps): the login and the Bot type. Every Greptile comment in the data is plain prose, so the
//     "**logic:**" prefix is still read when it is there, but it is not required. Greptile states a category, not a
//     severity. The style category is style.
//   - Macroscope (macroscopeapp): the login and the Bot type; it writes plain prose, a path and a line. A severity when
//     the body opens with an emoji and a word ("🔴 Critical", "🟠 High", "🟡 Medium", "🟢 Low"). The claim loses the
//     severity line, the repeated "path:line", a GitHub blob link and the footer ("Addressed in <sha>", "No longer
//     relevant as of <sha>"); the severity is kept.
//   - OpenAI Codex connector (chatgpt-codex-connector): the "![P2 Badge]" image, or the "Useful? React with" footer.
//     The badge is read from the raw body, because it sits inside a <sub> that the claim clean-up removes: P0 is
//     critical, P1 high, P2 medium, P3 low.
//   - Devin (devin-ai-integration): the "<!-- devin-review-comment {json} -->" payload or the review badge. The emoji
//     that opens the finding is the severity: 🔴 critical, 🟠 high, 🟡 medium, 🟢 low.
//
// A tool that states no severity gets medium, and Candidate.SeverityStated is false.
//
// # Claim
//
// The claim keeps the text a person reads. The parser removes HTML comments, <details> blocks (nested ones too),
// fenced suggestion blocks, <sub> footers, images and badge links and a short list of inline HTML tags. A comment, a
// block or a fence that never closes takes the rest of the text. The claim is one line, at most 600 runes (the FindingIn
// limit), cut with a "…" mark. A claim under 10 runes is not a claim: the comment is ignored as no_claim.
//
// A tool's own noise never opens a claim: the CodeRabbit header, the Macroscope severity line, path:line and blob URL,
// the Codex badge and the Devin emoji are all removed after they are read, and so are the footers that ask for feedback
// or confirm a fix. A body that is a tool saying it could not read the request ("Sorry, I'm unable to act on this
// request…") is not a claim: it is ignored as no_claim, because it says nothing about the diff.
//
// # Location
//
//   - The coordinates are line and start_line on commit_id. When line is null, or the caller says the comment is
//     outdated, they are original_line and original_start_line on original_commit_id.
//   - Such a comment is not dropped. Candidate.Outdated is true, Candidate.CommitID names the commit the lines are on,
//     and the lines are the original pair: 145 of the 411 real comments are like this, and a proof run that needs a
//     place at head can see that it has none instead of losing the finding.
//   - A comment on head needs no diff. A comment on an older commit is mapped through PR.Diffs for that commit, by its
//     old path. A rename gives the new path. A line moved by an insert or a delete above it gets its new number. A range
//     keeps the first and the last line that survive.
//   - A comment on the LEFT side points into the base file, which head does not change. It is not mapped.
//   - Dropped as outdated: line_deleted (no line of the range is left), file_deleted, no_diff (no diff was passed for
//     that commit) and bad_diff (hunks overlap, are out of order, hold a wrong op or more than MaxDiffOps lines).
//   - Dropped as no location: summary (a review body or an issue comment), no_path (empty, absolute or unclean path),
//     no_line (a comment on the file, or a line that cannot be true), range_too_large and no_commit.
//
// # Bounds
//
//   - MaxBodyBytes is 65536: a longer body is cut before it is read, at a rune boundary.
//   - MaxComments is 1000: IngestAll reads no more, and counts the rest as Unread.
//   - MaxCandidates is 200: IngestAll keeps the most severe (style last, then severity, then comment id) and counts the
//     rest as Overflow. A comment id is read once, and a repeat is counted as a duplicate.
//   - MaxRangeLines is 500, MaxDiffOps is 200000 lines per file and MaxPatchBytes is 1 MiB for ParsePatch.
//
// # Invariants
//
//   - Pure and deterministic: the same comment and PR give the same Result. Nothing panics on any body or diff.
//   - A Candidate always has a clean relative path, a line of at least 1, a valid severity and a claim of 10 to 600 runes.
//   - Every recognised comment that gives no candidate has a DropReason and a Cause, so the summary can say "dropped:
//     outdated" or "dropped: no location". Only the four proof drop reasons are used here.
//   - An empty Sources list means that no author counts. The package never guesses a default.
//
// # Failure modes
//
// Nothing here returns an error except ParsePatch, which returns ErrBadPatch. A format change by a tool shows as
// IgnoreNoMarker or a poor claim, never as a crash. A hunk list the caller got wrong shows as bad_diff, never as a
// wrong line.
//
// # Tests
//
// The goldens in testdata/<tool>/ are hand made from the formats that the tools document and show in public.
// testdata/real/bodies/<tool>/ holds ten comments captured from a public PR (pingdotgg/t3code), with the severity, the
// anchor and the words the claim must keep and lose: golden_test.go reads both. corpus_test.go feeds every line of the
// real 411-comment corpus (testdata/corpus, from the same public PRs) and asserts that no line panics and that at least
// 90% of the top-level comments become candidates; it logs the count per bot. recognise_test.go holds the login and
// marker table, the ignored authors and the claim clean-up. location_test.go holds the diff mapping (a line moved, a
// line deleted, a file renamed, ranges, a deleted file, a missing diff, bad hunks and the outdated anchor) and the
// no-location drops. batch_test.go holds the counts and the bounds. patch_test.go holds ParsePatch. fuzz_test.go holds
// FuzzIngestBody and FuzzParsePatch; run with go test -run=^$ -fuzz=FuzzIngestBody -fuzztime=30s.
package external
