package scenarios

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

func p4() e2e.Scenario {
	return e2e.Scenario{
		ID: "P4", Title: "Dispute and decision",
		// Two review rounds (the second is P4.4's): the listings and progress lines, the decision cards, the
		// refusals and the confirmations.
		Budget: e2e.Budget{Milestones: 14, Questions: 0, Decisions: 3},
		Steps:  []e2e.Step{p41(), p42(), p43(), p44()},
	}
}

func p41() e2e.Step {
	return e2e.Step{
		ID:   "P4.1",
		Does: "The author posts `heimdall challenge F1 <evidence>` with new evidence (a test path at head, a run id, or a memory or decision ref)",
		Sees: "a case opens, the resolver's recommendation (uphold, dismiss, needs human) appears on a decision card **with citations**, and the finding is still blocking",
		Tags: []string{"scripted"},
		// The review it disputes needs the whole chain; scripted: the reviewers and the resolver.
		Needs: reviewChain(),
		Run:   runP41,
	}
}

var (
	reUphold   = regexp.MustCompile(`(?i)\buphold\b`)
	reDismiss  = regexp.MustCompile(`(?i)\bdismiss\b`)
	reHuman    = regexp.MustCompile(`(?i)\bneeds a human\b`)
	reCitation = regexp.MustCompile(`\[\d+\]|https?://\S+|\b[\w./-]+\.\w+:\d+\b`)
)

// cardProblem says what is wrong with a decision card, or "". The contract: it @-mentions the approver, names
// exactly one of uphold, dismiss or "needs a human", and cites at least one source as [n], a link or file:line.
func cardProblem(m e2e.Message, approverChat string) string {
	if !mentions(m, approverChat) {
		return "it does not mention the approver " + approverChat
	}
	verdicts := 0
	for _, re := range []*regexp.Regexp{reUphold, reDismiss, reHuman} {
		if re.MatchString(m.Text) {
			verdicts++
		}
	}
	if verdicts != 1 {
		return fmt.Sprintf("it names %d recommendations, not exactly one", verdicts)
	}
	if !reCitation.MatchString(m.Text) {
		return "it cites nothing"
	}
	return ""
}

func runP41(ctx context.Context, e *e2e.Env) error {
	ch := e.Story.Challenge
	if ch == nil {
		return storyGap("a challenge")
	}
	pr, room, f, err := disputed(ctx, e)
	if err != nil {
		return err
	}
	card, err := converse(ctx, e, room, e.Story.Author().Chat, "heimdall challenge "+f.ID+" "+ch.Evidence, ofKind(e2e.KindDecision))
	if err != nil {
		return err
	}
	if p := cardProblem(card, e.Story.Approver().Chat); p != "" {
		return &e2e.Red{Expected: "a decision card with one recommendation and citations", Saw: p + ": " + card.Text}
	}
	fs, err := readFindings(ctx, e, pr)
	if err != nil {
		return err
	}
	if now, ok := findingByID(f.ID, fs); !ok || !now.Blocking {
		return &e2e.Red{Expected: f.ID + " still blocking while the case is open", Saw: describeFinding(now, ok)}
	}
	e.Set(keyFinding, f.ID)
	return nil
}

// disputed finishes the review and finds the finding the story's challenge is about.
func disputed(ctx context.Context, e *e2e.Env) (e2e.PR, string, e2e.Finding, error) {
	pr, err := reviewedPR(ctx, e)
	if err != nil {
		return pr, "", e2e.Finding{}, err
	}
	room, err := earlier[string](e, "the review", keyRoom)
	if err != nil {
		return pr, "", e2e.Finding{}, err
	}
	fs, err := readFindings(ctx, e, pr)
	if err != nil {
		return pr, room, e2e.Finding{}, err
	}
	p, ok := planted(e.Story, e.Story.Challenge.Finding)
	if !ok {
		return pr, room, e2e.Finding{}, storyGap("the planted issue the challenge is about")
	}
	f, found := findingFor(p, fs)
	if !found {
		return pr, room, f, &e2e.Red{Expected: "a finding for " + p.ID + " to dispute", Saw: "none at " + where(p)}
	}
	return pr, room, f, nil
}

