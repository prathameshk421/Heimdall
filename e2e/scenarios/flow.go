package scenarios

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

// ticketOf is the tracker ticket for a story's ticket spec, assigned to the developer.
func ticketOf(e *e2e.Env, spec e2e.TicketSpec) e2e.Ticket {
	return e2e.Ticket{Title: spec.Title, Description: spec.Description, AC: spec.AC, Assignee: e.Story.Author().Chat}
}

// createTicket writes the ticket in the tracker and leaves it where it is.
func createTicket(ctx context.Context, e *e2e.Env, spec e2e.TicketSpec) (string, error) {
	return e.World.Tracker().CreateTicket(ctx, ticketOf(e, spec))
}

// startTicket writes the ticket, assigns it to the developer and moves it to In Progress, as P1.1 does.
func startTicket(ctx context.Context, e *e2e.Env, spec e2e.TicketSpec) (string, error) {
	key, err := createTicket(ctx, e, spec)
	if err != nil {
		return "", err
	}
	if err := e.World.Tracker().Move(ctx, key, statusInProgress); err != nil {
		return "", err
	}
	return key, nil
}

// ensureStarted returns the scenario's ticket key, starting the story's ticket if no step has yet.
func ensureStarted(ctx context.Context, e *e2e.Env) (string, error) {
	if key, ok := e2e.Get[string](e, keyTicket); ok {
		return key, nil
	}
	key, err := startTicket(ctx, e, e.Story.Ticket)
	if err != nil {
		return "", err
	}
	e.Set(keyTicket, key)
	return key, nil
}

// awaitRoom waits for the room named for the ticket and returns its name.
func awaitRoom(ctx context.Context, e *e2e.Env, key string) (string, error) {
	var room string
	err := e.World.Clock().Until(ctx, "a room named for "+key, roomWait, func(ctx context.Context) (bool, string, error) {
		rooms, err := e.World.Chat().Rooms(ctx)
		if err != nil {
			return false, "", err
		}
		for _, r := range rooms {
			if strings.Contains(strings.ToLower(r), strings.ToLower(key)) {
				room = r
				return true, "", nil
			}
		}
		return false, fmt.Sprintf("%d rooms, none named for %s", len(rooms), key), nil
	})
	return room, err
}

// ensureRoom returns the change's room, starting the ticket and waiting for the room when no step has.
func ensureRoom(ctx context.Context, e *e2e.Env) (string, error) {
	if room, ok := e2e.Get[string](e, keyRoom); ok {
		return room, nil
	}
	key, err := ensureStarted(ctx, e)
	if err != nil {
		return "", err
	}
	room, err := awaitRoom(ctx, e, key)
	if err != nil {
		return "", err
	}
	e.Set(keyRoom, room)
	return room, nil
}

// openPR opens a pull request from a branch as the developer and remembers it.
func openPR(ctx context.Context, e *e2e.Env, branch, title string) (e2e.PR, error) {
	pr, err := e.World.GitHub().OpenPR(ctx, e.Story.Repo, branch, e.Story.BaseBranch, title, "", e.Story.Author().GitHub)
	if err != nil {
		return e2e.PR{}, err
	}
	e.Set(keyPR, pr)
	return pr, nil
}

// ensureGoodPR returns the open good_pr, opening it, titled with the ticket key, when no step has.
func ensureGoodPR(ctx context.Context, e *e2e.Env) (e2e.PR, error) {
	if pr, ok := e2e.Get[e2e.PR](e, keyPR); ok {
		return pr, nil
	}
	key, err := ensureStarted(ctx, e)
	if err != nil {
		return e2e.PR{}, err
	}
	return openPR(ctx, e, e.Story.GoodPR, key+": "+e.Story.Ticket.Title)
}

// freshPR closes the scenario's PR and opens good_pr again for the same ticket, so a new review round starts.
// The ticket and its room stay, which keeps the step quick.
func freshPR(ctx context.Context, e *e2e.Env) (e2e.PR, error) {
	if old, ok := e2e.Get[e2e.PR](e, keyPR); ok {
		if err := e.World.GitHub().Close(ctx, old); err != nil {
			return e2e.PR{}, err
		}
		delete(e.Vars, keyPR)
		delete(e.Vars, keyReviewed)
		delete(e.Vars, keyAsked)
	}
	return ensureGoodPR(ctx, e)
}

// reviewerHandles are the chat handles of the story's reviewers, in order.
func reviewerHandles(s *e2e.Story) []string {
	var out []string
	for _, name := range s.Reviewers() {
		out = append(out, s.Person(name).Chat)
	}
	return out
}

