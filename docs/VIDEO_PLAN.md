# Heimdall demo video plan

Rewritten Nov 7, 2026, 21:00 IST, after owner direction. Deadline **23:59 IST**. Internal: upload 23:15, submit 23:30.

**Superseded in part (21:40 IST):** the built film is `video/heimdall/` (first cut `video/heimdall-demo-v1-norm.mp4`, 2:00). Its script is `video/heimdall/vo/script.txt` and its look follows `docs/VIDEO_DESIGN.md`. The film runs on real Jira, Discord, Telegram and GitHub captures, with less text than the script below.

## The pitch the whole video sells

> **Heimdall captures what each developer knows and gives it to that developer's own reviewer agent. Then it makes every agent prove what it finds.**

Tagline: **LGTM is not evidence.** Hook line: **The dev is asleep. Their context isn't.**

Order of emphasis:

1. Per-dev context going to per-dev reviewers. This is the main story.
2. Reviewers with real failing tests, re-run by Heimdall.
3. Red-first tests and the judge, in a locked sandbox.
4. OpenCode agents on a small VM, inside Switch.
5. Go engineering that fits all this on 2 cores.

The refund bug is one example inside step 2. It is no longer the hook.

## What goes on screen, and how

**Real footage** shows what ran. **Motion graphics** explain the architecture from `HEIMDALL_MVP.md` and `TECH_SPEC.md`.

| Part | Treatment | State today |
|---|---|---|
| Pack pipeline (collect, redact, distill, approve, sign, encrypt, upload, read) | Hero motion graphic | Code merged and unit tested in `internal/pack`, `cmd/heimdall/pack_local.go`, `read_pack` tool. 0 packs on the VM. Not run against the live server |
| Graphiti team memory | Motion graphic using the real ontology from TECH_SPEC 9.2 | Graphiti and FalkorDB run on the VM. Graph is empty, so the graphic is illustrative |
| CI/CD | Motion graphic of tests entering the pipeline. VO: "becomes your CI gate" | `heimdall/acceptance` is a real check today. The Actions workflow is to be built after the video |
| Jira, Discord, Telegram, GitHub | **Real browser capture** | Real |
| OpenCode agents, VM | Real terminal plus graphic | Real, measured |
| Red-first judge, sandbox | Real rejection text plus graphic | Real |
| Reviewer failing test, line comments, Heimdall Review | **Real capture** | Real on PR #9, new take preferred |
| Proof re-run, red gate from a finding, approval, merge | Real capture if the take shows it, otherwise cut | Not seen live yet |

**One line I am holding:** no fake product screens. A graphic can explain the design, but I will not draw a Discord message, GitHub comment or Actions run that never happened, and no reviewer gets shown citing a pack unless one really did. Motion scenes for unrun parts carry a small `ARCHITECTURE` corner tag, and the README has a "live today vs built" table. Judges can clone the repo, so this protects the entry.

**The upgrade worth trying:** if one real pack gets through `heimdall pack refresh` and a reviewer reads it before the freeze, the hero graphic can cut straight into real footage. Time box in the timeline below. Blockers known: `upload_url` must be HTTPS, the distiller uses OpenRouter, and the reviewer skill has no `read_pack` step yet.

## Numbers, measured

| Claim on screen | Source |
|---|---|
| **Room in under a second** | `change.triggered` to `room.opened`: 0.3 to 0.9 s on KAN-16, 17, 18, 20, 21 |
| **Task handed over in about 3 s** | to `task.assigned`: 2.6 to 5.1 s across all tickets |
| **Agent working in about 12 to 23 s** | to `task.acked`: 11.5 s best (KAN-17), 23.2 s on KAN-21 |
| Reviewer finds and files in 91 s | KAN-21, assigned 14:41:18, filed 14:42:49 |
| `heimdall-server` 41.8 MiB, runner 10.3 MB | `docker stats`, `ps`, idle |
| Binaries 26 MB and 8.2 MB | `ls` on the VM |
| 2 vCPU, 2.7 GiB used, 9 containers, 7 agents | VM, re-measure at lock |
| Agent units use about 67 MB, one 580 MB; OpenCode about 320 MB when active | `systemctl`, `ps` |
| 241 commits in 4 days, about 3,978 tests | git, last `make fast` |

