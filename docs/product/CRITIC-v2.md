# Heimdall: second critic pass (review sub-agent, DeepSeek)

Status: review only. Nothing edited outside this file. Date: Nov 21 2026.
Read: `STRATEGY.md` (draft 2), `ROADMAP.md` (draft 2), `CRITIC-deepseek.md` (draft 1 critique),
`research/08-proof-agent-jobs.md`, `research/09-context-packs.md`.
Also read for claims I doubted: MergeHarbor docs and changelog, CodeRabbit docs and blog, `research/01`,
`research/10`, and the TellOnce paper.
Mode: kill weak ideas. No praise.

Draft 2 is better written than draft 1. That is the only good news, and it is not a reason to build it.

---

## 0. What the web check changed

| Claim in draft 2 | What the pages say | Verdict |
|---|---|---|
| Proof, gate and record are white space (`§5`) | MergeHarbor is GA (Oct 23 2026). It ships proof runs, a merge gate, "proven findings: block", a `regulated` preset, compliance evidence, and self-hosting. All eleven hosts are GA from Nov 1 2026 | False |
| Rules from the merge base is ours alone (`§5`) | MergeHarbor's docs: `CLAUDE.md`, `AGENTS.md`, `.cursorrules` are "read from the merge base and treated as team guidance" | False against MergeHarbor. True against Copilot only |
| Self-hosted review needs a rival enterprise contract (`§5`) | MergeHarbor offers self-hosting and on-premise installs. Self-hosted is now a sales-motion difference, not a product gap | Weaker than claimed |
| Ingesting rivals' findings is a wedge (`§3.1, A5`) | CodeRabbit exposes review findings as JSON, CSV and SARIF, plus runtime validation of a finding with a stored evidence manifest. Its Verify product tests a PR in a sandbox and returns bugs with recordings | Feasible, and partly already done by the rival |
| AI reviewers cannot fail a check (`draft 1 §0`) | Macroscope documents Check Run Agents that fail a check to block a merge | Dead. Do not reuse the old line |
| Memory is free distribution (`§3.2`) | claude-mem is at 118.5k stars and TellOnce is MIT, free, mines corrections, compiles them to runtime checks and enforces them in session. The 57.5% figure is real | The free half of the claim is taken by free tools |
| Draft 2 names zero competitors | Confirmed: `MergeHarbor`, `Ito`, `Heal`, `edgeverdict` appear in neither strategy nor roadmap | A strategy document with no named rival is a wish list |

The damaging sentence, in one line: **the product draft 2 describes shipped, in general availability, one month ago,
with more hosts, more evidence types and a regulated preset.**

I asked in draft 1 for the next research pass to search "runtime verification" and "proof run" instead of "code review".
That did not happen. `research/01` and `research/10` still do not name a single proof-run vendor. Four new research files
were added and none covers the class of product Heimdall wants to be. That is the most expensive hole left in the plan.

---

## 1. One product, or three glued together?

**Three, glued by one word.** The word is "proof", and the E1, E2, E3 tiers are the glue. A buyer cannot compare E3
(a team rule) to E1 (a failing test), so the sentence stays true and the product stays vague.

The seams, with the evidence:

1. **Three buyers, three budgets.** `§4` lists five buyers in one table. The memory user is a developer with no budget.
   The jobs buyer is a security lead at a self-hosted git shop. The check buyer is a platform lead mid rollout. `O1`
   changes the first buyer and leaves the other four in the table.
2. **Three runtimes, two state models.** `A1` promises "a stateless step, no database". `C2` promises a server,
   Postgres and a runner. Two install stories, two support loads, one small team.
3. **Three evidence types called one thing.** The fast check is E2, the proof run is E1, memory is E3. Collapsing them
   into "proof" is how the doc hides that they fail differently.
4. **The independence claim breaks in Stage C.** `§5` says "independent of the writer" is the moat. `C4` and `C6` make
   Heimdall the writer, then certify its own change. A security buyer sees that in the first call.
5. **The privacy seam.** Memory requires mining developer laptops. The regulated buyer, the works council and EU AI Act
   Annex III risk in `research/09 §4.2`, is the buyer who will ban it. `O4` is a patch over this, not a resolution.
6. **The concurrency seam.** `A` runs weeks 1 to 4, `B` weeks 3 to 8, `C` weeks 6 to 12. All three are in flight by
   week 6, and the doc's own risk table says "small team, wide surface".
7. **The pricing seam.** Three units: per committer, per merged PR, and an annual floor. The Jobs tier has no base price
   at all, so a 50-PR allowance is sold on a VM with no monthly fee. That tier cannot be quoted to a buyer today.
