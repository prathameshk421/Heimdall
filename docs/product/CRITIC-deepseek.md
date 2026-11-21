# Heimdall: critic pass (review sub-agent, DeepSeek)

Status: review only. Nothing edited outside this file. Date: Nov 21 2026.
Sources read: `STRATEGY.md`, `ROADMAP.md`, `IDEAS.md`, `research/01` to `research/05`.
Research file `06-reddit-x.md` does not exist.

Mode: kill bad ideas. Every verdict below is a judgment call from the evidence in the repo plus
the web checks listed in section 0. Where I could not verify, I say so.

---

## 0. Claims I checked against the live web

Each line says what I looked at, and whether the plan's claim survives.

| Claim in the plan | What I found | Verdict on the claim |
|---|---|---|
| "Nobody sells approval bound to the exact commit" (`STRATEGY.md` §2, `research/01 §6.3`) | GitHub ships two native settings: "dismiss stale pull request approvals when new commits are pushed" and "require approval of the most recent reviewable push". Both bind approval to the diff and drop it on a push. | False. This is a checkbox, not white space. Kill the claim. |
| "CodeRabbit self-host lists about $15k a month" | The self-hosted docs say Enterprise only, 500 or more seats. The AWS Marketplace page shows a placeholder price of $5,000,000 and tells you to contact sales. The $15k number is not on either page. | The seat floor is real and checked. The price is a placeholder. Do not quote $15k. |
| "LLM reviewers cannot block" | Correct for Claude Code Review. Its check run concludes neutral or skipped and never fails. Correct for Copilot review. | Holds. But teams already close the gap themselves with about 20 lines of YAML that parse the check output. The gate is not scarce. |
| "Nobody ties blocking to a model finding with attached proof" | Several do, and they ship. MergeHarbor has "proof runs" that write a test per finding, run it in a sandbox, and post the failing test. Ito builds and runs the app on every PR. Heal files only defects it reproduces, each with a failing test. Bugstack requires the failure to match the bug's own error. Breken opens fixes that already went red then green. edgeverdict (MIT, free) is literally "a review gate that verifies changes by executing tests". | Weaker than the plan says. The mechanism is no longer novel. What is still open is the *gate* plus the *record*, and that is a thin slice. |
| "Audit evidence is a paid requirement Heimdall can own" | Vanta already evaluates every code change as compliant or not, using two rules: approved by someone other than the author, and an automated check present. Drata ships CC8.1 example evidence as a PR plus CI results plus approval. | Half false. The evidence is already collected from GitHub by the systems of record. Heimdall would be a data source to a tool it does not control. |
| "Test written from the ticket before the code" is a usable floor | TDD-Bench Verified (449 issues, 12 Python repos): best fail-to-pass rate 23.6% with GPT-4o. Otter++ reports 37.0%. Coverage is good only when the test happens to be fail-to-pass. | Does not hold as a hard gate. Roughly 60 to 75% of tickets will not yield a valid red test on the first try. |
| "A blocking check is a fine product shape" | Flaky required checks destroy trust fast. Ten checks at 2% flake each give about 18% of correct PRs a spurious red. There is a merged PyTorch PR titled "stop blocking merges on failures the AI CI Advisor cleared". Admin bypass becomes the normal path. | Not addressed anywhere in the plan. This is the top risk, not a footnote. |
| Code review benchmark numbers (`research/01 §5`) | Consistent with what I see: vendors run their own bench and win, F1 sits in the 45 to 65% band. | Holds. Good research. |
| EU AI Act as weak pull (`research/02 T7.6`) | Consistent. It regulates AI systems, not AI-written code. | Holds. Good call not to sell it. |

The research is honest and better than most seed decks. The weakness is not the facts. It is the
one sentence built on top of the facts.

---

## 1. The five weakest assumptions in STRATEGY.md

### Assumption 1. "Nobody gates on a model finding with attached proof" is white space.