**"Sub 1 s Jira to agent working" is not true, so it is not used.** Sub-second is pickup to room. The agent takes 12 to 23 s to start, because it wakes through chat and a model call. All times start when Heimdall's poll sees the move, and Jira is polled every 30 s. The line used: "Under a second, there's a room. Seconds later, an agent is working." The take's real numbers go on screen.

## Script (2:55)

About 380 words of VO. Every line works muted because type carries the claim.

### 1. Hook (0:00 to 0:14)

Pitch dark. Silence, then one pulse. A clock: `02:47`. A PR card slides in, checks go green, `LGTM` stamps down. Elsewhere in frame, a laptop lid closed, one dim gold dot on it. The dot brightens. Thin gold threads start to rise from it, and the music enters.

Type, one line at a time: **The dev who'd catch this is asleep.** Hard cut. **Their context isn't.** Helm mark. **Heimdall.**

> VO: "Every team has one developer who would catch this bug. Tonight, they're asleep. AI reviewers read the diff. They don't know what your team knows. Heimdall does."

### 2. The promise (0:14 to 0:22)

The Bifröst draws across the frame. Three claims land on the beat.

**Captures what each dev knows. · Gives it to their reviewer agent. · Makes every agent prove it.**

> VO: "It captures what each developer knows, gives it to that developer's own reviewer agent, and makes every agent prove what it finds."

### 3. Capture: the hero sequence (0:22 to 0:50), `ARCHITECTURE`

This is the shot people remember. The laptop opens. We fly in.

| Time | Picture |
|---|---|
| 0:22 | Real-looking files lift off and orbit, each with its true name: `CLAUDE.md`, `AGENTS.md`, `docs/adr/004.md`, `git log --author`, `git blame`, Claude Code, Codex and OpenCode sessions, review history |
| 0:29 | They pass through a scanner plane. Secrets dissolve: `AKIA…` and `bearer …` burn into typed placeholders `[REDACTED:aws_key]`. Label: **gitleaks · entropy · PII. Before any model.** |
| 0:34 | The stream condenses into six cards, stacking into a pack: `ownership · invariants · incidents · conventions · review_history · unknowns`. One line expands: `Refund ≤ original payment [git:3f2a1c]`. Provenance glows |
| 0:39 | A second scan. A person's hand-cursor clicks **Approve**. An ed25519 signature stamps it. An `age` lock snaps shut. The sealed pack leaves along a beam |
| 0:44 | Pull back wide: three laptops, three colours of thread, three packs, three agents light up, each only reached by its own thread. Canary dots ride along. Label: **Raw transcripts never leave the laptop.** |
| 0:47 | Below the agents, a Graphiti graph blooms: `Person`, `Decision`, `Incident`, `Module`, `Field`, with edges `DECIDED`, `CAUSED_BY`, `OWNS`. A fact goes grey as a newer one replaces it. Label: **Team memory. Every fact knows when it was true.** |

> VO: "It starts on the developer's laptop, with what they already have. Git history. Docs and ADRs. Their Claude Code, Codex and OpenCode sessions. Secrets and personal data are stripped before any model sees them. What's left is distilled into a pack: what this dev owns, the rules they enforce, the incidents they remember, even what they don't know. They read it. They approve it. It's signed, encrypted and sent. The raw transcripts never leave the laptop. Three devs, three packs, three reviewers. And beneath them, shared memory: a Graphiti knowledge graph of your tickets, decisions and incidents."

### 4. The machine (0:50 to 1:06)

Camera dives along the beam into a cross-section of the VM, drawn as an isometric block.

| Time | Picture | Source |
|---|---|---|
| 0:50 | Seven agent cells light in sequence, each labelled with its role and its own `sa1000-0x` user. Firewall ring around each | Real names, graphic |
| 0:55 | Slam to real terminal, 1.5 s: `systemctl` listing `switch-agent-1000@01..07` | **Real** |
| 0:57 | Switch at the centre of the block. Rooms spawn per ticket. Lines run out to Discord and Telegram logos | Graphic |
| 1:01 | Real Discord and Telegram windows side by side, same card arriving in both | **Real** |
| 1:04 | Plate: `2 cores. 7 agents. 9 containers.` | Measured |

