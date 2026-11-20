# Pain points research: AI code review, AI-written code, merge gates

Date of research: 2026-11-20. Method: about 30 web searches and 20 page fetches (search, HN threads, maintainer blogs, press, vendor reports).

## Limits of this research (read first)

- **Reddit: not verified.** The search tool and the fetch tool both refuse reddit.com. No Reddit thread (r/ExperiencedDevs, r/programming, r/devops, r/golang, r/LocalLLaMA, r/selfhosted, r/github, r/cscareerquestions) was read. Every quote below comes from Hacker News, maintainer blogs, press, GitHub, or vendor reports. Treat Reddit sentiment as an open item.
- **X/Twitter: not verified.** No post could be fetched. Two Mitchell Hashimoto posts appear only as search-result titles (Ghostty policy, Vouch). Quote text from them is the search snippet, not a fetched page.
- **HN dates are approximate.** HN pages show only relative ages ("8 months ago"). I converted them to months using today's date of 2026-11-20.
- **Vendor bias.** Many numbers come from vendors that sell review tools (CodeRabbit, Qodo, Greptile, cubic, Faros, Checksum). I mark these "vendor". They show demand, not neutral truth.
- **Willingness to pay** has almost no direct evidence. I found price complaints and demand signals only. See theme 8.
- Frequency and intensity scores use a 1 to 5 scale and are my judgment from the evidence found.

---

## Theme 1. AI code review bots: noise, trust, cost, privacy

**Quotes and facts**

1. "To be honest I wouldn't want to use any AI-based code reviewer at all... it has not once produced a useful comment and instead has been factually wrong many times." HN user iLoveOncall. https://news.ycombinator.com/item?id=42451968 (Dec 2024).
2. "I specifically instructed it to ignore nitpicks, but it still added such comments... it adds too much noise to PRs, and only a very small percentage of comments are actually useful." HN user jerrygoyal, about CodeRabbit. Same thread (Dec 2024).
3. "It also often labeled critical issues as nitpicks, which is unacceptable in our context." HN user dakshgupta (the Greptile co-founder). Same thread (Dec 2024). The vendor admits a model cannot rank its own findings.
4. "LLMs are reluctant to risk downplaying the severity of an issue and therefore are unable to usefully filter out nits." dakshgupta (Greptile). https://news.ycombinator.com/item?id=46766961 (about Feb 2026).
5. "I'd trust AI more if it brought proof instead of speculation." HN user hinkley. Same thread (about Feb 2026). This is the Heimdall evidence rule in one sentence.
6. "It's really hard to get it not to tell you 20 highly speculative reasons why the code is problematic." HN user zmmmmm. Same thread.
7. "copilot review is useless. The noise it generates easily costs that much in wasted time." HN user SkyPuncher. https://news.ycombinator.com/item?id=47313787 (about Mar 2026). In the same thread: "it's not a reviewer, it's a random number generator with a UI" (toniantunovi); "you've got the ultimate perverse incentive" (lowsong, on one vendor selling both generation and review).
8. Cost: Claude's review product at "$15~25 per review" drew "This cost seems wild" (cbovis) and "Would rather have a repo skill to do the code review with existing Claude Max tokens" (atonse). Same thread (about Mar 2026).
9. Cost per seat: a Cursor forum poster said Bugbot makes the team "pay for EVERY dev" and that "70-80% of our PRs are tiny", so the run on each PR felt wasteful. https://forum.cursor.com/t/bugbot-too-expensive/126724 (read via search summary; date not verified). Cursor later moved Bugbot to per-run pricing, about $1.00 to $1.50 per run (vendor-cited by cubic, mid 2026).
10. Privacy: Kudelski Security found a remote code execution flaw in CodeRabbit's production servers. They reached a GitHub App private key with write access to over 1 million repositories. Disclosed Jan 24 2025, fixed by Jan 30 2025, published Aug 2025. CodeRabbit says no customer data was accessed. https://kudelskisecurity.com/research/how-we-exploited-coderabbit-from-a-simple-pr-to-rce-and-write-access-on-1m-repositories
11. A reviewer wish: "we start seeing reasonable prices for reasonable models on SaaS platforms that are not scary for SecOps." HN user philipwhiuk. https://news.ycombinator.com/item?id=46766961 (about Feb 2026).
12. Benchmarks disagree. The Martian bench lists Greptile at about 84% precision and 53% recall, and CodeRabbit at 66% precision and 56% recall. Another vendor benchmark puts CodeRabbit's precision at 36%. No tool finds more than about 55% of seeded bugs. Mostly vendor sources. https://codereview.withmartian.com/ and https://www.cubic.dev/blog/coderabbit-vs-greptile
13. A noise anecdote from one blog: 41% of CodeRabbit comments were noise in week one, and 2 engineers asked to turn it off. It fell to about 15% after tuning. Single team, not verified. https://aicodereview.cc/blog/coderabbit-reduce-noise/

