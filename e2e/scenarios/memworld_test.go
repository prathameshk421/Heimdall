package scenarios_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

// memWorld is a small, honest double of the world for testing the steps. It is never a product adapter: it holds
// just enough of a "product" (a gate that follows the rules, a room that obeys commands) to show that a step
// passes against a correct one and is red, in plain words, against a wrong one. The quirks switch on the wrong ones.
type memWorld struct {
	story *e2e.Story
	q     quirks

	now      time.Time
	nextPR   int
	prs      []*memPR
	branches map[string]map[string]string
	pushes   int

	msgs                  []e2e.Message
	findings              []e2e.Finding
	comments              []e2e.PRComment
	tasks                 map[string]int
	spend                 float64
	spendReads, taskReads int
}

// quirks are the ways a wrong product can behave. The zero value is a correct product.
type quirks struct {
	greenOnBadPR       bool          // the gate says success on a PR that has no ticket and is huge
	failureWithoutWhy  bool          // the gate fails and gives no reason
	slowGate           time.Duration // the gate shows nothing until this long after the PR opened
	noRerun            bool          // the check never re-runs on a new head
	greenWithoutAppr   bool          // the gate is green with no approval at all
	neverGreen         bool          // the gate never turns green
	countStaleApproval bool          // an approval on an older commit counts
	countAuthorAppr    bool          // the author's own approval counts
	githubRefusesSelf  bool          // GitHub itself refuses a self-approval
	authorCanUphold    bool          // the author's uphold command works
	agentCanUphold     bool          // an agent's uphold command works
	spendsLate         bool          // tokens are spent after the door step has measured its start
	wakesLate          bool          // an agent logs a task after the door step has measured its start
}

type memPR struct {
	e2e.PR
	title     string
	openedAt  time.Time
	reviewed  bool // every reviewer has submitted
	checked   string
	approvals []memApproval
}

type memApproval struct{ login, sha string }

var errNotSimulated = errors.New("the test double does not simulate this")

const (
	baseBranch = "main"
	linesInAny = 120
)

func newMemWorld(story *e2e.Story) *memWorld {
	return &memWorld{
		story: story,
		now:   time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC),
		branches: map[string]map[string]string{
			baseBranch:      {"README.md": "x"},
			story.BadPR:     {"README.md": "x", "a.go": "1", "b.go": "2", "c.go": "3"},
			story.GoodPR:    {"README.md": "x", "refund.go": "1"},
			story.FixBranch: {"README.md": "x", "refund.go": "2"},
		},
		tasks: map[string]int{},
	}
}

// ---- World ----

func (w *memWorld) Tracker() e2e.TrackerUser     { return w }
func (w *memWorld) GitHub() e2e.GitHubUser       { return w }
func (w *memWorld) Chat() e2e.ChatUser           { return w }
func (w *memWorld) Server() e2e.Observer         { return w }
func (w *memWorld) Heimdall() e2e.CLI            { return nil }
func (w *memWorld) Clock() e2e.Wait              { return w }
func (w *memWorld) Agents() e2e.AgentControl     { return memAgents{w} }
func (w *memWorld) Scripted() e2e.ScriptedAgents { return nil }
func (w *memWorld) Faults() e2e.Faults           { return nil }
func (w *memWorld) Runner() e2e.TestRunner       { return nil }
func (w *memWorld) Clean() e2e.CleanRoom         { return nil }

func (w *memWorld) Prereq(context.Context, e2e.Need) e2e.Availability {
	return e2e.Availability{OK: true}
}

// ---- clock: time moves one second for every look, so waits are fast and exact ----

func (w *memWorld) Now() time.Time                  { return w.now }
func (w *memWorld) Since(t time.Time) time.Duration { return w.now.Sub(t) }

func (w *memWorld) Until(ctx context.Context, what string, timeout time.Duration, cond func(context.Context) (bool, string, error)) error {
	start := w.now
	for {
		done, saw, err := cond(ctx)
		if err != nil {
			return fmt.Errorf("waiting for %s: %w", what, err)
		}
		if done {
			return nil
		}
		if w.now.Sub(start) >= timeout {
			return &e2e.Red{Expected: fmt.Sprintf("%s within %s", what, timeout), Saw: saw}
		}
		w.now = w.now.Add(time.Second)
	}
}

