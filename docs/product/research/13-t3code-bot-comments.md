# 13. Real bot review comments from pingdotgg/t3code

Status: dataset and analysis, Nov 21 2026. All GitHub data came from read-only `gh api` GET calls. No comment, review,
reaction, fork or push was made. The Go package was run on the comment bodies by a temporary program, then the program
was deleted. No Go code changed.

Files:

- `testdata/bench/t3code-comments.jsonl`: 411 inline bot comments, one JSON object per line.
- `testdata/bench/t3code-prs.yaml`: the 30 merged PRs they came from.

## 1. Method

I took 30 merged PRs from 2026, spread from February to November, and not the newest 30. The aim was to cover every
bot login that posts inline findings on this repo and both halves of the year. The repo is TypeScript and Rust.

For each PR I read four things with GET calls:

1. `GET /repos/pingdotgg/t3code/pulls/{n}` for the merge date, file count and commit count.
2. `GET /repos/pingdotgg/t3code/pulls/{n}/comments` for every inline comment.
3. `GET /repos/pingdotgg/t3code/pulls/{n}/commits` and `GET /repos/pingdotgg/t3code/commits/{sha}` for the patch of
   each later commit.
4. A GraphQL `reviewThreads` query for `isResolved` on each thread.

A comment is a bot comment when `user.type` is `Bot`. Replies are in the file and marked `is_reply: true`. The per-bot
counts below use top-level comments only, because a reply is not a finding: 383 top-level and 28 replies.

The label rule:

- `acted`: a later commit in the same PR added or removed a line inside the commented range.
- `dismissed`: the thread is resolved or has a reply, and no later commit changed those lines.
- `unknown`: neither.

Two details decide most of the labels. GitHub moves a comment forward when its line survives, so `commit_id` is often
the newest commit, not the one the reviewer saw. The stable anchor is `original_commit_id` plus `original_line`. I map
the range through each later commit's hunks and shift it by the line delta of every hunk above it. When the original
commit was rebased away, 78 comments (19%), I use the commits committed after `created_at`. When the bot itself writes
`Addressed in <sha>` and that sha is a later commit, I accept it: 11 comments.

Limits. A line label is a fact about the diff, not proof that the bug was real. A fix on a neighbouring line can be
missed. 145 of 411 comments are outdated at the API level (`line: null`), so the original pair is the only anchor. The
location mapping in `external` was not exercised, because no diffs were passed: every comment was placed on its own
commit with `Outdated` false.

## 2. The bots

The wider scan over 11 months of merged PRs found eight bot logins with inline comments. All eight appear in the 30
PRs.

| Login | Comments | Acted | Dismissed | Unknown | Acted % | Body format |
|---|---|---|---|---|---|---|
| `macroscopeapp[bot]` | 175 | 75 | 58 | 42 | 43% | Optional emoji and word (`🔴 Critical`, `🟠 High`, `🟡 Medium`, `🟢 Low`), then `` `path:line` `` and prose. Footer `Addressed in <sha>` or `No longer relevant as of <sha>`. Some bodies start with `<!-- MURMUR_IGNORE -->`. |
| `cursor[bot]` | 135 | 32 | 95 | 8 | 24% | `### Title`, then `**Medium Severity**`, then `<!-- DESCRIPTION START -->` and prose. |
| `coderabbitai[bot]` | 60 | 29 | 7 | 24 | 48% | Two headers. Feb to Oct: `_⚠️ Potential issue_ \| _🟡 Minor_`. Nov: `**<picture><img ...></picture> Minor** · **Functional Correctness** · **Quick win**`. Footer `<!-- fingerprinting:phantom:... -->`. |
| `greptile-apps[bot]` | 5 | 3 | 1 | 1 | 60% | Plain prose. No title, no severity, no marker. |
| `chatgpt-codex-connector[bot]` | 5 | 1 | 2 | 2 | 20% | Bold title with a shield badge `![P2 Badge](...)` inside `<sub><sub>`, then prose. |
| `devin-ai-integration[bot]` | 2 | 0 | 0 | 2 | 0% | `<!-- devin-review-comment {json} -->`, then an emoji (`🔴`, `🟡`) and a bold title. |
| `Copilot` | 1 | 0 | 1 | 0 | 0% | Plain prose. Login is `Copilot`, not `copilot-pull-request-reviewer[bot]`. |
| `t3-code-bot[bot]` | 0 | 0 | 0 | 0 | n/a | Replies only (`agreed. restored in <sha>`). Four replies, no top-level claim. |