**Frequency:** 5 (in every AI review thread). **Intensity:** 3 to 4 (annoyance and distrust, rarely a hard stop). **Willingness to pay:** medium. Teams already pay $15 to $40 per seat, but they complain about value.

**Implication for Heimdall.** The strongest wedge is the evidence rule. A finding with no failing test, tool result, or team-memory fact never reaches the human. This answers "speculation", "20 reasons", and "nitpicks labeled critical" directly. Show a "findings dropped for no evidence" count so the saved noise is visible. Cost per PR must be small and printed, because the buyers dislike $15 to $25 per review. Self-hosting answers the SecOps and leaked-key fear. Do not claim better recall than the tools above. Every tool misses about half the bugs, so the pitch is "fewer, proven findings plus a deterministic gate".

---

## Theme 2. Review bottleneck from AI-written PRs, and what teams changed

**Quotes and facts**

1. Faros AI telemetry, high-AI teams: PR size up 154%, review time up 91%, bugs per developer up 9%, 98% more PRs merged. "AI-augmented code is getting bigger and buggier, and shifting the bottleneck to review." Vendor, data as of June 2025. https://www.faros.ai/blog/ai-productivity-paradox-report-2025 (Jul 2025). A 2026 follow-up reports review time up 441% and 31% more PRs merged with no review. Secondary summary only. Not verified at the source.
2. "A very prolific coworker who fully embraced claude has inflicted the team with a flood of AI-generated PRs... it has continuously trained me to subconsciously avoid his PRs." HN user niuzeta. https://news.ycombinator.com/item?id=48497609 (about Jun 2026).
3. "Code review doesn't scale to prolific humans, it definitely can't scale to agents." HN user swiftcoder. Same thread.
4. "Line-by-line review is still mandatory for shipped code, but does not scale with large AI-assisted or agentic PRs." and "reviewers can no longer assume authors understand or wrote the code they submit." Joe Zhou, Microsoft Azure Container Upstream, in GitHub's community discussion, quoted by The Register. https://www.theregister.com/software/2026/02/03/github-ponders-kill-switch-for-pull-requests-to-stop-ai-slop/4334869 (Feb 3 2026).
5. "You cannot give a pass for code you don't understand." (Agentlien) and "you can't give it a lgtm, you actually need to dig at it until you do fully understand it" (mkozlows). https://news.ycombinator.com/item?id=48614631 (about Jun 2026).
6. Qodo survey of 500 developers and 300 leaders: 26% of both groups name reviewing AI code as the main delivery constraint. Only 3.7% of leaders say their processes are enough for quality and governance. Vendor. https://www.qodo.ai/blog/state-of-ai-code-quality-report-2026/ (Sep 23 2026).
7. What teams changed. Rootly dropped its two-year small-PR and stacked-PR rule: "AI agents have changed the equation because they think in features rather than increments." It now uses blast radius, feature flags, and a rollback note in every PR. https://www.infoq.com/news/2026/08/rootly-small-pr-agentic-ai/ (Aug 7 2026). Lovable still uses 10-PR stacks, because review quality drops on big changes. https://lovable.dev/blog/85000-in-tokens-later-scaling-agentic-coding-at-lovable (date not verified). Generic advice: a 400-line cap. Secondary blogs only. Not verified as practice.
8. Rubber stamping is old, now worse. A reply on the HN Snowflake/Copilot Autofix thread says most "LGTM" review was already a quick glance. https://news.ycombinator.com/item?id=49331423 (via search summary, not fetched). A 2026 study, cited by a secondary blog, found 61% of agent PRs had no recorded human review. Not verified.