8. **The loop diagram runs one way.** `§3` says each part makes the others stronger. Check the arrows. `A` works
   without `B`, because repo files already carry team rules at the merge base. `A` also works without `C`, because
   people and bots already open PRs. `B` sells nothing without `A`. `C` needs `A`. No dependency runs back from `A`.
   A one-way dependency is a roadmap, not one product.

### The cut

**Cut Stage B entirely. All seven work packages.**

Reasons, in order of weight:
- No budget. The user is an individual developer, and the buyer is not that person.
- Free rivals own both halves. claude-mem owns capture, TellOnce owns capture plus in-session enforcement, and
  Copilot Memory is bundled.
- It carries the only legal risk in the plan (`research/09 §4.2`), aimed at the buyer stage D needs.
- It costs weeks 3 to 8, which is exactly when `A7` and the first paid pilot need the same people.

Keep the file format (`rulecards` as YAML in the repo, read at the merge base) as an input to `A2`. That is the useful
half, and it needs no CLI, no miner and no redaction pipeline.

**Do not cut Stage C. Defer it behind one paid partner.** `research/08` scores self-hosted upgrade repair at 21 of 25,
the highest on the board, and it is the only part with a countable backlog and an outcome price. It is also the part
that must not run in parallel. Trigger: one self-hosted git team signs a letter of intent that names its red upgrade
count and its CVE list. Without that signature, C waits.

What replaces the free door: **A4 shadow mode plus the free Action on public repos.** Give away the deterministic
check, not the memory CLI. That is also a better open-source artifact, because the blocking part becomes auditable.

---

## 2. Draft 1 critique, point by point

### 2.1 Section 3, what was missing

| # | Gap from draft 1 | Draft 2 fix | Verdict |
|---|---|---|---|
| 1 | Latency budget with a number | `§7` gives under 2 min fast, under 15 min proof. `A2` accepts on a bench | PARTLY. A target exists. Nothing measures a customer repo, and nothing publishes the number |
| 2 | Flake policy as a product feature | `unknown` verdict, `A6` rerun rule, `A8` break-glass. No quarantine list, no retry-as-evidence | PARTLY |
| 3 | Break-glass and bypass | `A8`, plus "open until reviewed" in `§3` | FIXED |
| 4 | Import path for tools teams already run | `A2` reads SARIF, `A5` reads rival comments, `A2` computes diff coverage | FIXED |
| 5 | A "what we did not catch" report | `A3` prints "what we did not check" | PARTLY. Not checked is a disclaimer. A miss is a post-merge fact and no work package looks for one |
| 6 | Ingest incumbent reviewers as a signal | `A5` | FIXED as a plan item. The design is naive, see 3.2 |
| 7 | Retention, legal hold, offboarding | `D3` | FIXED as a plan item. Correctly last, because the regulated tier is last |
| 8 | Monorepo scale and test impact analysis | Nothing. No work package, no mention | NOT FIXED. The proof run still runs the customer's whole suite, so the stated buyer's repo is 45 minutes, not 15 |
| 9 | Output token limit is a design constraint | Still filed as bugs in Stage 0 (`D6`, `D7`) | PARTLY. `A3` implies structure. Nothing bounds reviewer output as a product rule |
| 10 | Cost ceiling the buyer sees before the invoice | `A10`, plus the cost line in `§8` | FIXED |

### 2.2 Section 4, ordering mistakes

| # | Mistake from draft 1 | Draft 2 | Verdict |
|---|---|---|---|
| 1 | Bench behind ten adoption items | `A0` in week 1, $3 cap | FIXED |
| 2 | Design partners after Stage 1 | GTM week 2 | FIXED |
| 3 | Selling to a buyer whose needs land in month six | Buyer changed, audit moved to an upsell | FIXED |
| 4 | Board shipped with no auth | `A9` | FIXED, but it sits at weeks 1 to 4. It is a live hole. Day 3, not week 3 |
| 5 | Shadow mode last | `A4` | FIXED |
| 6 | GitHub App not a work package | `C2` has it, but Action mode with `GITHUB_TOKEN` removes the need on day one | FIXED by a better route |
| 7 | Language profiles before the bench | `A0` week 1, `C3` week 6 and later | FIXED |
| 8 | Red-first bench after profiles | `A0` first | FIXED |
| 9 | Stage 1 order inverted | Check on PR before job runtime | FIXED |

### 2.3 Section 6, the reason the product fails

