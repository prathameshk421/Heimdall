package scenarios_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
	"github.com/prathameshk421/flip_the_switch_hackathon/e2e/scenarios"
)

// The keys of the values steps share, as the steps write them. A test that starts a step in the middle of a
// scenario stores what the earlier steps would have.
const (
	varPR      = "pr"
	varOldHead = "old-head"
	varRoom    = "room"
	varFinding = "finding"
)

func stepByID(t *testing.T, id string) e2e.Step {
	t.Helper()
	for _, sc := range scenarios.All() {
		for _, st := range sc.Steps {
			if st.ID == id {
				return st
			}
		}
	}
	require.FailNow(t, "no such step", id)
	return e2e.Step{}
}

// withLocations gives the story's planted issues the files and lines the fixture will have (T31).
func withLocations(st *e2e.Story) *e2e.Story {
	files := []string{"internal/refund/service.go", "api/openapi.yaml", "internal/refund/handler.go"}
	for i := range st.Planted {
		st.Planted[i].File = files[i]
		st.Planted[i].Lines = [2]int{10 + i, 20 + i}
	}
	return st
}

// runSteps runs the named steps, in order, as one scenario on the world. setup stands in for the steps before
// them; a step whose predecessor is not in the list is treated as independent.
func runSteps(t *testing.T, w e2e.World, st *e2e.Story, setup func(e *e2e.Env), ids ...string) map[string]e2e.Result {
	t.Helper()
	listed := map[string]bool{}
	for _, id := range ids {
		listed[id] = true
	}
	steps := []e2e.Step{{ID: "setup", Run: func(_ context.Context, e *e2e.Env) error {
		if setup != nil {
			setup(e)
		}
		return nil
	}}}
	for _, id := range ids {
		step := stepByID(t, id)
		if !listed[step.After] {
			step.After = ""
		}
		steps = append(steps, step)
	}
	now := time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)
	board := &e2e.Board{World: w, Story: st, Scripted: true, Now: func() time.Time { now = now.Add(time.Second); return now }}
	res := board.Run(context.Background(), []e2e.Scenario{{ID: "T", Steps: steps}})[0]
	out := map[string]e2e.Result{}
	for _, r := range res.Results {
		out[r.Step] = r
	}
	return out
}

func requirePass(t *testing.T, r e2e.Result) {
	t.Helper()
	require.Equal(t, e2e.StatusPass, r.Status, "%s: %s", r.Step, r.Why)
}

func requireRed(t *testing.T, r e2e.Result, words ...string) {
	t.Helper()
	require.Equal(t, e2e.StatusRed, r.Status, "%s should be red, why: %s", r.Step, r.Why)
	for _, w := range words {
		require.Contains(t, r.Why, w)
	}
}

// ---- P2.1: the door ----

func TestP21TheGateFailsWithinTheBarAndNamesTheRule(t *testing.T) {
	st := withLocations(fixture(t))
	requirePass(t, runSteps(t, newMemWorld(st), st, nil, "P2.1")["P2.1"])

	cases := map[string]struct {
		q     quirks
		words []string
	}{
		"success on a pull request with no ticket": {quirks{greenOnBadPR: true}, []string{"expected the gate to fail", "success"}},
		"failure with no reason":                   {quirks{failureWithoutWhy: true}, []string{"a summary sentence that names the rule"}},
		"failure after the bar":                    {quirks{slowGate: 20 * time.Second}, []string{"within 15s"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newMemWorld(st)
			w.q = c.q
			requireRed(t, runSteps(t, w, st, nil, "P2.1")["P2.1"], c.words...)
		})
	}
}

func TestP22NoAgentIsWokenAndNoTokenSpentByTheDoor(t *testing.T) {
	st := withLocations(fixture(t))
	requirePass(t, runSteps(t, newMemWorld(st), st, nil, "P2.1", "P2.2")["P2.2"])

	spends := newMemWorld(st)
	spends.q.spendsLate = true
	requireRed(t, runSteps(t, spends, st, nil, "P2.1", "P2.2")["P2.2"], "no token spent", "$")

	wakes := newMemWorld(st)
	wakes.q.wakesLate = true
	requireRed(t, runSteps(t, wakes, st, nil, "P2.1", "P2.2")["P2.2"], "no agent to log a task")
}