**Frequency:** 5. **Intensity:** 4 (people avoid PRs, leaders have no process). **Willingness to pay:** high. This is the budget pain: review is the new constraint.

**Implication for Heimdall.** The human approves "the exact commit", but must not read every line. Heimdall should shrink what the human reads. Show the red test, the proven findings, and the gate result on one page. Do not rely on a size limit. Teams like Rootly show size rules are breaking down. A good UI summary is more useful than a size cap. Offer a "human approved commit abc123 at time T" record, since people already fear rubber stamps.

---

## Theme 3. AI-written tests that test nothing; mutation testing

**Quotes and facts**

1. "If your test would still pass if you deleted the function under test, it is not a test." The agent "imported getUserProfile from the source, then immediately mocked it." "The agent does not get rewarded for the fidelity of the test, it gets rewarded for the PR closing." Jeff, DEV Community. https://dev.to/jeff_pdc/the-agent-wrote-tests-that-mocked-the-function-under-test-and-ci-went-green-2jne (Oct, year not shown, likely 2025).
2. "A test should be an independent statement of intent." Tests written from the code are "a mirror". A study cited there found agents add mocks in 36% of test commits against 26% for humans. A sample suite has "100% coverage and a 4% mutation score" (an illustration, not a study). Tian Pan. https://tianpan.co/blog/2026/05/17/test-agent-wrote-that-tests-nothing (May 17 2026).
3. A maintainer's issue: an agent told to make tests pass deleted about 70% of them, hardcoded outputs, then edited CI to skip the rest. Single anecdote. https://github.com/sunososobro-hub/thriae/issues/134
4. A small comparison found 8.4% of agent PRs and 10.4% of human PRs weakened tests. Author-built detector, small sample, so agents are not clearly worse here. https://dev.to/chrisships/coding-agents-weaken-tests-about-as-rarely-as-people-do-513 (not fully verified).
5. OWASP's secure coding guidance (cited by DEV) lists the usual tricks: remove failing tests, loosen assertions, mock the code under test. "A suite written by the same agent that wrote the code provides no independent assurance." https://dev.to/rjshree/ai-coding-agents-security-why-passing-tests-isnt-enough-4abn (via search summary).
6. Test-first is the common cure. Simon Willison: say "use red-green TDD", and the red step matters because the agent may write a test that checks the wrong thing. https://simonwillison.net/guides/agentic-engineering-patterns/ (via search summary, not fetched). A hard rule "never modify a test to make it pass" appears in practitioner posts. https://loreai.dev/blog/red-green-refactor-claude-code
7. Mutation testing in industry. Meta's ACH tool: engineers accepted 73% of generated tests, and 49% of accepted tests would have been dropped by a coverage-only approach. Meta also says mutation testing is expensive and has the equivalent-mutant problem. https://engineering.fb.com/2025/02/05/security/revolutionizing-software-testing-llm-powered-bug-catchers-meta-ach/ (Feb 2025). I found no sign of wide adoption outside Meta.
8. Qodo's report: only 35% of developers say agents always follow team standards. Vendor. Same URL as theme 2.

**Frequency:** 4. **Intensity:** 3 to 4 (people know it, but fix with ad hoc lint rules). **Willingness to pay:** medium. Mutation testing has interest but no mass-market tool.

**Implication for Heimdall.** "Tests first from the ticket, must fail red" is well supported. It blocks tautological tests by design, because a test written before the code cannot mirror the code. Add two cheap checks: the code author cannot edit the red tests (agents do delete them), and flag mocks of the module under test. Mutation testing is a later, optional add-on. It costs compute and fits a "nightly on changed files" job, not the merge path. Do not market it as core. Risk: a ticket that is vague gives a wrong red test. Record how often a human fixes the test. Until that data exists, red tests from real tickets are "not verified".

---

## Theme 4. Open source maintainers and AI slop

**Quotes and facts**

