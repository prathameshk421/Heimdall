# Demo run sheet

What to show, in order, on the real system. Timings are from the live runs on Nov 7. Update them after the final run.

## Before you press record

Open these tabs:

- Jira board, project KAN
- Discord: `heimdall-tests` and `heimdall-reviews`
- GitHub: the pull requests of `aarush323/heimdall-fixture-pay`

Check that `https://github.com/aarush323/heimdall-fixture-pay/pulls` has no leftover test pull request open.

## The story

| # | What happens | Where to look | What to say | Time |
|---|---|---|---|---|
| 1 | A ticket moves to In Progress: "Add refunds to the pay API", with its money rules and acceptance criteria | Jira | "A normal ticket. Nobody touches Heimdall." | 30 s until the next step |
| 2 | Heimdall opens a working room, creates the branch and hands the ticket to the Test Agent | Discord tests channel | "Tests get written from the ticket, before any code exists." | 20 s |
| 3 | The agent submits tests. Heimdall runs them in a sandbox, checks they fail for the right reason, and commits them | Discord tests channel, then the branch on GitHub | "The agent cannot just write tests that pass." | 5 to 10 min, cut this |
| 4 | A developer opens a pull request that passes every acceptance test but has two bugs | GitHub pull request | "Tests are green. Would you merge it?" | 1 min for acceptance |
| 5 | Three AI reviewers start, each on its own | Discord reviews channel | "They do not see each other's work." | 1 min after the PR |
| 6 | Reviewers report the bugs, each with a failing test | Discord card, then the Files tab on GitHub | "Not an opinion. A test that shows the bug." | 3 to 5 min, cut this |
| 7 | Heimdall runs each test itself and confirms it | "Bug confirmed" card, Heimdall Review comment | "Heimdall checks the reviewer's proof too." | 1 to 3 min |
| 8 | The merge stays blocked | `heimdall/gate` check, Heimdall Review comment | "Merge is impossible until this is fixed." | now |

## The two bugs in the pull request

Both pass every acceptance test. The ticket's money rules forbid both.

1. **Another merchant can refund your payment.** The handler checks that the `X-Merchant-ID` header is present, but never that the payment belongs to that merchant.
2. **Refunds can add up to more than the charge.** Each refund is compared to the payment amount alone, so two refunds of 60 on a payment of 100 both succeed.

## What each screen should show

**GitHub, Conversation tab:** one comment titled "Heimdall Review" that updates itself: verdict banner, pipeline diagram, merge checklist, findings table, one section per finding with its failing test, why it cannot merge, what happens next.

**GitHub, Files tab:** each finding as a comment on its line.

**GitHub, Checks:** `heimdall/acceptance` green and `heimdall/gate` red.

**Discord:** one card per milestone: pull request opened, tests passed, review started, review result with each finding spelled out, bug confirmed, merge blocked.

## If something goes wrong

- **A reviewer has not started after 2 minutes:** Heimdall mentions it again on its own. A reviewer that does not answer before the deadline is released, and the others still count.
- **Do not restart the server during a take.** A restart repeats running sandbox jobs.
