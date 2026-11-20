# Critic: Heimdall plan (Grok, 21 Nov 2026)

Read only: `STRATEGY.md`, `ROADMAP.md`, `IDEAS.md`, `research/01` to `research/05`. No other file was edited.

This pass checked live pages on 21 Nov 2026. X was not fetched. Reddit was not fetched. Treat both as still open, same as `research/02`.

Checked:

- Hacker News, Feb 2026, "There is an AI code review bubble": poor signal is the core issue, not a side issue. https://news.ycombinator.com/item?id=46766961
- Hacker News, about Mar 2026, Claude Code Review: "$15 to $25" called wild, Copilot called noise, and a user called the writer-plus-reviewer bundle a perverse incentive. https://news.ycombinator.com/item?id=47313787
- Hacker News, Jul 2026: a ~2000-dev company measured about 7% productivity from all AI tooling, and teams asked to turn CodeRabbit off because of comment walls. https://news.ycombinator.com/item?id=48743713
- Hacker News, Sep 2026: "volume without action is just noise." The author built a tool to post fewer comments. https://news.ycombinator.com/item?id=49799019
- Codacy, 26 Oct 2026 (a competitor): most AI reviewers cannot safely block a merge. The tools that do block are rule gates (Sonar, Semgrep, Snyk, Codacy). https://blog.codacy.com/ai-code-review-tools-compared-2026-why-most-cant-safely-block-a-merge-and-what-code-governance-fixes
- DEV, Oct 2026: an agent mocked the function under test and CI went green. A comment says to key the lint on files the PR changed. https://dev.to/jeff_pdc/the-agent-wrote-tests-that-mocked-the-function-under-test-and-ci-went-green-2jne
- Gitar, Jan 2026: already sells "review without the comment spam" via one living comment. https://gitar.ai/blog/ai-code-review-without-the-comment-spam

Repo facts used as measurements, not as marketing:

- 19 Nov 2026, ticket KAN-26: tests were rejected once, then verified red-first in three runs, commit `ab0d649`. Two of three reviewers stopped at an 8000-token reasoning cap with zero output. The gate stayed red. A person closed PR 16. Ticketless PR 15 was not reviewed. `docs/LIVE_RUN_H11.md`
- Token estimate for that run: about $0.24. OpenCode recorded cost 0. The provider balance was not read. `docs/LIVE_RUN_H11.md`
- `heimdall-server` idle 41.8 MiB, 63.9 MiB during work on 7 Nov, runner 10.7 MiB. The blog says this is not the whole stack. `docs/BLOG_MEDIUM.md`

## 1. Verdicts

KEEP means build it in the order below. CHANGE means the idea is right and the shape is wrong. DEFER means a buyer or a bench must come first. CUT means it makes the launch worse.

### Ideas