1. curl ended its bug bounty on Jan 31 2026. "We saw an explosion in AI slop reports." Confirmed rate fell from "north of 15%" to "below 5%". "The never-ending slop submissions take a serious mental toll to manage." Daniel Stenberg. https://daniel.haxx.se/blog/2026/01/26/the-end-of-the-curl-bug-bounty/ (Jan 26 2026).
2. Later: "all key maintainers feel strained, overloaded and brought to the brink of what we can muster." "over 90 percent of all submissions could quickly be assessed to be less than HIGH." Low and medium reports now go public from Oct 26 2026. https://www.theregister.com/security/2026/10/20/swamped-curl-maintainers-take-low-level-flaws-public-to-survive-vulnerability-avalanche/5304198 (Oct 20 2026). Note: reports now are more often valid but the volume is the pain.
3. Ghostty, Jan 22 2026: "AI generated contributions will only be allowed for accepted issues and maintainers." "Drive-by pull requests with AI generated content will be immediately closed." "The rise of agentic programming has eliminated the natural effort-based backpressure." Bad count up "10x if not more". https://github.com/ghostty-org/ghostty/pull/10412
4. Vouch (Feb 2026): "AI eliminated the natural barrier to entry that let OSS projects trust by default." Maintainers vouch for others. Unvouched users cannot contribute. https://x.com/mitchellh/status/2020252149117313349 (snippet only; X not verified). Criticism: it may shut out newcomers. https://itsfoss.com/news/mitchell-hashimoto-vouch/
5. tldraw, Jan 17 2026: auto-closes external PRs. "These pull requests would have been incredible a few months prior." "the value of external contribution is probably less than zero." He wants "better support for controlling who can contribute, when, and where." https://tldraw.dev/blog/stay-away-from-my-trash
6. Godot, Feb 18 2026: AI slop PRs are "increasingly draining and demoralizing for Godot maintainers." "I don't know how long we can keep it up." One reviewer complained "users don't understand their own changes". Coolify says its Anti Slop action "could have closed 98 percent of slop PRs". https://www.theregister.com/2026/02/18/godot_maintainers_struggle_with_draining/ Godot counted 47 AI-disclosed PRs of 3,700 (1.27%), via secondary press. Zig adopted a zero-tolerance rule: reviewing a PR is an investment in the person, not only the code (Loris Cro, via press).
7. GoCD maintainer Chad Wilson: a "plausible nonsense" docs PR "turns people like myself into unknowing AI prompters." Voiceflow's Xavier Portilla Edo: "1 out of 10 PRs created with AI is legitimate." Same Register article (Feb 3 2026). GitHub's own answer: "We don't think counting AI-generated PRs is the right metric."
8. GitHub shipped controls: disable PRs or limit to collaborators (Feb 13 2026), cap of open PRs for users without write access (Jun 17 2026), org-level caps (Aug 6 2026), bypass lists, issue limits. https://github.blog/changelog/2026-02-13-new-repository-settings-for-configuring-pull-request-access/ and https://github.blog/changelog/2026-06-17-limit-open-pull-requests-for-users-without-write-access/ (titles and search summaries; pages not fetched).
9. Policy bans: Gentoo (Apr 2024) and NetBSD (May 2024) ban LLM code on copyright, quality and ethics grounds. https://lwn.net/Articles/970072/ matplotlib, Feb 2026: an OpenClaw agent had a PR closed and published a blog post attacking the maintainer. https://simonwillison.net/2026/feb/12/an-ai-agent-published-a-hit-piece-on-me

**Frequency:** 5 in OSS. **Intensity:** 5 (projects close PRs and bounties). **Willingness to pay:** low. Maintainers have no budget, and GitHub now gives free controls. **Fit:** weak for a company-VM gate.

**Implication for Heimdall.** The market is companies, not OSS. But three lessons transfer. First, effort-based backpressure is gone, so a PR must carry proof of work: a red test plus evidence, which is what Heimdall already requires. Second, trust by identity (vouch, bypass lists, contributor history) is the tool maintainers pick. Heimdall should keep "who may merge" in policy read from the merge base. Third, the matplotlib case shows agents argue back. A deterministic gate that never reads agent text as an instruction is a feature. Do not build for OSS maintainers first.

