# Proofroom: Pitch Pack

Event: Flip the Switch hackathon (Track 01, Best Business Workflow)
Companion doc: PROOFROOM_MVP.md

> **Updated Nov 3 (backlog Part 8):** name Proofroom and tagline "LGTM is not evidence." are final (D59); the demo runs in Mattermost (D19); the demo bug is D57 (three findings from three people's context, one across repos); judging weights are 30/25/25/20 (V26).

Every number below is marked with its source. Before publishing anything, check each source link still says what we quote. Respondent surveys are estimates, not measurements.

---

## 1. Positioning

**Category:** Merge safety for teams that ship with AI agents.

**One line:** Proofroom makes every merge safe and definite. No LGTM, no slop, and every reviewer brings the context their team actually has.

**Long line (from the team):** Proofroom automates engineering change assurance. It coordinates context-aware agents across development teams to write failure-first tests, independently review changes and establish evidence before code merges, requiring human intervention only when engineering judgment is needed.

**Zero-touch line:** The developer writes code and answers questions. The tests are ready before the code, the reviews start the moment they push, and the agents ask in team chat (Mattermost in the demo; Slack, Teams or Telegram later) only when they need a human.

**Flow in one breath:** Jira ticket moves to In Progress, a room opens, the spec agent asks questions, failing tests get written, the dev builds, the PR goes through the slop check, the right agents review, CI/CD gates and a human approve, and rejects become Jira tickets.

**The problem in one sentence:** AI made writing code cheap, so PRs got bigger and reviews got slower, and the approval button still means "I skimmed it."

**Who it is for first:** Engineering teams of 10 to 200 developers who already use Claude Code or similar agents, and who have a review queue that grows every week.

**What we are not:** We are not another AI code reviewer that leaves comments. We do not auto-merge. We do not let a model decide whether code ships.

---

### Language rules (from the team brief)

- We are not another AI PR review bot or a generic multi-agent orchestrator.
- Never claim bug-free or guaranteed-safe merges. Say "safer, with evidence."
- Agents are not identical reviewers with different labels. Each carries a different team's context and a different job.
- The knowledge base is a capability, not the headline.
- No generic AI buzzwords. Say what the agent did, not that it is "intelligent."
- Philosophy line: humans provide engineering judgment; agents handle coordination, investigation and verification.

### Tagline candidates

- Ship with confidence. Not crossed fingers. (from the brief; human but generic)
- LGTM is not evidence.
- Nobody rubber-stamps here.
- Tests before code. Proof before merge.

Decided: **LGTM is not evidence.** (D59)

## 2. The four moats

Each moat is a sentence a judge can repeat back.

1. **No LGTM.** The merge button stays grey until the tests pass, the slop check passes, every finding is resolved with evidence, and a human approves. Approval means something.
2. **Anti-slop at the door.** A 1,400-line PR with no ticket is rejected in seconds, before any reviewer's time is spent.
3. **Personal context.** Your agent knows your ownership, your invariants and your incidents. The agent that reviews your code is the one that knows what you know.
4. **Cross-team agents.** A payments PR can bring in security, infrastructure and consumer-team agents, each reviewing from its own angle, in one room, under its own identity.

Supporting line for the stage: **"A generic AI reviewer reads the diff. Proofroom reads the diff, the ticket, the decision and the incident."**

---

## 3. Proof points (sourced, with caveats)

Use these carefully. Say "estimate" where the source is a survey.

| Claim we want to make | Source to cite | What the source actually says | How we phrase it |
|---|---|---|---|
| A large share of AI-assisted code is low-quality "slop" | Linux Foundation LF Research, Oct 2026 | Respondent estimates of 25% and 31% | "Survey respondents estimate 25 to 31% of AI-generated code is slop" |
| Review time rises as AI output grows | Chen and Stratton, review time study | Review time up about 49% | "One study found review time rose about 49%" |
| PR volume and review time rose together | Faros AI engineering report | PR volume up about 98%, review time up about 91% | "Faros AI reports PR volume up 98% and review time up 91% in teams using AI" |
| Minimal review is common | Graphite data on review depth | Review depth data (verify exact figure) | Only quote after checking the figure |
| Productivity effects of AI tools | NBER, May 2026 | Mixed results (verify exact figures) | Use as context, not as our headline |
| Scale of agent-written PRs | AIDev dataset | 932,791 agent PRs | "The AIDev dataset has nearly a million agent-authored PRs" |

**Counter-data, included for honesty:** In the same datasets, 83.8% of agent PRs are merged and 54.9% need no changes. Show this in the Q&A slide. Judges respect a pitch that names the counter-evidence. Our answer is that merged does not mean verified.

**Rule for the pitch:** Every number on a slide has a source footnote. Every survey number says "estimate." We do not invent numbers to make the slide look stronger.

---

## 4. Three-minute video script

Length target: 2 minutes 50 seconds. Screen recording with voiceover, plus one shot of a developer's face at the start.

**[0:00 to 0:20] Hook (face to camera)**
"Last week my team merged a 1,400-line PR that nobody actually read. It had an LGTM. It broke a refund consumer. This is how we stopped that happening."

**[0:20 to 0:45] The problem (slides, one number)**
"AI made code cheap. PRs got bigger. Reviews got slower. Surveys estimate a quarter to a third of AI code is slop. The approval button still means 'I skimmed it.'"

**[0:45 to 1:10] The idea (one diagram)**
"Proofroom runs in your team chat. Work starts when a Jira ticket moves to In Progress. Agents in a Switch room hold the context: the ticket, the decision, the incident. The merge is green only when the tests pass, the slop check passes, every finding is resolved, and a human approves."

**[1:10 to 1:45] Demo part 1: before the build**
Screen: Jira ticket PAY-142 moves to In Progress; a room opens in Mattermost. Spec agent asks one question. Developer answers. Test agent writes failing tests. Terminal shows them failing for the right reason.
Voiceover: "The tests come first, from the ticket and the decision. The agent builds against them. It cannot pass by deleting them."

**[1:45 to 2:10] Demo part 2: the door and the room**
Screen: The bad 1,400-line PR is rejected by the slop check in seconds. Then the good PR: Bob's agent finds partial refunds can exceed the payment, Eli's agent shows the ledger in another repo still reads `refund_ref` (INC-031), Ana's agent shows any merchant can read any refund. The author's agent disputes one; the evidence holds; the approver upholds it from the room. Fixes land with tests.
Voiceover: "The slop check runs before any reviewer's time is spent. Reviewers from other teams join the room under their own identities."

**[2:10 to 2:35] Proof of the gate**
Screen: GitHub check turns green after human approval.
Voiceover: "No LGTM. The check is plain code. No model decides the merge."

**[2:35 to 2:50] Close (face to camera)**
"Proofroom. Merge safety for teams that ship with agents. Built on Switch."

Production notes:
- Record at 1080p. Use the seeded repo only.
- No real company names or real Slack content on screen.
- Subtitles on. Captions in the description.
- Keep the demo claims small and true. Marketing lines in the hook and close can be bold; anything on screen must be real.

---

## 5. Live pitch (5 minutes)

Slide 1. Title. "Proofroom: every merge safe and definite."
Slide 2. The problem. One number, sourced.
Slide 3. The four moats (section 2).
Slide 4. The lifecycle: two workflows, one room (diagram from MVP section 3).
Slide 5. Live demo (90 seconds, or the recorded video if the network fails).
Slide 6. Why Switch is central: chat-initiated work, rooms, agent identities and roles, remote hosts, decisions in history.
Slide 7. What is real today and what is mocked. State it plainly.
Slide 8. Next: per-user memory permissions, auto-fix with human approval, Teams and Telegram.
Slide 9. Ask. "Try it on one repo this week."

Speaking notes for slide 6 (Switch centrality, 30% of judging): "Without Switch, the work starts in chat, the agents have no shared room, and the decisions have nowhere to live. Switch is the room. We added the merge gate."

---

## 6. Q&A preparation

**"Another team here also built PR review agents in a room. How is this different?"**
A review room comments on a PR. Proofroom starts at the ticket with failing tests, gives each reviewer one person's approved context, settles disputes by re-running evidence, and gates the merge with a required GitHub check that only plain code can turn green.

**"Isn't this just CodeRabbit?"**
An AI reviewer comments on the diff. Proofroom refuses to let a PR merge on comments alone. It starts before the code, with tests from the ticket. And reviewers come from different teams, each with its own context. Check CodeRabbit's and BugBot's current features before the pitch: they index the codebase and learn from feedback, so do not claim they have no context. The claim is: one shared reviewer, no per-team lens, no tests before code, and no merge gate with evidence.

**"Why do we need agents for this? Why not just tests?"**
Tests are the bar, not the whole story. The tests come from the agreed behaviour, which comes from a human-approved spec. The reviewers catch what tests do not: authorization gaps, consumer breakage and decisions made in Slack six months ago.

**"Can a model approve or bypass the gate?"**
No. The gate is plain code with five conditions. Agents file findings and fix them. Humans approve. No agent approves a merge.

**"What does the knowledge layer actually change?"**
We measure it, or we do not claim it. The demo shows one finding that only the memory could produce: the consumer agent citing the removed `refund_ref` incident. We will not claim broad review quality gains we have not measured.

**"Does team memory leak private information?"**
Team memory is scoped per team: the server decides which team partitions an agent may search, and personal context packs never enter team memory. We do not claim per-user permissions inside a team.

**"Who pays for the agents and the always-on machine?"**
The team pays for their own agent usage during the hackathon. Remote hosts use the team's own provider keys, stored outside the repo.

**"What if a reviewer agent is wrong?"**
A finding needs evidence. The author's agent can dismiss a finding with a reason, and a human settles disagreements. A wrong finding costs one comment, not a bad merge.

**"Why would a company trust agents across teams?"**
Membership is explicit. Linking rooms does not grant access. Each agent reviews only what it is invited to review.

---

## 7. Switch hackathon submission copy

**Project name:** Proofroom

**One-liner (under 140 characters):**
Every merge safe and definite: tests from the ticket first, slop rejected at the door, cross-team agents in one Switch room, no LGTM.

**Short description (about 120 words):**
Proofroom runs in the team's chat. Work starts when a Jira ticket moves to In Progress: a Switch room opens and a test-author agent reads the ticket, asks at most three questions, and writes failing integration and contract tests before any code exists. The developer's agent builds until they pass. When the PR opens, a plain-code slop check rejects oversized or unlinked PRs at the door. The author picks reviewers; each reviewer's agent runs on a Switch remote host with that person's approved context and team memory from tickets, PRs and decisions. Findings need evidence that Proofroom re-runs, disputes are settled by evidence first and humans last, and a human approves. The GitHub check turns green only when every condition holds. No LLM decides the merge.

**What Switch does in Proofroom:** Chat-initiated work, rooms as the shared record, agents with their own identities and roles, remote hosts for offline owners, decisions kept in room history.

**Tech stack:** Switch (rooms, agents, remote hosts), Graphiti on FalkorDB (team memory, temporal knowledge graph), a Go gate service, Jira and GitHub connectors, with a GitHub check.

**Links:** Repo, README, demo video, seeded repo.

---

## 8. Social plan (launch week)

Hashtag for the Switch prize: #HowISwitch. Post at least once a day Nov 4 to 7.

| When | Platform | Post |
|---|---|---|
| Nov 4 | X / LinkedIn | "Last week we merged a 1,400-line PR with an LGTM. Here's the rule we're building so that doesn't happen again." Short clip of the bad PR being rejected. #HowISwitch |
| Nov 5 | X | Thread: the four moats, one per post, each with a 10-second clip. |
| Nov 6 | LinkedIn | Post the proof-points slide with sources. Ask for counter-evidence. |
| Nov 7 | X / LinkedIn | The full video, with the submission link. |

Rules for social:
- Every clip is from the seeded repo.
- Never quote a survey number without its source and the word "estimate."
- Reply to every comment in the first two hours.

---

## 9. Blog draft (Best Blog side track)

**Title:** "We stopped using LGTM. Here is what replaced it."

Outline:
1. The moment: a 1,400-line PR, an LGTM, a broken consumer.
2. Why approval stopped meaning anything (the proof points, sourced, with counter-data).
3. The four rules: tests before code, slop at the door, personal context, cross-team agents.
4. What Switch gave us: rooms, identities, roles, remote hosts, history.
5. What we did not build: auto-merge, auto-fix without a human, a model that approves.
6. Honest limits: per-user memory permissions, Teams not in the demo, measured results still to come.
7. Try it on one repo this week.

Length: 900 to 1,200 words. Include one screenshot per section.

---

## 10. Side tracks

- **Best Blog (AirPods prize):** Use section 9.
- **Wispr Flow ($500 voice build):** Add voice intake for the spec questions. Only claim it if the voice flow works in the demo. Otherwise pitch it as the next step.
- **#HowISwitch social prizes:** Section 8.

---

## 11. Deck outline (for the Slides version, if needed)

1. Title
2. The problem (one sourced number)
3. The four moats
4. Two workflows, one room
5. Live demo or video
6. Switch centrality
7. Real today vs mocked
8. Counter-evidence and our answer
9. Next steps
10. Ask

---

## 12. Launch checklist

- [ ] Seeded repo public, README complete, one-command setup tested on a clean laptop
- [ ] Demo video under 3 minutes, captions on
- [ ] Every number on a slide has a source link checked this week
- [ ] Judging weights checked on the official page
- [ ] Name clash check for "Proofroom" done
- [ ] Submission text pasted in and reviewed by all four developers
- [ ] Backup recording of the demo ready in case the live run fails
- [ ] Submission done by 4 PM IST on Nov 7 (hard deadline 11:59 PM IST)

---

## 13. Name options

| Name | Notes | Clash check |
|---|---|---|
| Proofroom | Current choice. Says "proof before merge." | Not done |
| Merge Room | Plain and descriptive | Not done; likely crowded |
| Reviewroom | Close to the earlier working name | Not done |
| Greenlight | Good for the green check | Not done; likely crowded |
| Gateroom | Says "gate" directly | Not done |

Pick one by Wednesday so the repo, video and submission all use the same name.
