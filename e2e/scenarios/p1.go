package scenarios

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

func p1() e2e.Scenario {
	return e2e.Scenario{
		ID: "P1", Title: "Ticket to red tests",
		// A room for the change and one for the sensitive ticket: opening lines, the question, the tests-ready lines
		// and a refusal. The sign-off request is the one decision.
		Budget: e2e.Budget{Milestones: 6, Questions: 1, Decisions: 1},
		Steps:  []e2e.Step{p11(), p12(), p13(), p14(), p15(), p16()},
	}
}

func p11() e2e.Step {
	return e2e.Step{
		ID:   "P1.1",
		Does: "In the tracker, assign `ticket` to the developer and move it to In Progress",
		Sees: "within 45 s: a change exists, and a branch `<KEY>-<slug>` exists on GitHub",
		Tags: []string{"local"},
		// server: the poller makes the change; tracker: the person works in Jira; github-user and fixture-repo:
		// the branch is read on the story's repository.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedTracker, e2e.NeedGitHubUser, e2e.NeedFixtureRepo},
		Bar:   45 * time.Second,
		Run:   runP11,
	}
}

// runP11 is read as: the change is seen through its branch (the room follows in P1.2).
func runP11(ctx context.Context, e *e2e.Env) error {
	key, err := startTicket(ctx, e, e.Story.Ticket)
	if err != nil {
		return err
	}
	e.Set(keyTicket, key)
	var branch string
	err = e.World.Clock().Until(ctx, "a branch for "+key, 45*time.Second, func(ctx context.Context) (bool, string, error) {
		branches, err := e.World.GitHub().Branches(ctx, e.Story.Repo)
		if err != nil {
			return false, "", err
		}
		for _, b := range branches {
			if strings.HasPrefix(b, key+"-") {
				branch = b
				return true, "", nil
			}
		}
		return false, fmt.Sprintf("%d branches, none starting with %s-", len(branches), key), nil
	})
	if err != nil {
		return err
	}
	e.Set(keyBranch, branch)
	return nil
}

func p12() e2e.Step {
	return e2e.Step{
		ID:   "P1.2",
		Does: "nothing",
		Sees: "a room named for the ticket opens in the chat, with the ticket linked, the developer and `test-author` in it, and a first message that says what happens next in plain words",
		Tags: []string{"vm"},
		// server: the product opens it; chat-user and forward: the room is read through the port-forward to the chat.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedForward, e2e.NeedChatUser},
		After: "P1.1",
		Run:   runP12,
	}
}

func runP12(ctx context.Context, e *e2e.Env) error {
	key, err := earlier[string](e, "P1.1", keyTicket)
	if err != nil {
		return err
	}
	room, err := awaitRoom(ctx, e, key)
	if err != nil {
		return err
	}
	e.Set(keyRoom, room)
	members, err := e.World.Chat().Members(ctx, room)
	if err != nil {
		return err
	}
	for _, want := range []string{e.Story.Author().Chat, testAuthor} {
		if !contains(members, want) {
			return &e2e.Red{Expected: want + " in the room", Saw: "members " + strings.Join(members, ", ")}
		}
	}
	ms, err := readRoom(ctx, e, room)
	if err != nil {
		return err
	}
	return judgeOpening(ms, key)
}

// judgeOpening checks the room's first product message and that the ticket is linked somewhere in it.
func judgeOpening(ms []e2e.Message, key string) error {
	linked := false
	for _, m := range ms {
		linked = linked || strings.Contains(m.Text, key)
	}
	if !linked {
		return &e2e.Red{Expected: "the ticket " + key + " linked in the room", Saw: fmt.Sprintf("%d messages, none mentions it", len(ms))}
	}
	for _, m := range ms {
		if !m.FromAgent {
			continue
		}
		if words := len(strings.Fields(m.Text)); words < 6 {
			return &e2e.Red{Expected: "a first message that says what happens next", Saw: fmt.Sprintf("%q (%d words)", m.Text, words)}
		}
		return nil
	}
	return &e2e.Red{Expected: "a first message from the product", Saw: fmt.Sprintf("%d messages, none from the product", len(ms))}
}

