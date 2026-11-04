# Heimdall: start here

Bootstrap note for coding agents. T16 replaces this file with a symlink to the full `AGENTS.md`.

**Read in this order, then start:**
1. `docs/PROGRESS.md`: where the build is and what is next.
2. `docs/harness/AGENTS.md`: the rules (R1 to R12) and the per-task loop. They apply now, even though the hooks are not installed until T16.
3. `docs/STAGE_1_FIXES.md`, if Stage 1 is not finished.
4. `docs/MVP_SCENARIOS.md` section 7: the one build order. Then the task's row in `docs/IMPLEMENTATION_PLAN.md` and the spec sections it cites.

**When docs disagree (D138):** the highest-numbered backlog part from Part 8 on wins, then `docs/TECH_SPEC.md`. `docs/TEST_STRATEGY.md` and `docs/MVP_SCENARIOS.md` govern testing, and `docs/CODING_STANDARDS.md` governs code.

**Always:**
- Do not cut, reduce or mark anything optional unless the owner explicitly says so (D162). There is ample time. If blocked, stop and ask.
- No hacks, shortcuts or workarounds. Implement it cleanly and correctly, or say why it cannot be done.
- One task at a time, red test first, real check before done, evidence in `docs/TRACKER.md` and `docs/PROGRESS.md`. Anything not run is written "not verified".
- Never read, print or commit `.env`, `secrets/`, tokens or private keys. Never paste VM logins into a chat.
- Commit on `heimdall-build` as `T##: what`. Do not push unless the human asks.
- Before any live model run, state the expected cost. The testing cap is $3.
- If a real result contradicts a decision, change the decision and the spec in the same commit. Do not add a fallback.

**Access:** VM over `ssh heimdall-vm`, ports forwarded as in `docs/RUNBOOK.md`. GitHub logins are `aarush323` (author, active) and `rekdfr` (approver). The live demo repo is `aarush323/PBL_TOS_RISK_ANALYZER` (base `heimdall-demo`, never `main`), and the fixture repo is `aarush323/heimdall-fixture-pay`. Switch's source for checking facts is in `~/.t3/projects/switchthon/switch`.