func p42() e2e.Step {
	return e2e.Step{
		ID:   "P4.2",
		Does: "The author posts the challenge again with **no new evidence**",
		Sees: "the challenge is refused with the reason; nothing moves",
		Tags: []string{"scripted"},
		// The same chain as P4.1, whose case it repeats.
		Needs: reviewChain(),
		After: "P4.1",
		Run:   runP42,
	}
}

var reEvidence = regexp.MustCompile(`(?i)\bevidence\b`)

// runP42 reads "no new evidence" as the same evidence text again. A refusal names the missing evidence.
func runP42(ctx context.Context, e *e2e.Env) error {
	id, err := earlier[string](e, "P4.1", keyFinding)
	if err != nil {
		return err
	}
	pr, err := earlier[e2e.PR](e, "P4.1", keyPR)
	if err != nil {
		return err
	}
	room, err := earlier[string](e, "P4.1", keyRoom)
	if err != nil {
		return err
	}
	before, err := readRoom(ctx, e, room)
	if err != nil {
		return err
	}
	text := "heimdall challenge " + id + " " + e.Story.Challenge.Evidence
	if _, err := converse(ctx, e, room, e.Story.Author().Chat, text, func(m e2e.Message) bool {
		return reRefusal.MatchString(m.Text) && reEvidence.MatchString(m.Text)
	}); err != nil {
		return err
	}
	after, err := readRoom(ctx, e, room)
	if err != nil {
		return err
	}
	if b, a := count(before, ofKind(e2e.KindDecision)), count(after, ofKind(e2e.KindDecision)); a != b {
		return &e2e.Red{Expected: "nothing to move: no new decision card", Saw: fmt.Sprintf("%d cards, was %d", a, b)}
	}
	return stillOpen(ctx, e, pr, id)
}

// stillOpen checks a finding is neither upheld nor resolved.
func stillOpen(ctx context.Context, e *e2e.Env, pr e2e.PR, id string) error {
	fs, err := readFindings(ctx, e, pr)
	if err != nil {
		return err
	}
	f, ok := findingByID(id, fs)
	if !ok || f.Upheld || f.Resolved || !f.Blocking {
		return &e2e.Red{Expected: id + " unchanged: blocking, not upheld, not resolved", Saw: describeFinding(f, ok)}
	}
	return nil
}

func p43() e2e.Step {
	return e2e.Step{
		ID:   "P4.3",
		Does: "Approver types `heimdall uphold F1` in the room",
		Sees: "the finding becomes upheld, the decision is recorded with who, when, reason, and a GitHub comment mirrors it; the same command by the author, by an unmapped user or by an agent is refused",
		Tags: []string{"vm"},
		// server: the decision; tracker, github-user, fixture-repo, chat-user, forward: the change, the PR comment and
		// the room; scripted: the review the finding comes from.
		Needs: reviewChain(),
		After: "P4.1",
		Run:   runP43,
	}
}

var (
	reUpheld = regexp.MustCompile(`(?i)\bupheld\b`)
	reWhen   = regexp.MustCompile(`\d{4}-\d{2}-\d{2}|\d{1,2}:\d{2}`)
)

const upholdReason = "the evidence offered does not change the facts"

// runP43 types the same command as an agent, then the unmapped user, then the author, and only then as the approver.
// The agent's command gets no reply because commands from unmapped senders are ignored, so it goes first: the
// replies to the two later refusals prove the product had already handled it. The contract for the GitHub
// comment: it holds the finding id, the word "upheld", the approver's login, the reason and a date or time.
func runP43(ctx context.Context, e *e2e.Env) error {
	id, err := earlier[string](e, "P4.1", keyFinding)
	if err != nil {
		return err
	}
	pr, err := earlier[e2e.PR](e, "P4.1", keyPR)
	if err != nil {
		return err
	}
	room, err := earlier[string](e, "P4.1", keyRoom)
	if err != nil {
		return err
	}
	names := e.Story.Reviewers()
	if len(names) == 0 || e.Story.Stranger == "" {
		return storyGap("reviewers and a stranger")
	}
	cmd := "heimdall uphold " + id + " " + upholdReason
	if err := e.World.Chat().Say(ctx, room, reviewerAgent(names[0]), cmd); err != nil {
		return err
	}
	who := e.Story.Approver()
	for _, as := range []string{e.Story.Stranger, e.Story.Author().Chat} {
		if _, err := converse(ctx, e, room, as, cmd, refusal(who.Chat)); err != nil {
			return err
		}
	}
	if err := stillOpen(ctx, e, pr, id); err != nil {
		return err
	}
	if err := e.World.Chat().Say(ctx, room, who.Chat, cmd); err != nil {
		return err
	}
	return awaitUpheld(ctx, e, pr, id, who)
}