> VO: "All of it runs on one small VM. Each reviewer is an OpenCode agent with its own Unix user and its own firewall, living inside Switch. Switch gives every ticket a room and every agent an identity, and puts it all in Discord and Telegram, where your team already is."

### 5. Ticket to tests (1:06 to 1:38)

| Time | Picture | Source |
|---|---|---|
| 1:06 | Real Jira board, ticket dragged to In Progress | **Real** |
| 1:09 | Three clocks pop beside the footage: room `0.9 s`, task `3 s`, agent `23 s`, from the take | **Real** |
| 1:13 | Discord: Test Agent progress line. If it asks a question and someone answers, show it | **Real** |
| 1:18 | Tests drop into a glass box. Flags snap on: `--network=none`, `--read-only`, `--cap-drop=ALL`, `rootless`. Code runs inside | Graphic with real flags |
| 1:24 | **The judge.** A test turns green inside the box. Alarm tone. Real text stamps across: `passed without the feature on base`. **REJECTED.** It goes back to the agent. Real Discord rejection line if the take has one | **Real text** |
| 1:30 | Tests go red for the right reason. Tree hash pins. Commit appears on the real GitHub branch | **Real** |
| 1:34 | Test block flows on into a CI/CD pipeline strip: `tests → required check → gate`. Check `heimdall/acceptance` lights | Graphic |

> VO: "Here's a real ticket. It moves in Jira. Under a second later, there's a room. Seconds later, an agent is working, writing tests from the ticket before any code exists. And Heimdall doesn't trust them. It runs them in a locked sandbox, with no network and no capabilities. They must fail, and for the right reason. A test that passes before the feature exists goes straight back. That's a real rejection. What survives becomes your CI gate."

### 6. The review (1:38 to 2:12), the proof

| Time | Picture | Source |
|---|---|---|
| 1:38 | Real GitHub: PR opens, acceptance green. Type: **Tests pass. Would you merge it?** | **Real** |
| 1:43 | The beam splits into three. The three sealed packs from Act 3 unlock and flow in, one each, then the graph sends a pulse to all three. Each lane is labelled with its dev's colour | Graphic, callback |
| 1:48 | Real Discord: three reviewers ack, cut off from each other. Label: **They can't see each other's work.** | **Real** |
| 1:53 | Real comment: the failing test types out, `TestRefund_CumulativeCap`. Two refunds of 60 on a payment of 100. `FAIL` flash | **Real** |
| 1:59 | Evidence ladder rises: `E1 failing test`, `E2 tool run`, `E3 confirmed memory`, `E4 opinion`. E4 greys: **never blocks alone** | Graphic, from the skill |
| 2:03 | Heimdall re-runs the proof. **Bug confirmed.** Only if the take shows it | **Real** or cut |
| 2:06 | Fast three-way: GitHub Files tab line comment, Heimdall Review comment scrolling, Discord card | **Real** |

> VO: "A dev raises the PR. The tests pass. Now the reviewers wake, each with its own developer's context, cut off from each other. This one writes a test that fails on this commit: two refunds of sixty on a payment of a hundred. Heimdall runs that test itself. Not an opinion. Proof. On the exact line, on the PR, in chat."

### 7. The gate (2:12 to 2:28)

The beam hits a wall of ten plates, each a real clause name from `gate.go`: `ticket · slop · testset · tree pinned · test diff · CI · reviews · blocking findings · approval · policy`. They flip one by one. Real `heimdall/gate` check goes red, or green if the take reached it.

> VO: "Then ten rules decide, in plain Go. No model is anywhere in that path. Only a person can approve, and only that exact commit."

### 8. The engineering (2:28 to 2:44)

Fast, on the beat, each plate over real code or a real terminal.

`41.8 MiB` server → `10 MB` sandbox runner → `Event sourced` → `No public ports` → `304s cost no rate limit` → a scroll of real log lines from live failures (`uid_map`, `ping_group_range`, 1.4M tokens) → `241 commits. 4 days.`

> VO: "And it's Go, top to bottom. A forty-two megabyte server. A ten megabyte sandbox runner. Event sourced, with no public ports. Two cores. We broke it live, over and over, until it held."