| # | Verdict | Reason |
|---|---|---|
| A1 | KEEP | The drop counter is the only number a developer will screenshot. Noise is the daily complaint. |
| A2 | KEEP | One page is the product. A 1,400-line comment is how you get uninstalled. |
| A3 | CHANGE | Show the void-on-push. Do not say nobody sells it. GitHub already dismisses stale reviews. Your extra is the record, and that record is not signed yet. |
| A4 | KEEP | The author, person or agent, must not edit the pinned red tests. This is the independence claim in one rule. |
| A5 | CHANGE | Reject a mock of a file this PR changed. A blanket "no mocks" rule flags good tests and gets you mocked. |
| A6 | DEFER | Mutation score is a nightly extra. It is slow, it has equivalent mutants, and it is not why anyone installs. |
| A7 | KEEP | Diff coverage is a real E2 gate. Ship it as a check. Do not sell it as the company. Codecov and Codacy already do. |
| A8 | KEEP | Severity comes from the evidence tier. The Greptile founder already said a model cannot rank its own nits. |
| A9 | DEFER | A signed chain before one real merge is a demo toy. Export the event JSON first. Sign it when a buyer names the control. |
| A10 | DEFER | A control map with no auditor is a PDF. Write it with design partner one, not before. |
| A11 | CHANGE | Label suspected agent lines. Do not raise the evidence bar until you measure false labels. Trailers are optional and agents lie. |
| A12 | KEEP | Print the cost on the check. Mark it estimated until the provider balance is read. The H11 $0.24 is not a price. |
| B1 | KEEP | A gate on a PAT dies when that person leaves. Ship the GitHub App. |
| B2 | KEEP | Ticketless is the trial. Jira-first is why a stranger cannot try you. |
| B3 | CHANGE | Ship the generic webhook first. Slack Socket Mode is next. Switch stays, but it is not a buyer feature. |
| B4 | KEEP | `heimdall init` is the product for week one. Fifteen minutes is a target, not a fact. Time it on a fresh VM. |
| B5 | KEEP | Five resident agents plus Graphiti is how you lose the "one VM" line. Lite is survival, not a nice-to-have. |
| B6 | KEEP | One OpenAI-compatible URL. Without it, "air gap" is a lie. |
| B7 | KEEP | `merge_group` is small. Skip it and every queue repo looks broken. |
| B8 | KEEP | The GitHub Action is the trial. A VM is the sale. The roadmap buried this. Pull it forward. |
| B9 | DEFER | GitLab self-managed is a real second host. You have zero paying GitHub teams. |
| B10 | CUT | Forgejo pay score is 2. Hobby hosts will not fund the company. |
| B11 | DEFER | GitHub Issues is enough. Add Linear when a buyer asks. |
| B12 | KEEP | Shadow mode for two weeks. A blocking bot on day one gets the CodeRabbit treatment. |
| B13 | CHANGE | Add Python and Node/TS. Leave Java and Rust until a repo needs them. Five language packs in stage 1 is a month you do not have. |
| C1 | KEEP | Skip the model on docs, lockfiles, and tiny diffs. Bugbot users already said most PRs are tiny. |
| C2 | KEEP | The H11 run already read 5.6M cached tokens. Measure the hit rate. Do not claim a saving from a design sketch. |
| C3 | KEEP | A hard cap with a `budget hit` status. H11 died on length with no clean status. |
| C4 | KEEP | Linters and secret scanners before any model call. Do not pay a model to find an unused import. |
| C5 | KEEP | Same work as job runtime (WP 1.1). Do not build it twice. |
| C6 | CHANGE | Route to a flash model only after the bench shows recall. A cheaper miss is still a miss. |
| C7 | KEEP | Put cost, RAM, and recall on the site only after the bench. Targets in the strategy are not measurements. |
| D1 | KEEP | You want security buyers. PR text is an injection channel. Write the threat model before a pilot. |
| D2 | KEEP | Least privilege on the GitHub App. CodeRabbit's 2025 bug was a write key on a huge install base. |
| D3 | DEFER | SBOM, cosign, and SLSA at the first install a stranger runs. Not before the gate merges once. |
| D4 | KEEP | Publish the sandbox proof: no network, read-only, seccomp, escape tests. This is a demo people can trust. |
| D5 | KEEP | Secrets never reach the agent. Redact inputs. Allowlist egress. |
| D6 | DEFER | Fuzz SARIF and webhooks after the threat model names them. Good, not first. |
| E1 | KEEP | MCP "what proof is missing" is how an agent fixes the PR before a human opens it. Later, not the wedge. |
| E2 | KEEP | Read `AGENTS.md` as rules. It is small, and it stops dumb comments. Pull it ahead of personal context. |
| E3 | DEFER | Personal packs are a later moat and a privacy incident if you lead with them. |
| E4 | CUT | OSS maintainers do not pay. GitHub already added PR caps. Do not build a maintainer mode. |
| E5 | CUT | No Teams bot until a buyer asks. |
| E6 | CHANGE | The PR is the UI. Add login before anyone opens the board. Do not make the board the product. |
| E7 | CUT | No auto-fix. It breaks "we do not write the code", and Sonar/Gitar already fix CI. |
| E8 | CUT | No leaderboard from blog numbers. |
| F1 | KEEP | No bench, no claim. Replay real bug PRs and clean PRs. Publish n, cost, and misses. |
| F2 | KEEP | The red-first claim dies without a fail-then-pass score. |
| F3 | CHANGE | One clean run on 10 repos first. A nightly 20-repo set before that will rot. |
| F4 | KEEP | Gate your own repo. H11 did not merge. That is the bar. |

### Work packages

