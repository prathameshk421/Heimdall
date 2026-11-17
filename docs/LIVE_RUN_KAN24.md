# Live run KAN-24, Nov 16, 2026: what happened, why, and what to change

A real end-to-end run on the VM with the overhaul build (`heimdall-build` 05c6a29, hot-installed 16:11 UTC):
a Jira ticket, In Progress, the test author, the developer's PR, three reviewers, the gate, an approval, the merge.
Every number below is read from the VM: the `event` table, the new `tool_call` record and the server log.

Ticket: **KAN-24 "Look up a payment by id"**, three criteria (own payment 200; another merchant's 404 like unknown;
unknown 404 with a JSON error). Repo `aarush323/heimdall-fixture-pay`, base `heimdall-test/demo-base`.

## Timeline

| UTC | What | Source |
|---|---|---|
| 16:12:21 | my policy PR #12 (`agent:` to `seat:`) opens; Heimdall makes it an unlinked stream; slop and gate red | event |
| 16:16:21 | I merge #12 (no branch protection); Heimdall announces the merge to the feeds | event, log |
| 16:14 | KAN-24 created, assigned, moved to In Progress | Jira |
| 16:15:19 | `change.triggered` (Jira poll) | event |
| 16:15:20 | `room.opened` | event |
| 16:15:23 | `branch.ensured`, `task.assigned` to test-author | event |
| 16:15:53 | first tool call: `get_task` (30 s after the wake) | tool_call |
| 16:16:33 | `ack` (40 s after `get_task`) | tool_call |
| 16:16:33 to 16:19:28 | 22 reads: 4 `read_untrusted`, 18 `read_code` (2 refused) | tool_call |
| 16:20:32 | first `report_progress` (4 min after ack) | tool_call |
| 16:21:32 | `submit_tests`: one file, AC1 to AC3 mapped | event |

## Findings, root causes and fixes

### F1. The test author spends most of its time finding things the server already knows

**Seen.** 6 minutes from wake to submission. Each server call took about 0.3 s (`read_code` reads GitHub) or a few
ms; the rest is model turns. The agent needed about 9 round trips before it could start writing:

- 4 `read_untrusted` calls, one per blob: the ticket description and each criterion.
- `read_code "."` refused ("Name a file or folder"), then guesses: `go.mod`, `README.md`, `api/openapi.yaml`, `app`
  (refused: does not exist), `cmd/pay`, `internal`, then each of `internal/app`, `internal/api`,
  `internal/payments`, then 8 files.

**Root causes.**
1. `get_task` gives references, not content. `TaskEnvelope.Inputs.AcceptanceCriteria` holds `{id, ref}` and the
   description is an untrusted ref. The criteria are the whole job, yet each costs a round trip.
2. There is no map of the repository. `read_code` refuses the root, and nothing in the brief lists the tree, so the
   agent explores directory by directory.
3. `read_code` takes one path per call. The agent did batch (2 to 4 parallel calls per turn), but each batch is a
   model turn.
4. The seam (`app.New(app.Config)`) is in the profile (`seam:`), but the brief does not say so; the agent rediscovered
   it from `internal/app/app_test.go` (its own progress line says so).
5. The refusal for `"."` is wrong for this role: it says "The diff from heimdall_read_untrusted names every changed
   file". A test author works before any PR; there is no diff.

**Fixes (simple, in order of value).**
- `get_task` returns the ticket description and every criterion's text inline, inside the untrusted wrapper. Zero
  extra calls for the job itself.
- `get_task` returns a bounded repo map at the base SHA (paths only, test and vendor folders folded, about 200
  lines) and the profile's seam and tests folder.
- `read_code` accepts a list of paths (bounded total bytes) and accepts `"."` as "list the top level".
- Refusal texts per kind: a test author is told about the repo map, a reviewer about the diff.

Expected effect: ticket and layout known after `get_task`; one or two read turns for the files that matter.

### F2. Messages people saw did not match the work

**Seen.** Telegram showed "merged" and mail showed a failed check while KAN-24's agents had not finished.