func awaitUpheld(ctx context.Context, e *e2e.Env, pr e2e.PR, id string, who e2e.Person) error {
	err := e.World.Clock().Until(ctx, id+" to be upheld", replyWait, func(ctx context.Context) (bool, string, error) {
		fs, err := readFindings(ctx, e, pr)
		f, ok := findingByID(id, fs)
		return ok && f.Upheld, describeFinding(f, ok), err
	})
	if err != nil {
		return err
	}
	return e.World.Clock().Until(ctx, "a GitHub comment that mirrors the decision", replyWait, func(ctx context.Context) (bool, string, error) {
		cs, err := e.World.GitHub().Comments(ctx, pr)
		for _, c := range cs {
			if mirrors(c.Body, id, who.GitHub) {
				return true, "", nil
			}
		}
		return false, fmt.Sprintf("%d comments, none records the decision", len(cs)), err
	})
}

func mirrors(body, id, approver string) bool {
	return strings.Contains(body, id) && reUpheld.MatchString(body) && strings.Contains(body, approver) &&
		strings.Contains(body, upholdReason) && reWhen.MatchString(body)
}

func p44() e2e.Step {
	return e2e.Step{
		ID:   "P4.4",
		Does: "Two reviewers independently flag the same issue at high confidence with only reasoning",
		Sees: "the gate holds with reason `consensus_hold` and opens a human card; no model can clear it",
		Tags: []string{"scripted"},
		// The chain of a review round; scripted: two reviewers are told how to behave.
		Needs: reviewChain(),
		Run:   runP44,
	}
}

// scriptSameIssue is the behaviour the scripted reviewers are told: flag the named planted issue at high confidence
// with reasoning only. The scripted agent's behaviours are named "flag:<planted id>:high:reasoning".
func scriptSameIssue(plantedID string) string { return "flag:" + plantedID + ":high:reasoning" }

// runP44 starts a new review round. "No model can clear it" is read as: the gate stays red and the card goes to
// the approver, a human; nothing here lets a model clear it, so only the hold and the card are asserted.
func runP44(ctx context.Context, e *e2e.Env) error {
	names := e.Story.Reviewers()
	if len(names) < 2 || len(e.Story.Planted) == 0 {
		return storyGap("two reviewers and a planted issue")
	}
	room, err := ensureRoom(ctx, e)
	if err != nil {
		return err
	}
	pr, err := freshPR(ctx, e)
	if err != nil {
		return err
	}
	for _, name := range names[:2] {
		if err := e.World.Scripted().Script(ctx, reviewerAgent(name), scriptSameIssue(e.Story.Planted[0].ID)); err != nil {
			return err
		}
	}
	if _, err := requestReview(ctx, e, room); err != nil {
		return err
	}
	if _, err := awaitReviews(ctx, e, room, reviewWait, nil); err != nil {
		return err
	}
	if _, err := awaitGate(ctx, e, pr, "the gate to hold on consensus", gateWait, func(g e2e.GateState) bool {
		return g.State == "failure" && hasCode(g, "consensus_hold")
	}); err != nil {
		return err
	}
	_, err = awaitMessage(ctx, e, room, "a human card", replyWait, func(m e2e.Message) bool {
		return m.Kind == e2e.KindDecision && mentions(m, e.Story.Approver().Chat)
	})
	return err
}