// ---- P2.3: a fix moves the check to the new head ----

func TestP23ThePushedFixMovesTheCheckOnTheNewHead(t *testing.T) {
	st := withLocations(fixture(t))
	got := runSteps(t, newMemWorld(st), st, nil, "P2.1", "P2.3")
	requirePass(t, got["P2.1"])
	requirePass(t, got["P2.3"])

	w := newMemWorld(st)
	w.q.noRerun = true
	got = runSteps(t, w, st, nil, "P2.1", "P2.3")
	requirePass(t, got["P2.1"])
	requireRed(t, got["P2.3"], "in_progress", "within 15s")
}

// ---- P5.2 to P5.4: an approval counts only on the head, from someone who is not the author ----

// reviewedPR opens good_pr in the double with its review done and nothing open but the approval.
func reviewedPR(w *memWorld) func(e *e2e.Env) {
	return func(e *e2e.Env) {
		pr, _ := w.OpenPR(context.Background(), w.story.Repo, w.story.GoodPR, w.story.BaseBranch, "KAN-1: Add refunds", "", w.story.Author().GitHub)
		w.pr(pr.Number).reviewed = true
		e.Set(varPR, pr)
		e.Set(varOldHead, "sha-before-the-fix")
	}
}

func TestP52TheApprovalOnTheHeadTurnsTheGateGreenAndUnlocksTheMerge(t *testing.T) {
	st := withLocations(fixture(t))
	w := newMemWorld(st)
	requirePass(t, runSteps(t, w, st, reviewedPR(w), "P5.2")["P5.2"])

	for name, q := range map[string]quirks{
		"green with no approval at all": {greenWithoutAppr: true},
		"never green":                   {neverGreen: true},
	} {
		t.Run(name, func(t *testing.T) {
			bad := newMemWorld(st)
			bad.q = q
			requireRed(t, runSteps(t, bad, st, reviewedPR(bad), "P5.2")["P5.2"])
		})
	}
}

func TestP53AnApprovalOnAnOlderCommitDoesNotCount(t *testing.T) {
	st := withLocations(fixture(t))
	w := newMemWorld(st)
	requirePass(t, runSteps(t, w, st, reviewedPR(w), "P5.3")["P5.3"])

	bad := newMemWorld(st)
	bad.q.countStaleApproval = true
	requireRed(t, runSteps(t, bad, st, reviewedPR(bad), "P5.3")["P5.3"], "older commit", "success")
}

func TestP54TheAuthorsOwnApprovalDoesNotCount(t *testing.T) {
	st := withLocations(fixture(t))
	w := newMemWorld(st)
	requirePass(t, runSteps(t, w, st, reviewedPR(w), "P5.4")["P5.4"])

	refused := newMemWorld(st)
	refused.q.githubRefusesSelf = true // GitHub itself turns the review away; the gate still asks for an approval
	requirePass(t, runSteps(t, refused, st, reviewedPR(refused), "P5.4")["P5.4"])

	bad := newMemWorld(st)
	bad.q.countAuthorAppr = true
	requireRed(t, runSteps(t, bad, st, reviewedPR(bad), "P5.4")["P5.4"], "own approval", "success")
}

// ---- P3.3: the blocking set ----

func findingAt(st *e2e.Story, i int, id string) e2e.Finding {
	p := st.Planted[i]
	return e2e.Finding{
		FindingView: e2e.FindingView{ID: id, File: p.File, Line: p.Lines[0], Tier: p.Tier, Claim: "the claim for " + p.ID, EvidenceRef: "run 7"},
		Blocking:    true, Confirmed: true,
	}
}

func openedPR(w *memWorld) func(e *e2e.Env) {
	return func(e *e2e.Env) {
		pr, _ := w.OpenPR(context.Background(), w.story.Repo, w.story.GoodPR, w.story.BaseBranch, "KAN-1: Add refunds", "", w.story.Author().GitHub)
		e.Set(varPR, pr)
	}
}