Total: 383 top-level comments. 140 acted (37%), 164 dismissed (43%), 79 unknown (21%).

Two numbers stand out. Among the bots with more than 10 comments, cursor has the lowest acted rate (24%) and the highest dismissed rate (70%). 287 of 383
threads (75%) are resolved, so the team resolves bot threads in bulk. A resolved thread is not a fix, and the two
labels must not be merged. CodeRabbit has the highest acted rate of the large bots (48%) and the most unknowns (24 of 60), because many
of its comments land on the last commit and stay open.

## 3. Comments per PR

30 PRs, 383 top-level comments. The minimum is 2, the median is 10, and the maximum is 30.

| PR | Merged | Comments | Acted | Bots |
|---|---|---|---|---|
| 3 | 2026-02 | 25 | 4 | coderabbit 10, greptile 1, macroscope 14 |
| 4 | 2026-02 | 7 | 2 | coderabbit 7 |
| 10 | 2026-02 | 19 | 2 | coderabbit 6, devin 2, macroscope 11 |
| 13 | 2026-02 | 30 | 19 | coderabbit 6, greptile 1, macroscope 23 |
| 14 | 2026-02 | 22 | 10 | coderabbit 5, greptile 1, macroscope 16 |
| 23 | 2026-02 | 6 | 4 | coderabbit 4, greptile 2 |
| 128 | 2026-03 | 23 | 1 | cursor 13, macroscope 10 |
| 144 | 2026-03 | 23 | 9 | cursor 17, macroscope 6 |
| 1619 | 2026-04 | 9 | 3 | cursor 8, macroscope 1 |
| 2463 | 2026-05 | 16 | 1 | cursor 10, macroscope 6 |
| 2473 | 2026-05 | 17 | 9 | cursor 13, macroscope 4 |
| 2899 | 2026-06 | 15 | 8 | cursor 9, macroscope 6 |
| 2905 | 2026-06 | 25 | 0 | cursor 19, macroscope 6 |
| 3638 | 2026-07 | 28 | 16 | cursor 10, macroscope 18 |
| 3640 | 2026-07 | 22 | 2 | cursor 14, macroscope 8 |
| 3649 | 2026-08 | 3 | 1 | copilot 1, cursor 1, macroscope 1 |
| 3687 | 2026-07 | 13 | 5 | cursor 3, macroscope 10 |
| 3720 | 2026-07 | 3 | 3 | cursor 1, macroscope 2 |
| 5169 | 2026-08 | 5 | 0 | codex 2, cursor 3 |
| 5176 | 2026-08 | 5 | 1 | codex 2, cursor 2, macroscope 1 |
| 5240 | 2026-08 | 7 | 2 | cursor 5, macroscope 2 |
| 5260 | 2026-08 | 10 | 7 | cursor 4, macroscope 6 |
| 8994 | 2026-09 | 10 | 7 | cursor 2, macroscope 8 |
| 9006 | 2026-09 | 2 | 2 | codex 1, cursor 1 |
| 9008 | 2026-09 | 4 | 0 | macroscope 4 |
| 14521 | 2026-10 | 5 | 3 | macroscope 5 |
| 14527 | 2026-10 | 7 | 4 | coderabbit 2, macroscope 5 |
| 28460 | 2026-11 | 10 | 8 | coderabbit 8, macroscope 2 |
| 28487 | 2026-11 | 8 | 5 | coderabbit 8 |
| 40176 | 2026-11 | 4 | 2 | coderabbit 4 |

Distribution: 8 PRs have 5 comments or fewer, 9 have 6 to 10, 5 have 11 to 20, and 8 have 21 or more. One PR (2905)
has 25 comments and no acted label. This is a real cost: the ingest and proof budget must assume about 10 findings
per PR, not one.

## 4. Concrete claims versus style

I split the 383 top-level claims by hand into two groups. A claim is concrete when it asserts observable runtime
behaviour: a crash, a wrong value, a lost write, a race, a leak, a wrong URL, an OOM, a blocked UI. A claim is style
when it is about naming, duplication, docs, formatting, config consistency, dead code, API shape or meta commentary
on a test.

| Bot | Concrete | Style | Style share |
|---|---|---|---|
| macroscope | 163 | 12 | 7% |
| cursor | 110 | 25 | 19% |
| coderabbit | 49 | 11 | 18% |
| greptile | 4 | 1 | 20% |
| codex | 5 | 0 | 0% |
| devin | 2 | 0 | 0% |
| Copilot | 1 | 0 | 0% |
| **Total** | **334** | **49** | **13%** |

