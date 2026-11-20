# 09. Context packs: personal engineering memory as a product

Status: research, Nov 21 2026. Read only in the repo, web research, no code changed, nothing run against a model.
Every number carries a source link and a date, or says `not verified`. "Accessed" means the page was read on
Nov 21 2026 and shows no date of its own. Vendor numbers are vendor numbers. Most pricing pages were seen through
search snippets and third-party trackers, not loaded directly, so treat each price as `not verified` until someone
checks the vendor page.

## 0. The answer in ten lines

1. The idea is real, and the market already sees it. A free memory plugin for Claude Code has 118,595 GitHub stars.
   GitHub made Copilot Memory generally available on Oct 28 2026. SageOx raised $15M in May 2026 for team context on
   laptops. "Local memory for coding agents" alone is a crowded and free category.
2. Storing a rule does not make an agent follow it. One study found agents with Mem0 still broke 57.5% of the
   preferences that applied to them. The gap that pays is **enforcement with proof**, not memory.
3. Heimdall already owns the enforcement half: rule cards, a ranker, E3 for confirmed cards, a learning loop. It
   lacks the capture half: no OpenCode or Cursor parser, no signing, no approval UX, nothing live.
4. Recommended shape: **(c) one product, two doors.** A free, open-source local CLI and MCP server
   (`heimdall memory`) mines sessions, redacts, and asks the developer to approve rule cards. Any agent reads them
   while it writes code. The paid Heimdall server signs, scopes, enforces and measures those same cards at the merge
   gate.
5. Do not sell "memory" as its own SKU. Sell "your senior engineers' rules, checked on every PR, with the incident
   and the proof attached". Memory is the free wedge. Enforcement and audit are what a lead pays for.
6. Who pays: the platform or engineering lead at a 30 to 500 developer team that already uses coding agents. Price
   it inside the existing Heimdall tiers (proposal: Team $15, Regulated $35 per active committer per month).
7. Moat, honestly: mining is not a moat (five open tools do it). The moat is (a) cards bound to code ownership at the
   merge base, (b) outcome data per card from real reviews, (c) signed provenance that an auditor can verify, and
   (d) reading laptop sessions that hosted tools cannot see.
8. The biggest risk is trust. In Oct 2026 claude-mem's installer uploaded 30 days of prompts with no opt-in. In
   Germany any tool that can monitor staff needs the works council, and EU AI Act rules for worker monitoring apply
   from Dec 2 2027. Per-person scores must never reach a manager dashboard.
9. Deploy side: **FITS, narrowly**. Release evidence and agent-run smoke checks fit "hand over work with proof".
   Canary control and rollback do not. Harness, Argo Rollouts, LaunchDarkly and a well-funded AI SRE field own them.
10. Next step: build the capture half to the bar of section 5, run it on real history (Proof Bench), and only then
    put it on the landing page.

---

## 1. What Heimdall's personal context pack is today

Read from `docs/REVIEW_MOAT.md`, `docs/CONTEXT_CAPTURE_PLAN.md`, `docs/KNOWLEDGE_TOPOLOGY.md`, `docs/TECH_SPEC.md`
sections 9 and 10, `internal/cardgen/doc.go`, `internal/mine/doc.go`, `docs/product/STRATEGY.md` and the code
in `cmd/heimdall/cards*.go`.

1. **The unit is a rule card, not a pack.** Cards live as files in `<cards_dir>/<person>/`. Card v2 has
   `applies_to`, `symbols`, `triggers`, one `check`, a `proof` hint and a `status` (`internal/domain/rulecard`).
2. **Built: capture by the developer's own agent.** `heimdall cards scan` reads Claude Code and Codex sessions plus
   git on the laptop and writes `signals.md`. `cards prompt` (`internal/cardgen`) hands a prompt to the agent the
   developer already uses, which writes the cards. Scan found 25 signals on this repo after it moved to recall first.
3. **Built: the card commands.** `check`, `preview`, `push`, `pull`, `inbox`, `accept`, `reject`, `confirm` and
   `unconfirm` exist. They ran against test servers only. Human tokens are not wired in the VM compose, so `push`
   and `confirm` answer 401 there (`docs/PROGRESS.md`).
4. **Built: delivery to the reviewer.** The brief's `personal_context` and the `read_card` tool serve a reviewer only
   its own person's cards, wrapped as untrusted, read through `os.Root`.
5. **Built: cards can block.** A confirmed card reaches E3 only if it is active, has `applies_to` and `check`, its
   text hash is unchanged, its owner approves that path at the merge base, and outcomes have not muted it.
6. **Built: learning.** `internal/app/reviewlearn` keeps a Beta posterior per card, mutes after 3 or more
   dismissals, and turns a dismissal reason into a proposed card that its owner must accept.
7. **Removed: the encrypted pack.** TECH_SPEC section 10 (pack folder, ed25519 manifest, `age` encryption,
   `read_pack`) was removed on Nov 16 (H9). `internal/pack` is gone. No signing exists in the code today.
8. **Plan only:** OpenCode and Cursor parsers (scan lists `opencode.db` as a pointer, it does not parse it), the POV
   profile, the interview, the `context-author` skill, `propose_card` over MCP, repo-scoped cards and `cards whoami`.
9. **Measured once, synthetically.** One planted-bug PR, Haiku reviewers, 3 per arm: a card helped only its holder
   on the one-person bug (1/3). The code graph did more (G1 and G2 went from 0/3 to 3/3). Not run on the VM.
10. **Live state:** the VM reviewers do not map to people in the fixture policy, so live reviewers get no cards.
    `STRATEGY.md` section 7 lists packs as "a later moat, not the headline".

---

## 2. State of the art, 2025 to 2026

### 2.1 Where the raw material lives