func TestP33TheBlockingSetIsTheExpectedSet(t *testing.T) {
	st := withLocations(fixture(t))
	three := func() []e2e.Finding {
		return []e2e.Finding{findingAt(st, 0, "F1"), findingAt(st, 1, "F2"), findingAt(st, 2, "F3")}
	}

	good := newMemWorld(st)
	good.findings = three()
	requirePass(t, runSteps(t, good, st, openedPR(good), "P3.3")["P3.3"])

	missing := newMemWorld(st)
	missing.findings = three()[:2]
	requireRed(t, runSteps(t, missing, st, openedPR(missing), "P3.3")["P3.3"], "refund-read-authz")

	notBlocking := newMemWorld(st)
	notBlocking.findings = three()
	notBlocking.findings[1].Blocking = false
	requireRed(t, runSteps(t, notBlocking, st, openedPR(notBlocking), "P3.3")["P3.3"], "refund-ref-renamed")

	extra := newMemWorld(st)
	reasoning := e2e.Finding{
		FindingView: e2e.FindingView{ID: "F4", File: "internal/refund/service.go", Line: 90, Tier: "E4", Claim: "this looks racy", EvidenceRef: "reviewer note"},
		Blocking:    true,
	}
	extra.findings = append(three(), reasoning)
	requireRed(t, runSteps(t, extra, st, openedPR(extra), "P3.3")["P3.3"], "reasoning only")

	verified := newMemWorld(st)
	reasoning.Confirmed = true
	reasoning.Tier = "E1"
	verified.findings = append(three(), reasoning)
	requirePass(t, runSteps(t, verified, st, openedPR(verified), "P3.3")["P3.3"])
}

// ---- P4.3: who may uphold ----

func disputedFinding(w *memWorld) func(e *e2e.Env) {
	return func(e *e2e.Env) {
		openedPR(w)(e)
		w.findings = []e2e.Finding{findingAt(w.story, 0, "F1")}
		e.Set(varRoom, roomName)
		e.Set(varFinding, "F1")
		e.Capture.MarkAllSubmitted(w.now.Add(-time.Hour))
	}
}

func TestP43OnlyTheApproverMayUphold(t *testing.T) {
	st := withLocations(fixture(t))
	w := newMemWorld(st)
	requirePass(t, runSteps(t, w, st, disputedFinding(w), "P4.3")["P4.3"])
	require.True(t, w.findings[0].Upheld)
	require.Len(t, w.comments, 1)

	byAuthor := newMemWorld(st)
	byAuthor.q.authorCanUphold = true
	requireRed(t, runSteps(t, byAuthor, st, disputedFinding(byAuthor), "P4.3")["P4.3"])

	byAgent := newMemWorld(st)
	byAgent.q.agentCanUphold = true
	requireRed(t, runSteps(t, byAgent, st, disputedFinding(byAgent), "P4.3")["P4.3"], "F1")
}

// ---- P9.1: heimdall connect ----

const goodConnect = `case "$*" in *--token*) echo "connect takes no secrets as arguments" >&2; exit 2;; esac
printf 'Integration  Status     Needs\n'
printf 'github       connected  a token file\n'
printf 'jira         untested   a token file\n'`

func connectWorld(t *testing.T, st *e2e.Story, body string) e2e.World {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "heimdall")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\n"+body+"\n"), 0o700)) // #nosec G306 -- a script this test runs
	return e2e.NewLocalWorld(e2e.LocalConfig{Story: st, Agents: "scripted", HeimdallBinary: bin})
}

func TestP91ConnectPrintsATableAndNeverEchoesASecret(t *testing.T) {
	st := fixture(t)
	requirePass(t, runSteps(t, connectWorld(t, st, goodConnect), st, nil, "P9.1")["P9.1"])

	cases := map[string]struct{ script, word string }{
		"it echoes a secret from the environment": {"echo \"token is $GITHUB_TOKEN\"\n" + goodConnect, "echoed"},
		"it takes a secret as an argument":        {"printf 'Integration  Status  Needs\\ngithub  untested  a token\\n'", "argument"},
		"it prints no table":                      {"echo connected", "table"},
		"it fails":                                {"exit 1", "succeed"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			requireRed(t, runSteps(t, connectWorld(t, st, c.script), st, nil, "P9.1")["P9.1"], c.word)
		})
	}
}
