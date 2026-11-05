package scenarios

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

func p5() e2e.Scenario {
	return e2e.Scenario{
		ID: "P5", Title: "Fix to green, and the ways to cheat that must fail",
		// One review round, the verification and the closing line; every cheat is answered by the gate, not the room.
		Budget: e2e.Budget{Milestones: 8, Questions: 0, Decisions: 2},
		Steps:  []e2e.Step{p51(), p52(), p53(), p54(), p55(), p56(), p57()},
	}
}

func p51() e2e.Step {
	return e2e.Step{
		ID:   "P5.1",
		Does: "The author pushes a fix (new head Y)",
		Sees: "the old approval, if any, no longer counts; E1 and E2 evidence re-runs by itself and a pass resolves the finding; E3 and E4 findings on changed files go back to the filing reviewer; the blocking set shrinks only that way (D152)",
		Tags: []string{"local", "scripted"},
		// The review it fixes needs the whole chain; scripted: the reviewers and the evidence re-run.
		Needs: reviewChain(),
		Run:   runP51,
	}
}

var reApprovalWord = regexp.MustCompile(`(?i)approv`)

// runP51 has the approver approve the head X first, so that "the old approval no longer counts" is tested.
// The tier shown for a finding decides what must happen: E1 and E2 are resolved by the re-run; E3 and E4 are
// awaiting their reviewer.
func runP51(ctx context.Context, e *e2e.Env) error {
	pr, err := reviewedPR(ctx, e)
	if err != nil {
		return err
	}
	gh := e.World.GitHub()
	if err := gh.Review(ctx, pr, e.Story.Approver().GitHub, reviewApproved, pr.SHA); err != nil {
		return err
	}
	e.Set(keyOldHead, pr.SHA)
	fixed, err := gh.SetHead(ctx, pr, e.Story.FixBranch, e.Story.Author().GitHub)
	if err != nil {
		return err
	}
	e.Set(keyPR, fixed)
	g, err := awaitGate(ctx, e, fixed, "the check to re-run on the fix", gateWait, gateIs("failure"))
	if err != nil {
		return err
	}
	if !hasReason(g, reApprovalWord) {
		return &e2e.Red{Expected: "the old approval no longer to count, so the gate asks for an approval", Saw: describeGate(g)}
	}
	return awaitReruns(ctx, e, fixed)
}

// awaitReruns waits until every E1 and E2 finding is resolved and every E3 and E4 finding is back with its reviewer.
func awaitReruns(ctx context.Context, e *e2e.Env, pr e2e.PR) error {
	return e.World.Clock().Until(ctx, "the evidence to re-run on the fix", workWait, func(ctx context.Context) (bool, string, error) {
		fs, err := readFindings(ctx, e, pr)
		if err != nil {
			return false, "", err
		}
		for _, f := range fs {
			if p := rerunProblem(f); p != "" {
				return false, p, nil
			}
		}
		return len(fs) > 0, "no findings", nil
	})
}

func rerunProblem(f e2e.Finding) string {
	switch f.Tier {
	case "E1", "E2":
		if !f.Resolved {
			return fmt.Sprintf("%s (%s) is not resolved by the re-run", f.ID, f.Tier)
		}
	default:
		if !f.Awaiting {
			return fmt.Sprintf("%s (%s) did not go back to its reviewer", f.ID, f.Tier)
		}
	}
	return ""
}

func p52() e2e.Step {
	return e2e.Step{
		ID:   "P5.2",
		Does: "Approver approves the PR on Y (GitHub review)",
		Sees: "when and only when all clauses hold, `heimdall/gate` becomes `success` on Y and the merge button unlocks",
		Tags: []string{"local"},
		// server: the gate; github-user and fixture-repo: the review and the merge button.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedGitHubUser, e2e.NeedFixtureRepo},
		After: "P5.1",
		Run:   runP52,
	}
}

