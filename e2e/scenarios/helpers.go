package scenarios

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

// The keys of the values the steps of one scenario share through Env.Vars.
const (
	keyTicket   = "ticket"       // string: the tracker key
	keyBranch   = "branch"       // string: the change's branch on GitHub
	keyRoom     = "room"         // string: the change's room
	keyPR       = "pr"           // e2e.PR: the pull request under test, always at its latest head
	keyOldHead  = "old-head"     // string: the head commit before the author's fix
	keyQuiet    = "quiet"        // quietBaseline: spend and agent tasks before the door step
	keyReviewed = "reviewed"     // bool: reviewers were asked and have all submitted
	keyFinding  = "finding"      // string: the id (F1, ...) of the finding the dispute is about
	keyAsked    = "review-asked" // bool: the author asked for a review
)

// Waits for the places where the spec states no bar. A step that states a bar uses that bar instead.
const (
	gateBar    = 15 * time.Second // the door: p95 from D155
	roomWait   = 90 * time.Second // a room opening after a ticket moves
	replyWait  = 60 * time.Second // the product answering a command
	gateWait   = 2 * time.Minute  // a check re-running after a push or a review
	testsWait  = 10 * time.Minute // red tests being written and pinned
	reviewWait = 12 * time.Minute // every reviewer submitting (p50)
	workWait   = 15 * time.Minute // a review run settling
)

// Names the spec fixes.
const (
	testAuthor       = "test-author"
	statusInProgress = "In Progress"
	reviewApproved   = "APPROVED"
	botSuffix        = "[bot]"
)

// reviewerAgent is the agent of a reviewer: the person's name and "-reviewer" (TECH_SPEC section 16).
func reviewerAgent(person string) string { return person + "-reviewer" }

// earlier returns a value an earlier step of the scenario stored, or says which step has to run first.
func earlier[T any](e *e2e.Env, step, key string) (T, error) {
	v, ok := e2e.Get[T](e, key)
	if !ok {
		return v, &e2e.Red{Expected: "the " + key + " from an earlier step", Saw: step + " has not run, so it must run first"}
	}
	return v, nil
}

func storyGap(what string) error {
	return &e2e.Red{Expected: "the story to define " + what, Saw: "nothing, so this step cannot run on it"}
}

func mention(chat string) string { return "@" + chat }

func mentions(m e2e.Message, chat string) bool {
	for _, x := range m.Mentions {
		if strings.TrimPrefix(x, "@") == chat {
			return true
		}
	}
	return false
}

