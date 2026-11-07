# The review moat: personal context, the code graph and the learning loop

Status: built on branch `heimdall-moat`. Unit tested and measured on a local bench. **Not verified on the live VM.**

## 1. The problem

A generic reviewer can read the diff and the repository. So can CodeRabbit, Bugbot and Greptile. Three things decide who wins:

1. What the reviewer can see beyond the diff.
2. What it knows that is not in the repository.
3. Whether it improves from what happened to its past findings.

Public material shows where the others stand:

| Tool | Context | Learning | Scope of learning |
|---|---|---|---|
| Bugbot | Agent that pulls context at run time. Its first version ran 8 passes and kept what several passes agreed on | Learned rules from reactions, replies and human comments. Candidate rules are promoted, and noisy rules are disabled | Repository |
| CodeRabbit | Code graph rebuilt per review, linked issues, past PRs, path rules, 40+ linters, generated verification scripts | Learnings from chat | Not stated |
| Greptile | Graph of functions and files | Reactions, replies, team comments, first-to-last commit comparison | Team |

Bugbot's lessons shaped this design. Moving context from static prompts to run-time retrieval helped, tool design had an outsized effect, and an agent that is told to be cautious misses bugs. Their online metric is resolution rate: the share of flagged bugs fixed by merge.

**The gap we own.** All of them learn only from what reaches the git host. None of the public material describes a model of one person. The rules a senior engineer enforces and the incident behind each rule live on that person's PC, in sessions with their coding agents. A hosted service cannot read those sessions. Heimdall runs a CLI on that PC, and each reviewer agent speaks for one person.

## 2. What was wrong

Found by reading the code on `heimdall-build`:

| Gap | Effect |
|---|---|
| The reviewer's only code tool was `read_code` with an exact path | It could not search or find a caller. A bug in an untouched file was out of reach |
| `internal/cardgen` was not wired to any command | Nobody could make cards without writing them by hand |
| Cards were picked by glob only, and selection stopped at the first card over budget | Risky code in a new file woke no rule, and one large card hid the rest |
| Cards reached the model as one blob | Nothing recorded whether a rule was checked |
| Outcomes never fed back | A rule that was always dismissed stayed the same forever |
| The skill never told the reviewer to use team memory | The graph was not used even when it had data |
| Graph episodes held `uphold F2` with no claim and no file, and `finding.filed` was never ingested | Even with delivery fixed, the graph could extract almost nothing |
| Graphiti used its default entity types (Location, Object, and so on) | The types did not fit code review |
| The session collector matched the exact repo path | Sessions in worktrees were missed |

## 3. The design

```
 Developer's PC                    Heimdall server                         Reviewer agent
 sessions + git                    cards   code graph   outcomes   memory
      |                                \        |          |        /
 heimdall cards scan  (local)           \       v          v       /
      |                                  review brief, built per change  --->  hypotheses
 developer's own agent writes cards             |                              refute once
      |                                         |                              prove (E1, E2, E3)
 heimdall cards push  ---------------->  rule_checks and card refs  <-------   submit
                                                |
                                   upheld / dismissed / fixed / ignored
                                                |
                          card weights change, proposals wait for the owner
```

### Capture: `heimdall cards`

- `scan` mines sessions and git on the PC, and writes `signals.md`. Nothing leaves the machine.
- `prompt` hands the signals to the agent the developer already uses, so distilling costs Heimdall nothing and raw text stays local.
- `check`, `push`, `pull`, `preview`, `inbox`, `accept` and `reject` manage the cards. `preview` prints what a reviewer would receive for a diff.

The first miner used phrase lists and found 2 signals in this repository's sessions. The parser was fine, and 259 typed turns followed agent work. People correct agents in short casual words that no phrase list covers. So the scan now aims for recall and keeps every reaction to file work, and the model that reads it supplies precision. That gave 25 signals, including real rules phrased casually.

### Rule cards v2

A card now names `applies_to`, `symbols`, `triggers`, the one `check` that settles it, a `proof` hint, and a `status`. Old cards still parse.

The ranker (`internal/domain/rulecard`) scores by path specificity, dependent files from the graph, symbols, text in added lines, kind, and past outcomes. A card too large for the budget is skipped, and smaller cards still get in. A card that names only symbols missing at head is flagged as possibly stale.

