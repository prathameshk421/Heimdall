package scenarios

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

func p7() e2e.Scenario {
	return e2e.Scenario{
		ID: "P7", Title: "It survives bad days",
		// Several review rounds on one change: listings, progress lines, outage notices and the memory notice.
		Budget: e2e.Budget{Milestones: 12, Questions: 0, Decisions: 2},
		Steps:  []e2e.Step{p71(), p72(), p73(), p74(), p75()},
	}
}

// outage stops a component, runs during, and starts the component again whatever happened. A failure to start it
// again is reported next to any other error.
func outage(ctx context.Context, e *e2e.Env, c e2e.Component, during func(context.Context) error) error {
	if err := e.World.Faults().Stop(ctx, c); err != nil {
		return err
	}
	err := during(ctx)
	return errors.Join(err, e.World.Faults().Start(context.WithoutCancel(ctx), c))
}

// watcher records whether a gate was ever seen green and which reason codes it showed.
type watcher struct {
	green bool
	codes map[string]bool
}

func (w *watcher) sample(ctx context.Context, e *e2e.Env, pr e2e.PR) error {
	g, err := e.World.Server().Gate(ctx, pr)
	if err != nil {
		return err
	}
	w.green = w.green || (g.State == "success" && forHead(g, pr))
	if w.codes == nil {
		w.codes = map[string]bool{}
	}
	for _, r := range g.Reasons {
		w.codes[r.Code] = true
	}
	return nil
}

func neverGreen(w *watcher) error {
	if w.green {
		return &e2e.Red{Expected: "the check never to be green meanwhile", Saw: "the gate was green"}
	}
	return nil
}

func p71() e2e.Step {
	return e2e.Step{
		ID:   "P7.1",
		Does: "Kill the server mid-review and restart it",
		Sees: "the run continues from where it was; no duplicate room message, no duplicate check, nothing lost; the check was never green in between",
		Tags: []string{"local"},
		// The review chain; the faults are the injector's.
		Needs: reviewChain(),
		Run:   runP71,
	}
}

func runP71(ctx context.Context, e *e2e.Env) error {
	room, err := ensureRoom(ctx, e)
	if err != nil {
		return err
	}
	pr, err := ensureGoodPR(ctx, e)
	if err != nil {
		return err
	}
	if _, err := requestReview(ctx, e, room); err != nil {
		return err
	}
	w := &watcher{}
	if err := outage(ctx, e, e2e.ComponentServer, func(ctx context.Context) error { return w.sample(ctx, e, pr) }); err != nil {
		return err
	}
	if _, err := awaitReviews(ctx, e, room, reviewWait, func(ctx context.Context) error { return w.sample(ctx, e, pr) }); err != nil {
		return err
	}
	if err := neverGreen(w); err != nil {
		return err
	}
	return judgeNoDuplicates(ctx, e, pr, room)
}

func judgeNoDuplicates(ctx context.Context, e *e2e.Env, pr e2e.PR, room string) error {
	ms, err := readRoom(ctx, e, room)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, m := range ms {
		if !m.FromAgent {
			continue
		}
		if seen[m.Text] {
			return &e2e.Red{Expected: "no duplicate room message", Saw: "twice: " + brief(m.Text)}
		}
		seen[m.Text] = true
	}
	checks, err := e.World.Server().Checks(ctx, pr)
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for _, c := range checks {
		if names[c.Name] {
			return &e2e.Red{Expected: "no duplicate check", Saw: c.Name + " is there twice"}
		}
		names[c.Name] = true
	}
	return nil
}

func p72() e2e.Step {
	return e2e.Step{
		ID:   "P7.2",
		Does: "Stop Switch (or the chat) during a review",
		Sees: "the gate stays red with `agents_unreachable`, not green and not stuck \"pending\" forever; it recovers by itself when Switch returns",
		Tags: []string{"vm"},
		// The review chain, since Switch is what the reviewers run under.
		Needs: reviewChain(),
		Run:   runP72,
	}
}