Why it is weak: the white space closed between the research date and now. MergeHarbor sells proof
runs as a feature and charges 2 credits for each. Heal's whole pitch is "the failing test that
proves it". edgeverdict is MIT and does the deterministic part already. What none of them do is
make the proof a *required check with a signed record*, and that is a workflow opinion, not a
technology moat. A funded competitor can add a required check in a sprint.

Evidence that would prove me wrong: a scan of the 20 tools in `research/01` showing that none of
them can fail a required check on a proven finding, *and* that none can do it in six months. Also:
one buyer saying they would pay for the gate part specifically.

Cheapest test: two hours reading the check-run documentation of MergeHarbor, Ito, Greptile, and
CodeRabbit. Ask one question of each: what is the check conclusion when a finding is proven. Cost
near zero. The research already missed this class because it searched for "code review" and not
"runtime verification" or "proof run". Fix the search terms before the next pass.

### Assumption 2. "Audit evidence is a product we can own for regulated teams."

Why it is weak: the auditor relationship belongs to Vanta and Drata. They already compute a
compliant or non-compliant verdict per code change from the PR, the approver identity, and the
presence of a required check. A GRC lead will not replace the system of record to get a better
copy of evidence it already has. Heimdall's honest role is a *source* that feeds Vanta through
its API, and that is a much smaller business than the strategy describes.

Also, the evidence Heimdall produces is mostly a byproduct. Approval on the exact SHA is a GitHub
checkbox. The person approving is in GitHub. The tests are in CI. The only genuinely new artifact
is the mapping from those raw facts to a control, and that mapping is a document, not software.

Evidence that would prove me wrong: five GRC or compliance leads at companies with a live SOC 2
saying they would change the source of evidence, not just add a feed. Or one auditor saying "this
specific artifact is what I ask for and nobody gives it to me".

Cheapest test: five 30-minute calls. One question: "walk me through what you upload for CC8.1
today." Do not pitch. If the answer is "GitHub screenshots" after they already pay for Vanta, the
wedge is dead. Cost: one week of calendar.

### Assumption 3. A red test written from the ticket is a reliable floor for the proof loop.

Why it is weak: this is the load-bearing claim of the whole product and the external literature
puts it at 23.6% to 37% fail-to-pass. On the other 60 to 75% of real tickets, one of three things
happens. The agent writes a test that passes on broken code. The agent writes a test that fails for
the wrong reason (the plan already rejects compile errors, which is good). Or the test never
settles and a person writes it, which means the "proof" is now human work and the value claim
collapses.

The research admits this as "not verified" (`research/02 T3 implication`). That is not good enough
for the thing the product is named after.

Evidence that would prove me wrong: F2 Track C on 60 real issues with a modern model, showing a
fail-to-pass rate above 60% and a test-fixup rate below 20%. Anything lower and the gate needs a
different primary evidence type.

Cheapest test: the smoke run already priced in `research/04 §2.4`: 20 issues, about $2 to $3.
Run it this week, before building language profiles or a Slack app. If F2P lands near 30%, rewrite
the pitch around diff coverage plus tool evidence, and treat generated tests as an optional bonus.

### Assumption 4. The first buyer exists: 30 to 500 developers, already uses coding agents, has a
live SOC 2 or PCI audit, and has a rule against sending code to a SaaS reviewer.

Why it is weak: the two halves of that ICP pull in opposite directions. The rule against SaaS code
review is mostly a large-enterprise policy. A 30 to 500 developer company with a live PCI audit is
usually fine with CodeRabbit, because the alternative to SaaS review is no review. The companies
that truly cannot upload code are the ones that already have a paid enterprise contract, an
appliance, or an internal platform team that will build it.

The plan also never names how this buyer is reached. There is no channel: no marketplace presence
that fits self-hosted, no auditor referral, no OSS funnel that reaches a company VM. `research/05
§5` says this honestly and then the roadmap ignores it.

Evidence that would prove me wrong: ten companies in that band that today run no AI review
*because* of a code-residency rule, and can name the rule. Not "we are careful about data". A
named policy.

Cheapest test: 20 cold notes to platform leads, one question: "what stops you running an AI
reviewer on your PRs?" If the modal answer is cost, noise, or "we tried it and turned it off", the
self-host wedge is wrong and the noise wedge is right.

