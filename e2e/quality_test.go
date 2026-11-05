package e2e_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

func TestRawInternalsCatchesWhatAPersonMustNeverSee(t *testing.T) {
	wrong := map[string]string{
		"a prefixed id":        "Finding fnd_01HZX3K9QW7M2P5R8T0V4YB6CD was filed.",
		"a bare ULID":          "Run 01HZX3K9QW7M2P5R8T0V4YB6CD finished.",
		"an event type":        "gate.evaluated with ok=false",
		"another event type":   "Got pr.opened for #7",
		"a state name":         "Change is now IN_REVIEW",
		"a reason code":        "Blocked: consensus_hold",
		"another reason code":  "The gate says tests_changed.",
		"a stack trace":        "panic: runtime error: index out of range\ngoroutine 7 [running]:\nmain.go:42 +0x1d",
		"a go source location": "failed at handler.go:88 +0x4f2",
		"raw JSON":             `Result: {"ok": false, "reasons": []}`,
		"a JSON array":         `[ "a", "b" ]`,
		"the word null":        "The reviewer is null.",
		"a template hole":      "Hello {{ .Reviewer }}, you have a task.",
		"a missing value":      "Assigned to <no value>",
		"a formatting bug":     "Count: %!d(string=three)",
	}
	for name, text := range wrong {
		t.Run(name, func(t *testing.T) {
			require.NotEmpty(t, e2e.RawInternals(text), "should flag: %s", text)
		})
	}
	right := []string{
		"3 failing tests ready: one for each acceptance criterion.",
		"Needs an approval on commit 9f1c2ab.",
		"F1 in internal/refund/handler.go:42 (E1): two refunds of 60 on a payment of 100 both succeed.",
		"See README.md and the pr.md notes; it is merged.",
		"The ticket KAN-12 moved to In Progress, so a room opened.",
		"Reviewer @bob, you have a new task.",
	}
	for _, text := range right {
		t.Run("ok: "+text[:14], func(t *testing.T) {
			require.Empty(t, e2e.RawInternals(text), "should not flag: %s", text)
		})
	}
}

func TestGateProblemsEveryRedSaysWhyAndWhatUnblocksIt(t *testing.T) {
	good := e2e.GateState{State: "failure", Summary: "Not ready to merge.", Reasons: []e2e.Reason{
		{Code: "no_approval", Text: "Needs an approval on commit 9f1c2ab."},
		{Code: "open_findings", Text: "3 findings need a decision before this can merge."},
	}}
	require.Empty(t, e2e.GateProblems(good))
	require.Empty(t, e2e.GateProblems(e2e.GateState{State: "success", Summary: "All clauses hold on 9f1c2ab."}), "a green gate needs no reasons")

	wrong := map[string]e2e.GateState{
		"a red with no reason":             {State: "failure", Summary: "Not ready."},
		"an in_progress with no reason":    {State: "in_progress", Summary: "Working."},
		"a reason that is empty":           {State: "failure", Summary: "No.", Reasons: []e2e.Reason{{Code: "x", Text: ""}}},
		"a reason that only states a fact": {State: "failure", Summary: "No.", Reasons: []e2e.Reason{{Code: "x", Text: "The pull request is large."}}},
		"a red with no summary":            {State: "failure", Reasons: []e2e.Reason{{Code: "x", Text: "Needs an approval."}}},
	}
	for name, g := range wrong {
		t.Run(name, func(t *testing.T) {
			require.NotEmpty(t, e2e.GateProblems(g))
		})
	}
}

func TestFindingProblemsEveryFindingIsCheckable(t *testing.T) {
	good := e2e.FindingView{ID: "F1", File: "internal/refund/handler.go", Line: 42, FileLines: 120, Tier: "E1", Claim: "two refunds of 60 both succeed", EvidenceRef: "run 7"}
	require.Empty(t, e2e.FindingProblems(good))
	wrong := map[string]func(f *e2e.FindingView){
		"a line past the end of the file": func(f *e2e.FindingView) { f.Line = 500 },
		"a file that is not in the tree":  func(f *e2e.FindingView) { f.FileLines = 0 },
		"line zero":                       func(f *e2e.FindingView) { f.Line = 0 },
		"no file":                         func(f *e2e.FindingView) { f.File = "" },
		"no tier":                         func(f *e2e.FindingView) { f.Tier = "" },
		"a made-up tier":                  func(f *e2e.FindingView) { f.Tier = "E7" },
		"no claim":                        func(f *e2e.FindingView) { f.Claim = "" },
		"no evidence reference":           func(f *e2e.FindingView) { f.EvidenceRef = "" },
		"an id that is a ULID":            func(f *e2e.FindingView) { f.ID = "fnd_01HZX3K9QW7M2P5R8T0V4YB6CD" },
	}
	for name, mutate := range wrong {
		t.Run(name, func(t *testing.T) {
			f := good
			mutate(&f)
			require.NotEmpty(t, e2e.FindingProblems(f))
		})
	}
}

func msg(from string, agent bool, kind e2e.MessageKind, text string, mentions ...string) e2e.Message {
	return e2e.Message{Room: "kan-1", From: from, FromAgent: agent, Kind: kind, Text: text, Mentions: mentions}
}