| # | Remedy from draft 1 | Draft 2 | Verdict |
|---|---|---|---|
| 1 | Split the gate in two, fast required and slow advisory | `§3`, `A2`, `A3`, `A4` | FIXED |
| 2 | Shadow mode before blocking | `A4` | FIXED |
| 3 | Break-glass as a feature | `A8` | FIXED |
| 4 | Publish p95 next to recall | `§7` targets, landing page "measured numbers only" | PARTLY. No work package measures p95 on a customer repo. No recall number exists yet. The only planned measurement is a bench |
| 5 | A timeout never passes silently | `unknown` always shows a person | FIXED |

Net: draft 2 fixed the sequencing criticism almost completely. It did not fix the market criticism, and it did not fix
the one gap that keeps the product off the stated buyer (monorepo scale). The plan improved. The business did not.

---

## 3. The five riskiest assumptions, each with a cheap test

### 3.1 The white space is real

Assumption: proof, gate and record are still open.
Cheapest test: 2 hours in the docs of MergeHarbor, CodeRabbit, Greptile, Qodo and Gitar. Four questions each: can it
self-host, can it fail a required check, does it run a test to prove a finding, does it read rules at the merge base.
Cost: $0, no engineer, no model.
Kills it if two of five do all four. Today MergeHarbor does all four, and CodeRabbit does three.

### 3.2 A finding that cannot be proven is noise

Assumption: "prove or drop" is a useful filter, and the drop count is the value (`§1`, `A5`).
Why it is weak: proof is asymmetric. A test can show a bug is real. No test shows a finding is false. So "dropped"
means "we did not prove it", and the summary prints that as "noise". That is a claim of correctness without evidence,
and most review comments are design and style, which no test settles.
Cheapest test: take 100 real comments from one partner's CodeRabbit or Copilot history. Classify each as
behaviourally checkable or judgement. Try to write a failing test for 20 of them. Cost: 2 engineer days, no model.
Kills it if fewer than 25% are testable, or if the reviewers say they still read the dropped ones.

### 3.3 A red test from a ticket works often enough

Assumption: the proof run's main evidence (write the test first, watch it fail, then fix) holds as a gate.
Why it is weak: the external rate is 23.6% to 37%. Draft 2 moves this into the risk table, and `A0` does not measure
it. `A0` measures a bench of known bugs plus a 20-PR reviewer run. Nothing in the plan measures fail-to-pass on real
tickets, so the load-bearing number stays unknown.
Cheapest test: 20 real issues from one repo, one model, count fail-to-pass and test-fixup. About $3 at the stated
pricing. 3 days.
Kills it under 40%. Then the pitch has to be diff coverage plus tool evidence, and the name "proof" gets thinner.

### 3.4 The fast check lands under 2 minutes on a real repo

Assumption: `§7` and `A2`. Nothing has run.
Cheapest test: replay the exact deterministic set (secrets, SARIF, pinned-test guard, diff coverage, approval binding)
on the last 30 merged PRs of one real repo, offline, with the clock running and no model call. Half a day.
Kills it over 2 minutes p95, before the model budget is even spent.

### 3.5 The gate stays required, and the buyer pays before it does

Assumption: `§3` and `§8`. The doc bets revenue on the right to block while giving the insight away free.
Cheapest test, in two steps. First, one hour in a partner's branch-protection settings and audit log: admin bypasses
per 100 merges over 12 months, and required checks removed in the same window. Then 10 calls that ask for money: a
30-day paid pilot on one repo, two printed numbers, $5k. Count signatures, not interest.
Kills it if bypass is over 2%, or if fewer than 3 of 10 sign without a discount.

---

## 4. Work package verdicts

Counts: 7 KEEP, 5 CHANGE, 7 CUT, 11 DEFER. The active quarter is 12 work packages, not 30.

### Stage A

| WP | Verdict | One line |
|---|---|---|
| A0 | KEEP | Add two things to the same week: the 100-comment classification set from 3.2, and the timed replay from 3.4. The bench alone does not test the assumptions the pitch stands on |
| A1 | KEEP | Move to week 1 next to A0. "No database" is the design constraint that decides the next nine months |
| A2 | CHANGE | Drop "memory rules" from the scope. Add affected-test selection, or the check is fast on small repos only and the stated buyer never gets it |
| A3 | CHANGE | Print three states: proven, refuted with a counterexample, unproven. "Dropped" is an unproven claim about a rival's finding |
| A4 | KEEP | Ship it first. It is the free door, the trust path and the pitch in one package |
| A5 | CHANGE | Narrow to findings a test can settle, and record the testable share. If it lands near 10%, this is a reprint of the rival, not a filter |
| A6 | CHANGE | Keep the pinned-test guard and the rerun rule. Mock detection and mutation stay out of the merge path: false positives on real seams, and compute nobody will pay for |
| A7 | KEEP | The highest value item on the board. Until it exists, everything else is a slide |
| A8 | KEEP | Cheap, and it removes the best objection in the room |
| A9 | KEEP | Day 3, not week 3. It is a live hole in a product sold to security teams |
| A10 | KEEP | No buyer signs a blank model bill |