func runP52(ctx context.Context, e *e2e.Env) error {
	pr, err := earlier[e2e.PR](e, "P5.1", keyPR)
	if err != nil {
		return err
	}
	gh := e.World.GitHub()
	g, err := e.World.Server().Gate(ctx, pr)
	if err != nil {
		return err
	}
	if g.State == "success" {
		return &e2e.Red{Expected: "the gate not to be green before the approval", Saw: describeGate(g)}
	}
	if err := gh.Review(ctx, pr, e.Story.Approver().GitHub, reviewApproved, pr.SHA); err != nil {
		return err
	}
	if _, err := awaitGate(ctx, e, pr, "the gate to become success on the fix", gateWait, gateIs("success")); err != nil {
		return err
	}
	open, err := gh.Mergeable(ctx, pr)
	if err != nil {
		return err
	}
	if !open {
		return &e2e.Red{Expected: "the merge button to unlock", Saw: "GitHub still blocks the merge"}
	}
	return nil
}

func p53() e2e.Step {
	return e2e.Step{
		ID:   "P5.3",
		Does: "Approve on an older commit X after Y was pushed",
		Sees: "gate stays red, reason \"approval is for an older commit\"",
		Tags: []string{"local"},
		// server: the gate; github-user and fixture-repo: the push and the review.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedGitHubUser, e2e.NeedFixtureRepo},
		After: "P5.1",
		Run:   runP53,
	}
}

var reOlder = regexp.MustCompile(`(?i)older commit`)

// pushNewHead has the author push one more commit, so the PR has a head no review has seen, and waits until the
// only thing the gate still asks for is an approval. Without that, a gate red for another reason would hide a
// product that wrongly counts the approval.
func pushNewHead(ctx context.Context, e *e2e.Env, pr e2e.PR) (e2e.PR, error) {
	sha, err := e.World.GitHub().Push(ctx, e.Story.Repo, pr.Head, e.Story.Author().GitHub, e2e.Change{
		Files: map[string]string{"NOTES.txt": "A note for the reviewers, written on " + shortSHA(pr.SHA) + ".\n"}, Message: "Add a note for the reviewers",
	})
	if err != nil {
		return pr, err
	}
	pr.SHA = sha
	e.Set(keyPR, pr)
	g, err := awaitGate(ctx, e, pr, "the gate to ask only for an approval", gateWait, gateIs("failure"))
	if err != nil {
		return pr, err
	}
	for _, r := range g.Reasons {
		if !reApprovalWord.MatchString(r.Text) {
			return pr, &e2e.Red{Expected: "only the approval to be missing before the check", Saw: describeGate(g)}
		}
	}
	return pr, nil
}

func runP53(ctx context.Context, e *e2e.Env) error {
	pr, err := earlier[e2e.PR](e, "P5.1", keyPR)
	if err != nil {
		return err
	}
	old, err := earlier[string](e, "P5.1", keyOldHead)
	if err != nil {
		return err
	}
	if pr, err = pushNewHead(ctx, e, pr); err != nil {
		return err
	}
	if err := e.World.GitHub().Review(ctx, pr, e.Story.Approver().GitHub, reviewApproved, old); err != nil {
		return err
	}
	g, err := awaitGate(ctx, e, pr, "the gate to answer the old approval", gateWait, func(g e2e.GateState) bool {
		return g.State == "success" || hasReason(g, reOlder)
	})
	if err != nil {
		return err
	}
	if g.State == "success" {
		return &e2e.Red{Expected: "the gate to stay red because the approval is for an older commit", Saw: describeGate(g)}
	}
	return nil
}

func p54() e2e.Step {
	return e2e.Step{
		ID:   "P5.4",
		Does: "The author approves their own PR",
		Sees: "not counted, reason shown",
		Tags: []string{"local"},
		// server: the gate; github-user and fixture-repo: the review as the author.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedGitHubUser, e2e.NeedFixtureRepo},
		After: "P5.1",
		Run:   runP54,
	}
}

var reOwnApproval = regexp.MustCompile(`(?i)\b(author|own|someone else|another person)\b`)