### Assumption 5. People want a gate.

Why it is weak: the highest scoring pain in the research is "review cannot keep up" (rank 1, score
80). The gate answers a different question: "can I prove control". Those are two buyers and two
budgets. Worse, a gate is the only product shape that can *cost* the buyer money and time on a bad
day. Every competitor is advisory for a reason: advisory tools cannot be blamed for a blocked
release. Heimdall's differentiator requires a required check on every PR, in a container, running
the customer's suite. That is the slowest possible check in the pipeline.

Evidence that would prove me wrong: a design partner who asks for blocking in week one, unprompted,
and keeps it on for a month. Or a measured p95 gate time under 5 minutes on a real 200-developer
repo.

Cheapest test: ship shadow mode first and watch whether anyone asks to turn blocking on. If nobody
asks after two weeks of real PRs, the gate is a feature of the report, not the product.

---

## 2. Verdicts

Cut means remove from the plan. Defer means keep the idea, remove it from the next two quarters.
Change means keep the intent, change the shape. I cut 11 items and defer 14. The roadmap drops from
about 40 work packages to about 22.

### IDEAS.md

#### A. Core promise

| # | Verdict | One line |
|---|---|---|
| A1 | KEEP | Cheapest way to make the noise claim measurable. Ship it in the first release. |
| A2 | KEEP | This is the product. If only one item survives, it is this one. |
| A3 | CHANGE | GitHub already does this natively. Record that it was on and stop selling it as new. |
| A4 | KEEP | Agents really do delete tests. Cheap, and it protects the one thing you claim. |
| A5 | DEFER | A heuristic with false positives on legitimately mocked seams. It is a lint rule, not a moat. |
| A6 | DEFER | Mutation testing is compute heavy and slow. Keep it out of the merge path. |
| A7 | CHANGE | Do not build a coverage engine. Ingest Codecov or Coveralls output as E2. |
| A8 | KEEP | This is the actual rule that makes the product work, and it costs nothing. |
| A9 | CHANGE | Sign the export. Drop "hash-chained" from the pitch. No buyer asked for a chain, one auditor asked for a signature. |
| A10 | DEFER | A document, not a product. Write it after a design partner asks, or feed Vanta instead. |
| A11 | CUT | Trailers are optional and trivially stripped. Detection is unreliable and changes no decision. |
| A12 | KEEP | Cheap, on brand, and it kills the surprise-bill objection. |

#### B. Adoption

| # | Verdict | One line |
|---|---|---|
| B1 | KEEP | Non negotiable. A PAT install fails any security review on day one. |
| B2 | KEEP | Without ticketless, no trial exists. This is a bug fix, not a feature. |
| B3 | CHANGE | Ship the webhook notifier only. Cut the native Slack app until a partner asks for two way. |
| B4 | KEEP | The install is the adoption story. |
| B5 | KEEP | One binary plus Postgres. Cut the embedded SQLite option, it is a support trap. |
| B6 | KEEP | One adapter covers local, gateway, and air-gapped cases. |
| B7 | KEEP | Small, and it blocks every team that uses a queue today. |
| B8 | KEEP | Move it up. The Action is the only cheap trial, and the fallback when the gate is not installed. |
| B9 | DEFER | Zero proof on GitHub yet. A second host before the first works is how small teams die. |
| B10 | CUT | No pay, no pull. Revisit only if a paying Forgejo user appears. |
| B11 | CHANGE | GitHub Issues only, same token. Linear on request. |
| B12 | KEEP | Move to the front of Stage 1. It removes the biggest political objection. |
| B13 | CHANGE | Python and Node only. Java and Rust are permanent maintenance bills with no proof of demand. |

#### C. Lightweight and cheap