func runP72(ctx context.Context, e *e2e.Env) error {
	room, err := ensureRoom(ctx, e)
	if err != nil {
		return err
	}
	pr, err := ensureGoodPR(ctx, e)
	if err != nil {
		return err
	}
	if _, err := requestReview(ctx, e, room); err != nil {
		return err
	}
	w := &watcher{}
	err = outage(ctx, e, e2e.ComponentSwitch, func(ctx context.Context) error {
		if _, err := awaitGate(ctx, e, pr, "the gate to say the agents are unreachable", workWait, func(g e2e.GateState) bool {
			return g.State == "failure" && hasCode(g, "agents_unreachable")
		}); err != nil {
			return err
		}
		return w.sample(ctx, e, pr)
	})
	if err != nil {
		return err
	}
	if err := neverGreen(w); err != nil {
		return err
	}
	_, err = awaitGate(ctx, e, pr, "the gate to recover by itself", workWait, func(g e2e.GateState) bool {
		return !hasCode(g, "agents_unreachable")
	})
	return err
}

func p73() e2e.Step {
	return e2e.Step{
		ID:   "P7.3",
		Does: "Stop memory",
		Sees: "findings that needed memory fall to unverified and stop blocking; the room says so",
		Tags: []string{"local"},
		// The review chain; memory is the thing stopped.
		Needs: append(reviewChain(), e2e.NeedMemory),
		Run:   runP73,
	}
}

var reMemory = regexp.MustCompile(`(?i)\bmemory\b`)

// runP73 reads "the room says so" as an agent message that mentions memory, posted after memory stopped.
func runP73(ctx context.Context, e *e2e.Env) error {
	pr, err := reviewedPR(ctx, e)
	if err != nil {
		return err
	}
	room, err := earlier[string](e, "the review", keyRoom)
	if err != nil {
		return err
	}
	before, err := readRoom(ctx, e, room)
	if err != nil {
		return err
	}
	known := knownIDs(before)
	return outage(ctx, e, e2e.ComponentMemory, func(ctx context.Context) error {
		if err := awaitUnverified(ctx, e, pr); err != nil {
			return err
		}
		_, err := awaitMessage(ctx, e, room, "the room to say memory is down", replyWait, func(m e2e.Message) bool {
			return !known[m.ID] && reMemory.MatchString(m.Text)
		})
		return err
	})
}

// awaitUnverified waits until every planted issue that needs memory is shown unverified and not blocking.
func awaitUnverified(ctx context.Context, e *e2e.Env, pr e2e.PR) error {
	return e.World.Clock().Until(ctx, "memory findings to fall to unverified", gateWait, func(ctx context.Context) (bool, string, error) {
		fs, err := readFindings(ctx, e, pr)
		if err != nil {
			return false, "", err
		}
		checked := 0
		for _, p := range e.Story.Planted {
			if !p.NeedsMemory {
				continue
			}
			checked++
			f, ok := findingFor(p, fs)
			if !ok || f.Confirmed || f.Blocking {
				return false, p.ID + ": " + describeFinding(f, ok), nil
			}
		}
		if checked == 0 {
			return false, "", storyGap("a planted issue that needs memory")
		}
		return true, "", nil
	})
}

func p74() e2e.Step {
	return e2e.Step{
		ID:   "P7.4",
		Does: "Stop the runner or Postgres for 30 s",
		Sees: "the gate shows `verification_pending`, never green; recovery needs no manual step",
		Tags: []string{"local"},
		// The review chain, so the PR has been reviewed before the runner stops.
		Needs: reviewChain(),
		Run:   runP74,
	}
}

const downFor = 30 * time.Second

// runP74 stops the runner, has the author push a commit that needs verifying, and watches the gate for 30 s on the
// world's clock.
func runP74(ctx context.Context, e *e2e.Env) error {
	pr, err := reviewedPR(ctx, e)
	if err != nil {
		return err
	}
	w := &watcher{}
	err = outage(ctx, e, e2e.ComponentRunner, func(ctx context.Context) error {
		sha, err := e.World.GitHub().Push(ctx, e.Story.Repo, pr.Head, e.Story.Author().GitHub, e2e.Change{
			Files: map[string]string{"NOTES.txt": "A note written while the runner is down.\n"}, Message: "Add a note",
		})
		if err != nil {
			return err
		}
		pr.SHA = sha
		return watchFor(ctx, e, pr, w, downFor)
	})
	if err != nil {
		return err
	}
	if err := neverGreen(w); err != nil {
		return err
	}
	if !w.codes["verification_pending"] {
		return &e2e.Red{Expected: "the gate to show verification_pending while the runner is down", Saw: "reasons seen: " + codeList(w.codes)}
	}
	_, err = awaitGate(ctx, e, pr, "the gate to recover by itself", gateWait, func(g e2e.GateState) bool {
		return g.State != "in_progress" && !hasCode(g, "verification_pending")
	})
	return err
}