func contains(list []string, want string) bool {
	for _, x := range list {
		if x == want {
			return true
		}
	}
	return false
}

func p13() e2e.Step {
	return e2e.Step{
		ID:   "P1.3",
		Does: "answer the one question in the room (if the Story has one)",
		Sees: "the question was a single message with concrete options and an @-mention of the developer; after the answer, no second question is asked",
		Tags: []string{"vm"},
		// server: the product asks; chat-user and forward: the person answers in the room. Live agents may play the
		// test-author at L5, but the step does not need them.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedForward, e2e.NeedChatUser},
		After: "P1.2",
		Run:   runP13,
	}
}

var reOptions = regexp.MustCompile(`(?im)\bor\b|^\s*(?:[-*]|\d+[.)])\s`)

// runP13 reads "concrete options" as: the question offers alternatives joined by "or" or as a list.
func runP13(ctx context.Context, e *e2e.Env) error {
	room, err := earlier[string](e, "P1.2", keyRoom)
	if err != nil {
		return err
	}
	if e.Story.Question == "" {
		ms, err := readRoom(ctx, e, room)
		if err != nil {
			return err
		}
		if n := count(ms, ofKind(e2e.KindQuestion)); n > 0 {
			return &e2e.Red{Expected: "no question, because the story has none", Saw: fmt.Sprintf("%d questions", n)}
		}
		return nil
	}
	q, err := awaitMessage(ctx, e, room, "the one question", testsWait, ofKind(e2e.KindQuestion))
	if err != nil {
		return err
	}
	if !mentions(q, e.Story.Author().Chat) {
		return &e2e.Red{Expected: "the question to @-mention the developer " + e.Story.Author().Chat, Saw: fmt.Sprintf("mentions %v", q.Mentions)}
	}
	if !reOptions.MatchString(q.Text) {
		return &e2e.Red{Expected: "concrete options in the question", Saw: q.Text}
	}
	return answerOnce(ctx, e, room)
}

// answerOnce answers as the developer and checks the product asked exactly one question, before and after.
func answerOnce(ctx context.Context, e *e2e.Env, room string) error {
	before, err := readRoom(ctx, e, room)
	if err != nil {
		return err
	}
	known := knownIDs(before)
	if n := count(before, ofKind(e2e.KindQuestion)); n != 1 {
		return &e2e.Red{Expected: "a single question", Saw: fmt.Sprintf("%d questions", n)}
	}
	if err := e.World.Chat().Say(ctx, room, e.Story.Author().Chat, e.Story.Answer); err != nil {
		return err
	}
	_, err = awaitMessage(ctx, e, room, "the product to carry on after the answer", testsWait, func(m e2e.Message) bool {
		return !known[m.ID] && m.Kind != e2e.KindQuestion
	})
	if err != nil {
		return err
	}
	after, err := readRoom(ctx, e, room)
	if err != nil {
		return err
	}
	if n := count(after, ofKind(e2e.KindQuestion)); n != 1 {
		return &e2e.Red{Expected: "no second question after the answer", Saw: fmt.Sprintf("%d questions", n)}
	}
	return nil
}

func p14() e2e.Step {
	return e2e.Step{
		ID:   "P1.4",
		Does: "nothing",
		Sees: "commits appear on the branch under `tests/heimdall/<KEY>/`, authored by the App, one test per AC at least, **every test fails on the base branch with an assertion**, and the room says \"N failing tests ready\" with the AC map",
		Tags: []string{"local", "scripted"},
		// server: the product writes the tests; github-user and fixture-repo: the commits are read on GitHub;
		// chat-user and forward: the room announces them; scripted: the test-author is the scripted agent;
		// runner is reached through World.Runner, which has no separate prerequisite.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedForward, e2e.NeedGitHubUser, e2e.NeedChatUser, e2e.NeedFixtureRepo, e2e.NeedScripted},
		After: "P1.3",
		Run:   runP14,
	}
}