| # | Verdict | One line |
|---|---|---|
| C1 | KEEP | Highest cost win per line of code in the whole backlog. |
| C2 | KEEP | Fold into the prompt design. Do not make it a work package. |
| C3 | KEEP | The hard cap is what makes the cost claim true. |
| C4 | KEEP | Move into Stage 1. Free, exact evidence beats a model call every time. |
| C5 | KEEP | This is 1.1 and it deletes the controller. Highest value structural change on the list. |
| C6 | DEFER | You cannot tune routing before the bench exists. Sequence it after 2.1. |
| C7 | KEEP | Rules already require measured numbers. This is the only marketing that is allowed. |

#### D. Security of Heimdall

| # | Verdict | One line |
|---|---|---|
| D1 | KEEP | Small, and security buyers read it first. |
| D2 | KEEP | Mostly done. Finish it and write it down. |
| D3 | CHANGE | SBOM and cosign now, they are 30 minutes in CI. SLSA later. |
| D4 | KEEP | The escape test suite is a sales asset, not a hygiene item. |
| D5 | KEEP | Required before any PR text reaches an agent. |
| D6 | DEFER | Good practice, no buyer. Do it when the parsers stop changing. |

#### E. Reach

| # | Verdict | One line |
|---|---|---|
| E1 | CHANGE | Read-only "why is my PR blocked" tool. Fold into 1.3. No write API. |
| E2 | KEEP | Cheap, common, and it makes the gate feel like part of the repo. |
| E3 | CUT | It was already "later moat". A later moat for a product with no present is a distraction. |
| E4 | CHANGE | No maintainer features. Use public repos as the free Action funnel and nothing more. |
| E5 | CUT | Already refused. Keep it refused. |
| E6 | CHANGE | The PR is the main UI. Board is for export and audit only, and it must have auth. |
| E7 | CUT | Agreed. It also breaks the independence claim. |
| E8 | CUT | Agreed. It breaks the honesty rule. |

#### F. Proof the product works

| # | Verdict | One line |
|---|---|---|
| F1 | KEEP | The only thing that turns any claim in the strategy into a fact. |
| F2 | KEEP | Run a 20-issue smoke now. This tests assumption 3, which everything rests on. |
| F3 | DEFER | Nightly infra with no decision attached. Do it once F1 is stable. |
| F4 | KEEP | Do it as soon as the gate can run on this repo. Not before. |

### ROADMAP.md work packages