So 87% of the claims are worth a test. Cursor is the main source of style comments, mostly "Duplicated X across two
files". Style comments are acted on less often (13 of 49, 27%) than concrete ones (127 of 334, 38%), but the team
does not ignore them.

## 5. Mismatches with `internal/domain/external/doc.go`

The real data was fed to the package through a temporary program. Result: 195 of 383 top-level comments become
candidates, 188 are ignored. The gaps:

| # | The doc says | The PRs show | Affected |
|---|---|---|---|
| 1 | Four tools: CodeRabbit, Copilot, Cursor Bugbot, Greptile. | Three more logins post top-level findings: Macroscope, the Codex connector and Devin. `t3-code-bot` posts replies only. Macroscope alone is 46% of the data. | 182 of 383 (47.5%) ignored as `unknown_author`. |
| 2 | Greptile carries `**logic:**`, `**syntax:**` or `**style:**`, the word Greptile, or "Prompt To Fix With AI". | All 5 comments are plain prose with none of these marks. | 5 of 5 ignored as `no_marker`. |
| 3 | The Copilot login is `copilot-pull-request-reviewer` or `Copilot`. | The real login is `Copilot`. `toolFor` accepts it, but `listed` compares the exact normalised login, so a policy that lists the documented `copilot-pull-request-reviewer[bot]` drops it. | 1 of 1 ignored as `not_in_sources`. |
| 4 | CodeRabbit opens with a `_category_ \| _severity_` line. | 40 of 60 match (`_⚠️ Potential issue_ \| _🟡 Minor_`). 20 of 60 use `**<picture>...</picture> Minor** · **Functional Correctness** · **Quick win**`. The parser reads no severity for those 20 and the claim starts with `Minor · Functional Correctness · Quick win`. | 20 of 60: `SeverityStated` false, severity medium, noisy claim. |
| 5 | CodeRabbit is recognised by "the word CodeRabbit" or a fingerprinting comment. | The new format has no word in the header. The mark survives only because the footer HTML comment says "an auto-generated comment by CodeRabbit" and the icon URLs contain `coderabbit`. The fingerprint footer is present in 49 of 60. | 20 of 60 depend on footer branding, not on the finding. |
| 6 | CodeRabbit severity words include Trivial and Nitpick. | The data has Critical 4, Major 20, Minor 36, and no Trivial or Nitpick. | Coverage gap, no failure. |
| 7 | Cursor states `**High Severity**` in the doc example. | 81 Medium, 36 Low, 18 High. The common case is Medium. | 117 of 135 are not High. |
| 8 | A tool that states no severity gets medium. | Macroscope, codex and devin state a severity in the body (`🟠 High`, `P1`, `🔴`). They are not read at all. | 182 comments, see row 1. |
| 9 | A claim is the text a person reads, after HTML, `<details>`, images and badges are removed. | Macroscope repeats `path:line` and the severity inside the claim (129 start with an emoji, 7 contain a GitHub blob URL). The new CodeRabbit header leaves `Minor · Category · Quick win` at the front. | 149 claims carry tool noise. |
| 10 | The location is `line` and `start_line` on `commit_id`; when `line` is null, `original_line` on `original_commit_id`. | 145 of 411 comments have `line: null`, and `commit_id` points at the newest commit. A caller that walks commits after `commit_id` will miss the fix. The original pair is the only correct anchor. | 145 (35%). |

Row 4 is the one that will bite first. The new header is the only format CodeRabbit uses in November, and it is the
format a current PR will show. The parser still produces a candidate, but the severity is lost and the claim opens
with the tool's own label instead of the finding.

Row 1 is the strategic one. If the policy lists Macroscope, the package cannot read it. That is 175 real findings in
this sample, and 75 of them were acted on.

## 6. Ten comments to prove with a failing test

These are the clearest cases: a pure function or a small unit, a stated input and output, and a claim that a test can
settle without a running app. The label is from section 1. "Acted" means the PR changed the commented lines.