| WP | Verdict | Reason |
|---|---|---|
| Stage 0 | KEEP | Stop here until a fresh PR merges with no hand repair. H11 did not. |
| 1.1 | KEEP | One task, one process, then exit. This deletes the resident seats. |
| 1.2 | KEEP | Switch optional. Same change as 1.1. |
| 1.3 | KEEP | The proof comment on the PR is the thing a human reads. |
| 1.4 | CHANGE | Webhook notifier in this stage. Slack after a pilot asks. Do not block install on Socket Mode. |
| 1.5 | KEEP | Ticketless default. A ticket adds context when the PR names one. |
| 1.6 | KEEP | Three services and `init`. Time a fresh Ubuntu VM. Publish the clock. |
| 1.7 | KEEP | Base URL plus key. Drop the two hardcoded provider names. |
| 1.8 | CHANGE | Python and Node/TS profiles only. Java and Rust wait. |
| 1.9 | KEEP | Answer `merge_group` or queue users cannot adopt. |
| 1.10 | KEEP | Shadow before any design partner. This is the trust step. |
| 2.1 | KEEP | Proof Bench on 10 repos before a landing page and before Show HN. |
| 2.2 | KEEP | Red-first bench. Record the first numbers, including the misses. |
| 2.3 | KEEP | $0 model path for trivial PRs, proven on the bench. |
| 2.4 | KEEP | Hard budget and a cost line. H11 shows why. |
| 2.5 | KEEP | Stable prefix, measured cache-read share. |
| 2.6 | KEEP | SARIF tools before the model. Do not bundle Semgrep's rule licence. Use Opengrep and your own rules. |
| 2.7 | CHANGE | Ship the tamper guard and the mock-of-changed-file check. Defer mutation. |
| 2.8 | DEFER | Model routing after 2.1 compares recall and cost. |
| 2.9 | KEEP | Measure the whole Lite stack, idle and under a 10-PR burst. The 41.8 MiB figure is the server only. |
| 3.1 | CHANGE | Do it before any public board. Today the board has no login. If the board stays on a private network, say that in the readme and still add a session. |
| 3.2 | DEFER | Signed export after one real merge exists. A tamper demo of a fake file will get you laughed off HN. |
| 3.3 | DEFER | Audit pack with the first design partner and one GRC reader. Not a solo writing task. |
| 3.4 | KEEP | Threat model, injection tests, escape tests. Required before a bank clone. |
| 3.5 | DEFER | Signed images when the first external install happens. |
| 3.6 | DEFER | Same as A11. Label later. Do not gate on a guess. |
| 3.7 | DEFER | Metrics when a user runs it. A Grafana JSON file is not a feature. |
| 4.1 | DEFER | GitLab after three paying GitHub teams. |
| 4.2 | KEEP | MCP for "what proof is missing". Build it when agents are in the pilot, not in a distant stage 4 if that slips a year. |
| 4.3 | KEEP | Pull `AGENTS.md` into stage 2. It is small. |
| 4.4 | DEFER | Linear and the `WorkItem` port on request. |
| 4.5 | CUT | Forgejo adapter. |
| 4.6 | DEFER | Helm when a buyer runs Kubernetes. Compose is the path. |
| 4.7 | DEFER | Personal context packs stay off the headline. |
| GTM landing page | CHANGE | After 2.1, and only with measured cost, whole-stack RAM, recall, and the miss rate. |
| GTM Show HN | CHANGE | After dogfood merges its own PR. A tamper demo before that is a skit. |
| GTM design partners | CHANGE | After two weeks of shadow mode on their repo. Not after Slack and four languages. |
| GTM free OSS | CUT | Maintainers will not pay, and you have no Action yet. |

## 2. What they will mock on launch day

1. The slogan. "Nothing merges without proof" meets H11: the gate stayed red, two reviewers emitted nothing, and a person closed the PR. Pre-empt: do not use that sentence until a recorded run merges. Put the H11 table on the site.

2. The RAM line. 41.8 MiB is the server process. The plan still needs Postgres, a runner, and today also Graphiti and resident agents. Pre-empt: publish whole-stack RSS, idle and under load, with the machine and the commit. The blog already warns that 41.8 MiB is not the stack. Use that warning.

3. The price line. About $0.24 is an estimate from tokens. OpenCode stored cost 0. The provider balance was not read. Two reviewers did not finish. Pre-empt: print "estimated" until the balance moves. Compare with Claude or CodeRabbit only after 100 finished PRs.