**A plain card grants no tier.** It says where to look. A finding that a card led to needs E1 to E3 to block. The gate is unchanged.

**A confirmed card can reach E3 (owner decision, Nov 8).** `heimdall cards confirm NAME` lets a person stand behind a rule. A finding then cites `memory_ref: card:<file>#<path>`, and the same E3 re-query that checks team memory decides. It holds only when all of these are true:

- The card is active, has `applies_to` and a `check`, and is an invariant, incident or judgement.
- The text is exactly what the owner confirmed. Any edit ends the confirmation.
- The path is inside `applies_to`. A trigger wakes a card but does not widen its force.
- The owner is an approver of that path in the policy at the **merge base**, by the same rule the gate uses for a GitHub approval. A PR cannot edit its way out, and a card gives nobody power over code they do not own.
- Its record has not muted it, so a rule that keeps being dismissed stops blocking by itself.

Every failure leaves the finding at E4, which never blocks. It is re-checked on every push. People can still `dismiss` or `challenge` the finding, and the policy's consensus and severity rules still apply. Only a human token can confirm, and `accept` does not confirm.

The risk that stays: a weak model can misread the code and cite a card that does not apply. It then holds a merge until a person dismisses it, as with any E3 finding. Each dismissal counts against the card, so repeat errors mute it.

### Code graph

`internal/app/codegraph` builds on demand from `CodeReader.Tarball`, so it needs no new service and no model.

- It runs personalized PageRank from the changed files over **reversed** edges, which ranks the files that depend on the change.
- Go callers must import the definition's folder, so `refunds.Service.Create` does not match `payments.Service.Create`.
- **Removed but still used:** a definition the change deleted while code still uses its name.
- **Contract strings:** a flag, env var, JSON field or route that left a changed file while other files still name it. A compiler and a symbol graph both miss these.

Reviewers also get `search_code` and `find_symbol`.

### Knowledge graph

- Episodes now start with a plain `summary`: who, what, the claim, the file, the reason and the date.
- Findings that reach upheld or resolved are ingested with their claim.
- Keys moved to `v2`, because the catalog refuses changed content under an old key.
- The Graphiti entity types are now Component, Invariant, Incident, Decision, Person, Requirement and Procedure. **This one edit is in `deploy/graphiti/config.yaml` and takes effect only on redeploy.**
- The brief fetches memory for the change's symbols and files, so a weak model need not think to ask. A `confirmed` fact is E3 evidence, so team decisions can block.

### The brief

`get_task` now returns `personal_context` (obligations with WHY, CHECK and PROOF), `change_map`, `prior_findings`, `team_memory` and `your_record`. Each section fails alone. Production uses a cheap model, so server-built context is the highest leverage we have.

### Skill

Read the brief, write bold hypotheses (recall first), check them, try once to refute each, prove what survives, and answer every card in `rule_checks`. This follows Bugbot's finding that caution costs recall, and adds a refute step to hold precision.

### Learning loop

`internal/app/reviewlearn` folds the event log into outcomes per reviewer and per card:

- **Useful:** upheld or resolved.
- **Noise:** dismissed or withdrawn.
- **Ignored:** still open at merge. This counts as half a negative.

A Beta posterior weights each card, so a proven card rises and a noisy card fades. A card mutes after 3 or more dismissals with a low posterior.

A person's dismissal reason or `remember` note becomes a **proposed** card. It acts only after its owner runs `accept`. Proposals come only from mapped human actors, so a chat message or an agent cannot steer a reviewer. A rejected card stays on disk and never comes back.

## 4. Measured results

Bench: `testdata/reviewbench/settle`. One PR with 3 planted bugs, gold answers written before any run, and a clean control PR. Reviewers were Haiku as a stand-in for the weak production model, 3 per arm. Each bug is counted only for a concrete, correct claim at the right place.

| Bug | A: today, no cards | B: today, old cards | D: graph, no cards | C: graph and cards |
|---|---|---|---|---|
| G1: `Lookup` returns nil, so `statements/monthly.go` panics (file not in the diff) | 0/3 | 0/3 | **3/3** | **3/3** |
| G2: flag renamed, `scripts/nightly.sh` stops the payout (file not in the diff) | 0/2 valid | 0/3 | **3/3** | **3/3** |
| G3: bank silently drops lines above 500 (only one person knows) | 0/3 | 1/3, the card holder | 0/3 | 1/3, the card holder |