func reviewCommand(s *e2e.Story) string {
	parts := []string{"heimdall review"}
	for _, h := range reviewerHandles(s) {
		parts = append(parts, mention(h))
	}
	return strings.Join(parts, " ")
}

var reAsked = regexp.MustCompile(`(?i)\bask`)

// listsReviewers accepts the product's message that lists every reviewer and says they were asked.
func listsReviewers(s *e2e.Story) func(e2e.Message) bool {
	return func(m e2e.Message) bool {
		if !reAsked.MatchString(m.Text) {
			return false
		}
		for _, h := range reviewerHandles(s) {
			if !strings.Contains(m.Text, h) {
				return false
			}
		}
		return true
	}
}

// requestReview has the author ask for the review in the room and waits for the product's listing of the reviewers.
func requestReview(ctx context.Context, e *e2e.Env, room string) (e2e.Message, error) {
	m, err := converse(ctx, e, room, e.Story.Author().Chat, reviewCommand(e.Story), listsReviewers(e.Story))
	if err == nil {
		e.Set(keyAsked, true)
	}
	return m, err
}

// The contract for the progress line (P3.2): "N of M reviews in", posted as a milestone.
var reProgress = regexp.MustCompile(`(\d+) of (\d+) reviews in`)

// progressOf reads the line's counts.
func progressOf(m e2e.Message) (n, total int, ok bool) {
	g := reProgress.FindStringSubmatch(m.Text)
	if g == nil {
		return 0, 0, false
	}
	n, _ = strconv.Atoi(g[1])
	total, _ = strconv.Atoi(g[2])
	return n, total, true
}

// awaitReviews waits until the room says every reviewer has submitted and returns when it said so. poll, when not
// nil, runs on every look, so a step can watch something else meanwhile.
func awaitReviews(ctx context.Context, e *e2e.Env, room string, timeout time.Duration, poll func(context.Context) error) (time.Time, error) {
	var at time.Time
	err := e.World.Clock().Until(ctx, "every reviewer to have submitted", timeout, func(ctx context.Context) (bool, string, error) {
		if poll != nil {
			if err := poll(ctx); err != nil {
				return false, "", err
			}
		}
		ms, err := readRoom(ctx, e, room)
		if err != nil {
			return false, "", err
		}
		saw := "no progress line yet"
		for _, m := range ms {
			if n, total, ok := progressOf(m); ok {
				saw = fmt.Sprintf("%d of %d reviews in", n, total)
				if n == total {
					at = m.At
					return true, saw, nil
				}
			}
		}
		return false, saw, nil
	})
	return at, err
}

// awaitFindings waits for the findings to be shown on the PR and reads them.
func awaitFindings(ctx context.Context, e *e2e.Env, pr e2e.PR) ([]e2e.Finding, error) {
	err := e.World.Clock().Until(ctx, "the findings to be shown", gateWait, func(ctx context.Context) (bool, string, error) {
		fs, err := e.World.Server().Findings(ctx, pr)
		return len(fs) > 0, "no findings yet", err
	})
	if err != nil {
		return nil, err
	}
	return readFindings(ctx, e, pr)
}

// reviewedPR returns good_pr with its review done: a ticket, a room, a PR, the reviewers asked and all submitted.
// It does the work again only when no earlier step of the scenario has.
func reviewedPR(ctx context.Context, e *e2e.Env) (e2e.PR, error) {
	if done, _ := e2e.Get[bool](e, keyReviewed); done {
		pr, err := earlier[e2e.PR](e, "the review", keyPR)
		return pr, err
	}
	room, err := ensureRoom(ctx, e)
	if err != nil {
		return e2e.PR{}, err
	}
	pr, err := ensureGoodPR(ctx, e)
	if err != nil {
		return e2e.PR{}, err
	}
	if asked, _ := e2e.Get[bool](e, keyAsked); !asked {
		if _, err := requestReview(ctx, e, room); err != nil {
			return e2e.PR{}, err
		}
	}
	at, err := awaitReviews(ctx, e, room, reviewWait, nil)
	if err != nil {
		return e2e.PR{}, err
	}
	e.Capture.MarkAllSubmitted(at)
	if _, err := awaitFindings(ctx, e, pr); err != nil {
		return e2e.PR{}, err
	}
	e.Set(keyReviewed, true)
	return pr, nil
}

// reviewChain lists the prerequisites of a step that begins with a finished review. Order matters: the server is
// probed first, so a board with nothing built never calls out to GitHub.
func reviewChain() []e2e.Need {
	return []e2e.Need{e2e.NeedServer, e2e.NeedForward, e2e.NeedTracker, e2e.NeedGitHubUser, e2e.NeedChatUser, e2e.NeedFixtureRepo, e2e.NeedScripted}
}