### 9. Close (2:44 to 2:55)

Back to the closed laptop, `02:47`. Threads still glowing. The gate opens and the full Bifröst spectrum runs to the horizon over the Asgard painting. **LGTM is not evidence.** Then `Heimdall · built on Switch · repo link`. Final hit on the last beat.

> VO: "Humans bring judgment. Agents bring evidence. Heimdall."

### The 20 s cut for #HowISwitch

Closed laptop, threads → files fly → pack seals → three agents → failing test `FAIL` → gate → logo. Music only.

Copy: "The dev who'd catch this bug is asleep. Their context isn't. Heimdall gives each developer's knowledge to their own reviewer agent, then makes it prove what it finds. Built on Switch. #HowISwitch"

## Production

**Hyperframes** ([heygen-com/hyperframes](https://github.com/heygen-com/hyperframes), Apache 2.0) replaces my earlier hand-made renderer. It renders HTML and seekable GSAP to deterministic MP4, which is the same method as the best entries in the awesome-opus5-5 list. It needs Node, so it stays in `video/` and never in the product.

Direction taken from the references:

- From the TanStack launch-film prompt: every transition demonstrates a feature instead of replacing a title card. No spinning logos, no fake dashboards, no tiny code. Must work muted.
- From `/brag`: hook first, real UI, 0.3 s per word on screen, stagger crossfades, every frozen frame postable.
- From the deterministic-render method: every frame is a function of its number. Stills from each scene and each transition get checked before a full render.

Look: plum night `#2a2238`, gold `#d9b064`, Bifröst gradient, Instrument Serif, Geist and Geist Mono, painted art from `site/art/`.

Audio: music by Sascha Ende, ende.app (CC BY 4.0, credit in the description), cuts on its beat data. Voice: AI unless a person records by 22:45.

Capture: `ffmpeg x11grab` at 1080p, always on during takes. Discord, Telegram, Jira and GitHub in the browser at 125% zoom, notifications off. Frame scan for emails, tokens and VM hosts before upload.

## Timeline (IST)

| Time | System | Video | Submission |
|---|---|---|---|
| 21:00 | Stabilise VM, reviewer `read_pack` step | Hyperframes, hero Act 3 | Owner flips repo public after scan |
| 21:30 | **Take 1** on a new ticket, recording | Acts 1, 2, 4 | README rewrite |
| 21:50 | Fix what broke. Pack box, 20 min | Acts 7, 8, 9 | Blog draft |
| 22:10 | **Take 2** | Act 5 and 6 graphics | |
| 22:30 | **Live freeze** | Cut footage in | Blog review |
| 22:45 | | Lock VO, captions, mix | |
| 23:05 | | Final render, QA | |
| 23:15 | | YouTube upload, "Not for kids", check logged out | |
| 23:30 | | | Submit. Blog live. Post the 20 s cut |

## Submission blockers

1. Repo `prathameshk421/flip_the_switch_hackathon` is **private**, and `aarush323` is not admin. That owner must make it public, after a secrets scan.
2. `origin/main` is behind `heimdall-build`. Needs a yes to push.
3. README still says agents run on fakes. Rewrite with: which agents, how the channel is set up, how to run it, and live vs built.

## Needs a person

| What | Who | By |
|---|---|---|
| Discord and Telegram open in the browser I can reach (pane, or connect Claude in Chrome) | You | Before take 1 |
| Provider credit if we try the real pack or Graphiti | You | 21:50 |
| Repo public | `prathameshk421` | 22:00 |
| YouTube upload and submission form | You | 23:15 |

## Naming

The stack is **Graphiti**, the temporal knowledge graph. "Graphite" is only an unrelated code review company, and it stays out of the video and README.

## Sources

[Hyperframes](https://github.com/heygen-com/hyperframes), [awesome-opus5-5-videos](https://github.com/yihui-dev/awesome-opus5-5-videos), [brag](https://github.com/latent-spaces/brag), [Devpost tips](https://info.devpost.com/blog/6-tips-for-making-a-hackathon-demo-video), [DevTools Academy page](https://www.devtoolsacademy.com/hackathons/flip-the-switch).