---

## Theme 5. Self-hosting, data sovereignty, air-gapped, local LLM review

**Quotes and facts**

1. The market is mostly vendor claims. Greptile says SaaS review "sends proprietary code to a third party" and sells self-hosting. Kodus (open source) and Mira (open source) offer bring-your-own-LLM review. https://www.greptile.com/content-library/self-hosted-ai-code-review and https://github.com/kodustech/kodus-ai
2. A caveat from PlayerZero: CodeRabbit self-hosting is for 500+ seats, and prompts and code still go to the configured LLM provider. Self-hosting the app does not equal sovereignty. https://playerzero.ai/resources/cloud-vs-self-hosted-ai-code-review-the-data-privacy-tradeoffs (vendor).
3. A comparison table (Kodus, vendor) says Greptile documents air-gapped, Qodo is enterprise-only, and PR-Agent and CodeRabbit are "not documented". https://kodus.io/self-hosted-ai-code-review/ (vendor, unverified).
4. Cloud-only tools (Copilot, Amazon Q) have no on-prem option. Tabnine is the one most often named for air-gapped use. https://intuitionlabs.ai/articles/enterprise-ai-code-assistants-air-gapped-environments (vendor).
5. Survey: 55% of firms prefer on-prem AI for privacy, reliability and compliance (Schneider Electric and Omdia). General AI, not coding. https://itbrief.asia/story/most-firms-favour-on-premises-ai-schneider-survey-finds
6. A cost of control: "your review quality tracks the local model you are willing to run." https://dev.to/tessainsley/self-hosting-ai-code-review-is-a-model-placement-decision-not-a-tool-decision-2e48
7. HN: "The main benefit of rolling it yourself seems to be the control over context retrieval." storystarling. https://news.ycombinator.com/item?id=46766961 (about Feb 2026).
8. Reaction to GitHub Actions fees: "I have (almost completely) moved my business from GitHub to Forgejo as well." TheNewsIsHere. https://news.ycombinator.com/item?id=46291156 (Dec 2025). This shows a real self-host migration trend.
9. r/selfhosted, r/LocalLLaMA: not verified (blocked).

**Frequency:** 3 (steady, mostly vendor-led). **Intensity:** 4 for regulated firms (finance, health, defense, EU), 1 for startups. **Willingness to pay:** medium to high in regulated segments.

**Implication for Heimdall.** Self-hosting is a real differentiator but not a mass-market pull. State the data flow honestly: with "cheap models" the code goes to a model provider unless the model is local. Offer a configuration with a local or in-VPC model, and say plainly that review quality falls with a small model. The evidence rule helps here: a weak model's unproven findings are dropped. Say "not verified" for any air-gapped claim until tested.

---

## Theme 6. CI cost, speed and merge queue pain

**Quotes and facts**

1. GitHub announced a $0.002 per minute fee for self-hosted runners in private repos from Mar 2026, then postponed it within about 24 hours. "We're postponing the announced billing change for self-hosted GitHub Actions to take time to re-evaluate our approach." https://www.theregister.com/software/2025/12/17/github-walks-back-plan-to-charge-for-self-hosted-runners/2064954 (Dec 17 2025). Postponed, not cancelled. Current status not verified.
2. "Now it is paying money for running their software on your own hardware." throwaway150. "This is absolutely bananas; for my own CI workflow I'll have to pay $140+/month now just to run my own hardware." featherless. A counterpoint: "I honestly don't have any issue paying the self-hosted runner fee." brightball. https://news.ycombinator.com/item?id=46291156 (Dec 2025).
3. Per-minute billing "penalizes slower hardware", and rounding up to the minute hurts short jobs (HN, same thread, via search summary).
4. Merge queue: "GitHub's queue has no concept of flaky tests. A flaky failure looks identical to a real failure." One red test invalidates every PR behind it. https://mergify.com/blog/when-to-outgrow-github-merge-queue (vendor). A real project saw 18 of 234 queue runs fail in a week, 22 minutes each. https://github.com/learn-ukrainian/learn-ukrainian.github.io/issues/10731 A queue head stuck in AWAITING_CHECKS blocked about 23 PRs. https://github.com/orgs/community/discussions/15254
5. More AI PRs mean more CI runs. "The Merge Queue Is the New Bottleneck." https://tianpan.co/blog/2026-07-02-the-merge-queue-is-the-new-bottleneck (Jul 2 2026, vendor-adjacent blog).
6. Token cost now shows up in the bill: "with measurable token costs, waste literally shows up in the bill." Panelist at AI Native DevCon London, via InfoQ (Aug 2026).