| # | PR | Bot | Place | Label | Test it supports |
|---|---|---|---|---|---|
| 1 | 2463 | macroscope | `apps/server/src/sourceControl/azureDevOpsPullRequests.ts:65` | dismissed | `updatedAt` uses `closedDate ?? creationDate ?? Option.none()`. `Option.none()` is truthy, so `??` never falls through. Assert that an active PR (closedDate None, creationDate Some) returns creationDate. Pure function, one branch. Cursor reports the same bug in the same PR, which also tests duplicate claims. |
| 2 | 3649 | Copilot | `packages/shared/src/sourceControl.ts:140` | dismissed | `isSshRemoteUrl` uses a case-sensitive prefix. Assert that `SSH://host/repo` is true. One line, one assertion. |
| 3 | 3649 | macroscope | `packages/shared/src/sourceControl.ts:136` | acted | The SCP regex accepts `/` after the host. Assert that `deploy@github.com/project/repo` is not an SSH remote. Pure regex, and the PR fixed it. |
| 4 | 14527 | macroscope | `packages/shared/src/path.ts:71` | acted | `newProjectFolderName` returns reserved Windows names such as `con`. Assert that the result is never a reserved name. Pure, and the PR fixed it. |
| 5 | 28460 | macroscope | `apps/web/src/components/settings/StorageSettings.tsx:95` | acted | On blur, `resolvePastedPath("", value)` treats the whole value as a paste. Assert that pasting `/mnt/new` after `/mnt/old/` gives `/mnt/new`, not `/mnt/old//mnt/new`. Pure helper, and the PR fixed it. |
| 6 | 3 | macroscope | `apps/desktop/src/main.ts:333` | dismissed | Replacing `/` with `-` makes `feature/test` and `feature-test` the same slug. Assert the two inputs give different names. Pure. The bot later marked it "no longer relevant". |
| 7 | 14 | greptile | `apps/web/src/components/ThreadTerminalDrawer.tsx:367` | dismissed | A second `resize` listener is registered on the same window. Assert that one resize event makes one API call. A counter test catches it, and greptile is otherwise unreadable by the parser. |
| 8 | 3638 | cursor | `apps/web/src/components/settings/ScheduledTasksSettings.tsx:169` | dismissed | `scheduleFromDraft` treats an empty weekday set as all seven days. Assert that clearing every weekday does not produce a daily schedule. Pure reducer. |
| 9 | 13 | coderabbit | `apps/server/src/processRunner.ts:269` | acted | A write to `child.stdin` after the child exits can emit an unhandled error. Assert that a child that closes early leaves no unhandled rejection. Needs a fake child, but the PR fixed it. |
| 10 | 10 | devin | `apps/server/src/wsServer.ts:249` | unknown | `ws.send` inside the catch block throws when the socket is already closed. Assert that a closed socket during `routeRequest` does not throw. A mock socket makes it deterministic. |

Ordering: 1 to 8 need no I/O and are the best first tests. 9 and 10 need a fake process or socket and are a second
wave. Cases 1 and 2 are the cheapest to write. Case 5 has the clearest user-visible wrong value. Case 7 is the only
way to show that a Greptile claim can be proven at all with the current parser, because the parser drops it.

## 7. What this changes for the ingest work

1. Add Macroscope. It is the largest source on this repo (175 of 383 comments, 46%) and 43% of its comments were
   acted on. Without it, Heimdall reads half the review traffic.
2. Read the November CodeRabbit header. The `**<picture>... Minor** · **Category** · **Quick win**` line is the
   current format. The parser reads no severity from it and puts tool noise in the claim.
3. Fix the Greptile mark. Real Greptile comments on this repo carry no marker, so the login and the Bot type are all
   there is, the same rule Copilot already uses.
4. Accept both Copilot logins in `Sources`, or normalise them. A policy written from the doc drops the real author.
5. Add the Codex connector (`P0` to `P3`) and Devin (JSON payload in an HTML comment, emoji severity) when the
   sources list allows them. Devin's payload has `file_path`, `start_line`, `end_line` and `side`, which is better
   data than the diff position.
6. Strip the Macroscope prefix (`path:line`, emoji, severity) and its footer (`Addressed in`, `No longer relevant as
   of`) from the claim. The footer is also a free resolution signal.
7. Keep the original pair. `original_commit_id` and `original_line` are the anchor for "did a later commit change
   this". `commit_id` is the newest commit and is not safe for that question.

## 8. Not verified

- The location mapping (renames, moved lines, LEFT side, deleted files) was not run. Every comment was placed on its
  own commit with no diff.
- The 30 PRs are a sample, not all merged PRs. The per-bot rates are statements about this sample.
- The `acted` label is a diff fact. It does not say that the claim was correct or that the fix was the right one.
- The style split is my reading of each claim, not a rule in code. A second reader may move a few borderline cases
  (API shape, test commentary).
- The wider 11 month scan only read 20 PRs per month to find bot logins. It is not a full count of bot traffic.
