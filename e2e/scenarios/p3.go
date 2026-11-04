package scenarios

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

func p3() e2e.Scenario {
	return e2e.Scenario{
		ID: "P3", Title: "Three reviewers, three different bugs",
		// The listing, a progress line per reviewer, the summary, the unavailable notice and the waiver; one decision
		// card for the findings.
		Budget: e2e.Budget{Milestones: 8, Questions: 0, Decisions: 2},
		Steps:  []e2e.Step{p31(), p32(), p33(), p34(), p35(), p36()},
	}
}

func p31() e2e.Step {
	return e2e.Step{
		ID:   "P3.1",
		Does: "As author, post `heimdall review @bob @eli @ana` in the room, or comment `/heimdall review @bob @eli @ana` on the PR",
		Sees: "a message listing the three reviewers and that they were asked; a comment from someone not mapped to a person is ignored and the reply says how to link",
		Tags: []string{"vm"},
		// server: the product answers; tracker, github-user, fixture-repo: a change with a PR to review;
		// chat-user and forward: the command is typed in the room (the PR-comment form is not used).
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedForward, e2e.NeedTracker, e2e.NeedGitHubUser, e2e.NeedChatUser, e2e.NeedFixtureRepo},
		Run:   runP31,
	}
}

var reLink = regexp.MustCompile(`(?i)\blink`)

func runP31(ctx context.Context, e *e2e.Env) error {
	room, err := ensureRoom(ctx, e)
	if err != nil {
		return err
	}
	if _, err := ensureGoodPR(ctx, e); err != nil {
		return err
	}
	if e.Story.Stranger == "" {
		return storyGap("a stranger")
	}
	if _, err := requestReview(ctx, e, room); err != nil {
		return err
	}
	// A command from a chat user mapped to nobody is ignored, and the reply says how to link.
	if _, err := converse(ctx, e, room, e.Story.Stranger, reviewCommand(e.Story), matching(reLink)); err != nil {
		return err
	}
	ms, err := readRoom(ctx, e, room)
	if err != nil {
		return err
	}
	if n := count(ms, listsReviewers(e.Story)); n != 1 {
		return &e2e.Red{Expected: "the stranger's command to be ignored, so one listing of the reviewers", Saw: fmt.Sprintf("%d listings", n)}
	}
	return nil
}

func p32() e2e.Step {
	return e2e.Step{
		ID:   "P3.2",
		Does: "nothing",
		Sees: "within 12 min (p50): every reviewer has submitted, and the room posts a **fixed-template** progress line only (no findings leak to other reviewers before all are in)",
		Tags: []string{"vm", "scripted"},
		// server: the reviews run there; chat-user and forward: the progress lines are read in the room; scripted:
		// the reviewers are the scripted agents.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedForward, e2e.NeedChatUser, e2e.NeedScripted},
		Bar:   reviewWait,
		After: "P3.1",
		Run:   runP32,
	}
}

// runP32 fixes the contract for the progress line: "N of M reviews in", a milestone, where M is the number of
// reviewers the story names. Findings never appear in it; the capture's check 9 fails any that do.
func runP32(ctx context.Context, e *e2e.Env) error {
	room, err := earlier[string](e, "P3.1", keyRoom)
	if err != nil {
		return err
	}
	at, err := awaitReviews(ctx, e, room, reviewWait, nil)
	if err != nil {
		return err
	}
	e.Capture.MarkAllSubmitted(at)
	ms, err := readRoom(ctx, e, room)
	if err != nil {
		return err
	}
	want := len(e.Story.Reviewers())
	for _, m := range ms {
		if _, total, ok := progressOf(m); ok && total != want {
			return &e2e.Red{Expected: fmt.Sprintf("progress lines out of %d reviewers", want), Saw: m.Text}
		}
	}
	return nil
}

func p33() e2e.Step {
	return e2e.Step{
		ID:   "P3.3",
		Does: "nothing",
		Sees: "the summary contains every id in the Story's `expected_blocking_set`, and any other blocking finding has evidence the code verified (scripted runs: exactly the set; D156), each finding with file:line, the claim, the evidence tier and a link to the evidence (run, memory fact or tool output)",
		Tags: []string{"scripted"},
		// server: the summary is the product's; github-user and fixture-repo: each finding's file is read at the head;
		// scripted: the findings come from the scripted reviewers.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedGitHubUser, e2e.NeedFixtureRepo, e2e.NeedScripted},
		After: "P3.2",
		Run:   runP33,
	}
}