Severity moved too. Arm A rated the flag break low or medium, as "any script might". Arms C and D rated it high and cited the line. Reviewers in A and B flagged only the latent nil in `payout/runner.go`.

**Noise on the clean PR.** The one finding was the log line with an IBAN, which Priya's card allows. Arm A filed it 3/3. In arm C, Priya's reviewer filed nothing and answered every rule, and the two reviewers without that card filed it. In arm B on the buggy PR, the unfiltered blob pulled Sam's reviewer into an off-target high finding from a card about retries, and the ranker kept that card out in arm C.

**Easy bench (`pay-demo`, arms A and B only).** Generic reviewers already found the two main bugs 3/3, so cards added no recall. Cards still changed severity and noise. Eli's card rated the wire-field bug high, where all 3 generic reviewers rated it low. Bob's card stopped a false idempotency finding that all 3 generic reviewers filed. Eli's reviewer still filed it.

**What this does and does not show.**

- It is one synthetic PR that I wrote, with 3 reviewers per arm, scored by me. It shows the mechanism works. It is not a benchmark and gives no comparison with any other product.
- Haiku is not DeepSeek Flash. The live model may do better or worse.
- Grep was not available to the eval agents, so arms C and D ran without `search_code` and `find_symbol`. Those tools are **not measured**.
- One arm A run broke the rules by listing folders and found the script that way, so it is excluded from G2. One arm C reviewer made a stray tool call that failed and returned no data.
- Team memory, `prior_findings`, `your_record` and the learning loop were **not** in the bench. Unit tests cover them.

## 5. What I suggest next

Ordered by value for effort.

1. **Author intent from the PC.** Claude transcripts hold `pr-link` records, so the CLI can tie a session to its PR and summarize what the author corrected, rejected and did not test. No hosted reviewer can see this. It can land first as a section in the PR body, which reviewers already read.
2. **Run the bench on real history.** Take fix commits from real repos, replay the diff that introduced each bug, and use only cards dated before it. This removes my authorship bias, and it is the gate for any skill change.
3. **Prompt self-improvement.** Feed `reviewlearn` misses to a reflection step that proposes a skill edit. Keep it only if it beats the current skill on a held-out bench, as GEPA does with its train and validation split. Add caps on length and cost, because GEPA has none built in.
4. **Show resolution rate.** `Snapshot.Report()` has it per reviewer. Put it on the dashboard, since it is the number the market uses.
5. **Owner judgement beyond one reviewer.** Priya's card stopped only her own reviewer. Let a judgement card from a path's owner reach the other reviewers on that path, as a note.
6. **Different lenses.** Reviewers with no cards review the same way. Give each one a lens (correctness, security, contracts) so that consensus means more.
7. **Tree-sitter.** Non-Go languages use line patterns. Tree-sitter would improve them, at the cost of native code in a `CGO_ENABLED=0` build.

## 6. Sources

- [Building Bugbot](https://cursor.com/blog/building-bugbot) and [Bugbot learning](https://cursor.com/blog/bugbot-learning)
- [CodeRabbit: context engineering](https://coderabbit.ai/blog/context-engineering-ai-code-reviews)
- [Greptile: memory and learning](https://www.greptile.com/docs/how-greptile-works/memory-and-learning)
- [Greptile: Martian benchmark write-up](https://www.greptile.com/content-library/greptile-martian-code-review-benchmark), which is vendor-published
- [Zep: temporal knowledge graph for agent memory](https://arxiv.org/abs/2501.13956)
- [Feedback-normalized developer memory](https://arxiv.org/abs/2605.01567): its point that proximity is not utility, and its logged, gated memory selection
- [GEPA in DSPy](https://dspy.ai/api/optimizers/GEPA)
- [Aider-style repo map with personalized PageRank](https://openoxide.pages.dev/features/searching/repo-mapping)
- [retro](https://github.com/craftwork-design/retro) and [session-recall](https://github.com/buildingopen/session-recall): community tools that mine sessions into rules with approval