### Stage B

| WP | Verdict | One line |
|---|---|---|
| B1 | CUT | Session readers are the start of the second product |
| B2 | CUT | Redaction only matters because B1 exists |
| B3 | CUT | A terminal approval UI for a product with no buyer |
| B4 | CUT | Writing AGENTS.md is 200 lines and every rival and free tool already does it. It is not a door |
| B5 | DEFER | Signed sync is good work for the wrong quarter. It returns with the memory product, if it returns |
| B6 | CUT | Per-card outcome data for a card system nobody installed |
| B7 | CUT | An open-source split of a product you are not building |

### Stage C

| WP | Verdict | One line |
|---|---|---|
| C1 | DEFER | Correct shape, wrong quarter. It also removes Switch, which is a real cleanup, so do it when C starts |
| C2 | DEFER | A VM install is a second support product. Only sell it to a partner who signed |
| C3 | DEFER | Python and Node are weeks of maintenance. Only worth it once C4 has a buyer |
| C4 | DEFER | The strongest scored idea on the board, and still a week 20 item. Trigger: one self-hosted git partner with a named red PR count |
| C5 | DEFER | GitLab is the natural second host and still not this quarter |
| C6 | DEFER | The cleanest demo in the deck. It is not the first paid job |
| C7 | CUT | Webhook versus polling is an ops preference. Polling is fine at this size, and C is not being built now |

### Stage D

| WP | Verdict | One line |
|---|---|---|
| D1 | DEFER | Sign the record and show the offline verifier as a demo. Drop the chain language |
| D2 | DEFER | A feed into Vanta is an upsell for a customer you do not have |
| D3 | DEFER | Correct, and correctly last |
| D4 | CHANGE | Move the threat model and the sandbox escape suite into Stage A. An Action that holds a model key in a customer's CI is sold on those two documents. Fuzzing, SBOM and signed releases can wait |
| D5 | DEFER | Answer it on a call. Do not build it |

---

## 5. The single change that most affects payment in 90 days

**Stop giving shadow mode away, and sell a 30-day paid pilot on one repo with two printed numbers.**

`§8` gives the whole loop away free: proof check in shadow mode, any number of repos, BYO key. The paid tier then sells
"required check, break-glass, memory sync, history". That prices the insight at zero and the right to block at $15 a
seat. Two of the five riskiest assumptions above say that is backwards. If nobody wants to block, revenue is zero and
the free tier is the product. And at $15 a seat the buyer compares Heimdall to CodeRabbit at $24 to $48 and asks why a
second reviewer costs 60%.

Replace those two rows with:

| Plan | Price | Includes |
|---|---|---|
| Free | $0 | Public repos only. Deterministic checks and the summary comment. No private repo |
| Pilot | $5,000 for 30 days, one repo | Shadow mode plus the proof run, on a private repo, with two numbers printed at the end: drop rate and p95 check time. Cancel if either misses the target agreed in writing |
| Team | $15 per active committer per month | Required check, break-glass, proof history, private repos |
| Jobs | Start at $400 per VM per month, 50 merged proof PRs included, then per-fix pricing | Only after one signed partner. The tier needs a base price or it cannot be quoted |

Why this is the biggest lever:
- It creates a payment event in week 4 instead of a seat negotiation in month 6.
- It forces the two numbers the pitch needs, on a buyer's own repo, before any landing page.
- It makes the buyer name a success threshold in writing, which is the only honest way to test `3.2` and `3.5`.
- It costs no engineering. It is a pricing page and one sentence in the sales call.

Second in effect, and it needs no debate: cut Stage B. That returns six weeks to `A7` and the pilot, and it removes the
only part of the plan that a regulated buyer will refuse.

---

## 6. Not verified

- MergeHarbor's check conclusion when a proof run fails on an unproven finding. Read from config keys and the
  changelog, not from a live repo.
- CodeRabbit Verify results from a real run. Docs only. Self-hosted Verify is not offered, which is the one opening
  left in that row.
- Every price in section 5. None has been shown to a buyer.
- Whether any buyer will accept a $5k pilot fee. This is the assumption the pilot exists to test.