4. The install. Switch, Jira, an afternoon, and a Go-only sandbox. That is a hackathon, not a trial. Pre-empt: ticketless, no Switch, one `init`, and a Python or TS fixture in the readme before you tweet.

5. The category. Greptile's founder posted "there is an AI code review bubble" in Feb 2026. In Jul 2026 a large team asked to disable CodeRabbit. A new bot that comments is dead on arrival. Pre-empt: the first comment is a drop count. If nothing is proven, the bot stays quiet. Silence is the feature.

## 3. What would make developers tell someone

1. A drop counter on every PR. Evidence: HN 46766961 (signal is the core issue), HN 48743713 (teams begging to disable the bot), HN 49799019 (fewer comments, even if you miss some). People share a number. They do not share a policy PDF.

2. One updated comment: proven, not proven, needs a person. Evidence: Gitar already moved to one living comment because spam lost (https://gitar.ai/blog/ai-code-review-without-the-comment-spam). Review time is the pain in `research/02` rank 1. Shrink the read.

3. A red test the code author cannot edit, plus a reject when a test mocks a file the PR changed. Evidence: the Oct 2026 DEV post, and the comment that says to key the rule on the diff. This is the bug developers already got burned by. Cursor owns Graphite, so a vendor that writes the code will not pin a test against itself. HN 47313787 called that incentive out.

4. A printed cost, and $0 when the model does not run. Evidence: Claude at $15 to $25 was called wild on HN 47313787. One user runs a gist at about $0.04 a review. Another said they would rather spend Claude Max tokens than pay again. A hard skip on lockfiles is the contrast.

5. Dogfood with a public miss list. Evidence: the same HN thread says a tool that finds about eight issues on clean and dirty diffs is a random number generator. Publish recall and the bugs you miss (F1, 2.1). Trust follows a miss list. It does not follow an F1 from a vendor blog. `research/01` already says those benches disagree.

## 4. Is the regulated self-host wedge right?

No. Not as the first wedge.

It is the right year-two buyer. It is the wrong first product.

The words are taken. CodeRabbit sells a control layer. Qodo sells governance. On 26 Oct 2026 Codacy told the market that LLM reviewers comment and rule tools gate. A new binary that says "SOC 2 merge gate" sounds like their blog.

You cannot pass a bank's vendor review. The board has no login. The live path does not merge. Install needs Switch and Jira. A CISO asks for your SOC 2, a pen test, and SSO. You have none.

GitHub already binds review to the latest push if the repo dismisses stale approvals. Auditors already sample PRs. Your extra is typed evidence and an independent test. Lead with that.

Self-host pain is real and less frequent than noise (`research/02`: noise at frequency 5, self-host at 3). A heavy self-host also loses to a gist. See the $0.04 review on HN 47313787.

**Alternative wedge: proof or silence, on the pull request, in shadow mode, with no VM.**

Ship A1, A2, A4, A5 (changed), A8, A12, B8, B12, C1, and F4. The check posts one page and a drop count. It does not block for two weeks. The Action is the trial. The VM is what you sell when a platform lead says the diff cannot leave the network. The signed pack (A9, A10) is the invoice, after they have seen the drop count on their own repo.

This wedge still serves the regulated buyer. It does not ask them to be user one. It gives developers a screenshot. The audit buyer has a budget and a long cycle. The developer has a PR open today.

## 5. Launch story

Use only the measurements you have. Do not post this as a product claim. Post it as the run.

On 19 Nov 2026 Heimdall took ticket KAN-26 on a fixture payments repo, rejected the first tests because two already passed on the base, then verified a second set as red-first across three runs and committed `ab0d649`. Two of three reviewers then stopped at an 8,000-token reasoning cap with zero output, the gate stayed red, and a person closed the PR. The server process was 41.8 MiB idle on 7 Nov, and the token estimate for that unfinished run was about $0.24, which is not a price until the provider balance is read.

A second post is allowed after a later run merges with no hand repair, and after whole-stack RAM and a 10-repo bench exist. Until then, the honest run is the only story that survives the replies.

## Order if you keep one month

1. Finish Stage 0. One merged PR, no hand repair. Dogfood the repo.
2. Shadow comment: drop count, one page, cost line, tamper guard, mock-of-changed-file.
3. Action mode and `init` on a three-service compose. Time it.
4. Proof Bench on 10 repos. Publish misses.
5. Then talk to a fintech lead. Not before.