// runP54 accepts that GitHub itself may refuse a self-approval. Then the product has nothing new to show, and
// the gate must simply still ask for an approval; when GitHub took it, the reason must say it is the author's.
func runP54(ctx context.Context, e *e2e.Env) error {
	pr, err := earlier[e2e.PR](e, "P5.1", keyPR)
	if err != nil {
		return err
	}
	if pr, err = pushNewHead(ctx, e, pr); err != nil {
		return err
	}
	var refused *e2e.ReviewRefusedError
	err = e.World.GitHub().Review(ctx, pr, e.Story.Author().GitHub, reviewApproved, pr.SHA)
	if err != nil && !errors.As(err, &refused) {
		return err
	}
	g, err := awaitGate(ctx, e, pr, "the gate to answer the author's approval", gateWait, func(g e2e.GateState) bool {
		return g.State == "success" || refused != nil || hasReason(g, reOwnApproval)
	})
	if err != nil {
		return err
	}
	if g.State == "success" || !hasReason(g, reApprovalWord) {
		return &e2e.Red{Expected: "the author's own approval not to count, with the reason shown", Saw: describeGate(g)}
	}
	return nil
}

func p55() e2e.Step {
	return e2e.Step{
		ID:   "P5.5",
		Does: "Push a change that deletes a heimdall test or edits `heimdall.yaml` / policy in the PR",
		Sees: "red (`tests_changed`, `policy_changed`), and policy is read from the merge base, not the PR",
		Tags: []string{"local"},
		// server: the gate; github-user and fixture-repo: the pushes.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedGitHubUser, e2e.NeedFixtureRepo},
		After: "P5.1",
		Run:   runP55,
	}
}

// loosenedPolicy is a policy file that would let anything through, if the PR's copy were read.
const loosenedPolicy = "version: 1\nslop: { max_changed_lines: 1000000, require_ticket: false }\n"

// runP55 pushes the two cheats in turn. That the policy comes from the merge base is seen as: the PR's loosened
// policy changes nothing, and the gate stays red naming the change. At the end the PR goes back to the fix, so
// later steps start from a head that is not broken.
func runP55(ctx context.Context, e *e2e.Env) error {
	pr, err := earlier[e2e.PR](e, "P5.1", keyPR)
	if err != nil {
		return err
	}
	key, err := earlier[string](e, "the review", keyTicket)
	if err != nil {
		return err
	}
	commits, err := e.World.GitHub().Commits(ctx, e.Story.Repo, pr.Head)
	if err != nil {
		return err
	}
	test := testFile(commits, "tests/heimdall/"+key+"/")
	if test == "" {
		return &e2e.Red{Expected: "a heimdall test in the PR to delete", Saw: "none under tests/heimdall/" + key + "/"}
	}
	cheats := []struct {
		code   string
		change e2e.Change
	}{
		{"tests_changed", e2e.Change{Delete: []string{test}, Message: "Remove a failing test"}},
		{"policy_changed", e2e.Change{Files: map[string]string{"heimdall.yaml": loosenedPolicy}, Message: "Loosen the policy"}},
	}
	for _, c := range cheats {
		if pr, err = pushCheat(ctx, e, pr, c.code, c.change); err != nil {
			return err
		}
	}
	back, err := e.World.GitHub().SetHead(ctx, pr, e.Story.FixBranch, e.Story.Author().GitHub)
	if err != nil {
		return err
	}
	e.Set(keyPR, back)
	return nil
}

func pushCheat(ctx context.Context, e *e2e.Env, pr e2e.PR, code string, c e2e.Change) (e2e.PR, error) {
	sha, err := e.World.GitHub().Push(ctx, e.Story.Repo, pr.Head, e.Story.Author().GitHub, c)
	if err != nil {
		return pr, err
	}
	pr.SHA = sha
	_, err = awaitGate(ctx, e, pr, "the gate to fail with "+code, gateWait, func(g e2e.GateState) bool {
		return g.State == "failure" && hasCode(g, code)
	})
	return pr, err
}

func p56() e2e.Step {
	return e2e.Step{
		ID:   "P5.6",
		Does: "Someone edits an earlier command comment to say `uphold`",
		Sees: "ignored",
		Tags: []string{"local"},
		// The review it acts on needs the chain; the poller's replay is read from the server.
		Needs: reviewChain(),
		Run:   runP56,
	}
}