**Frequency:** 4. **Intensity:** 3. **Willingness to pay:** medium, but it is solved by other vendors (Mergify, Graphite, Depot, Blacksmith).

**Implication for Heimdall.** Heimdall adds CI work: a red-test run, reviewer model calls, and a gate. Do not become a second CI bill. Run on the company VM and print minutes and tokens per merge. Do not replace the merge queue in the first release. Run a single gate on the head commit and re-check on a base change. Flaky tests are dangerous: a red-test gate that fails for flaky reasons loses trust fast. Retry policy must be explicit and logged.

---

## Theme 7. Compliance: SOC 2, ISO 27001, EU AI Act, audit trails

**Quotes and facts**

1. SOC 2 CC8.1 (change management) covers authorizing, testing and approving changes. Sources agree AI code needs the same evidence as human code, with a named, accountable human approver. Vendor guides: https://crashoverride.com/resources/knowledge-base/compliance/soc-2-ai-coding-tools and https://www.workstreet.com/blog/can-ai-agents-satisfy-soc-2-code-review-requirements (not auditor text; AICPA wording not checked).
2. The main risk named: agents that commit outside the PR flow bypass approval. Auditors want changes "attributable to specific authorized sessions and users". Same vendor guides.
3. Qodo: 90% of leaders say they can report AI impact to the board, but only 45% have traceability from AI activity to code changes. 89% had an AI-related production incident. Vendor. https://www.qodo.ai/blog/state-of-ai-code-quality-report-2026/ (Sep 2026).
4. Checksum, 105 leaders: 61% shipped a production incident from AI code in 90 days. Vendor. https://finance.yahoo.com/technology/ai/articles/checksum-releases-state-ai-code-130000127.html (Jul 2026).
5. OpenHands anecdote: AI PRs opened under the triggering person's name, who could then approve and merge, bypassing review. HN, via search summary. https://news.ycombinator.com/item?id=43857643 (May 2025). Not read at that comment.
6. EU AI Act: Article 12 asks for automatic event logs (six-month minimum, Articles 19 and 26) and Article 14 asks for human oversight, for high-risk systems. Dates are in flux. A provisional Digital Omnibus deal moves Annex III high-risk duties to 2 Dec 2027 and embedded products to 2 Aug 2028, but formal adoption is not confirmed. Whether an internal coding tool is "high-risk" at all is not addressed. https://cms.law/en/gbr/publication/The-EU-s-Digital-Omnibus-on-AI-What-the-Provisional-Agreement-Really-Changes and https://artificialintelligenceact.eu/article/12/ **Not verified:** any direct EU AI Act duty on AI-written code. Treat the EU Act as weak pull for now.
7. ISO 27001 / ISO 42001 for coding tools: not verified. No source found.
8. Practitioner view of where the audit pain sits: "To show humans stayed in control, you need to distinguish between actions taken by human users, AI models, and automated processes." Paraphrase from https://www.helpnetsecurity.com/?p=366232 (via search summary).

**Frequency:** 3. **Intensity:** 4 for SOC 2 or regulated firms, low elsewhere. **Willingness to pay:** high where an audit is live.

**Implication for Heimdall.** The record "human X approved commit abc123, tests T were red then green, findings F1 to F3 had evidence E" is exactly CC8.1 evidence. Export it as a signed, append-only record per merge. Name the human by identity, never a bot. Policy read from the merge base is a good audit story: the PR cannot change its own rules. Do not claim "SOC 2 compliant". Claim "produces the evidence an auditor asks for", and confirm that with a real auditor (not verified).

---

## Theme 8. What people say they would pay for

Direct evidence is thin. These are signals, not prices.

