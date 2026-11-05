package scenarios

import (
	"context"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

func p2() e2e.Scenario {
	return e2e.Scenario{ID: "P2", Title: "The door: slop is turned away in seconds", Steps: []e2e.Step{p21(), p22(), p23(), p24()}}
}

// noTicketTitle is a title with no ticket key in it, for the pull request that has no ticket.
const noTicketTitle = "A big change with no ticket"

func p21() e2e.Step {
	return e2e.Step{
		ID:   "P2.1",
		Does: "Open `bad_pr` (very large, no ticket)",
		Sees: "`heimdall/gate` is `failure` within 15 s (p95, D155) and its summary names the rule(s) in a sentence a developer understands",
		Tags: []string{"local"},
		// server: the gate is the product's; github-user and fixture-repo: the person opens the story's bad PR.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedGitHubUser, e2e.NeedFixtureRepo},
		Bar:   gateBar,
		Run:   runP21,
	}
}

func runP21(ctx context.Context, e *e2e.Env) error {
	e.Set(keyQuiet, snapshotQuiet(ctx, e))
	pr, err := openPR(ctx, e, e.Story.BadPR, noTicketTitle)
	if err != nil {
		return err
	}
	g, err := awaitGate(ctx, e, pr, "the gate to fail", gateBar, gateIs("failure"))
	if err != nil {
		return err
	}
	if len(g.Reasons) == 0 || len(strings.Fields(g.Summary)) < 4 {
		return &e2e.Red{Expected: "a summary sentence that names the rule, and a reason for each", Saw: describeGate(g)}
	}
	return nil
}

// quietBaseline is what the door step saw before the PR opened, to compare after it.
type quietBaseline struct {
	spend    float64
	hasSpend bool
	tasks    map[string]int
	hasTasks bool
}

// snapshotQuiet reads spend and agent tasks if the drivers can. A driver that cannot (it is not built) leaves the
// field unset on purpose: P2.2 reads again and reports that driver's error, so nothing is hidden.
func snapshotQuiet(ctx context.Context, e *e2e.Env) quietBaseline {
	var q quietBaseline
	if s, err := e.World.Server().Spend(ctx); err == nil {
		q.spend, q.hasSpend = s, true
	}
	if t, err := e.World.Agents().Tasks(ctx); err == nil {
		q.tasks, q.hasTasks = t, true
	}
	return q
}

func p22() e2e.Step {
	return e2e.Step{
		ID:   "P2.2",
		Does: "nothing",
		Sees: "**no agent was woken and no token was spent** (OpenRouter usage unchanged across the step, and no agent logged a task)",
		Tags: []string{"vm"},
		// server: spend is read from it; the agents' task log is read on the VM through the forward.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedForward},
		After: "P2.1",
		Run:   runP22,
	}
}

func runP22(ctx context.Context, e *e2e.Env) error {
	base, err := earlier[quietBaseline](e, "P2.1", keyQuiet)
	if err != nil {
		return err
	}
	spend, err := e.World.Server().Spend(ctx)
	if err != nil {
		return err
	}
	tasks, err := e.World.Agents().Tasks(ctx)
	if err != nil {
		return err
	}
	if !base.hasSpend || !base.hasTasks {
		return &e2e.Red{Expected: "spend and agent tasks read before the PR opened", Saw: "they could not be read then"}
	}
	if spend > base.spend {
		return &e2e.Red{Expected: "no token spent", Saw: fmt.Sprintf("usage went from $%.4f to $%.4f", base.spend, spend)}
	}
	for agent, n := range tasks {
		if n != base.tasks[agent] {
			return &e2e.Red{Expected: "no agent to log a task", Saw: fmt.Sprintf("%s logged %d tasks, was %d", agent, n, base.tasks[agent])}
		}
	}
	return nil
}

func p23() e2e.Step {
	return e2e.Step{
		ID:   "P2.3",
		Does: "Push a fix that adds the ticket key and removes the extra files",
		Sees: "the check re-runs on the new head and moves to `in_progress`",
		Tags: []string{"local"},
		// server: the check re-runs; tracker: the ticket whose key goes in; github-user and fixture-repo: the push.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedTracker, e2e.NeedGitHubUser, e2e.NeedFixtureRepo},
		Bar:   gateBar,
		After: "P2.1",
		Run:   runP23,
	}
}

// runP23 reads "the extra files" as the files bad_pr changes that good_pr, the real change, does not. The key goes
// in the commit message.
func runP23(ctx context.Context, e *e2e.Env) error {
	pr, err := earlier[e2e.PR](e, "P2.1", keyPR)
	if err != nil {
		return err
	}
	key, err := createTicket(ctx, e, e.Story.Ticket)
	if err != nil {
		return err
	}
	e.Set(keyTicket, key)
	gh := e.World.GitHub()
	bad, err := gh.ChangedFiles(ctx, e.Story.Repo, e.Story.BaseBranch, e.Story.BadPR)
	if err != nil {
		return err
	}
	good, err := gh.ChangedFiles(ctx, e.Story.Repo, e.Story.BaseBranch, e.Story.GoodPR)
	if err != nil {
		return err
	}
	sha, err := gh.Push(ctx, e.Story.Repo, pr.Head, e.Story.Author().GitHub, e2e.Change{
		Delete: without(bad, good), Message: key + ": " + e.Story.Ticket.Title,
	})
	if err != nil {
		return err
	}
	pr.SHA = sha
	e.Set(keyPR, pr)
	_, err = awaitGate(ctx, e, pr, "the check to move to in_progress on the new head", gateBar, gateIs("in_progress"))
	return err
}

// without lists what is in a and not in b.
func without(a, b []string) []string {
	var out []string
	for _, x := range a {
		if !contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

func p24() e2e.Step {
	return e2e.Step{
		ID:   "P2.4",
		Does: "Open `good_pr`",
		Sees: "`heimdall/gate` is `in_progress` (not failure) within 15 s and `heimdall/acceptance` runs",
		Tags: []string{"local"},
		// server: the checks are the product's; tracker: good_pr belongs to a started ticket; github-user and
		// fixture-repo: the person opens the story's good PR.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedTracker, e2e.NeedGitHubUser, e2e.NeedFixtureRepo},
		Bar:   gateBar,
		Run:   runP24,
	}
}

func runP24(ctx context.Context, e *e2e.Env) error {
	pr, err := ensureGoodPR(ctx, e)
	if err != nil {
		return err
	}
	var gate e2e.GateState
	err = e.World.Clock().Until(ctx, "the gate and the acceptance check to show", gateBar, func(ctx context.Context) (bool, string, error) {
		g, err := e.World.Server().Gate(ctx, pr)
		if err != nil {
			return false, "", err
		}
		checks, err := e.World.Server().Checks(ctx, pr)
		if err != nil {
			return false, "", err
		}
		gate = g
		return g.State != "" && hasCheck(checks, "heimdall/acceptance"), describeGate(g), nil
	})
	if err != nil {
		return err
	}
	e.Capture.AddGate(gate)
	if gate.State != "in_progress" {
		return &e2e.Red{Expected: "the gate in_progress", Saw: describeGate(gate)}
	}
	return nil
}

func hasCheck(checks []e2e.Check, name string) bool {
	for _, c := range checks {
		if c.Name == name {
			return true
		}
	}
	return false
}