| Agent | Location and format | Notes |
|---|---|---|
| Claude Code | `~/.claude/projects/<encoded-path>/*.jsonl`, one file per session, subagents in subfolders | Deleted after `cleanupPeriodDays`, default 30, minimum 1 ([Claude docs](https://code.claude.com/docs/en/claude-directory), accessed). Users report early loss even with a high value ([issue](https://github.com/anthropics/claude-code/issues/2543)). Auto memory writes `MEMORY.md` under the same project folder and loads the first 200 lines or 25 KB ([archcore](https://archcore.ai/blog/claude-code-memory/), accessed; release version `not verified`) |
| Codex | `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` | Format varies by version (TECH_SPEC 10.2). Already parsed by `cards_sessions.go` |
| OpenCode | `~/.local/share/opencode/opencode.db`, SQLite since v1.2.0; old JSON files kept | ([changelog v1.2.0](https://learnopencode.com/en/changelog/v1.2.0)). One user saw 2.4 GB after a month ([dev.to](https://dev.to/whtsky/garbage-collecting-opencodes-24-gb-session-database-7b5)) |
| Cursor | `globalStorage/state.vscdb`, SQLite, table `cursorDiskKV`, keys `composerData:<id>` and `bubbleId:*` | No official export or API. Updates have wiped or hidden history; one file grew to 97 GB ([forum](https://forum.cursor.com/t/state-vscdb-grew-from-3-1-gb-97-gb-macos/166013), [forum](https://forum.cursor.com/t/update-wiped-agent-chat-history-cursordiskkv-agents-window-empty-restore-crashes/168900)). Undocumented, so it breaks |
| SpecStory | `.specstory/history/*.md` per repo, or `~/.specstory/history` via `specstory run claude` | Local Markdown from many agents ([docs](https://docs.specstory.com/integrations/terminal-coding-agents), accessed). A cheap extra source: if a team already runs it, read its Markdown |
| Windsurf / Devin Desktop | `~/.codeium/windsurf/memories/`, rules in `.windsurf/rules` (now `.devin/rules`) | Memories are local and per workspace ([docs](https://docs.windsurf.com/windsurf/cascade/memories.md), accessed) |

**Trace standard.** The OpenTelemetry GenAI conventions define `invoke_agent` and tool spans with
`gen_ai.agent.name`, `gen_ai.tool.name` and `gen_ai.tool.call.id`. They moved to their own repository with release
v1.42.0 on Jun 12 2026 and are still experimental, with no 1.0 ([dev.to summary](https://dev.to/gabrielanhaia/opentelemetry-genai-semantic-conventions-your-llm-traces-should-look-like-this-in-2026-3ff6),
2026; `not verified` against the spec repo). Use their attribute names in our normalized record so a later OTel
export is a mapping, not a rewrite. Do not wait for agents to emit OTel; none of the four above writes it to disk.

**Takeaway.** Two of the four sources are SQLite. Go can read them with `modernc.org/sqlite`, which needs no cgo, so
the `CGO_ENABLED=0` build stays. Open the file read-only and copy it first, because the agent may hold a write lock.

### 2.2 Extracting durable rules from sessions

| Work | What it does | Result | Lesson for us |
|---|---|---|---|
| TellOnce, [arXiv 2606.13174](https://arxiv.org/abs/2606.13174) (Jun 11 2026) | Turns corrections into scoped, revisable rules, reconciles new feedback, and prepares checks where it can verify | Agents with Mem0 still broke **57.5%** of applicable preference checks. TellOnce cut violations by 19.3 to 42.3 points on ClawArena vs PGMem | Remembering is not obeying. Each rule needs a scope and, where possible, a check. This is the Heimdall card design |
| Personalized skills, [arXiv 2608.10319](https://arxiv.org/abs/2608.10319) (Aug 10 2026) | Rule bootstrap plus LLM refinement over real developer traces, replayed with a simulated developer | Task-specific skills beat personal ones overall. Personal skills help only when the preference recurs often | Rank by repeat count. A one-off preference is noise. Incidents are the exception, because their cost is high |
| Misalignment study, [arXiv 2605.29442](https://arxiv.org/abs/2605.29442) (May 28 2026) | 20,574 real sessions from 1,639 repos | **91.49%** of visible resolutions needed an explicit user correction. Constraint violations grow in share over time | The corrections we mine exist at scale. Rule-following is the failure that is growing |
| DECODE, [arXiv 2607.25130](https://arxiv.org/abs/2607.25130) (Jul 27 2026) | 53.6K developer edits of AI code from 1K+ developers | Developers edit little and abandon AI code fast | Silent edits and reverts in git are a second signal next to chat corrections |
| Self-evolving coding agents survey, [arXiv 2608.03392](https://arxiv.org/abs/2608.03392) (Aug 4 2026) | Taxonomy of what agents update: memory, skills, workflow, context | Names feedback reliability, overfitting and reversibility as the open problems | Keep every learned item reversible and logged |
| shadowclone ([repo](https://github.com/theonly1me/shadowclone), 26 stars, accessed) | Reads Claude Code, Codex, Cursor, Pi, Antigravity sessions, finds corrections, writes skills | Every source off by default with its own consent | Copy the consent model |
| A 30-day self-mining write-up ([barazany.dev](https://barazany.dev/blog/mining-claude-history-ultracode), accessed) | Mined 2,089 typed messages, kept 23 global rules | Anecdote, one person | Expect roughly 1 rule per 100 messages. `not verified` beyond one user |

### 2.3 Memory systems

| System | Model | Scale signal | Fit for us |
|---|---|---|---|
| claude-mem ([repo](https://github.com/thedotmack/claude-mem)) | Claude Code hooks; Haiku compresses each tool call into an observation; injects 50 at start | **118,595 stars**, Apache-2.0 (GitHub API, Nov 21 2026) | Shows demand. It stores what happened, not rules, and the Oct 2026 upload incident shows the trust risk (section 4) |
| Mem0 / OpenMemory ([repo](https://github.com/mem0ai/mem0)) | Extract and store facts; hosted MCP | 69,283 stars (Nov 21 2026). $24M raised by Oct 2025. Hobby free, Starter $19, Pro $249 per month ([theaiagentindex](https://theaiagentindex.com/agents/mem0), 2026). Local OpenMemory reported removed on Jul 29 2026 ([mnemoverse](https://mnemoverse.com/docs/library/memory-mcp-servers-compared), 2026, `not verified`) | Generic. The TellOnce result above is measured on it |
| Zep / Graphiti ([repo](https://github.com/getzep/graphiti)) | Temporal knowledge graph; facts carry valid and invalid times | 32,963 stars (Nov 21 2026). Zep self-reports 94.7% LoCoMo and 90.2% LongMemEval ([particula](https://particula.tech/blog/agent-memory-frameworks-tested-mem0-zep-letta-cognee-2026), 2026, vendor numbers) | Already Heimdall's team memory (TECH_SPEC 9). Right for team facts. Too heavy for a laptop |
| Letta (MemGPT) | Agent manages its own memory blocks and files | 25,505 stars. 74.0% LoCoMo with filesystem tools, Aug 12 2025 (same source) | Self-editing memory is the wrong default when a human must approve |
| Cognee | Graph plus vector pipeline | 33,433 stars; $7.5M seed ([cognee](https://www.cognee.ai/cognee-raises-seven-million-five-hundred-thousand-dollars-seed), 2026) | The fallback behind the memory port (TECH_SPEC 9.5) |
| Supermemory | Hosted memory API and plugins for Claude Code, Cursor, Codex, OpenCode | 31,675 stars. Pro $19 per month ([toolradar](https://toolradar.com/tools/supermemory/pricing), 2026, `not verified`) | Hosted; code leaves the laptop |
| Basic Memory | Markdown files plus MCP, local | 4,647 stars, AGPL-3.0 (Nov 21 2026) | Closest to our "cards are files" shape |
| ByteRover | File-based local memory with git-like versioning, team cloud | Elastic License 2.0. Pro $14.90, Team $149 per month ([saasworthy](https://www.saasworthy.com/product/byterover-dev/pricing), Oct 2026, `not verified`) | Direct competitor for team sync |
| deja-vu ([repo](https://github.com/vshulcz/deja-vu)) | Search old sessions, sync over SSH | 1,638 stars; 131 points on HN, Jul 15 2026 ([HN](https://news.ycombinator.com/item?id=48923111)) | Proves the "sessions are a knowledge base" instinct |

**Benchmarks do not transfer.** Mem0's LongMemEval number fell from a claimed 93.4% to 73.8% under another vendor's
harness ([particula](https://particula.tech/blog/agent-memory-frameworks-tested-mem0-zep-letta-cognee-2026), 2026).
LongMemEval and LoCoMo test chat recall, not "did the agent follow the rule". MemoryAgentBench
([arXiv 2507.05257](https://arxiv.org/abs/2507.05257), ICLR 2026) adds selective forgetting (overwrite on
contradiction) and finds no current system masters all four skills. None of these benchmarks measures code review.
We must build our own eval (section 5.9).

### 2.4 Reflection and learning loops

| Work | Mechanism | Number | Use |
|---|---|---|---|
| ACE, [arXiv 2510.04618](https://arxiv.org/abs/2510.04618) (Oct 2025, ICLR 2026) | Generator, reflector, curator. Small itemized edits to a playbook, never a full rewrite | +10.6% on AppWorld; matched the top production agent with DeepSeek-V3.1; 82.3% less latency than GEPA offline ([repo](https://github.com/ace-agent/ace)) | Cards are ACE playbook bullets. Never let a model rewrite the whole card set ("context collapse") |
| Dynamic Cheatsheet, [arXiv 2504.07952](https://arxiv.org/abs/2504.07952) (EACL 2026) | Keeps short reusable heuristics, not transcripts | Claude 3.5 Sonnet more than doubled on AIME; +9% GPQA-Diamond | Short cards beat long context |
| Voyager, [arXiv 2305.16291](https://arxiv.org/abs/2305.16291) (2023) | A skill library of verified code, added only after it works | Classic | A card enters only with evidence |
| Bugbot learned rules ([changelog](https://cursor.com/changelog/04-08-26), Apr 8 2026) | Reactions, replies and human comments become candidate rules; promoted on positive signal, disabled on false positives | 110,000+ repos, 44,000+ learned rules, 78% resolution rate (vendor) | Our `reviewlearn` loop is the same shape, but per person, not per repo |
| Greptile memory ([changelog](https://www.greptile.com/changelog), May 12 2026) | Learns from team comments, reactions, and whether issues were fixed between commits | Vendor, no number | Same |
| Copilot Dreaming ([changelog](https://github.blog/changelog/2026-10-28-github-intelligence-puts-your-code-context-and-history-to-work/), Oct 28 2026) | Reflects on past work; edits memories directly and opens a PR for `AGENTS.md` or skill changes | Preview, opt-in by admin and user | The PR-as-approval pattern is right. GitHub ships it for free |
| Feedback-normalized developer memory, [arXiv 2605.01567](https://arxiv.org/abs/2605.01567) (May 2 2026) | Logs every retrieval and its outcome; a learned ranker runs in shadow behind gates | Full config gave no accuracy gain over the deterministic ranker (80.0% both) | Keep the deterministic ranker. Log retrievals so a learned one can be judged later |

### 2.5 Privacy, redaction, poisoning, signing

- **Secrets first, PII second.** A practical 2026 stack is a deterministic secret scanner plus a PII detector plus a
  check of tool-call arguments ([particula](https://particula.tech/blog/presidio-vs-gliner-pii-redaction-llm), 2026).
  Presidio is the common PII default (11,989 stars, MIT, Nov 21 2026), but it is Python. In Go we already embed the
  gitleaks rules (`internal/secretscan`, go.mod `gitleaks/v8 v8.30.1`). Add entropy, PII regex with Luhn, and a
  team deny-list. Skip ML PII detection in v1; card text is short and the human reviews it anyway.
- **Drop tool outputs.** They hold most secrets and most noise. `cards_sessions.go` already drops them.
- **Memory poisoning is a named threat.** OWASP lists Memory and Context Poisoning as ASI06 in its 2026 Agentic Top
  10. A systematic study measured 50.46% average attack success, higher for agents that write memory aggressively,
  and found prompt-injection defenses do not cover it ([arXiv 2606.04329](https://arxiv.org/abs/2606.04329), Jun 2026;
  [summary](https://arxiv.org/html/2606.04329v1)). Cisco reported an npm package that rewrote Claude Code memory
  files (Apr 2026, secondary [summary](https://dev.to/suraj_khaitan_f893c243958/memory-poisoning-is-the-prompt-injection-that-survives-the-session-nfh),
  `not verified` at the primary). Our answer: only a human writes an active card, signed cards only, untrusted
  wrapper at read time, and a card can never widen its own scope.
- **Signing.** Sigstore's model-signing wraps an in-toto Statement in a DSSE envelope inside a Sigstore bundle, and
  supports keyless (OIDC), certificates or plain keys with one format ([Red Hat](https://next.redhat.com/2025/04/10/model-authenticity-and-transparency-with-sigstore/),
  Apr 2025). Offline verification needs the bundle to carry its inclusion proof. Use DSSE plus in-toto with an
  ed25519 key by default, and Sigstore keyless as an option for companies with OIDC.
- **Differential privacy** does not fit. A card is a handful of sentences that a named person approves and stands
  behind. Noise would make it wrong. Use consent, redaction and human review instead. (Judgement, not a source.)

### 2.6 Staleness and conflict

- Copilot memories are checked against the current code before use and expired after 28 days in preview
  ([changelog](https://github.blog/changelog/2026-01-15-agentic-memory-for-github-copilot-is-in-public-preview/),
  Jan 15 2026). A vendor blog reports 90 days at GA ([Unblocked](https://getunblocked.com/blog/github-agent-memory-only-knows-github/),
  `not verified`).
- CodeRabbit shows "may be stale" on learnings and says the model may ignore learnings that conflict
  ([docs](https://docs.coderabbit.ai/knowledge-base/learnings), accessed; [kb](https://kb.coderabbit.ai/articles/8897909738-what-do-i-do-if-learnings-appear-to-be-not-working)).
  Its `usage_count` is "best effort, not proof" that a learning applied.
- Graphiti invalidates contradicted facts with `invalid_at` (TECH_SPEC 9.1).
- **Our rule:** a card is stale when its symbols vanish at head, its globs match nothing, its `review_by` date
  passes, or it has not fired in 90 days. A card is superseded only by a newer card its owner approved. Two confirmed
  cards that contradict become a visible E4 and a question to both owners. A model never picks the winner
  (`CONTEXT_CAPTURE_PLAN.md` 3.6).

---

## 3. Who sells personal or team coding context today

| Product | What it captures | Scope | Price | Adoption signal | Weak spot |
|---|---|---|---|---|---|
| Claude Code `CLAUDE.md` and auto memory | Rules you write; notes Claude writes | Repo files shared via git; auto memory machine-local | In the subscription | `CLAUDE.md` posts reach 700+ points on HN ([HN](https://news.ycombinator.com/item?id=46098838), Nov 30 2025) | Auto memory stays on one laptop; no approval, no proof it was followed |
| Copilot Memory + Dreaming | Repo facts from agent work and review; reflection | Repo, shared across Copilot features | Included in paid Copilot; Dreaming billed in AI credits | GA Oct 28 2026 ([changelog](https://github.blog/changelog/2026-10-28-github-intelligence-puts-your-code-context-and-history-to-work/)) | Knows only GitHub ([Unblocked](https://getunblocked.com/blog/github-agent-memory-only-knows-github/), vendor). No person model. No laptop sessions from other agents |
| Cursor rules, Team Rules, Bugbot learned rules | Hand-written rules; review reactions | Project, team dashboard, repo | Team Rules on Team and Enterprise plans | Bugbot: 110,000+ repos, 44,000+ rules (Apr 8 2026) | Cursor removed Memories in v2.1 (Nov 2025) ([forum](https://forum.cursor.com/t/are-my-memories-gone/144057)). Project rules do not reach Bugbot |
| Windsurf / Devin Desktop memories | Auto memories | Workspace, local only | Free, no credits | `not verified` | Docs tell you to write rules instead for reliable reuse |
| CodeRabbit learnings | Chat replies to the bot | Org | $24 per dev per month annual entry tier; $48 mid tier ([macroscope](https://macroscope.com/content/coderabbit-pricing-2026), 2026, `not verified`) | `not verified` | Stale learnings, ignored on conflict, usage "best effort" |
| Greptile memory | Comments, reactions, fix-between-commits | Team | Free individual tier from Jun 29 2026; Pro $1 per credit ([withorb](https://www.withorb.com/pricing-atlas/greptile), Nov 4 2026) | `not verified` | Repo signal only |
| Augment Context Engine | Code index, standalone MCP since Feb 6 2026 | Repo, cross-repo hosted | Standard $20, Business $100 per month for up to 50 seats, plus a 40% fee on model usage ([repowise](https://repowise.dev/blog/comparisons/augment-code-pricing), 2026, `not verified`) | `not verified` | Code, not people |
| Unblocked | Code, PRs, Slack, Jira, docs; MCP for agents | Org | Platform $29 per user per month annual; Code Review $19 ([pricing](https://getunblocked.com/pricing/), accessed) | $20M raised ([itechguides](https://www.itechguides.com/unblocked-raises-20m-for-ai-to-help-developers-understand-codebases/), date `not verified`) | Hosted; reads what is written down, not laptops |
| Tabnine | Context engine, "coaching guidelines" | Org | $39 and $59 per user per month ([eesel](https://eesel.ai/blog/tabnine-pricing), 2026, `not verified`) | Reported acquired by Tricentis Jul 2026 (one source, `not verified`) | Guidelines are hand-written |
| Swimm | Docs synced to code | Org | Custom, by lines of code ([makerstack](https://makerstack.co/reviews/swimm-review/), Sep 2026) | `not verified` | Docs, not rules; human writes them |
| Glean | Enterprise search incl. code | Company | Quote only; third-party estimate $50 to $75 per user per month, about 100-seat minimum ([atlan](https://atlan.com/know/ai-agent/data-for-ai/glean-alternatives/), `not verified`) | `not verified` | Not built for review |
| Sourcegraph | Cody now enterprise only, from $16K per year ([usagepricing](https://usagepricing.com/blueprint/activity/2026-06-08-sourcegraph-cody-enterprise-only), Jun 2026, `not verified`); Amp spun out Dec 2025 | Code search | as stated | `not verified` | Code, not people |
| Pieces | Local long-term memory of a developer's work | Person | Paid tiers $18.99 to $22.99 per user per month ([toolradar](https://toolradar.com/tools/pieces-app/pricing), Nov 2026, `not verified`) | `not verified` | Personal recall, no team, no enforcement |
| SageOx (Ox CLI) | Team conventions, decisions, recorded sessions; primes agents at session start | Team, versioned like code | Free to start; paid `not verified` ([blog](https://sageox.ai/blog/introducing-sageox)) | $15M seed led by Canaan, May 6 2026 ([BusinessWire](https://www.businesswire.com/news/home/20260506467841/en/Humans-and-AI-Agents-Are-Now-Coworkers-But-Not-Yet-a-Team.-SageOx-Raises-$15M-to-Fix-That.)) | **The closest competitor.** No sign of review enforcement, ownership scope, or proof |
| Mem0, Supermemory, ByteRover | Generic memory, coding plugins | Person or team | See 2.3 | See 2.3 | Generic; not code-review aware |

**What the table says.** Repo-level memory is now bundled for free by the platforms (GitHub, Cursor, Anthropic,
Windsurf). Team context for agents has one funded specialist (SageOx) and several small ones. Nobody in public
material does all four of: (1) capture from the laptop across agents, (2) per-person scope bound to code ownership,
(3) enforcement at the merge gate with proof, (4) a signed record. Heimdall can. Points 1 and 2 alone are not enough
to charge for, because free tools already do most of point 1.

---

## 4. Demand: is the gap real?

### 4.1 Evidence for

- **Knowledge silos cost time.** 68% of developers hit a knowledge silo at least weekly. For a 50-developer team,
  searching for answers costs 333 to 651 hours a week and answering others costs 278 to 568 hours
  ([Stack Overflow 2022 survey via CIO Dive](https://www.ciodive.com/news/knowledge-silos-software-retention-developers/630046/),
  2022; old but the most specific number found). 30% hit silos 10 or more times a week (2024 survey, Stack
  Overflow marketing [page](https://stackoverflow.co/teams/resources/your-developers-deserve-better-insights-from-the-2024-developer-survey)).
- **The senior reviewer is the bottleneck.** Teams with high AI adoption merge 98% more PRs while review time rises
  91% ([Faros 2025](https://www.faros.ai/blog/ai-productivity-paradox-report-2025), Jun 2025 data). The 2026 follow-up
  reports median review time up about 5x and 31% more PRs merged with no review
  ([Faros 2026](https://www.faros.ai/research/ai-acceleration-whiplash), vendor telemetry, correlational).
- **Trust in AI output is low.** 46% of developers distrust AI output accuracy, up from 31%
  ([Stack Overflow 2025](https://stackoverflow.co/company/press/archive/stack-overflow-2025-developer-survey/));
  only 3% report high trust ([DORA 2025 via InfoQ](https://www.infoq.com/news/2025/09/dora-state-of-ai-in-dev-2025),
  Sep 2025).
- **People already build this for themselves.** claude-mem at 118,595 stars. On HN: "Vibe coding creates a bus factor
  of zero" (223 points, Aug 20 2025, [HN](https://news.ycombinator.com/item?id=44966856)); Beads (111 points, Nov 28
  2025, [HN](https://news.ycombinator.com/item?id=46075616)); OKF Agent Memory (81 points, Sep 5 2026,
  [HN](https://news.ycombinator.com/item?id=49581240)); "Ask HN: Why do AI agents keep repeating mistakes your team
  already fixed?" (Mar 16 2026, [HN](https://news.ycombinator.com/item?id=47399209)). HN points read Nov 21 2026.
- **Money follows it.** SageOx $15M seed (May 2026). Copilot Memory GA (Oct 2026). Bugbot learned rules (Apr 2026).
  Platforms do not ship a feature to GA without usage.
- **The research says corrections are everywhere.** 91.49% of visible misalignment resolutions need an explicit user
  correction, across 20,574 sessions ([arXiv 2605.29442](https://arxiv.org/abs/2605.29442)).

### 4.2 Evidence against

- **"Nobody pays for memory."** The strongest products are free (claude-mem, Basic Memory, Windsurf memories) or
  bundled (Copilot Memory, Claude auto memory). Paid generic memory sits at $14 to $19 per month for a person.
- **Memory has a skeptic camp.** On HN people worry about "context bloat and poisoning" and ask for a "memory arena"
  because hundreds of projects make claims nobody compares ([HN thread on PMB](https://news.ycombinator.com/item?id=48720682),
  Jun 29 2026). Windsurf's own docs tell users to write rules rather than trust auto memories.
- **Personal rules help less than you think.** Task-specific skills beat developer-specific skills, unless the
  preference recurs often ([arXiv 2608.10319](https://arxiv.org/abs/2608.10319)). Our own bench: a personal card
  helped only its holder, 1 of 3 runs.
- **Privacy is a real fear, and one vendor just proved it.** claude-mem installer v15.2.0 and later uploaded the full
  text of the last 30 days of prompts, with project names and paths, to cmem.ai as soon as the setup page loaded,
  with no way to stop it ([issue #5176](https://github.com/thedotmack/claude-mem/issues/5176), opened Oct 29 2026,
  closed Nov 10 2026). That is exactly the fear a "mine my sessions" product triggers.
- **Labor law.** In Germany, §87(1) no. 6 BetrVG gives the works council co-determination over any technical system
  objectively able to monitor behavior or performance ([arkeontech](https://arkeontech.de/en/blog/betriebsrat-ki-einfuehrung-87-betrvg-2026/),
  2026, practitioner summary). The EU AI Act treats AI that monitors or evaluates workers as high risk (Annex III),
  now from **Dec 2 2027** after the Digital Omnibus, Regulation (EU) 2026/1744 ([usercentrics](https://usercentrics.com/knowledge-hub/eu-ai-act-high-risk-delay-article-50-transparency-consent/),
  2026, `not verified` against the Official Journal). A per-person "record" of dismissed findings is close to that
  line.
- **"My sessions are mine."** Sessions mix work, personal notes and frustration. Any product that sends them to an
  employer server will be refused by the people whose knowledge it needs. (Judgement, consistent with the sources
  above.)

### 4.3 Verdict

The demand is real for two narrow things and weak for the broad one.

- **Real:** (a) agents should stop repeating mistakes the team already fixed, and (b) review should not wait on the
  one senior who knows why. Leads feel (b) as a cost, which is where budget comes from.
- **Weak:** "capture my tacit knowledge" as a product of its own. Individuals will not pay, and free tools cover it.
- **Condition:** the developer must own the pipeline. Nothing raw leaves the laptop, every card is approved, and no
  manager sees a per-person score. Without that, works councils and senior engineers block it, and they are the
  people whose knowledge it needs.

So the honest pitch is not "we capture your seniors". It is "your seniors' rules get checked on every PR, with the
incident behind each rule, and they decide what is in it".

---

## 5. The sellable shape

### 5.1 Options

| Option | For | Against |
|---|---|---|
| (a) A feature inside Heimdall review only | Coherent; reuses the gate and E3 | Value shows only after the full server is installed. Install today is "an afternoon" (STRATEGY 3). No bottom-up adoption |
| (b) A standalone engineering-memory CLI and MCP server, sold on its own | Fast adoption; any agent reads it | Crowded and free (section 3). A second product for a small team. Competes with Copilot Memory on its home ground |
| **(c) One product, two doors (recommended)** | Free local CLI spreads bottom-up and feeds the paid server. The paid part is what nobody else has: ownership-scoped enforcement with proof | Must keep the CLI genuinely useful alone, or nobody installs it |

### 5.2 The recommended shape

- **Door 1, free and open source: `heimdall memory` (the existing `heimdall cards`, renamed for the outside world).**
  Runs on the laptop. Reads Claude Code, Codex, OpenCode and Cursor sessions plus git, each source opt-in. Redacts.
  Proposes rule cards with the incident behind each one. The developer approves. A local MCP server and generated
  files (`AGENTS.md` section, `SKILL.md`, `.cursor/rules/*.mdc`) give the approved cards to every agent the developer
  uses. Value on day one: the developer's own agents stop repeating corrected mistakes. No server needed.
- **Door 2, paid: the Heimdall server.** The developer pushes approved, signed cards. The server scopes each card to
  the code its owner approves (policy at the merge base), serves it to reviewers and to teammates' agents on those
  paths, checks it on every PR, records which cards held and which were dismissed, and puts the card, its incident
  and the check result into the per-merge evidence record.
- **How it stays one product.** A card is the same object in both doors. On the laptop it is guidance. At the gate a
  confirmed card is E3 evidence under the existing `rulecard.CanBlock` rules. "Agents only hand over work with proof"
  extends naturally: the author's agent sees the reviewer's rules before it opens the PR, and the PR body states
  which cards it checked. A finding that cites a card must still show proof.

### 5.3 Who pays, what price, what wedge

- **Buyer:** the platform or engineering lead at 30 to 500 developers, already on coding agents, with 2 to 5 seniors
  who are the review bottleneck. Same buyer as STRATEGY section 2. Regulated teams pay more for the signed record.
- **User:** the senior engineer. They must want it. The pitch to them is fewer repeat review comments and their
  agents following their rules, with full control of what leaves the laptop.
- **Price (proposal, not tested with buyers):** no new SKU. Door 1 free. Door 2 inside Heimdall Team ($15 per active
  committer per month) and Regulated ($35). Benchmarks: CodeRabbit $24 to $48, Unblocked $19 to $29, Tabnine $39 to
  $59 per seat, Copilot Memory bundled (sources in section 3). If a team wants the card server without the gate,
  test $6 to $8 per committer per month as "Heimdall Memory for teams". `not verified`: no buyer has seen any price.
- **Wedge:** "Install in five minutes, run `heimdall memory scan`, approve ten rules, and your Claude Code and Cursor
  follow them tomorrow." Then: "Your teammates' agents and the PR reviewer can follow them too" leads to the server.
- **Moat (honest).** Mining is copyable within a month. What compounds:
  1. **Authority bound to ownership.** A card blocks only in code its owner approves, read from policy at the merge
     base. No memory product has this.
  2. **Outcome data per card.** Upheld, dismissed, fixed and ignored counts per card from real reviews. This is the
     resolution-rate data Bugbot has per repo, but per person and per rule.
  3. **Signed provenance.** An auditor can verify who approved which rule and that a merge was checked against it.
  4. **Laptop reach.** Hosted reviewers cannot read local sessions. Copilot Memory knows only GitHub.
  None of these is proven in use yet.

### 5.4 Risks to this shape

| Risk | Answer |
|---|---|
| GitHub or Cursor adds per-person rules with enforcement | Our edge is self-hosted, cross-agent and the signed record. Move fast on the record |
| SageOx adds review | They prime agents; we gate merges. Watch them; integrate (read their `.sageox/`) if they win the laptop |
| Developers refuse to run a miner | Every source off by default; show the exact bytes that would leave; nothing uploads without `push` |
| Cards rot | Staleness rules (2.6) and outcome-based muting already exist |
| Distraction from the core proof loop | Build order in 5.10 keeps the gate first |

### 5.5 Pipeline

```
 laptop (free CLI, Go)                                       server (Heimdall, Go)
 ─────────────────────                                       ─────────────────────
 S0 sources (opt-in each) ── S1 normalize ── S2 redact
                                   │
                    S3 signals (deterministic, recall first)
                                   │
              S4 distill (developer's agent or local model)
                                   │
              S5 interview (max 5 questions) ── S6 validate
                                   │
                    S7 approve (human, per card)
                                   │
              S8 sign (DSSE + in-toto, ed25519) ── local MCP + exports
                                   │
                    S9 push (age-encrypted) ───────────────▶  verify, scope by policy at merge base
                                                              S10 serve: brief, read_card, teammates' MCP
                                                              S11 enforce: rule_checks, E3 if confirmed
                                                              S12 learn: outcomes, mute, stale, proposals ──▶ inbox on laptop
```

| Stage | What runs | Model | Where in the code |
|---|---|---|---|
| S0 sources | Claude JSONL, Codex rollouts, OpenCode SQLite (plus legacy JSON), Cursor `state.vscdb`, SpecStory Markdown, `git log`/`blame`, the person's own GitHub review comments, a named notes folder | none | `cmd/heimdall/cards_sessions.go` (Claude, Codex exist); new `internal/adapters/sessions/{opencode,cursor,specstory}` with `modernc.org/sqlite`, read-only on a copy |
| S1 normalize | One record per turn: `session_id`, `agent` (`gen_ai.agent.name`), `role`, `text`, `files[]`, `tool` (`gen_ai.tool.name`), `ts`, `repo`, `cwd`, `pr_link` | none | new pure package `internal/domain/sessionlog` |
| S2 redact | gitleaks rules, entropy, PII regex with Luhn, internal hostnames, team deny-list; drop tool outputs; scan tool-call args; typed placeholders | none | `internal/secretscan` (exists) |
| S3 signals | Corrections after agent file edits; reverts and quick human edits of AI code (git); repeated instructions across sessions; "always/never/don't" turns; review comments; incident and ticket links. Cluster by file, symbol and a simhash of the text; count repeats | none | extend `cards_scan.go`; `internal/mine` co-change rules become "forgotten companion" card leads |
| S4 distill | Turn clusters into candidate cards in the schema (5.6). Default: the developer's own agent through `cards prompt` (zero cost to us). Option: a local 7B to 14B instruct model through Ollama (`not verified` for quality). Hosted model only if the developer opts in | developer's agent or local | `internal/cardgen` (exists) |
| S5 interview | Max 5 questions, each tied to a dated moment ("On Oct 30 you reverted the retry change. Was the rule...") | same agent | `cards prompt --interview` (designed, CONTEXT_CAPTURE_PLAN 6.5) |
| S6 validate | Schema; globs match at HEAD; symbols exist (warning); one-sentence falsifiable claim, max 400 chars; no quote over 200 chars; secret scan pass 2 (any hit refuses); fingerprint dedupe; conflict check against active cards on the same scope | none | `cards check` (exists, extend) |
| S7 approve | Terminal review per card: claim, why, incident, scope, check, the redacted evidence lines, and the exact bytes that would leave. Keys: accept, edit, reject, later. Rejected fingerprints never return | none | new `heimdall memory review` (TUI) |
| S8 sign | DSSE envelope over an in-toto Statement, predicate type `https://heimdall.dev/rule-card/v1`, subject = card text SHA-256. ed25519 key in the OS keychain; Sigstore keyless optional | none | new `internal/adapters/attest` (`secure-systems-lab/go-securesystemslib/dsse`, `sigstore/sigstore-go` for keyless) |
| Local serve | `heimdall memory mcp` (stdio): `rules_for_paths(paths)`, `search_rules(q)`, `get_card(id)`. Plus `heimdall memory export` writes a marked block into `AGENTS.md`, a `SKILL.md`, `.cursor/rules/heimdall.mdc` | none | new; reuse `internal/domain/rulecard` ranker |
| S9 push | Encrypt the bundle with `age` to the server recipient (already in go.mod, `filippo.io/age v1.3.2`); upload with a human token | none | `cards push` (exists) |
| S10 serve | Verify the signature; refuse if the signer is not the card's person; scope by policy at merge base; rank (exists); serve to reviewers, and to teammates' agents only as notes on paths the owner approves | none | `read_card`, brief (exist) |
| S11 enforce | `rule_checks` answer every card; a finding cites `card:<id>`; E3 only under `CanBlock` | the reviewer's model | exists |
| S12 learn | Upheld, dismissed, fixed, ignored per card; Beta weight; mute; stale flags; proposals back to the owner's inbox | none | `internal/app/reviewlearn` (exists) |

### 5.6 Rule card schema (v3, compatible with v2)

```yaml
---
id: rc_bob_refund_cap            # stable slug; shown as a short name, never a ULID in chat
schema: heimdall.dev/rule-card/v3
person: bob                      # must equal the token's person (cards whoami)
repo: acme/pay                   # new: cards are repo-scoped
kind: invariant                  # invariant | incident | convention | judgement | unknown
claim: A refund total may not exceed the original capture, counting refunds already made.
why: INC-031 paid a customer twice when two partial refunds raced.
incident:
  ref: jira:INC-031              # or pr:#88, git:3f2a1c, postmortem path
  date: 2026-03-14
applies_to: ["internal/refunds/**"]
symbols: ["refunds.Service.Create"]
triggers: ["refund", "partial"]  # wake the card; never widen its force
check:
  type: test                     # test | semgrep | grep | reasoning
  hint: a test that issues two partial refunds whose sum exceeds the capture
proof: E1 failing test, or E2 tool output
severity: high
status: active                   # proposed | active | muted | stale | superseded | rejected
sources:                         # provenance; refs only, never raw text
  - {kind: session, agent: claude-code, ref: "2026-09-12#turn-41", sha256: "..."}
  - {kind: review, ref: "pr:#88#c1203"}
review_by: 2027-03-01
supersedes: null
confirmed: {by: bob, text_sha256: "...", at: 2026-11-21T10:02:00Z}   # set only by a human token
---
Optional longer note, max 600 tokens. The ranker never reads it for scope.
```

The signature lives next to the card as `<id>.dsse.json`. Any edit to the text changes the hash and ends the
confirmation, as today.

### 5.7 Models: what runs where

| Step | Default | Why |
|---|---|---|
| Collect, redact, signal, validate, rank, sign | Deterministic Go | Cheap, testable, no data leaves |
| Distill and interview | The developer's own agent (Claude Code, Codex, OpenCode, Cursor) | It already holds the session and can ask; costs us nothing; raw text stays local |
| Distill without an agent | Local model through Ollama | `not verified` for quality; measure on the bench before shipping |
| Team-level merge of similar cards from several people (server) | Cheap hosted model (`deepseek/deepseek-v4-flash`, already configured) on approved card text only, never sessions | Card text is short and already approved |
| Review | The reviewer's configured model | Unchanged |
| Embeddings | None in v1 | Path, symbol and keyword ranking already work (`rulecard` ranker). Add embeddings only if the bench shows misses |

### 5.8 Privacy and trust rules (product requirements, not options)

1. Every source is off until the developer turns it on, per source, per repo. (shadowclone pattern.)
2. Nothing leaves the laptop except approved, signed cards. `push` prints the exact bytes first.
3. No raw transcript, diff or tool output ever reaches the server.
4. Telemetry is off. The CLI makes no network call except `push`, `pull` and `inbox` to the company's own server.
5. A card's outcome stats are visible to its owner and, in aggregate per card, to the team. There is no per-person
   score view for managers. `your_record` stays the reviewer agent's private input. This keeps the product away
   from "monitoring performance" under §87 BetrVG and EU AI Act Annex III. Get a legal read before selling in the
   EU (`not verified`).
6. Revocation: `heimdall memory revoke` deletes the person's cards on the server and voids their signatures.
7. Only a human token sets `active` or `confirmed`. An agent, a chat message or the learning loop can only propose.

### 5.9 Evaluation

| Metric | How | Target (proposal) |
|---|---|---|
| Extraction precision | Share of proposed cards the owner accepts | above 40% |
| Rule yield | Accepted cards per 1,000 typed messages | measure; one anecdote gives about 11 ([barazany.dev](https://barazany.dev/blog/mining-claude-history-ultracode)) |
| Redaction leak rate | Plant canary secrets and PII in test sessions; count any that reach a card or the wire | 0 |
| Author-side violation rate | Share of PRs that break an applicable active card, with and without the local MCP, as in TellOnce | down, measured per team |
| Card resolution rate | Findings citing a card that were upheld or fixed, over all findings citing it | above 60%; Bugbot reports 78% for its whole product |
| Noise | Dismissals per card per 100 PRs | mute threshold already exists |
| Knowledge gain | Δ(recall on planted bugs, precision on clean PRs, severity accuracy) per 1,000 tokens of cards (CONTEXT_CAPTURE_PLAN 1.3) | positive on Proof Bench |
| Real-history replay | Take real fix commits; replay the bug-introducing diff; allow only cards dated before it | the gate for any claim on the landing page |
| Staleness | Share of active cards flagged stale; time to fix | report only |

### 5.10 Build order

1. Repo-scoped cards and `cards whoami` (CONTEXT_CAPTURE_PLAN 5.5). Without them cards silently die.
2. OpenCode and Cursor adapters on `modernc.org/sqlite`, with golden fixtures.
3. `heimdall memory review` approval TUI and per-source consent.
4. Local MCP and the exports. This is the free door.
5. DSSE signing and verification on push.
6. Wire human tokens on the VM so `push` and `confirm` work live.
7. Real-history replay on Proof Bench. Publish only what it shows.

---

## 6. Side question: does deployment-side work fit?

**Verdict: FITS, narrowly. Evidence yes, control no.**

**What fits.** The product promise is "agents only hand over work with proof" and "nothing merges without proof, and
the proof is on record". Deploy is the next hand-over. Three things extend the same record with little new surface:

1. **Release evidence.** Ingest the deploy event (which commit, who approved, which environment) into the same
   hash-chained record, so one export covers change and release. SOC 2 CC8.1 and the other controls in STRATEGY
   section 2 cover changes to production, not only merges.
2. **Agent-run smoke checks as proof.** After deploy to staging, run the PR's acceptance tests and the incident
   cards' regression tests from the sandbox against the live URL. Attach pass or fail to the record. This reuses the
   runner and the tests Heimdall already writes.
3. **Deploy-risk from cards.** Incident cards already say "this broke prod once". Flag a PR that touches their scope
   as needing a canary, and say so in the PR.

**What does not fit.** Traffic shifting, metric analysis, automatic rollback and incident response:

| Competitor | What they own | Note |
|---|---|---|
| Argo Rollouts | Canary and blue-green on Kubernetes; `AnalysisTemplate` against Prometheus | Free, open source, standard ([oneuptime guide](https://oneuptime.com/blog/post/2026-02-09-argo-rollouts-progressive-delivery-analysis/view), Feb 2026) |
| Harness | Continuous Verification with AI; continue, pause or roll back | Third-party estimate $23K to $41K per year for 200 developers ([aitoolsatlas](https://aitoolsatlas.ai/tools/harness-ai/pricing), `not verified`) |
| LaunchDarkly | Guarded releases with automatic rollback, now "Release Autopilot" on every plan | Paid Guardian add-on retired, Nov 2026 ([pricingsaas](https://pricingsaas.com/companies/launchdarkly/diffs/2026W46), `not verified` at LaunchDarkly) |
| AI SRE agents | Resolve AI at a $1.5B valuation (Apr 16 2026, [beri.net](https://www.beri.net/article/resolve-ai-190m-ai-sre-production-incidents-2026)); Traversal $120M Series B at $1B (Nov 2026, [mezmo](https://www.mezmo.com/learn/the-2026-ai-sre-market-map-agents-harnesses-and-the-data-layer)); Cleric $9.8M seed | Well funded, and they own production telemetry |
| Sleuth | Started as deploy tracking and DORA metrics; now sells AI agent governance | A deploy-tracking pivot is a warning that tracking alone did not scale ([yespress](https://yespress.io/sleuth), `not verified`) |

**Reasons for the verdict.**

- Evidence fits because it reuses the record, the runner and the cards, and the same buyer already pays for audit
  evidence.
- Control does not fit because it needs production telemetry, traffic control and on-call trust, where free OSS and
  funded vendors lead, and because the Lite install target (STRATEGY 4) cannot carry it.
- Timing: after the proof loop works on real repos. A webhook that accepts deploy events and one "smoke on staging"
  job type are enough for the first version. Integrate with Argo Rollouts as an `AnalysisTemplate` web metric
  rather than replace it.

---

## 7. Open questions for the owner

1. Rename the outside-facing CLI from `cards` to `memory`? The word "memory" sells, the word "card" is precise.
2. Accept the rule in 5.8.5: no per-person score for managers, ever? It costs a dashboard and buys trust.
3. Publish the local CLI as open source (Apache-2.0) before the server is easy to install, or after?
4. STRATEGY section 7 says packs are "not the headline". This research says: make them the free wedge, keep the gate
   as the headline. Change STRATEGY in the same commit if you agree.

## 8. Sources not linked above

- [Mining Claude Code and Codex logs into a knowledge base](https://instavar.com/research/agents/mining-claude-code-codex-jsonl-logs-knowledge-base-2026), 2026
- [Simon Willison: extracting Claude Code transcripts](https://simonw.substack.com/p/a-new-way-to-extract-detailed-transcripts)
- [Anthropic: donating Agent Skills to the Agentic AI Foundation](https://www.anthropic.com/news/donating-agent-skills-to-the-agentic-ai-foundation): Anthropic reports 1 million skills published and about 280 million installs (vendor, date `not verified`)
- [Copilot Memory controls](https://github.blog/changelog/2026-05-26-copilot-memory-has-more-controls-for-deletion-scope-and-the-copilot-cli/), May 26 2026
- [Bugbot learning blog](https://cursor.com/blog/bugbot-learning)
- [MemoryAgentBench](https://arxiv.org/abs/2507.05257)
- [GitHub Copilot GDPR guide (Germany)](https://compound.law/en-DE/tools/copilot-github/), practitioner, 2026
- [Unblocked: memory MCP servers compared](https://getunblocked.com/blog/memory-mcp-servers-compared/), vendor, 2026
- [SageOx: Claude Code integration](https://sageox.ai/docs/developers/claude-code)
- [DORA 2025 report](https://dora.dev/dora-report-2025/), Sep 23 2025
- HN search via `hn.algolia.com` API, stories since Jan 1 2025, read Nov 21 2026. reddit.com and X were not used.