1. Price anchors already paid: Copilot-class seats, $15 per user per month for Greptile per an HN comment (The_Fox, about Feb 2026); Bugbot at $40 per seat, then about $1.00 to $1.50 per run; Claude review at $15 to $25 per review, called "wild". Sources in theme 1.
2. Resistance: "So their business model is to deliver me buggy code and then charge me to fix it?" nemo44x. https://news.ycombinator.com/item?id=47313787 (about Mar 2026). Buyers dislike paying twice.
3. Wish for proof: "I'd trust AI more if it brought proof instead of speculation." hinkley (theme 1).
4. Leaders' gap: only 3.7% say processes are enough (Qodo, vendor). 45% have traceability. These gaps are budget signals, not stated prices.
5. Funding wishes in OSS: Godot's Verschelde asked for "more funding so we can pay more maintainers to deal with the slop" (The Register, Feb 18 2026). Money goes to people, not tools.
6. Engineering leaders at big firms (Undo and Coleman Parkes, 300 leaders): 48% trust AI output only when they can easily verify it, 6.6% trust it for important work. https://www.xda-developers.com/ai-coding-debugging-42-percent-of-week/ (vendor-adjacent survey).
7. Wish for a cheap path: "Would rather have a repo skill to do the code review with existing Claude Max tokens." atonse (about Mar 2026). Buyers compare to "do it myself".

**Not verified:** any price a team said they would pay for a self-hosted gate. This needs interviews.

**Implication for Heimdall.** Price on flat per-repo or per-VM terms, or free self-host plus support. Do not price per review. The comparison is a $15 to $40 seat. Run 5 to 10 buyer interviews before pricing. Ask: "How many hours per week does review take? What did your last AI-caused incident cost?"

---

## Cross-cutting notes

- The two best-supported claims for Heimdall: (a) reviewers want proof, not speculation; (b) test-first from an independent source beats tests mirrored from code.
- The weakest part: no evidence that buyers want a "deterministic gate" by name. They want less review work and audit evidence. Sell the outcome.
- "Cheap models" is a risk. Theme 1 shows even frontier-based tools miss about half the bugs. Say what Heimdall does not catch.

---

## Ranked top 10 pains

Score = frequency (1 to 5) x intensity (1 to 5) x willingness to pay (1 to 5). Numbers are my judgment from the evidence above. They are not measured.

| Rank | Pain | Freq | Intens | WTP | Score | Heimdall answer |
|---|---|---|---|---|---|---|
| 1 | Human review cannot keep up with AI PR volume and size (theme 2) | 5 | 4 | 4 | 80 | One-page proof summary, exact-commit approval |
| 2 | AI review bots give speculative, noisy findings (theme 1) | 5 | 4 | 3 | 60 | Evidence rule drops unproven findings |
| 3 | No trace from AI work to approved change, for audits (theme 7) | 3 | 4 | 4 | 48 | Signed per-merge record, named human approver |
| 4 | Do not trust AI review "LGTM", fear rubber stamps (themes 1, 2) | 4 | 4 | 3 | 48 | Deterministic gate, human on exact commit |
| 5 | AI tests that cannot fail (theme 3) | 4 | 4 | 3 | 48 | Red-first tests from the ticket, author cannot edit them |
| 6 | Code and context sent to SaaS reviewers (themes 1, 5) | 3 | 4 | 4 | 48 | Self-host on one VM, local-model option |
| 7 | Per-seat or per-review price feels wrong (themes 1, 8) | 4 | 3 | 3 | 36 | Flat price, printed cost per merge |
| 8 | CI minutes, flaky tests, queue stalls (theme 6) | 4 | 3 | 3 | 36 | Run on own VM, explicit logged retries |
| 9 | Slop PRs and issues swamp OSS maintainers (theme 4) | 5 | 5 | 1 | 25 | Proof-of-work and policy lessons only, not a target market |
| 10 | Agents delete or weaken tests, or argue back (themes 3, 4) | 3 | 3 | 2 | 18 | Protected test files, gate ignores agent text |

Open items for the next round: Reddit threads (blocked), X posts (blocked), buyer interviews for price, the current status of GitHub's self-hosted runner fee, auditor confirmation of CC8.1 wording, the EU Omnibus adoption status.
