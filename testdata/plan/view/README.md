# View, Missing and status goldens (`testdata/plan/view/`)

M0 step 3 (`Fold`, `Missing`) and M5 step 4 (`Render`) reproduce these files byte for byte. The rules below are the
spec for each value. Every rule cites the document it comes from. Where the documents do not decide something, the
rule says "golden decision" and names the open question it settles for now. The owner may change any of those, and
then the goldens change with it.

Sources: `AGENT_INTERACTION_ARCH.md` F1 (facts and views), 4.1 (the board), H1, H7; `TECH_SPEC.md` 3.2 (event
catalog), 3.4 (invariants), 5.1 (workflow), 11 (finding states, blocking set), 12.4 (merge rule);
`internal/domain/workflow/definition.go` (`Default()`).

## Provenance

| Directory | Events | Where they come from |
|---|---|---|
| `kan-22/` | 58 | **Recorded.** The `event` table on the VM, exported Nov 16, 2026 (`~/.t3/raw-logs/KAN-22.jsonl`, not in the repo) |
| `kan-23/` | 45 | **Recorded**, same export (`KAN-23.jsonl`) |
| `merge-without-approval/` | 14 | **Authored** by hand. No recorded change merged without an approval. Its SHAs, ids, room and AC are made up |

For the recorded logs, `script.jsonl` keeps each event's `type`, `actor` (`actor_kind:actor_id`), `occurred_at` and
`payload` exactly as stored (Postgres JSONB key order). `internal/testkit/eventlog` then fills what it always
fills: event ids, `correlation_id`, `causation_id` (null), `blob_refs` (empty), `prev_hash` and `hash`. So those
envelope fields differ from the VM's. Version n is line n, as on the VM. The logs were scanned for secrets (none).
They hold real test identifiers: the Jira account id of the assignee, Switch room ids, `sw_` message refs and test
GitHub logins.

`policy.yaml` in each directory is `heimdall.yaml` of `aarush323/heimdall-fixture-pay` (public) at the change's
merge base: `4a0f699` for KAN-22, `9b49652` for KAN-23. `Missing(v, p)` takes it (TECH_SPEC 3.4.5: policy is read
at the merge base). `merge-without-approval/` uses KAN-22's.