// ---- tracker ----

func (w *memWorld) CreateTicket(context.Context, e2e.Ticket) (string, error) { return "KAN-1", nil }
func (w *memWorld) Move(context.Context, string, string) error               { return nil }
func (w *memWorld) Edit(context.Context, string, e2e.Ticket) error           { return nil }

// ---- GitHub ----

func (w *memWorld) pr(number int) *memPR {
	for _, p := range w.prs {
		if p.Number == number {
			return p
		}
	}
	return nil
}

func (w *memWorld) sha(branch string) string {
	return fmt.Sprintf("sha%04d-%s", w.pushes, branch[len(branch)-1:])
}

func (w *memWorld) OpenPR(_ context.Context, repo, head, _, title, _, _ string) (e2e.PR, error) {
	w.nextPR++
	w.pushes++
	p := &memPR{PR: e2e.PR{Repo: repo, Number: w.nextPR, Head: head, SHA: w.sha(head)}, title: title, openedAt: w.now}
	p.checked = p.SHA
	w.prs = append(w.prs, p)
	return p.PR, nil
}

func (w *memWorld) Push(_ context.Context, _, branch, _ string, c e2e.Change) (string, error) {
	files := w.branches[branch]
	for path, content := range c.Files {
		files[path] = content
	}
	for _, path := range c.Delete {
		delete(files, path)
	}
	w.pushes++
	sha := w.sha(branch)
	for _, p := range w.prs {
		if p.Head == branch {
			p.SHA = sha
			p.title += " " + c.Message
			p.openedAt = w.now
			if !w.q.noRerun {
				p.checked = sha
			}
		}
	}
	return sha, nil
}

func (w *memWorld) ChangedFiles(_ context.Context, _, base, branch string) ([]string, error) {
	return w.changed(base, branch), nil
}