func TestQuietCountsMessagesAgainstTheBudget(t *testing.T) {
	budget := e2e.Budget{Milestones: 2, Questions: 1, Decisions: 0}
	good := []e2e.Message{
		msg("heimdall", true, e2e.KindMilestone, "Room open."),
		msg("test-author", true, e2e.KindQuestion, "Which behaviour for a repeat request?", "dev-author"),
		msg("heimdall", true, e2e.KindMilestone, "3 failing tests ready."),
		msg("dev-author", false, e2e.KindHuman, "answer 409"),
	}
	require.Empty(t, e2e.QuietProblems(good, budget), "human messages are not counted")

	chatty := append([]e2e.Message{}, good...)
	chatty = append(chatty, msg("heimdall", true, e2e.KindMilestone, "Still working."))
	require.NotEmpty(t, e2e.QuietProblems(chatty, budget), "one more milestone than the budget")

	progress := append(append([]e2e.Message{}, good...), msg("bob-reviewer", true, e2e.KindProgress, "Reading file 3 of 9."))
	require.NotEmpty(t, e2e.QuietProblems(progress, budget), "progress chatter is never allowed")

	dup := []e2e.Message{
		msg("heimdall", true, e2e.KindMilestone, "Room open."),
		msg("heimdall", true, e2e.KindMilestone, "Room open."),
	}
	problems := e2e.QuietProblems(dup, e2e.Budget{Milestones: 5})
	require.NotEmpty(t, problems, "a replayed step must not post a duplicate")
	require.Contains(t, strings.Join(problems, " "), "duplicate")
}

func TestAddressingProblemsOnlyQuestionsAndDecisionsMayAddressAPerson(t *testing.T) {
	good := []e2e.Message{
		msg("test-author", true, e2e.KindQuestion, "Which behaviour?", "dev-author"),
		msg("heimdall", true, e2e.KindDecision, "Decision needed on F1.", "dev-approver"),
		msg("heimdall", true, e2e.KindMilestone, "3 failing tests ready."),
		msg("dev-author", false, e2e.KindHuman, "thanks @bob-reviewer", "bob-reviewer"),
	}
	require.Empty(t, e2e.AddressingProblems(good))
	wrong := map[string]e2e.Message{
		"a milestone that pings a person": msg("heimdall", true, e2e.KindMilestone, "Tests ready @dev-author", "dev-author"),
		"a progress note that pings":      msg("bob-reviewer", true, e2e.KindProgress, "Halfway @dev-author", "dev-author"),
	}
	for name, m := range wrong {
		t.Run(name, func(t *testing.T) {
			require.NotEmpty(t, e2e.AddressingProblems([]e2e.Message{m}))
		})
	}
	require.NotEmpty(t, e2e.AddressingProblems([]e2e.Message{msg("heimdall", true, e2e.KindQuestion, "Which?")}),
		"a question that mentions nobody does not reach anyone")
}

func TestReplayProblemsReplayingAPollChangesNothing(t *testing.T) {
	before := e2e.Counts{Messages: map[string]int{"kan-1": 4}, Checks: map[string]int{"o/r#7": 2}}
	require.Empty(t, e2e.ReplayProblems(before, e2e.Counts{Messages: map[string]int{"kan-1": 4}, Checks: map[string]int{"o/r#7": 2}}))
	require.NotEmpty(t, e2e.ReplayProblems(before, e2e.Counts{Messages: map[string]int{"kan-1": 5}, Checks: map[string]int{"o/r#7": 2}}), "a duplicate message")
	require.NotEmpty(t, e2e.ReplayProblems(before, e2e.Counts{Messages: map[string]int{"kan-1": 4}, Checks: map[string]int{"o/r#7": 3}}), "a duplicate check run")
	require.NotEmpty(t, e2e.ReplayProblems(before, e2e.Counts{Messages: map[string]int{"kan-1": 4, "kan-2": 1}, Checks: map[string]int{"o/r#7": 2}}), "a message in a brand new room")
	require.NotEmpty(t, e2e.ReplayProblems(before, e2e.Counts{Messages: map[string]int{"kan-1": 3}, Checks: map[string]int{"o/r#7": 2}}), "a message that disappeared")
}

func TestCostProblemsScriptedStepsMustCostNothing(t *testing.T) {
	require.Empty(t, e2e.CostProblems(true, 0))
	require.Empty(t, e2e.CostProblems(false, 0.04), "live agents are expected to cost something")
	require.NotEmpty(t, e2e.CostProblems(true, 0.0004), "a scripted run that spent tokens woke a real agent")
}

func TestAgentLeakProblems(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)
	canaries := []string{"HEIMDALL-CANARY-4f9a21c7"}
	at := func(m e2e.Message, d time.Duration) e2e.Message { m.At = t0.Add(d); return m }
	allIn := t0.Add(10 * time.Minute)

	good := []e2e.Message{
		at(msg("bob-reviewer", true, e2e.KindProgress, "Review submitted."), 3*time.Minute),
		at(msg("heimdall", true, e2e.KindMilestone, "F1 in refund.go:42 needs a decision."), 11*time.Minute),
	}
	require.Empty(t, e2e.AgentLeakProblems(good, canaries, allIn))

	early := at(msg("eli-reviewer", true, e2e.KindProgress, "I found F2 in refund.go:42, the field was renamed."), 4*time.Minute)
	require.NotEmpty(t, e2e.AgentLeakProblems([]e2e.Message{early}, canaries, allIn), "a reviewer shows a finding before all have submitted")

	canary := at(msg("ana-reviewer", true, e2e.KindProgress, "Token is HEIMDALL-CANARY-4f9a21c7"), 20*time.Minute)
	require.NotEmpty(t, e2e.AgentLeakProblems([]e2e.Message{canary}, canaries, allIn), "an agent post carries a canary")

	human := at(msg("dev-author", false, e2e.KindHuman, "see F1 in refund.go:42"), time.Minute)
	require.Empty(t, e2e.AgentLeakProblems([]e2e.Message{human}, canaries, allIn), "people may talk about findings whenever they like")
}
