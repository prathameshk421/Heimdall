# Heimdall blog plan

Target: Best Blog side track. It is judged on **clarity, accuracy, and how much it helps a first-time Switch user** (DevTools Academy page). So a pure engineering flex loses. The post must teach Switch while it shows the engineering. Publish by 23:30 IST on Nov 7, and link it in the submission.

Raw material: `blog.md` (decision log, some facts now old: it says AWS with 8 vCPU preferred, the VM has 2) and `docs/PROGRESS.md` (the live failures).

## 1. What is worth writing about

Scored 1 to 5. Only things that ran live go in.

| Topic | Reader value | Help to a Switch first-timer | Live proof | Verdict |
|---|---|---|---|---|
| Wiring agents to Switch: rooms, identities, what we got wrong | 5 | 5 | 5 | **Spine of the post** |
| Red-first test judge: agents cannot write tests that pass | 5 | 2 | 5 | **Core section** |
| Evidence tiers and the ten-clause gate | 5 | 2 | Partly | **Core section** |
| Sandbox: six live failures of rootless Podman under systemd | 5 | 3 | 5 | **War story, best read in the post** |
| Prompts never reached the model; DSML loop to 1.4M tokens | 5 | 4 | 5 | **War story** |
| Polling with ETags instead of webhooks, the rate-limit incident | 4 | 3 | 5 | Short section |
| Event log and outbox in Go, 41.8 MiB server | 4 | 1 | 5 | Short section with figure |
| Graphiti memory and trust labels | 4 | 2 | **None yet** | Only if the take shows it |
| Reviewer context packs (age, Gitleaks) | 3 | 1 | None | Leave out |
| Mutation testing | 3 | 1 | None | Leave out |
| Harness hooks for coding agents | 4 | 1 | 5 | Sidebar, or a second post |

## 2. Format

**Shape of a big-company engineering post.** My search found no written Uber rule, so this is the pattern their posts commonly follow, from my own reading: introduction with the stakes, background, the problem with a concrete failure, the architecture with a diagram, implementation details with real numbers, challenges and what broke, results, next steps. Choose one topic that earns the read (*Writing for Developers*). Write what you learned (Mixmax).

**Title:** "We made AI reviewers prove it: building a merge gate on Switch"

**Length:** 1,800 to 2,200 words, 5 figures. Figures come from the video's rendered scenes, so both match.

| # | Section | Content | Words | Figure |
|---|---|---|---|---|
| 1 | Introduction | Two refunds of 60 on a payment of 100. Green tests. One approval. What we built, in three sentences | 120 | Hook frame |
| 2 | Background | Code got cheap. Review did not. Why more comments do not fix it. Sourced numbers only, marked "estimate" | 150 | None |
| 3 | The system | Seven stages. Agents propose, code decides | 200 | Pipeline |
| 4 | Why Switch, and how to start | **First-timer section.** What a room, an agent identity and a bridge are. How to connect one agent. A steps list with commands that we ran | 400 | Switch map |
| 5 | Things Switch taught us | `connection_id` must come from the client and be reused. `heartbeat_interval_seconds` is a float. `!` posts are swallowed. Mattermost and Telegram deliver only on a mention. Switch's env allowlist drops new variables. Reviewers need a private room per ticket, so each ticket has one session | 250 | None |
| 6 | Tests that must fail first | Red-first rule. Real rejection text. Why a tree hash pins them | 200 | Rejection card |
| 7 | Evidence, not opinion | E1 to E4 table. Ten clauses. `gate.Evaluate` code | 200 | Evidence ladder |
| 8 | What broke | Sandbox failures in order. DSML loop. Prompts blocked by our own file rules | 350 | Failure timeline |
| 9 | Results | Only measured numbers, with dates: 1 s to room, 91 s to findings, 41.8 MiB, 241 commits | 100 | Numbers |
| 10 | Limits and next | State what is not verified. Accuracy is scored, and it reads as confident | 100 | None |

## 3. Writing rules: ASD-STE100, about 80% of the way

The standard is free from [asd-ste100.org](https://asd-ste100.org/about_STE.html) (Issue 9, Jan 2025). Summaries count 53 writing rules plus a restricted dictionary. Check the official rules, because I read only summaries.

**Apply fully (sections 3 to 9):**

- Procedure sentences: 20 words at most. Description sentences: 25 words at most.
- Active voice. Verb in the simple tenses. Commands for steps.
- One instruction per sentence. One topic per paragraph, 6 sentences at most.
- One word for one meaning. If it is a "room", it is always a "room". Never "channel" for the same thing.
- Keep articles ("the", "a"). No noun stacks of more than three words: not "merge gate clause evaluation result".
- No "-ing" form as a modifier where a plain verb works. No phrasal verbs where one verb exists.
- Warnings before the step they apply to.

**Relax on purpose (the 20%):**

- The introduction and the section openers may use voice and rhythm.
- Technical names stay as they are (`--cap-drop=ALL`, `event sourcing`), even outside the STE dictionary.
- The restricted dictionary is not enforced word by word. It would block normal terms for engineers.
- One dry joke per war story, if the fact itself is funny.

**Check:** a small script flags sentences over the limits and common passive forms. Target: at least 80% of sentences pass. The score is not published in the post.

**Accuracy rule:** every number has a date and a source in `PROGRESS.md` or a primary link. Write "not verified" where that is true. No "should work".

## 4. Other formats

- `#HowISwitch` post with the 20 s cut, for the keyboard prize (WeMakeDevs page).
- Short version of section 8 as a thread after submission.

## Sources

- [DevTools Academy event page](https://www.devtoolsacademy.com/hackathons/flip-the-switch)
- [Simplified Technical English](https://en.wikipedia.org/wiki/Simplified_Technical_English), [About ASD-STE100](https://asd-ste100.org/about_STE.html)
- [How to write an engineering blog post (Mixmax)](https://engineering.mixmax.com/blog/how-to-write-an-engineering-blog-post), [Writing for Developers](https://livebook.manning.com/book/writing-for-developers/part-1)