// The contract for the tests-ready line (P1.4, P1.6): "N failing tests ready", then one "AC<i>" per criterion for the map.
var reReady = regexp.MustCompile(`(\d+) failing tests? ready`)

func runP14(ctx context.Context, e *e2e.Env) error {
	key, branch, room, err := changeFacts(e, "P1.3")
	if err != nil {
		return err
	}
	ready, err := awaitMessage(ctx, e, room, "the failing tests to be ready", testsWait, matching(reReady))
	if err != nil {
		return err
	}
	if err := judgeTestCommits(ctx, e, key, branch); err != nil {
		return err
	}
	runs, err := e.World.Runner().RunOnBase(ctx, e.Story.Repo, e.Story.BaseBranch, branch)
	if err != nil {
		return err
	}
	if err := judgeRuns(runs, len(e.Story.Ticket.AC)); err != nil {
		return err
	}
	return judgeReady(ready, len(runs), len(e.Story.Ticket.AC))
}

func changeFacts(e *e2e.Env, step string) (key, branch, room string, err error) {
	if key, err = earlier[string](e, step, keyTicket); err != nil {
		return
	}
	if branch, err = earlier[string](e, "P1.1", keyBranch); err != nil {
		return
	}
	room, err = earlier[string](e, "P1.2", keyRoom)
	return
}

// judgeTestCommits checks the commits under the tests directory exist and are the App's.
func judgeTestCommits(ctx context.Context, e *e2e.Env, key, branch string) error {
	commits, err := e.World.GitHub().Commits(ctx, e.Story.Repo, branch)
	if err != nil {
		return err
	}
	prefix := "tests/heimdall/" + key + "/"
	found := 0
	for _, c := range commits {
		if !touches(c, prefix) {
			continue
		}
		found++
		if !strings.HasSuffix(c.Author, botSuffix) {
			return &e2e.Red{Expected: "the tests to be authored by the App (a " + botSuffix + " login)", Saw: "authored by " + c.Author}
		}
	}
	if found == 0 {
		return &e2e.Red{Expected: "commits under " + prefix, Saw: fmt.Sprintf("%d commits on %s, none there", len(commits), branch)}
	}
	return nil
}

func touches(c e2e.Commit, prefix string) bool {
	for _, f := range c.Files {
		if strings.HasPrefix(f, prefix) {
			return true
		}
	}
	return false
}

func judgeRuns(runs []e2e.TestRun, criteria int) error {
	if len(runs) < criteria {
		return &e2e.Red{Expected: fmt.Sprintf("at least one test for each of the %d criteria", criteria), Saw: fmt.Sprintf("%d tests", len(runs))}
	}
	for _, r := range runs {
		if !r.Failed || !r.Assertion {
			return &e2e.Red{Expected: "every test to fail on the base branch with an assertion", Saw: fmt.Sprintf("%s failed=%v on an assertion=%v", r.Name, r.Failed, r.Assertion)}
		}
	}
	return nil
}

// judgeReady checks the announcement's count and that it maps every acceptance criterion as AC1 to ACn.
func judgeReady(m e2e.Message, tests, criteria int) error {
	var said int
	if _, err := fmt.Sscanf(reReady.FindStringSubmatch(m.Text)[1], "%d", &said); err != nil || said != tests {
		return &e2e.Red{Expected: fmt.Sprintf("the room to say %d failing tests ready", tests), Saw: m.Text}
	}
	for i := 1; i <= criteria; i++ {
		if !regexp.MustCompile(fmt.Sprintf(`\bAC ?%d\b`, i)).MatchString(m.Text) {
			return &e2e.Red{Expected: fmt.Sprintf("AC%d in the room's map of tests to criteria", i), Saw: m.Text}
		}
	}
	return nil
}

func p15() e2e.Step {
	return e2e.Step{
		ID:   "P1.5",
		Does: "Push a commit to the branch that edits or deletes a heimdall test",
		Sees: "the next PR check is red with reason `tests_changed` (the pinned tree hash no longer matches)",
		Tags: []string{"local"},
		// server: the check is the product's; github-user and fixture-repo: the person pushes and opens a PR.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedGitHubUser, e2e.NeedFixtureRepo},
		After: "P1.4",
		Run:   runP15,
	}
}