// runP56 has the approver, who may uphold, post a harmless comment and edit it into an uphold command. After the
// poller has run again over the edit, the finding must not be upheld. If the review has left no finding open, the
// first finding is used.
func runP56(ctx context.Context, e *e2e.Env) error {
	pr, err := reviewedPR(ctx, e)
	if err != nil {
		return err
	}
	fs, err := readFindings(ctx, e, pr)
	if err != nil {
		return err
	}
	if len(fs) == 0 {
		return &e2e.Red{Expected: "a finding to try to uphold", Saw: "no findings"}
	}
	target := openOrFirst(fs)
	if err := editIntoUphold(ctx, e, pr, target.ID); err != nil {
		return err
	}
	if err := e.World.Server().Replay(ctx); err != nil {
		return err
	}
	fs, err = readFindings(ctx, e, pr)
	if err != nil {
		return err
	}
	if f, ok := findingByID(target.ID, fs); ok && f.Upheld {
		return &e2e.Red{Expected: "an edited comment to be ignored", Saw: target.ID + " was upheld"}
	}
	return nil
}

// openOrFirst picks a finding that is still open, or the first one.
func openOrFirst(fs []e2e.Finding) e2e.Finding {
	for _, f := range fs {
		if f.Blocking && !f.Resolved {
			return f
		}
	}
	return fs[0]
}

// editIntoUphold posts a harmless comment as the approver and edits it into an uphold command.
func editIntoUphold(ctx context.Context, e *e2e.Env, pr e2e.PR, id string) error {
	who := e.Story.Approver().GitHub
	gh := e.World.GitHub()
	cid, err := gh.Comment(ctx, pr, who, "Reading the findings now.")
	if err != nil {
		return err
	}
	return gh.EditComment(ctx, pr, who, cid, "/heimdall uphold "+id+" "+upholdReason)
}

func p57() e2e.Step {
	return e2e.Step{
		ID:   "P5.7",
		Does: "Merge the PR on GitHub",
		Sees: "the change closes, the room gets one closing message, and a digest episode is queued to memory",
		Tags: []string{"vm"},
		// server: the change closes there; github-user and fixture-repo: the merge; chat-user and forward: the closing
		// message is read in the room.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedForward, e2e.NeedGitHubUser, e2e.NeedChatUser, e2e.NeedFixtureRepo},
		After: "P5.1",
		Run:   runP57,
	}
}

// runP57 fixes the closing line's contract: one product message with the ticket key and the word "merged". The
// digest is read on the dashboard, which must show it queued. The approver approves the current head first if the
// gate is not green, since earlier steps moved the head.
func runP57(ctx context.Context, e *e2e.Env) error {
	pr, err := earlier[e2e.PR](e, "P5.1", keyPR)
	if err != nil {
		return err
	}
	key, err := earlier[string](e, "the review", keyTicket)
	if err != nil {
		return err
	}
	room, err := earlier[string](e, "the review", keyRoom)
	if err != nil {
		return err
	}
	who := e.Story.Approver().GitHub
	if err := e.World.GitHub().Review(ctx, pr, who, reviewApproved, pr.SHA); err != nil {
		return err
	}
	if _, err := awaitGate(ctx, e, pr, "the gate to be green before the merge", gateWait, gateIs("success")); err != nil {
		return err
	}
	if err := e.World.GitHub().Merge(ctx, pr, who); err != nil {
		return err
	}
	return judgeClosed(ctx, e, room, key)
}

var reMerged = regexp.MustCompile(`(?i)\bmerged\b`)
var reDigest = regexp.MustCompile(`(?i)\bdigest\b`)

func judgeClosed(ctx context.Context, e *e2e.Env, room, key string) error {
	closing := func(m e2e.Message) bool { return strings.Contains(m.Text, key) && reMerged.MatchString(m.Text) }
	if _, err := awaitMessage(ctx, e, room, "the closing message", replyWait, closing); err != nil {
		return err
	}
	ms, err := readRoom(ctx, e, room)
	if err != nil {
		return err
	}
	if n := count(ms, closing); n != 1 {
		return &e2e.Red{Expected: "one closing message", Saw: fmt.Sprintf("%d", n)}
	}
	dash, err := e.World.Server().Dashboard(ctx, key)
	if err != nil {
		return err
	}
	if !reDigest.MatchString(dash) {
		return &e2e.Red{Expected: "a digest queued to memory, shown on the dashboard", Saw: "no digest on the dashboard for " + key}
	}
	return nil
}