// brief shortens text for a failure line.
func brief(text string) string {
	if len(text) > 40 {
		return text[:40] + "..."
	}
	return text
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// ---- the room ----

// readRoom reads the whole transcript and adds it to what the person saw.
func readRoom(ctx context.Context, e *e2e.Env, room string) ([]e2e.Message, error) {
	ms, err := e.World.Chat().Transcript(ctx, room)
	if err != nil {
		return nil, err
	}
	e.Capture.AddMessages(ms...)
	return ms, nil
}

func knownIDs(ms []e2e.Message) map[string]bool {
	known := map[string]bool{}
	for _, m := range ms {
		known[m.ID] = true
	}
	return known
}

// awaitMessage waits for a product message that match accepts and returns it.
func awaitMessage(ctx context.Context, e *e2e.Env, room, what string, timeout time.Duration, match func(e2e.Message) bool) (e2e.Message, error) {
	var found e2e.Message
	err := e.World.Clock().Until(ctx, what, timeout, func(ctx context.Context) (bool, string, error) {
		ms, err := readRoom(ctx, e, room)
		if err != nil {
			return false, "", err
		}
		for _, m := range ms {
			if m.FromAgent && match(m) {
				found = m
				return true, "", nil
			}
		}
		return false, fmt.Sprintf("%d messages in the room, none like it", len(ms)), nil
	})
	return found, err
}

// converse says text as a person and waits for a product message, posted after it, that match accepts.
func converse(ctx context.Context, e *e2e.Env, room, as, text string, match func(e2e.Message) bool) (e2e.Message, error) {
	before, err := readRoom(ctx, e, room)
	if err != nil {
		return e2e.Message{}, err
	}
	known := knownIDs(before)
	if err := e.World.Chat().Say(ctx, room, as, text); err != nil {
		return e2e.Message{}, err
	}
	return awaitMessage(ctx, e, room, "a reply to "+brief(text), replyWait, func(m e2e.Message) bool {
		return !known[m.ID] && match(m)
	})
}

func ofKind(k e2e.MessageKind) func(e2e.Message) bool {
	return func(m e2e.Message) bool { return m.Kind == k }
}

func matching(re *regexp.Regexp) func(e2e.Message) bool {
	return func(m e2e.Message) bool { return re.MatchString(m.Text) }
}

func count(ms []e2e.Message, match func(e2e.Message) bool) int {
	n := 0
	for _, m := range ms {
		if m.FromAgent && match(m) {
			n++
		}
	}
	return n
}

var reRefusal = regexp.MustCompile(`(?i)\b(can(no|')t|can not|not allowed|refus\w*|only|unable|not able)\b`)

// refusal accepts a product message that refuses and says who may do it. The contract: a refusal has a reason
// sentence with one of the words in reRefusal and names, by chat handle, the person who may.
func refusal(whoMay string) func(e2e.Message) bool {
	return func(m e2e.Message) bool {
		return reRefusal.MatchString(m.Text) && strings.Contains(m.Text, whoMay)
	}
}

// ---- the gate ----

func describeGate(g e2e.GateState) string {
	codes := make([]string, 0, len(g.Reasons))
	for _, r := range g.Reasons {
		codes = append(codes, r.Text)
	}
	return fmt.Sprintf("gate %s on %s: %q; reasons: %s", g.State, shortSHA(g.Head), g.Summary, strings.Join(codes, "; "))
}

func hasCode(g e2e.GateState, code string) bool {
	for _, r := range g.Reasons {
		if r.Code == code {
			return true
		}
	}
	return false
}

func hasReason(g e2e.GateState, re *regexp.Regexp) bool {
	for _, r := range g.Reasons {
		if re.MatchString(r.Text) {
			return true
		}
	}
	return false
}

// forHead reports whether the gate is about the PR's current head. A gate without a commit is taken at its word.
func forHead(g e2e.GateState, pr e2e.PR) bool {
	return g.Head == "" || pr.SHA == "" || g.Head == pr.SHA
}

// awaitGate waits until the gate for pr's head satisfies ok, then records it as seen.
func awaitGate(ctx context.Context, e *e2e.Env, pr e2e.PR, what string, timeout time.Duration, ok func(e2e.GateState) bool) (e2e.GateState, error) {
	var last e2e.GateState
	err := e.World.Clock().Until(ctx, what, timeout, func(ctx context.Context) (bool, string, error) {
		g, err := e.World.Server().Gate(ctx, pr)
		if err != nil {
			return false, "", err
		}
		last = g
		return forHead(g, pr) && ok(g), describeGate(g), nil
	})
	if err != nil {
		return last, err
	}
	e.Capture.AddGate(last)
	return last, nil
}

func gateIs(state string) func(e2e.GateState) bool {
	return func(g e2e.GateState) bool { return g.State == state }
}

// ---- findings ----

// readFindings reads the findings a person sees on the PR and adds each to what they saw, with the number of lines
// its file has at the PR's head.
func readFindings(ctx context.Context, e *e2e.Env, pr e2e.PR) ([]e2e.Finding, error) {
	fs, err := e.World.Server().Findings(ctx, pr)
	if err != nil {
		return nil, err
	}
	for _, f := range fs {
		view := f.FindingView
		if view.File != "" {
			n, err := e.World.GitHub().FileLines(ctx, e.Story.Repo, pr.Head, view.File)
			if err != nil {
				return nil, err
			}
			view.FileLines = n
		}
		e.Capture.AddFinding(view)
	}
	return fs, nil
}

// planted finds a planted issue of the story by its id.
func planted(s *e2e.Story, id string) (e2e.Planted, bool) {
	for _, p := range s.Planted {
		if p.ID == id {
			return p, true
		}
	}
	return e2e.Planted{}, false
}

// findingFor finds the finding a person would match to a planted issue: the one at its file and lines.
func findingFor(p e2e.Planted, fs []e2e.Finding) (e2e.Finding, bool) {
	for _, f := range fs {
		if f.File == p.File && f.Line >= p.Lines[0] && f.Line <= p.Lines[1] {
			return f, true
		}
	}
	return e2e.Finding{}, false
}

func findingByID(id string, fs []e2e.Finding) (e2e.Finding, bool) {
	for _, f := range fs {
		if f.ID == id {
			return f, true
		}
	}
	return e2e.Finding{}, false
}

func where(p e2e.Planted) string { return fmt.Sprintf("%s:%d-%d", p.File, p.Lines[0], p.Lines[1]) }
