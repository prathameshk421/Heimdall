# Heimdall demo script: shot list (4 minutes)

Source: `docs/DEMO.md` (scene timings and claims). Each claim was checked against `docs/PROGRESS.md` and the test names in `cmd/heimdall-server/`.

Labels:

- **LOCAL** means the scene can be recorded now. It uses the laptop and the GitHub fixture repository. It does not need the VM, Switch, Jira or real agents.
- **LIVE** means the scene needs the VM, Switch, Jira or real agents. No LIVE scene is built yet.

Scenes 1 to 7 and scene 10 are LOCAL. Scenes 8 and 9 are LIVE. Record the LIVE scenes last, and only after the work exists.

The live commands in scenes 3, 5 and 10 write to the fixture repository. Run them only on `aarush323/heimdall-fixture-pay`.

## Pre-flight checklist

Run these steps before you record.

- [ ] Use a worktree at a commit where `make fast` exits 0. Last run: exit 0 at `e22f5d6`, 3,692 tests in 25 s, archcheck and lint clean (Nov 7, 2026).
- [ ] Start Docker. The live tests start a Postgres container.
- [ ] Confirm the owner has set the GitHub App settings for the live tests. Do not open `.env` or `secrets/` to check.
- [ ] Set the `gh` login for scenes 2 and 3 to a login that can read `aarush323/heimdall-fixture-pay`.
- [ ] Run the `gh pr view` command in scene 2 once. Its output changes after each live run.
- [ ] Check whether pull request #1 already has an approval from `prathameshk421`. Last recorded: yes (Nov 5). If it does, show it and do not approve again.
- [ ] Check that pull request #4 is open and approved by `temporaryhameshakagroup-alt` on its head commit. Scene 10 fails without this.
- [ ] Time each live command once. Live tests take longer than a few seconds. If a command runs past its slot, record it before the take and cut to the saved output.
- [ ] Save the output of each live command to a file before the take. Use it for the fallback lines below.
- [ ] If `make fast` fails at lint on macOS, follow the Mac lint note in `docs/PROGRESS.md`. Build the linter with the `SDKROOT` command in that note.
- [ ] Record at 1080p. Show only the fixture repository and seeded data. Turn on captions.

## Scene 1. The problem (0:00 to 0:20). LOCAL

**Screen:** Open `README.md` at the top in the editor.

**On-screen text:** "LGTM is not evidence."

**Narration:** "An approval does not prove that code works. Heimdall asks for evidence first."

## Scene 2. The pull request is blocked (0:20 to 0:50). LOCAL

**Command:**

```bash
gh pr view 1 -R aarush323/heimdall-fixture-pay --json mergeStateStatus,reviewDecision,statusCheckRollup
```

**On-screen text:** JSON output. At the last recorded run, `mergeStateStatus` was `BLOCKED`. The `heimdall/gate` check shows conclusion `failure`.

**Narration:** "This pull request is blocked. Its Heimdall check failed."

**Fallback:** If the output differs, record what you see and change the narration to match. Do not record a state you did not see.

## Scene 3. The slop rule fails on the real diff (0:50 to 1:30). LOCAL

**Command:**

```bash
go test -tags live -count=1 -run TestLiveGateOnFixturePR -v ./cmd/heimdall-server/
```

Then open pull request #1 on GitHub. Click the Checks tab, then click `heimdall/gate`.

**On-screen text:** The terminal prints `--- PASS: TestLiveGateOnFixturePR`. The check summary says the slop rule failed. The last run named three reasons: 660 changed lines outside the tests, no test changed, and paths outside the expected set.

**Narration:** "The slop rule failed on the real diff. No reviewer had to read it."

**Fallback:** Show the saved output from the last run. Caption it with the date. Do not show a new run that you did not see pass.

## Scene 4. A second person approves (1:30 to 2:10). LOCAL

**Command:** Use a GitHub login that is not `aarush323`. The last run used `prathameshk421`. Pass its token for this command only.

```bash
GH_TOKEN=$(gh auth token --user <login>) gh pr review 1 --approve -R aarush323/heimdall-fixture-pay
```

**On-screen text:** The pull request page shows one approval.

**Narration:** "A second person approves the pull request on GitHub."