// runP33 matches a planted issue to a finding by its file and lines, since a person sees locations, not our ids.
// A blocking finding outside the expected set is allowed only when its evidence was verified by code.
func runP33(ctx context.Context, e *e2e.Env) error {
	pr, err := earlier[e2e.PR](e, "P3.1", keyPR)
	if err != nil {
		return err
	}
	fs, err := awaitFindings(ctx, e, pr)
	if err != nil {
		return err
	}
	var missing []string
	expected := map[string]bool{}
	for _, id := range e.Story.ExpectedBlockingSet {
		p, ok := planted(e.Story, id)
		if !ok {
			return storyGap("a planted issue " + id)
		}
		f, found := findingFor(p, fs)
		if !found || !f.Blocking {
			missing = append(missing, id+" at "+where(p))
		}
		expected[f.ID] = found
	}
	if len(missing) > 0 {
		return &e2e.Red{Expected: "a blocking finding for each of " + strings.Join(e.Story.ExpectedBlockingSet, ", "), Saw: "none for " + strings.Join(missing, "; ")}
	}
	for _, f := range fs {
		if f.Blocking && !expected[f.ID] && !f.Confirmed {
			return &e2e.Red{Expected: "any other blocking finding to have verified evidence", Saw: fmt.Sprintf("%s at %s:%d blocks on reasoning only", f.ID, f.File, f.Line)}
		}
	}
	return nil
}

func p34() e2e.Step {
	return e2e.Step{
		ID:   "P3.4",
		Does: "nothing",
		Sees: "the planted issue that needs cross-team context (consumer break) is backed by an E3 memory fact marked confirmed with its source, or by a runner-confirmed E1; a finding with only reasoning is shown as unverified and does **not** block",
		Tags: []string{"scripted"},
		// server: the product shows the evidence; github-user and fixture-repo: files read at the head; scripted: the
		// reviewers; memory: the cross-team fact is read from Graphiti.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedGitHubUser, e2e.NeedFixtureRepo, e2e.NeedScripted, e2e.NeedMemory},
		After: "P3.3",
		Run:   runP34,
	}
}

// runP34 reads "marked confirmed with its source" as Confirmed with a non-empty evidence reference.
func runP34(ctx context.Context, e *e2e.Env) error {
	pr, err := earlier[e2e.PR](e, "P3.1", keyPR)
	if err != nil {
		return err
	}
	fs, err := readFindings(ctx, e, pr)
	if err != nil {
		return err
	}
	checked := 0
	for _, p := range e.Story.Planted {
		if !p.NeedsMemory {
			continue
		}
		checked++
		f, ok := findingFor(p, fs)
		if !ok || !f.Confirmed || strings.TrimSpace(f.EvidenceRef) == "" {
			return &e2e.Red{Expected: p.ID + " backed by a confirmed memory fact or a confirmed run, with its source", Saw: describeFinding(f, ok)}
		}
	}
	if checked == 0 {
		return storyGap("a planted issue that needs memory")
	}
	for _, f := range fs {
		if !f.Confirmed && f.Blocking {
			return &e2e.Red{Expected: "a finding with only reasoning to be unverified and not block", Saw: fmt.Sprintf("%s at %s:%d blocks", f.ID, f.File, f.Line)}
		}
	}
	return nil
}

func describeFinding(f e2e.Finding, found bool) string {
	if !found {
		return "no finding there"
	}
	return fmt.Sprintf("%s tier %s confirmed=%v evidence %q", f.ID, f.Tier, f.Confirmed, f.EvidenceRef)
}

func p35() e2e.Step {
	return e2e.Step{
		ID:   "P3.5",
		Does: "nothing",
		Sees: "`heimdall/gate` is `failure` and its summary lists every unmet clause in plain language (\"3 findings need a decision\", \"no approval on this commit\")",
		Tags: []string{"local"},
		// server: the gate; github-user and fixture-repo: the findings' files are read at the head.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedGitHubUser, e2e.NeedFixtureRepo},
		After: "P3.3",
		Run:   runP35,
	}
}