| WP | Verdict | One line |
|---|---|---|
| Stage 0 exit criteria | KEEP | Keep it, but add a kill date. If the merged PR does not land by a named day, ship shadow mode instead. |
| 1.1 Job runtime | KEEP | First. It deletes the controller, the seats, and the firewall unit. |
| 1.2 Config: Switch optional | KEEP | Small and it unblocks everything. |
| 1.3 PR as primary surface | KEEP | Move above 1.1. It needs no new runtime and it is the actual product. |
| 1.4 Slack app + webhook | CHANGE | Webhook notifier only. Cut Socket Mode until a design partner asks. |
| 1.5 Ticketless default | KEEP | A config flip that decides whether anyone can try the product. |
| 1.6 Lite compose + init | KEEP | The 15 minute install is the whole adoption bet. |
| 1.7 Any OpenAI endpoint | KEEP | Small, and it is the air-gap story. |
| 1.8 Language profiles | CHANGE | Python and Node. Prove demand for the rest. |
| 1.9 GitHub merge queue | KEEP | Small. A missing `merge_group` blocks every queue user silently. |
| 1.10 Shadow mode | KEEP | Move to 1.1. It is the trust path and the free trial at once. |
| 2.1 Proof Bench | KEEP | Start the 20-PR smoke in week one, not after ten adoption items. |
| 2.2 Red-first bench | KEEP | This is the assumption test. It gates the pitch, not just the quality. |
| 2.3 Deterministic skip | KEEP | Straight cost win, and it is already promised on the pricing page. |
| 2.4 Token budget and cost line | KEEP | Without it the cost claim is a guess. |
| 2.5 Prompt shape for caching | CHANGE | Fold into 2.8. One experiment, not two work packages. |
| 2.6 Analyzers first | KEEP | Move to Stage 1. It is free proof and it makes the gate useful on day one. |
| 2.7 Test integrity checks | CHANGE | Ship the pinned test guard only. Mock detector and mutation are deferred. |
| 2.8 Model routing | KEEP | Only after 2.1. Before that you are tuning without a signal. |
| 2.9 Footprint budget | CHANGE | Measure, publish, do not fix. Size is a claim, not a roadmap. |
| 3.1 Board auth | KEEP | Move to Stage 1. The board is shipped unauthenticated today. That is a live hole, not a later feature. |
| 3.2 Signed record + verifier | CHANGE | Sign it, keep the verifier as the 60 second demo. Drop the chain language. |
| 3.3 Audit pack | DEFER | Do not write three weeks of control mapping before one auditor says it is useful. |
| 3.4 Threat model and self-audit | KEEP | Security buyers ask for it before they read anything else. |
| 3.5 Release hygiene | CHANGE | cosign and SBOM now. SLSA and offline bundle later. |
| 3.6 AI provenance | CUT | Same reason as A11. Unreliable input, no decision changes. |
| 3.7 Observability | CHANGE | Metrics endpoint and JSON logs. No Grafana JSON until a buyer asks. |
| 4.1 GitLab self-managed | DEFER | Correct idea, wrong year. |
| 4.2 MCP server | CHANGE | Read-only, and fold into 1.3. |
| 4.3 AGENTS.md rules | KEEP | Small and it makes the gate feel native. |
| 4.4 Linear + GitHub Issues | CHANGE | GitHub Issues only. |
| 4.5 Forgejo/Gitea | CUT | Same as B10. |
| 4.6 Helm chart | DEFER | Until a buyer runs Kubernetes. Compose covers the target buyer. |
| 4.7 Context packs | CUT | Same as E3. |
| GTM: landing page after 2.1 | CHANGE | Publish after the smoke run, with `not verified` on everything unmeasured. Waiting for the full bench costs a quarter. |
| GTM: Show HN after 1.6 and 3.2 | CHANGE | Do it after shadow mode works and the tamper clip exists. Do not wait for the signed record. |
| GTM: 3 partners after Stage 1 | CHANGE | Start before Stage 1. Partners should shape the Lite profile, not grade it. |
| GTM: free OSS shadow mode | KEEP | Distribution, not revenue. Treat it as an install funnel with a number attached. |

---

## 3. What is missing

These are gaps in the plan, not objections to it. Several are more valuable than things on the list.

1. **A latency budget with a number.** Nothing in the plan measures p95 gate time on a real repo.
   That number decides adoption more than recall does. Add a target, a measurement, and a hard
   timeout that reports `unknown` and never blocks on its own.

2. **A flake policy as a product feature.** The research mentions flaky tests once. The plan has no
   work package for it. You need: a retry with the retry recorded as evidence, a quarantine list,
   an `unknown` verdict, and a rule that a rerun-pass is never reported as a clean pass. Without
   this, one bad week trains the whole team to ignore the check.

3. **Break-glass and bypass.** Every bank and every payment company needs an emergency merge path.
   Heimdall has no story for it. The right answer is a bypass that requires a reason, names a
   person, gets recorded, and fails the audit report until it is reviewed. That is also the single
   best audit artifact you could sell, and it is absent.

4. **An import path for evidence teams already have.** Sonar, Snyk, Semgrep, Codecov, Copilot
   review. The plan reads SARIF but does not sell "keep your stack, we turn its output into one
   decision". That is the cheapest possible adoption and it also fixes the quality problem, because
   E2 evidence is exact.

5. **A "what we did not catch" report.** The research says publish your misses (`research/02` cross
   cutting notes). No work package does it. This is the trust builder that makes shadow mode
   credible, and it is the opposite of what every vendor does.

6. **Ingesting the incumbent reviewers as a signal.** Copilot, Claude, CodeRabbit, Bugbot all post
   structured findings. Treating them as candidate findings that must be proven is a better product
   than competing with them, and it makes Heimdall installable next to a tool the team already
   likes. This is missing and it is the cheapest wedge on the board.

7. **Retention, legal hold, and offboarding.** A compliance buyer asks what happens to the record
   when the product is removed, how long it is kept, and whether it can be exported per year. No
   answer today.