**Fallback:** If the approval already exists (see the pre-flight check), show it on the pull request page. Do not approve again.

## Scene 5. Heimdall reads the approval (2:10 to 2:40). LOCAL

**Command:**

```bash
go test -tags live -count=1 -run TestLiveGateAfterApproval -v ./cmd/heimdall-server/
```

**On-screen text:** The test reads one approval on the head commit. The output lists the rules that still fail. The last run named these:

- the approver is not in the domain for the changed paths
- the review requests are not known
- the test tree hash at head is not known
- the CI checks `build`, `lint` and `heimdall/acceptance` are missing

The output lists six reasons in total. Show the list as it prints.

**Narration:** "Heimdall reads the approval. The gate still lists the rules that fail."

**Fallback:** Show the saved output from the last run. Caption it with the date.

## Scene 6. The local checks pass (2:40 to 3:05). LOCAL

**Command:**

```bash
make fast
```

**On-screen text:** The format, vet, dependency rule, lint and unit test steps run. The command ends with exit status 0.

**Narration:** "One command runs the format, vet, dependency, lint and unit test checks."

**Fallback:** If the command fails on macOS at lint, use the Mac lint note in `docs/PROGRESS.md` before you record.

## Scene 7. What is still open (3:05 to 3:25). LOCAL

**Screen:** Scroll in `README.md` to the section "Does not work yet".

**On-screen text:** The table shows that live Jira, live Switch and live model calls are not verified.

**Narration:** "Agents, Jira and Switch still run on fakes. The green result you will see next used seeded agent events."

## Scene 8. A ticket opens a room (3:25 to 3:40). LIVE

**Depends on:** live Jira and live Switch calls. Steps P1.1 and P1.2 in `docs/MVP_SCENARIOS.md`. Neither is verified.

**Screen:** Move a test ticket to In Progress in Jira. Show the Switch room that opens for it, with a first message.

**On-screen text:** A Switch room named for the ticket. A first message from Heimdall.

**Narration:** "When a ticket moves, Heimdall opens a room for it."

**Fallback:** If P1.1 and P1.2 do not run on the VM before recording day, cut this scene. Show the README "What works today" table instead. Use this narration: "The ticket to room step runs on fakes. Live Jira and Switch calls come next."

## Scene 9. Reviewers report findings (3:40 to 3:52). LIVE

**Depends on:** live model agents and the review flow. Steps P3.2 and P3.3 in `docs/MVP_SCENARIOS.md`. Neither is run.

**Screen:** Start the review in the room, as the author. Show three reviewer findings. Each finding shows a file, a line and its evidence.

**On-screen text:** Three findings from three reviewers. Each has a file and a line.

**Narration:** "Each reviewer reports a finding, and each finding shows its evidence."

**Fallback:** If no live reviewer run exists, cut this scene. Show the "How it works" steps 6 and 7 in `README.md`. Use this narration: "Reviewer agents read the diff and file findings. This step does not run on the real system yet."

## Scene 10. The check turns green (3:52 to 4:00). LOCAL, with seeded events

**Before:** Pull request #4 on the fixture repository must be open. It must be approved on its head commit by `temporaryhameshakagroup-alt`, not by its author.

**Commands:**

```bash
go test -tags live -count=1 -run TestLiveGateGreenPath -v ./cmd/heimdall-server/
gh pr view 4 -R aarush323/heimdall-fixture-pay --json mergeStateStatus,statusCheckRollup
```

**On-screen text:** The test prints `--- PASS: TestLiveGateGreenPath`. The final gate result has no reasons. The pull request shows `heimdall/gate` with conclusion `SUCCESS` and merge state `CLEAN`.

**Narration:** "The check turns green only after a person who is not the author approves this exact commit. The room, tests and reviewer events in this run are seeded. The agents will produce them later."

**Fallback:** If the test fails, show the saved output from the last passing run. Caption it with the date. Do not show a green result you did not see.

## Scene timing check

The scene lengths add up to 240 seconds (4:00):

| Scenes | Length (s) |
|---|---|
| 1 to 7 | 20, 30, 40, 40, 30, 25, 20 |
| 8 to 10 | 15, 12, 8 |

The live commands in scenes 3, 5 and 10 must fit their slots. Use the pre-flight timing check to confirm this.