**Root cause.** Both came from my policy PR #12, not from KAN-24. Heimdall treats every PR in a watched repository
as work: a PR with no ticket becomes an "unlinked" stream (D146), fails slop and the gate (correctly), and then its
merge is announced to the feeds like any change's merge. The GitHub check `heimdall/gate` is red on it, which is what
GitHub mails.

**Fixes.**
- The board announces nothing for an unlinked stream except its one line "this PR names no ticket"; a merge of a PR
  the gate held red is reported as "merged without passing the gate" (an anomaly, F1 rule 1), never as a normal merge.
- One status message per change (H7) instead of a stream of milestone lines; then a feed shows the state of each
  change and an unrelated PR cannot read like progress on a ticket.
- Policy changes to the demo base go through a ticket like any change, or the base branch is protected.

### F3. A policy rename broke reading the policy at old bases

**Seen.** `policy unavailable for card proposals ... field agent not found in type policy.Person` for three refs; the
slop rule on PR #12 said `policy_invalid`.

**Root cause.** The policy is read at the merge base and decoded strictly. Renaming `people.<p>.agent` to `seat`
(M0 step 7) makes every older base unreadable. The card-proposal job reads several refs (main and two old branches),
so it is now off for them, quietly.

**Fix.** A policy version: `version: 2` means `seat`. The decoder reads version 1 by its own struct and converts it
(one function, tested), so old bases stay readable and nothing guesses. A strict failure stays a failure for an
unknown version.

### F4. My own record redacts too much

**Seen.** The `tool_call` record shows `"sha":"[redacted]"` and test names as `[redacted]`.

**Root cause.** The redaction pattern treats any 40-character run of letters, digits and `_` as a possible key, which
matches every commit SHA and long Go test names.

**Fix.** Redact only known credential shapes (forge, chat and model tokens, private keys) and high-entropy strings
that are not hex SHAs; test with real SHAs and test names as must-keep cases.

### F5. Test quality: good, and the verification was quick

**Seen.** `tests.verified` 16:23:17 (red first, three runs, 1 min 45 s after submission), `tests.committed`
16:23:23 as `6c4380a`. One file, 142 lines, `tests/heimdall/KAN-24/get_payment_test.go`.

**What is right.**
- One test per criterion, tagged `heimdall:AC<n>`, all through the app's own seam (`app.New(app.Config)`) over
  HTTP, with a fixed clock. Black-box: nothing reaches into the store.
- AC2 is tested the strong way: the other merchant's 404 must be byte-identical to the unknown-id 404, and must not
  contain the payment id. A weak test would only check the status code.
- AC1 creates a decoy payment first, so a handler that returns "the latest payment" fails.
- It writes its assumptions down at the top ("ticket silent: lookup is scoped by X-Merchant-ID ...") and says it found
  no injection in the ticket text.
- My implementation, written before reading the tests, passed all three, and the earlier KAN-22 tests still pass.

**What is missing or weak.**
- No test for a request with no `X-Merchant-ID` (the ticket is silent; the existing route answers 401). This is a
  gap the test plan step (TA2, "every criterion has a behaviour and at least one negative case") would have made the
  author decide on, and the survivor loop (TA4) would surface. Neither is wired yet.
- AC1 compares id, merchant, amount and currency, not `created_at`, which the API also returns.
- No property or boundary case (for example an id with a slash or a very long id). Fine for this ticket.

### F6. Reviewers were never picked: the slop rule could not pass (the flow stopped silently)

**Seen.** PR #13 opened 16:25:34; `slop.evaluated {pass: false, failing_rules: [scope]}`; CI green at 16:26; the gate
red on "slop_passed: scope" and "the review requests are not known". No reviewer was ever picked; KAN-24 stopped at 15
events. KAN-22's first PR (Nov 7) failed the same way.