func runP15(ctx context.Context, e *e2e.Env) error {
	key, branch, _, err := changeFacts(e, "P1.4")
	if err != nil {
		return err
	}
	commits, err := e.World.GitHub().Commits(ctx, e.Story.Repo, branch)
	if err != nil {
		return err
	}
	target := testFile(commits, "tests/heimdall/"+key+"/")
	if target == "" {
		return &e2e.Red{Expected: "a heimdall test on the branch to edit", Saw: "none under tests/heimdall/" + key + "/"}
	}
	author := e.Story.Author().GitHub
	sha, err := e.World.GitHub().Push(ctx, e.Story.Repo, branch, author, e2e.Change{
		Files: map[string]string{target: "// edited by a person\n"}, Message: key + ": edit a test",
	})
	if err != nil {
		return err
	}
	pr, err := openPR(ctx, e, branch, key+": "+e.Story.Ticket.Title)
	if err != nil {
		return err
	}
	pr.SHA = sha
	_, err = awaitGate(ctx, e, pr, "the gate to fail with the tests changed", gateWait, func(g e2e.GateState) bool {
		return g.State == "failure" && hasCode(g, "tests_changed")
	})
	return err
}

// testFile is the first file under the tests directory that the branch's commits touch.
func testFile(commits []e2e.Commit, prefix string) string {
	for _, c := range commits {
		for _, f := range c.Files {
			if strings.HasPrefix(f, prefix) {
				return f
			}
		}
	}
	return ""
}

func p16() e2e.Step {
	return e2e.Step{
		ID:   "P1.6",
		Does: "Move a ticket in a sensitive domain (Story `tenant_ticket`)",
		Sees: "the room says tests need sign-off and **nobody is told to build** until `heimdall approve-tests` by the domain approver; a non-approver's command is refused with a clear reason",
		Tags: []string{"vm"},
		// server and tracker: a sensitive ticket moves; chat-user and forward: the room and both commands.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedForward, e2e.NeedTracker, e2e.NeedChatUser},
		Run:   runP16,
	}
}

var reSignoff = regexp.MustCompile(`(?i)sign[- ]?off`)

// runP16 reads "nobody is told to build" as: no "failing tests ready" line before the domain approver acts.
func runP16(ctx context.Context, e *e2e.Env) error {
	spec, domain := e.Story.TenantTicket, e.Story.Person(e.Story.DomainApprover)
	if spec == nil || domain.Chat == "" {
		return storyGap("a tenant_ticket and a domain_approver")
	}
	key, err := startTicket(ctx, e, *spec)
	if err != nil {
		return err
	}
	room, err := awaitRoom(ctx, e, key)
	if err != nil {
		return err
	}
	if _, err := awaitMessage(ctx, e, room, "the request for test sign-off", replyWait, matching(reSignoff)); err != nil {
		return err
	}
	if err := noBuildYet(ctx, e, room); err != nil {
		return err
	}
	if _, err := converse(ctx, e, room, e.Story.Author().Chat, "heimdall approve-tests", refusal(domain.Chat)); err != nil {
		return err
	}
	if err := noBuildYet(ctx, e, room); err != nil {
		return err
	}
	if err := e.World.Chat().Say(ctx, room, domain.Chat, "heimdall approve-tests"); err != nil {
		return err
	}
	_, err = awaitMessage(ctx, e, room, "the failing tests to be ready after sign-off", testsWait, matching(reReady))
	return err
}

func noBuildYet(ctx context.Context, e *e2e.Env, room string) error {
	ms, err := readRoom(ctx, e, room)
	if err != nil {
		return err
	}
	if n := count(ms, matching(reReady)); n > 0 {
		return &e2e.Red{Expected: "nobody to be told to build before sign-off", Saw: "the room already says the tests are ready"}
	}
	return nil
}