// watchFor samples the gate until d has passed on the world's clock.
func watchFor(ctx context.Context, e *e2e.Env, pr e2e.PR, w *watcher, d time.Duration) error {
	clock := e.World.Clock()
	start := clock.Now()
	return clock.Until(ctx, fmt.Sprintf("%s of watching the gate", d), d+10*time.Second, func(ctx context.Context) (bool, string, error) {
		if err := w.sample(ctx, e, pr); err != nil {
			return false, "", err
		}
		return clock.Since(start) >= d, fmt.Sprintf("%s so far", clock.Since(start)), nil
	})
}

func codeList(codes map[string]bool) string {
	var out []string
	for c := range codes {
		out = append(out, c)
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}

func p75() e2e.Step {
	return e2e.Step{
		ID:   "P7.5",
		Does: "Break `heimdall.yaml` in the default branch",
		Sees: "the server refuses to start with a message naming the key; at runtime the gate fails closed with `policy_invalid`",
		Tags: []string{"local"},
		// server: restarted and read; github-user and fixture-repo: the base branch is edited and a PR pushed;
		// tracker: the PR belongs to a started ticket.
		Needs: []e2e.Need{e2e.NeedServer, e2e.NeedTracker, e2e.NeedGitHubUser, e2e.NeedFixtureRepo},
		Run:   runP75,
	}
}

const brokenKey = "max_changed_lines"

var reBrokenValue = regexp.MustCompile(brokenKey + `:\s*\d+`)

// runP75 breaks one number in the policy (a word where the lines limit should be), so the file stays whole except
// for the key it names. The policy is put back at the end, and the server started again.
func runP75(ctx context.Context, e *e2e.Env) (err error) {
	pr, err := ensureGoodPR(ctx, e)
	if err != nil {
		return err
	}
	gh, approver := e.World.GitHub(), e.Story.Approver().GitHub
	orig, err := gh.ReadFile(ctx, e.Story.Repo, e.Story.BaseBranch, "heimdall.yaml")
	if err != nil {
		return err
	}
	if !reBrokenValue.MatchString(orig) {
		return &e2e.Red{Expected: "the base policy to set " + brokenKey, Saw: "heimdall.yaml on " + e.Story.BaseBranch + " does not"}
	}
	broken := reBrokenValue.ReplaceAllString(orig, brokenKey+": many")
	if _, err := gh.Push(ctx, e.Story.Repo, e.Story.BaseBranch, approver, e2e.Change{Files: map[string]string{"heimdall.yaml": broken}, Message: "Break the policy"}); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, restorePolicy(context.WithoutCancel(ctx), e, orig)) }()
	if err := failsClosed(ctx, e, pr); err != nil {
		return err
	}
	return refusesToStart(ctx, e)
}

// restorePolicy puts the base policy back and makes sure the server runs.
func restorePolicy(ctx context.Context, e *e2e.Env, orig string) error {
	_, err := e.World.GitHub().Push(ctx, e.Story.Repo, e.Story.BaseBranch, e.Story.Approver().GitHub, e2e.Change{
		Files: map[string]string{"heimdall.yaml": orig}, Message: "Put the policy back",
	})
	return errors.Join(err, e.World.Faults().Start(ctx, e2e.ComponentServer))
}

func failsClosed(ctx context.Context, e *e2e.Env, pr e2e.PR) error {
	sha, err := e.World.GitHub().Push(ctx, e.Story.Repo, pr.Head, e.Story.Author().GitHub, e2e.Change{
		Files: map[string]string{"NOTES.txt": "A note pushed under a broken policy.\n"}, Message: "Add a note",
	})
	if err != nil {
		return err
	}
	pr.SHA = sha
	_, err = awaitGate(ctx, e, pr, "the gate to fail closed", gateWait, func(g e2e.GateState) bool {
		return g.State == "failure" && hasCode(g, "policy_invalid")
	})
	return err
}

func refusesToStart(ctx context.Context, e *e2e.Env) error {
	if err := e.World.Faults().Stop(ctx, e2e.ComponentServer); err != nil {
		return err
	}
	err := e.World.Faults().Start(ctx, e2e.ComponentServer)
	var refused *e2e.StartRefusedError
	switch {
	case errors.As(err, &refused):
		if !strings.Contains(refused.Output, brokenKey) {
			return &e2e.Red{Expected: "the refusal to name " + brokenKey, Saw: refused.Output}
		}
		return nil
	case err != nil:
		return err
	}
	return &e2e.Red{Expected: "the server to refuse to start on a broken policy", Saw: "it started"}
}