**Root cause.** Intake built the slop facts with `ExpectedPaths` nil ("the test author's submission is not loadable
through the ports yet"). The test author does send `expected_paths` with `submit_tests`, but nothing stored them, so
every PR that touches product code was "outside the expected set". With F1 rule 3 the `pick` row waits on `slop`, so
the change sat with no reviewers and nothing said why except the gate check.

**Fix (done, `7769242`, deployed).** `tests.submitted` carries `expected_paths`, the fold keeps them on the current
submission, intake passes them to the slop rule. Test: a PR inside them passes scope, one outside fails.

**What it says about the design.** A silently impossible rule is the worst kind of state bug: every part was "correct"
and the flow still could not move. Two guards follow: (1) `Missing` already names "the slop rules pass on <head>
(failing: scope)"; the board must post that sentence in the change's thread the moment it is true, so a person sees
the stall in seconds, not minutes; (2) every rule needs one live case that passes, not only cases that fail.

## Fixed and deployed during the run (16:35 UTC build `aec93304`)

| Finding | Commit |
|---|---|
| F6 slop scope reads expected paths | `7769242` |
| F1 ticket text and repo map in `get_task`; `read_code "."` and several paths; refusals per role | `4b71443` |
| F4 record keeps SHAs and test names, still hides credentials | `4b71443` |
| F2 a merge over a red gate is announced as such | `4b71443` |
| Policy: `rekdfr` (tester) is an approver at the demo base, for the run | fixture `366f529` |

Not fixed yet: F3 (a policy version and conversion), the board posting `Missing` in the thread (H7), and the live
status message (needs the Switch edit operation).

KAN-24 cannot go on: its submission was recorded before the fix, so it has no expected paths, and a Justification
section written only to get past the bug would be a workaround. A fresh ticket, **KAN-25 "List a merchant's
payments"**, runs the whole flow on the fixed build.

## KAN-25 (fixed build)

| UTC | What |
|---|---|
| Nov 16 17:16:54 | triggered; room, branch (base `366f529`, rekdfr is an approver), task |
| 17:17:31 | `get_task` (7.2 KB: ticket text and repo map included) |
| 17:18:22 | ack; then `report_progress` 8 s later |
| 17:18:30, 17:19:22 | **2 batched `read_code` calls, 13 files** (KAN-24: 18 single reads, 2 refused) and one `ask_knowledge` |
| 17:22:48 | first submission: two files, AC1 to AC3, `expected_paths` stored (F6 fix works) |
| 17:24:33 | **rejected**: "list_payments_scope_test.go never imports the product" (F7) |
| 17:32:19 | resubmission: the author merged both files into one to get past F7 |
| 17:32:20 | the verification effect "finishes" in 1 s with **no verdict** (F8); nothing happens for 12 h |
| Nov 17 06:01 | F7 and F8 fixed and deployed; the stuck effect replayed once by hand |

### F7. A correct test package was refused: a Go check judged files, Go judges packages

**Seen.** Two test files in one package; the second used the first's helpers. The placeholder check
(`effects.placeholderReasons`) parses each file's imports and refused the second as "never imports the product".
The author lost a full round (about 9 minutes) and then restructured good tests to satisfy a wrong rule.

**Root cause.** A per-file rule for a per-package language fact, and no test of the check at all. It is also
Go-only code in the server, which the plan says moves into a language-neutral test lint (M4 step 5).

**Fix (done, `df6cc48`).** Judge each test directory: refused only when it has imports and none leaves the standard
library. First test of the check. The move into the checks stays owed.

### F8. A resubmission after a rejection was never verified, and nothing said so

**Seen.** Outbox row 188 (`test_quality`) done at 17:32:20, one second after the resubmission, with no event and no
log line. KAN-25 waited 12 hours.

**Root cause.** The executor's idempotency question was "has this change ever had a verdict?" instead of "does the
current submission have its verdict?". After any rejection, every resubmission was skipped forever. The verdict was
also built as caused by the first submission. The view already answers the right question; the executor did not use
it. And nothing watches for "submitted, no verdict, no effect pending", so the stall was silent.

**Fix (done, `2284011`).** Skip only when the latest submission is judged; cause the verdict by it. Test: a
resubmission after a rejection is judged. Still owed: a reconcile pass (at boot and every few minutes) that re-drives
any submission with no verdict and no pending effect, and a log line whenever an executor skips.

**What F7 and F8 say together.** Both bugs lived in the rejection path, which no live run had exercised before and
no test covered end to end. The happy path was tested; the "author retries" path was not. Each rule and each
executor needs its retry case.
