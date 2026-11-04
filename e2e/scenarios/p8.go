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

func p8() e2e.Scenario {
	return e2e.Scenario{ID: "P8", Title: "Local mode: a stranger can run it", Steps: []e2e.Step{p81(), p82()}}
}

func p81() e2e.Step {
	return e2e.Step{
		ID:   "P8.1",
		Does: "In a clean container with its own Docker daemon: follow only the README to start, with the file tracker and polling, no Jira, no public URL (D157)",
		Sees: "`heimdall doctor` is green; a ticket file creates a change",
		Tags: []string{"container"},
		// container: the whole step happens inside it; nothing outside is touched.
		Needs: []e2e.Need{e2e.NeedContainer},
		Run:   runP81,
	}
}

// ticketFile is how a person writes a ticket for the file tracker: a heading, the description, then a list of
// acceptance criteria. This is the contract the file tracker reads.
func ticketFile(spec e2e.TicketSpec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n\n", spec.Title, spec.Description)
	for _, ac := range spec.AC {
		fmt.Fprintf(&b, "- %s\n", ac)
	}
	return b.String()
}

func runP81(ctx context.Context, e *e2e.Env) error {
	room := e.World.Clean()
	doc, err := room.Exec(ctx, "heimdall", "doctor")
	if err != nil {
		return err
	}
	if doc.ExitCode != 0 {
		return &e2e.Red{Expected: "heimdall doctor to be green", Saw: fmt.Sprintf("exit %d: %s", doc.ExitCode, doc.Stdout+doc.Stderr)}
	}
	if err := room.WriteFile(ctx, "tickets/"+slug(e.Story.Ticket.Title)+".md", ticketFile(e.Story.Ticket)); err != nil {
		return err
	}
	return e.World.Clock().Until(ctx, "the ticket file to create a change", roomWait, func(ctx context.Context) (bool, string, error) {
		res, err := room.Exec(ctx, "heimdall", "changes")
		return strings.Contains(res.Stdout, e.Story.Ticket.Title), res.Stdout, err
	})
}

var reNotSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(title string) string {
	return strings.Trim(reNotSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
}

func p82() e2e.Step {
	return e2e.Step{
		ID:   "P8.2",
		Does: "Push a PR",
		Sees: "red-then-green in under 20 minutes total with real agents on the reader's own OpenRouter key, and the dashboard shows the same story",
		Tags: []string{"container", "live"},
		// container: the product runs in it; live: real agents; github-user and fixture-repo: the PR is pushed to the
		// story's repository and approved there.
		Needs: []e2e.Need{e2e.NeedContainer, e2e.NeedLive, e2e.NeedGitHubUser, e2e.NeedFixtureRepo},
		Bar:   20 * time.Minute,
		After: "P8.1",
		Run:   runP82,
	}
}

var reGreen = regexp.MustCompile(`(?i)\b(green|success|passed)\b`)

// runP82 reads the product in the container as the one that watches the PR: it needs no review request, it runs
// the review itself. Red is the gate failing on the findings; green is the gate after the fix and an approval.
func runP82(ctx context.Context, e *e2e.Env) error {
	pr, err := openPR(ctx, e, e.Story.GoodPR, e.Story.Ticket.Title)
	if err != nil {
		return err
	}
	if _, err := awaitGate(ctx, e, pr, "the gate to go red", 20*time.Minute, gateIs("failure")); err != nil {
		return err
	}
	fixed, err := e.World.GitHub().SetHead(ctx, pr, e.Story.FixBranch, e.Story.Author().GitHub)
	if err != nil {
		return err
	}
	if err := e.World.GitHub().Review(ctx, fixed, e.Story.Approver().GitHub, reviewApproved, fixed.SHA); err != nil {
		return err
	}
	if _, err := awaitGate(ctx, e, fixed, "the gate to go green", 20*time.Minute, gateIs("success")); err != nil {
		return err
	}
	dash, err := e.World.Clean().Dashboard(ctx)
	if err != nil {
		return err
	}
	if !strings.Contains(dash, strconv.Itoa(pr.Number)) || !reGreen.MatchString(dash) {
		return &e2e.Red{Expected: fmt.Sprintf("the dashboard to show PR %d and that it went green", pr.Number), Saw: brief(dash)}
	}
	return nil
}