8. **Monorepo scale.** "Run the tests" on a 40,000 test monorepo is 45 minutes. Test impact analysis
   is marked P3 in `research/03 §9` and does not appear in the roadmap at all. Without it the
   product works on small repos only, which is the opposite of the stated buyer.

9. **The output token problem from the live run.** D6 is in Stage 0. Fine. But the plan treats it as
   a bug, and it is really a design constraint: reviewer output must be bounded and structured.
   Say so, or it will come back.

10. **A model cost ceiling the buyer can see before the first invoice.** BYO key means the buyer
    carries an unknown bill. Add a per-repo monthly cap and a local-model SKU with a stated quality
    drop. This is a sales blocker for finance buyers.

---

## 4. Ordering mistakes

1. **Proof Bench is in Stage 2, behind ten adoption items.** The pitch is measured proof. The
   landing page, the Show HN, the design partner deck, and the pricing all depend on numbers you do
   not have until week twenty. Move a 20-PR smoke into week one. It costs about $3.

2. **Design partners are scheduled after Stage 1.** Backwards. Three partners in week two will tell
   you which Lite profile to build, which host to support, and whether the gate or the report is
   the thing they want. Building first and selling second is how the Lite profile ends up wrong.

3. **Stage 3 is the buyer you say you will sell to first.** Audit pack, signed record, and board
   auth are all last. Either move the minimum audit artifact into Stage 1 (signed export plus one
   control page) or stop calling the regulated team the first buyer. The current plan sells to a
   buyer whose requirements land in month six.

4. **The board shipped with no authentication and auth is in Stage 3.** That is not a sequencing
   preference. It is a live vulnerability in a product sold to security teams. It belongs in
   Stage 0.

5. **Shadow mode is last in Stage 1.** It is the cheapest trust mechanism and the only politically
   safe way to install a blocking tool. Move it to first.

6. **The GitHub App is not a work package in Stage 1 at all.** B1 exists in the ideas file and
   never appears in the roadmap. PAT installs are the current state and they will fail every
   security review.

7. **Language profiles come before the bench that would justify them.** Four runner profiles is
   weeks of work on an unproven core. Two profiles and a bench is a better trade.

8. **Red-first bench sits after the language profiles.** You will build four runtimes before you
   learn whether the test agent works at all.

9. **Winter order inside Stage 1 is inverted.** Job runtime takes the longest and delivers the
   least visible change. The PR surface plus the webhook plus shadow mode can land sooner and are
   what anyone actually sees.

---

## 5. Positioning: right or wrong

**The self-host half is right. The audit-evidence half is the weak half, and the two are sold as
one sentence.**

Right: self-host is a real constraint with real money behind it. CodeRabbit's 500 seat floor is
confirmed on its own docs. Greptile self-host is a sales call. PR-Agent is free but has no gate.
That gap is real and it is the only durable fact in the strategy table.

Wrong: audit evidence is not a wedge, it is a feature of a system of record you do not own. Vanta
already decides per code change whether the change was approved by someone other than the author
and had an automated check. Drata's own documentation names the PR plus CI plus approval as CC8.1
evidence. An auditor does not want a second, smaller evidence store. A GRC lead does not want to
explain a new tool to the auditor. So the regulated pitch requires you to win a channel you do not
have, in front of a buyer who already bought the thing you are selling.

### The strongest alternative, argued honestly

**Wedge: the noise filter that runs on your own runner.**

Promise to the engineering manager, not the CISO: "your AI review tool posts 40 comments. We post
the 4 that have a failing test or a tool result, and we print what we dropped."

Why this is stronger:

- It sells the pain that scored highest in the research (review cannot keep up, score 80) and the
  one sentence the research actually found from a real engineer ("I'd trust AI more if it brought
  proof instead of speculation").
- It installs as a GitHub Action in ten minutes, with no VM, no Postgres, and no security review.
  That kills the entire adoption problem in Stage 1.
- It works next to CodeRabbit, Copilot, Greptile, and Bugbot instead of against them. You ingest
  their findings and prove or drop each one. That is a product the incumbents cannot copy without
  admitting their own findings are unproven.
- It has a natural paid upgrade: install the gate, keep the record. The regulated story becomes an
  upsell to a customer you already have, instead of a cold sale to a buyer who has Vanta.
- It survives the "funded competitor copies the evidence page" risk, because the value is in the
  drop rate on the customer's own repo, not in a document.

The second strongest alternative, worth naming: **proof of work for agent-authored PRs, sold to
the platform team that is rolling out coding agents.** The trigger event is concrete (a rollout
decision), the buyer has a budget line, and today the answer is "nothing". This is arguably a
better segment than the noise filter, because the noise filter competes with a tool the team
already pays for. If I had to pick one buyer for the first five calls, it is the platform lead
mid-rollout, not the compliance lead.

What I would keep from the current positioning: "nothing merges without proof" is a good internal
rule and a good demo line. It is a bad first sentence for a cold buyer, because it sounds like a
new obstacle.

Concrete change: keep the strategy document, change the first buyer to the platform or DevEx lead
mid agent rollout, change the first surface to an Action plus shadow mode, and make the audit pack
an upsell that a customer asks for after ninety days.

---

## 6. The single biggest reason this product fails

**The gate is the product, and a gate that is slow or flaky gets switched off.**

Every competitor survives because it is advisory. Heimdall's only differentiated claim needs a
required check on every PR, inside a container, running the customer's own suite. On a real repo
that means tens of minutes, occasional flakes, and one blocked release. The first engineer who is
blocked at 6pm on a Friday asks for the admin bypass. GitHub allows it by default. After that
Heimdall is a comment bot with extra steps, and the audit record becomes a record of a check
everyone ignored.

This is not a quality problem you can out-engineer. It is a shape problem. The plan even has the
evidence in front of it: the research notes a real project where 18 of 234 queue runs failed in a
week, and there is a merged PyTorch PR that stops blocking merges on a bot's findings because the
blocks were costing more than they saved. Those are the buyers you are targeting, and they already
voted.

What to do about it, in order:

1. **Split the gate in two.** A fast required check that is deterministic and takes under 2
   minutes: secrets, analyzers, diff coverage, pinned test guard, approval binding. A slow proof
   run that is advisory, async, cached, and may report `unknown` without blocking. Only the fast
   half is required on day one.
2. **Ship shadow mode before blocking.** Two weeks of comments with a weekly digest of what would
   have blocked. Let the team ask for the gate. Do not install it.
3. **Add break-glass as a feature, not a workaround.** Bypass requires a reason, names a person,
   is recorded, and fails the audit report until reviewed. This turns your biggest adoption
   blocker into your best audit artifact.
4. **Publish p95 gate time next to recall.** Both numbers, on the landing page, with the bench that
   produced them. If p95 is over ten minutes, fix that before adding any feature.
5. **Never let a timeout pass silently.** A run that did not finish is `unknown`, and `unknown`
   shows a person. A silent pass is the one failure mode that destroys the product's only claim.

Runner-up failure, for the record: distribution. A self-hosted B2B tool with no marketplace, no
auditor channel, and no PLG path is a hard sell even when it works. The fix is the Action plus
shadow mode funnel above, which is a second reason to move that work to the front.

---

## 7. One page summary

- The research is good. The sentence built on it is not.
- Two of the five market facts do not survive a web check: approval-on-SHA is a GitHub checkbox,
  and self-host pricing is a placeholder, not $15k.
- The white space is narrower than claimed. Proof runs exist and one of them is MIT.
- The regulated wedge sells a feature of Vanta's product to a buyer who already has Vanta.
- The proof loop depends on generated tests, and generated tests from tickets land at 23.6% to 37%.
- A required check that is slow or flaky teaches the org to bypass it. No work package addresses
  this.
- Cut 11, defer 14, change 18. The roadmap gets smaller, not bigger.
- The first surface should be a GitHub Action in shadow mode, not a VM.
- The first buyer should be the platform lead mid agent rollout, not the compliance lead.
- Measure p95 gate time before anything else, then recall, then cost.
