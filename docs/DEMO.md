# Heimdall demo script (4 minutes)

Scenes 1 to 7 and scene 10 use only steps that run today. Scenes 8 and 9 are planned. They depend on work that is not built or not yet run on the real system. Record them last, and only after that work exists.

## Before you record

- Start Docker. The live tests start a Postgres container.
- Set the active `gh` login to an account that can read `aarush323/heimdall-fixture-pay`.
- The live tests read the GitHub App credentials at run time and never print them. A test skips with a message if they are missing.
- The two live commands write to the fixture repository. Run them only on that repository.
- Run the `gh pr view` command once before you record. Its output changes after each live run.
- For scene 4, use a GitHub login that is not the author of pull request #1. The last run used `prathameshk421`.
- If `make fast` fails at lint on macOS, follow the Mac lint note in `docs/PROGRESS.md`.

## Scene 1. The problem (0:00 to 0:20). Built

**Action:** Open `README.md` at the top in the editor.

**Viewer sees:** The title and the line "LGTM is not evidence."

**Say:** "An approval does not prove that code works, so Heimdall asks for evidence first."

## Scene 2. The pull request is blocked (0:20 to 0:50). Built

**Action:** Run this command in a terminal.

```bash
gh pr view 1 -R aarush323/heimdall-fixture-pay --json mergeStateStatus,reviewDecision,statusCheckRollup
```

**Viewer sees:** JSON output. At the last recorded run, `mergeStateStatus` was `BLOCKED`. The `heimdall/gate` check shows a failed conclusion.

**Say:** "This pull request is blocked, and its Heimdall check failed."

## Scene 3. The slop rule fails on the real diff (0:50 to 1:30). Built

**Action:** Run this command in a terminal.

```bash
go test -tags live -count=1 -run TestLiveGateOnFixturePR -v ./cmd/heimdall-server/
```

Then open pull request #1 on GitHub. Click the Checks tab, and then click `heimdall/gate`.

**Viewer sees:** The terminal prints `--- PASS: TestLiveGateOnFixturePR`. The check summary says the slop rule failed. The last run named three reasons: 660 changed lines outside the tests, no test changed, and paths outside the expected set.

**Say:** "The slop rule failed on the real diff, so no reviewer had to read it."

## Scene 4. A second person approves (1:30 to 2:10). Built

**Action:** Run this command with a GitHub login that is not the author.

```bash
gh pr review 1 --approve -R aarush323/heimdall-fixture-pay
```

**Viewer sees:** The pull request page shows one approval.

**Say:** "A second person approves the pull request on GitHub."

## Scene 5. Heimdall reads the approval (2:10 to 2:40). Built

**Action:** Run this command in a terminal.

```bash
go test -tags live -count=1 -run TestLiveGateAfterApproval -v ./cmd/heimdall-server/
```

**Viewer sees:** The test reads the approval on the head commit. The last run recorded one approval. The output also names the rules that still fail. Those were: the approver is not in the domain for the changed paths, the review requests are not known, and the CI checks `build`, `lint` and `heimdall/acceptance` are missing.

**Say:** "Heimdall reads the approval, and the gate lists the rules that still fail."

## Scene 6. The local checks pass (2:40 to 3:05). Built

**Action:** Run this command in the repository.

```bash
make fast
```

**Viewer sees:** The format, vet, dependency, lint and unit test steps run. The command exits with status 0.

**Say:** "A pre-commit hook runs these checks before each commit."

## Scene 7. What is still open (3:05 to 3:25). Built

**Action:** Scroll in `README.md` to the section "Does not work yet".

**Viewer sees:** Live Jira, Switch and model calls are not verified. The green path ran once with seeded events.

**Say:** "Agents, Jira and Switch still run on fakes. The green result you will see next used seeded agent events."

## Scene 8. A ticket opens a room (3:25 to 3:40). Planned

**Depends on:** live Jira and live Switch calls (`docs/MVP_SCENARIOS.md`, steps P1.1 and P1.2). Neither is verified.

**Action:** Move a test ticket to In Progress in Jira.

**Viewer sees:** A Switch room named for the ticket, with a first message.

**Say:** "When a ticket moves, Heimdall opens a room for it."

## Scene 9. Reviewers report findings (3:40 to 3:52). Planned

**Depends on:** live model agents and the review flow (`docs/MVP_SCENARIOS.md`, steps P3.2 and P3.3). Neither is run.

**Action:** Start the review in the room, as the author.

**Viewer sees:** Three reviewers each report a different finding. Each finding shows a file, a line and its evidence.

**Say:** "Each reviewer reports a finding, and each finding shows its evidence."

## Scene 10. The check turns green (3:52 to 4:00). Built, with seeded events

**Before:** pull request #4 on the fixture repository must be open and approved on its head commit by a login that is not the author. The last run used `temporaryhameshakagroup-alt`.

**Action:** Run these commands in a terminal.

```bash
go test -tags live -count=1 -run TestLiveGateGreenPath -v ./cmd/heimdall-server/
gh pr view 4 -R aarush323/heimdall-fixture-pay --json mergeStateStatus,statusCheckRollup
```

**Viewer sees:** The test prints `--- PASS: TestLiveGateGreenPath` and the final gate result with no reasons. The pull request shows `heimdall/gate` with conclusion `SUCCESS` and the merge state `CLEAN`.

**Say:** "The check turns green only because a person who is not the author approved this exact commit. The room, tests and reviewer events in this run are seeded; the agents will produce them."