func (w *memWorld) changed(base, branch string) []string {
	var out []string
	for path := range w.branches[branch] {
		if _, ok := w.branches[base][path]; !ok {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

func (w *memWorld) Review(_ context.Context, pr e2e.PR, as, state, sha string) error {
	if as == w.story.Author().GitHub && w.q.githubRefusesSelf {
		return &e2e.ReviewRefusedError{Reason: "Can not approve your own pull request"}
	}
	if state == "APPROVED" {
		p := w.pr(pr.Number)
		p.approvals = append(p.approvals, memApproval{as, sha})
	}
	return nil
}

func (w *memWorld) Mergeable(_ context.Context, pr e2e.PR) (bool, error) {
	return w.gateOf(w.pr(pr.Number)).State == "success", nil
}

func (w *memWorld) Comments(context.Context, e2e.PR) ([]e2e.PRComment, error) {
	return w.comments, nil
}

func (w *memWorld) FileLines(context.Context, string, string, string) (int, error) {
	return linesInAny, nil
}

func (w *memWorld) SetHead(context.Context, e2e.PR, string, string) (e2e.PR, error) {
	return e2e.PR{}, errNotSimulated
}
func (w *memWorld) ReadFile(context.Context, string, string, string) (string, error) {
	return "", errNotSimulated
}
func (w *memWorld) Comment(context.Context, e2e.PR, string, string) (int64, error) {
	return 0, errNotSimulated
}
func (w *memWorld) EditComment(context.Context, e2e.PR, string, int64, string) error {
	return errNotSimulated
}
func (w *memWorld) Merge(context.Context, e2e.PR, string) error { return errNotSimulated }
func (w *memWorld) Close(context.Context, e2e.PR) error         { return errNotSimulated }
func (w *memWorld) Branches(context.Context, string) ([]string, error) {
	return nil, errNotSimulated
}
func (w *memWorld) Commits(context.Context, string, string) ([]e2e.Commit, error) {
	return nil, errNotSimulated
}

// ---- the gate: the rules of a correct product, with the quirks of a wrong one ----

func (w *memWorld) hasKey(p *memPR) bool { return strings.Contains(p.title, "KAN-") }

func (w *memWorld) gateOf(p *memPR) e2e.GateState {
	g := e2e.GateState{Head: p.checked, At: w.now}
	if w.now.Sub(p.openedAt) < w.q.slowGate {
		return e2e.GateState{}
	}
	files := w.changed(baseBranch, p.Head)
	var reasons []e2e.Reason
	if !w.hasKey(p) {
		reasons = append(reasons, e2e.Reason{Code: "no_ticket", Text: "Add the ticket key to the pull request title or a commit message."})
	}
	if len(files) > 2 {
		reasons = append(reasons, e2e.Reason{Code: "too_large", Text: fmt.Sprintf("This change touches %d files; remove the ones that are not part of the ticket and push again.", len(files))})
	}
	if len(reasons) > 0 {
		return w.turnedAway(g, reasons)
	}
	if !p.reviewed {
		g.State, g.Summary = "in_progress", "The reviewers have been asked; wait for them to submit."
		g.Reasons = []e2e.Reason{{Code: "reviews_pending", Text: "Wait for the reviewers to submit their reviews."}}
		return g
	}
	return w.approvalClauses(g, p)
}

func (w *memWorld) turnedAway(g e2e.GateState, reasons []e2e.Reason) e2e.GateState {
	if w.q.greenOnBadPR {
		g.State = "success"
		return g
	}
	g.State = "failure"
	if w.q.failureWithoutWhy {
		return g
	}
	g.Summary, g.Reasons = "This pull request was turned away at the door: it has no ticket key or is too large.", reasons
	return g
}

func (w *memWorld) counts(p *memPR) bool {
	for _, a := range p.approvals {
		author := a.login == w.story.Author().GitHub
		fresh := a.sha == p.SHA
		if (!author || w.q.countAuthorAppr) && (fresh || w.q.countStaleApproval) {
			return true
		}
	}
	return false
}

func (w *memWorld) approvalClauses(g e2e.GateState, p *memPR) e2e.GateState {
	if (w.counts(p) || w.q.greenWithoutAppr) && !w.q.neverGreen {
		g.State, g.Summary = "success", "Every condition is met."
		return g
	}
	g.State, g.Summary = "failure", "Not ready to merge: an approval is missing."
	g.Reasons = []e2e.Reason{{Code: "no_approval", Text: "Ask someone other than the author to approve this commit."}}
	for _, a := range p.approvals {
		switch {
		case a.login == w.story.Author().GitHub:
			g.Reasons = append(g.Reasons, e2e.Reason{Code: "no_approval", Text: "The author's own approval does not count; ask someone else to approve."})
		case a.sha != p.SHA:
			g.Reasons = append(g.Reasons, e2e.Reason{Code: "no_approval", Text: "The approval is for an older commit; approve again on this one."})
		}
	}
	return g
}

// ---- observer ----

func (w *memWorld) Gate(_ context.Context, pr e2e.PR) (e2e.GateState, error) {
	return w.gateOf(w.pr(pr.Number)), nil
}

func (w *memWorld) Checks(context.Context, e2e.PR) ([]e2e.Check, error) {
	return []e2e.Check{{Name: "heimdall/gate", Status: "in_progress"}, {Name: "heimdall/acceptance", Status: "in_progress"}}, nil
}

func (w *memWorld) Findings(context.Context, e2e.PR) ([]e2e.Finding, error) { return w.findings, nil }
func (w *memWorld) Dashboard(context.Context, string) (string, error)       { return "", nil }
func (w *memWorld) Health(context.Context) error                            { return nil }

// Spend counts its calls: the board reads it twice around each step and the door step once more, so six reads
// come before the step under test and a late spend starts on the seventh.
func (w *memWorld) Spend(context.Context) (float64, error) {
	w.spendReads++
	if w.q.spendsLate && w.spendReads > 6 {
		return w.spend + 0.02, nil
	}
	return w.spend, nil
}
func (w *memWorld) Counts(context.Context) (e2e.Counts, error) { return e2e.Counts{}, nil }
func (w *memWorld) Replay(context.Context) error               { return nil }

type memAgents struct{ w *memWorld }

func (a memAgents) Stop(context.Context, string) error  { return errNotSimulated }
func (a memAgents) Start(context.Context, string) error { return errNotSimulated }
func (a memAgents) Tasks(context.Context) (map[string]int, error) {
	a.w.taskReads++
	out := map[string]int{}
	if a.w.q.wakesLate && a.w.taskReads > 1 {
		out["bob-reviewer"] = 1
	}
	for k, v := range a.w.tasks {
		out[k] = v
	}
	return out, nil
}