var (
	reDecisions = regexp.MustCompile(`(\d+) findings? needs? a decision`)
	reApproval  = regexp.MustCompile(`(?i)\bapproval\b`)
)

// runP35 fixes the wording: "N findings need a decision" with N the number of blocking findings still open, and
// a clause that mentions the approval.
func runP35(ctx context.Context, e *e2e.Env) error {
	pr, err := earlier[e2e.PR](e, "P3.1", keyPR)
	if err != nil {
		return err
	}
	g, err := awaitGate(ctx, e, pr, "the gate to fail", gateWait, gateIs("failure"))
	if err != nil {
		return err
	}
	fs, err := readFindings(ctx, e, pr)
	if err != nil {
		return err
	}
	open := 0
	for _, f := range fs {
		if f.Blocking && !f.Resolved {
			open++
		}
	}
	if !hasReason(g, reApproval) {
		return &e2e.Red{Expected: "a clause saying there is no approval on this commit", Saw: describeGate(g)}
	}
	for _, r := range g.Reasons {
		if m := reDecisions.FindStringSubmatch(r.Text); m != nil {
			if n, _ := strconv.Atoi(m[1]); n == open { // m[1] is digits by the pattern
				return nil
			}
		}
	}
	return &e2e.Red{Expected: fmt.Sprintf("a clause \"%d findings need a decision\"", open), Saw: describeGate(g)}
}

func p36() e2e.Step {
	return e2e.Step{
		ID:   "P3.6",
		Does: "Stop one reviewer agent before the request",
		Sees: "it does not ack within 60 s, so the room says `unavailable`, the gate stays red, and an approver can waive it with a reason",
		Tags: []string{"vm"},
		// server: the product reacts; tracker, github-user, fixture-repo: a PR to review; chat-user and forward: the
		// request and the waiver are typed in the room; agent start and stop is World.Agents.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedForward, e2e.NeedTracker, e2e.NeedGitHubUser, e2e.NeedChatUser, e2e.NeedFixtureRepo},
		Bar:   replyWait,
		Run:   runP36,
	}
}

var (
	reUnavailable = regexp.MustCompile(`(?i)\bunavailable\b`)
	reWaived      = regexp.MustCompile(`(?i)\bwaiv`)
)

// runP36 starts a new review round on a PR whose first reviewer is stopped. The waiver is typed as
// `heimdall waive-reviewer @<chat> <reason>`. The agent is started again at the end, so later steps are not harmed.
func runP36(ctx context.Context, e *e2e.Env) (err error) {
	room, err := ensureRoom(ctx, e)
	if err != nil {
		return err
	}
	pr, err := freshPR(ctx, e)
	if err != nil {
		return err
	}
	names := e.Story.Reviewers()
	if len(names) == 0 {
		return storyGap("reviewers")
	}
	agent, chat := reviewerAgent(names[0]), e.Story.Person(names[0]).Chat
	if err := e.World.Agents().Stop(ctx, agent); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, e.World.Agents().Start(context.WithoutCancel(ctx), agent)) }()
	if _, err := converse(ctx, e, room, e.Story.Author().Chat, reviewCommand(e.Story), func(m e2e.Message) bool {
		return reUnavailable.MatchString(m.Text) && strings.Contains(m.Text, chat)
	}); err != nil {
		return err
	}
	if _, err := awaitGate(ctx, e, pr, "the gate to stay red", gateWait, gateIs("failure")); err != nil {
		return err
	}
	return waive(ctx, e, pr, room, chat)
}

// waive has the approver waive an unavailable reviewer with a reason and checks that the room agrees and the gate
// no longer waits for that reviewer.
func waive(ctx context.Context, e *e2e.Env, pr e2e.PR, room, chat string) error {
	text := "heimdall waive-reviewer " + mention(chat) + " the agent is down and the others covered the change"
	if _, err := converse(ctx, e, room, e.Story.Approver().Chat, text, matching(reWaived)); err != nil {
		return err
	}
	_, err := awaitGate(ctx, e, pr, "the gate to stop waiting for the reviewer", gateWait, func(g e2e.GateState) bool {
		return g.State == "failure" && !hasReason(g, reUnavailable)
	})
	return err
}