The two Nov 7 sequences that stopped the live system are both inside the recorded KAN-22 log (`docs/PROGRESS.md`,
KAN-22 notes): the `task.assigned` events after the push (versions 40 to 45, "a push to a PR in review caused an
illegal `task.assigned` on replay") and the merged `pr.closed` after an approval that arrived during review
(versions 49 and 58, "refused on replay"). `kan-22/point-4` and `point-5` fold through both without error. The
earlier authored directories `nov7-merge-from-ready/` and `nov7-push-in-review/` were removed: they restated those
sequences with made-up events.

## Layout

```
<change>/change.txt        the change id, one line
<change>/script.jsonl      input: {at, actor, type, payload}, one per line
<change>/log.jsonl         change.Event envelopes built from the script (eventlog_test keeps them equal)
<change>/policy.yaml       the policy at the merge base
<change>/point-N/prefix.jsonl   the first lines of log.jsonl, whole lines
<change>/point-N/view.json      Fold(prefix)
<change>/point-N/missing.json   Missing(view, policy)
<change>/point-N/status.txt     Render(view, missing)
```

A single-point directory (`merge-without-approval/`) has `view.json`, `missing.json` and `status.txt` beside the
log and folds the whole log.

| Point | Prefix (versions) | What is true there |
|---|---|---|
| kan-22/point-1 | 1-11 | tests verified and committed; no PR |
| kan-22/point-2 | 1-17 | PR #10 open on `ee4539d`; slop failed on scope; CI green |
| kan-22/point-3 | 1-39 | pushed `19ba050`; three reviews on it; F1 upheld by a person, F2 filed; both E4 |
| kan-22/point-4 | 1-54 | pushed `59effae`; new tasks; approval on head; reviewer-1 and reviewer-3 reviewed it, reviewer-2 not yet; acceptance not reported |
| kan-22/point-5 | 1-58 | merged |
| kan-23/point-1 | 1-8 | tests submitted, not verified |
| kan-23/point-2 | 1-16 | PR #11 open; slop passed; CI green; no reviewers picked |
| kan-23/point-3 | 1-28 | reviewer-3 reviewed with F1 (E1) and F2, F3 (E4) |
| kan-23/point-4 | 1-39 | all three reviewed; eleven findings, five claim E1; none checked yet |
| kan-23/point-5 | 1-45 | F4, F5, F8, F9 dropped to E4; F1 verified; gate red on F1 and approval |

Cross-check: wherever the recorded log has a `gate.evaluated` on the same head as the point, its reasons name the
same unmet clauses as `missing.json` (kan-22 point-4, point-5; kan-23 point-2, point-5). That is a check on these
rules, not a source for them.

## Serialization

- `view.json`, `missing.json`: Go `encoding/json` with `SetEscapeHTML(false)` and `SetIndent("", "  ")`, which
  ends each document with one newline. Struct fields in the order given below. Maps are written by Go with sorted
  keys (so findings sort as `F1`, `F10`, `F11`, `F2` ...). A list is never `null`: an absent or `null` list in a
  payload folds to `[]`. Non-ASCII text (for example an em dash in a progress line) is written as is.
- `status.txt`: UTF-8, lines joined by `\n`, one `\n` at the end.
- SHAs in `view.json` are as the events carry them. Text in `missing.json` and `status.txt` shortens a SHA to its
  first 7 characters.

## View

The fold reads events in version order. A later event overwrites what an earlier one set. Nothing changes without an
event (F1: "folded from its events"). Unknown or out-of-order facts are folded, never refused (F1 rule 1).

```
View {
  trigger:   null | {ticket, repo, assignee, source, acceptance_criteria: [AC ids in payload order]}
  room:      "" | room_id
  branch:    null | {repo, branch, base_sha}
  plan:      null          (no plan event exists in TECH_SPEC 3.2 yet)
  tests:     null | Tests
  pr:        null | PR
  tasks:     {task_id: Task}
  reviews:   {reviewer: Review}
  findings:  {ref: Finding}
  decisions: [Decision in event order]
  approvals: [Approval in event order]
  closed:    null | {merged, head_sha}
  rejected:  null | {by, reason}
}
Tests    {sha, refs, ac_map, verified: null | {tree_hash, needs_signoff, run_ids}, rejected: [reasons],
          approved_by: "" | login, committed: null | {sha, tree_hash}}
PR       {number, author, ticket, head_sha, picked: null | {people, by, method},
          slop: null | {sha, pass, failing_rules}, ci: {sha: {check: conclusion}}, gate: null | {sha, ok, reasons}}
Task     {agent, role, deadline, status, progress}
Review   {head_sha, finding_refs, clean, pack_date}
Finding  {id, tier, severity, confidence, state, by, changed_by, checked, claim}
Decision {kind, target, login, reason, source_ref}
Approval {login, commit_sha}
```

Event by event:

| Event | Effect |
|---|---|
| `change.triggered` | sets `trigger` |
| `room.opened` | sets `room` |
| `branch.ensured` | sets `branch` |
| `tests.submitted` | replaces `tests` with a new submission: `verified` and `committed` null, `rejected` empty, `approved_by` "" (a new submission is what the later events are about) |
| `tests.verified` / `tests.committed` / `tests.rejected` / `tests.approved` | set `verified` / `committed` / `rejected` / `approved_by` on the current submission |
| `pr.opened` | sets `pr`; `picked`, `slop`, `gate` null; `ci` empty |
| `pr.pushed` | sets `pr.head_sha`. Nothing else is cleared: `slop`, `gate` and each review keep the SHA they were about, so a stale value is visible as stale |
| `slop.evaluated` | sets `pr.slop`, stamped with `pr.head_sha` at that moment (the payload carries no SHA) |
| `ci.reported` | sets `pr.ci[sha][check] = conclusion`, keyed by the payload's SHA. Every SHA ever reported is kept |
| `gate.evaluated` | sets `pr.gate` from the payload (`sha`, `ok`, `reasons`). `Missing` does not read it |
| `reviewers.picked` | sets `pr.picked` |
| `task.assigned` | adds `tasks[task_id]` with status `assigned`, progress "" |
| `task.acked` / `task.timed_out` / `task.failed` | sets that task's status to `acked` / `timed_out` / `failed` |
| `task.progress` | sets that task's `progress` to the text; the status does not change |
| `review.submitted` | sets `reviews[reviewer]`: `head_sha` is `pr.head_sha` at that moment, `clean` is "no finding refs" |
| `finding.filed` | adds `findings[ref]` from the payload; `by` is the filer; `changed_by` "", `checked` false |
| `finding.verified` / `finding.state_changed` | update `tier`, `severity`, `state`, `confidence` and `id` from any of them the payload carries; `changed_by` is the payload's `by`. `checked` becomes true on `finding.verified`, or on `finding.state_changed` from a `system` actor (the verifier) |
| `decision.recorded` | appends a decision |
| `approval.recorded` | appends an approval |
| `pr.closed` | sets `closed` |
| `change.rejected` | sets `rejected`: `by` is the event's actor id, `reason` the payload's |
| `decision.recorded` of kind `reject` | also sets `rejected`: `by` is the login, `reason` the decision's |

`by` follows one rule: it is always the filer. Who changed the finding last is `changed_by`. A task's status changes
only on a task event: no event joins a `review.submitted` or `tests.submitted` to a task id, so the fold does not
guess (open question Q3). That is why every task in these logs stays `acked`.

`approval.invalidated` removes the matching approval. Events these logs do not contain (`question.*`, `step.*`,
`ticket.changed`, `pr.linked`, `review.unavailable`, `review.waived`, `challenge.filed`, `case.*`, `verdict.*`,
`change.merged`, `finding.recheck_requested`) change nothing in the view yet. Their fold is M0's decision and needs a golden
before M3 or M5 read them.

## Missing

`Missing(v, p)` lists what the change still needs. F1 rule 3: a workflow row whose `after` rows are done, and
which is not done itself, is a need. Rows are visited in `Default()` order, then the gate's clauses. A row that is not
done but waits on an `after` row that is not done gives nothing.

```
Need {step, role, clause, subject, text}
```

`step` is the row id. `role` is the row's role in `Default()` (`test-author`, `reviewer`, `resolver`), else "".
`clause` is set only for the gate (below). `subject` is the reviewer, finding ref, check name or AC id the need is
about, else "". `text` is one of the sentences in the table, exactly.

When each row is done, and the need it gives when it is not:

| Row | Done when | Need text |
|---|---|---|
| `open_room` | `room` is set | `the change's room is opened` |
| `branch` | `branch` is set | `the ticket branch is created` |
| `write_tests` | `tests` is set | `the test author submits tests` |
| `test_qa` | `tests.verified` is set | `the sandbox verifies the tests` |
| `signoff` | `tests.verified` is set and either `needs_signoff` is false (the row's `when` does not hold, TECH_SPEC 3.2) or `approved_by` is set | `a person approves the tests (sensitive domain)` |
| `commit` | `tests.committed` is set | `the tests are committed to the branch` |
| `wait_pr` | `pr` is set | `a pull request for <ticket> is opened` |
| `slop` | `pr.slop` is about the current head and passed | `the slop rules pass on <head> (failing: <rules, comma separated>)` when it failed on head; `the slop rules are evaluated on <head>` when it has not run on head |
| `pick` | `pr.picked` is set | `reviewers are picked` |
| `review` | every picked person has a review whose `head_sha` is the current head | one need per such person, in pick order: `<reviewer> reviews <head>`, subject the reviewer |
| `verify` | every finding is `checked` or E4 (E4 has no verification, TECH_SPEC 11 tiers table) | one need per such finding, by ref number: `<ref> evidence is verified (<tier>)`, subject the ref |
| `mutation` | never listed (open question Q1) | none |
| `deliberate` | no case is open (golden decision: the row runs disputes, TECH_SPEC 11; these logs hold no `case.opened`) | none in these goldens |

`review` reads TECH_SPEC 12.4 `covered(r, head)` as "reviewed on head": no event says which files a review touched,
so a review on an earlier head does not cover the new one (open question Q4). The recorded gate agrees at kan-22
point-4.

The gate row (`on: any`, no `after`) is expanded into the clauses of TECH_SPEC 12.4 that the view and the policy can
decide. Golden decision: they are listed only once `pr` is set, because the gate evaluates a head and there is none
before (in all three recorded logs the first `gate.evaluated` follows `pr.opened`). In 12.4 order:

| Clause | Unmet when | Need text | Subject |
|---|---|---|---|
| `ticket` | `pr.ticket` is "" | `the pull request is linked to a ticket` | "" |
| `ac_covered` | an AC id of `trigger.acceptance_criteria` has no test in `tests.ac_map` (only when `tests` is set) | `a test covers <AC>` | the AC id |
| `ci` | for `heimdall/acceptance`, then each `ci_required` check of the policy: `pr.ci[head][check]` is not `success` | `<check> succeeds on <head>`, or `<check> succeeds on <head> (it reported <conclusion>)` when it reported something else | the check |
| `blocking` | a finding is in the blocking set: state in `open, disputed, escalated, upheld, fix_claimed, verifying`, tier from E1 to the policy's `blocking.min_tier`, severity at least `blocking.min_severity` (low < medium < high < critical). One need per finding, by ref number. An E4 finding never blocks (TECH_SPEC 3.4.3) | `<ref> is resolved (<tier> <severity>, <state>)` | the ref |
| `approval` | no approval has `commit_sha` equal to the head, a login other than `pr.author`, and a login that is the `github` of a person named in any `approvers` list of the policy | `an approver who is not the author approves <head>` | "" |

Clauses 12.4 has that the view cannot decide are not in `Missing`: the pinned tree hash on head, the test-directory
diff, `policy(merge_base) = policy(head)`, and consensus blocking (11.2b; findings are not merged by the fold). The
gate check summary carries those (open question Q5). Which domains the change touches is also not in any event, so
the approval clause accepts an approver of any domain (open question Q6). In these logs the answer is the same
either way.

A finding filed with state `open` at E1 is in the blocking set before it is verified (kan-23 point-3 and point-4).
That is what the recorded payloads say; TECH_SPEC 11 has a `proposed` state that these payloads never use (open
question Q7).

## Anomalies

`merge-without-approval/` folds and has a `missing.json` that still asks for an approval after the merge. It has no
`anomaly.json`. F1 rule 1 says a surprising fact "is reported as an anomaly by a pure check" and H1 asks that the two
Nov 7 sequences "fold to a view with an anomaly and no error". No document defines an anomaly's code, fields or
which facts are anomalous, and under F1 the Nov 7 merge (approval during review, then a merge with a green gate on
the approved head) is normal behaviour. So the anomaly goldens are **NOT READY** (open question Q2). M0 step 3's
"fold with an anomaly" half waits on the answer; the "no error" half is covered by kan-22.

## Open questions (for the owner)

1. **Q1 `mutation`.** The row is in `Default()` but marked Later in TECH_SPEC 5.1, and no event in 3.2 records a
   mutation run, so under F1 rule 3 it would be a need on every change forever. Goldens leave it out of every
   `missing.json`. Should `Missing` skip Later rows, or should a `mutation.evaluated` event be added?
2. **Q2 Anomalies.** What is an anomaly (code vocabulary, fields: event version or index, text), and which facts
   are anomalous under F1? Is "merged with no valid approval on head" the only one for now?
3. **Q3 Task completion.** `review.submitted` and `tests.submitted` carry no task id. Should they (so the fold can
   mark a task `submitted`), or is a task's status only what task events say?
4. **Q4 Review coverage.** Should `review.submitted` carry the SHA and the files reviewed, so `covered(r, head)` can
   be computed as 12.4 defines it?
5. **Q5 Gate clauses outside the view.** Should `Missing` read the last `gate.evaluated` on head for the clauses it
   cannot compute (tree hash, test-dir diff, policy change, consensus), or leave them to the check summary only?
6. **Q6 Changed domains.** Which event carries the changed paths, so the approval clause can use
   `approvers(domains(changed paths))`?
7. **Q7 `proposed`.** Recorded `finding.filed` payloads use `open` (E1 to E3) or `unverified` (E4), never
   `proposed`. Should an unverified E1 claim block before the verifier runs?

Owed with the owner: KAN-21 (27 events, closed unmerged and then rejected) is exported too and has no golden yet.
