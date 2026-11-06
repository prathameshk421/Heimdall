package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agenttools"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/gateuse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/deliberation"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

const reviewJourneyHead2 = id.SHA("a200000000000000000000000000000000000002")

type journeyIndex struct{}

func (journeyIndex) ActiveTask(context.Context, id.AgentName, workflow.Role) (ports.ActiveTaskRef, error) {
	return ports.ActiveTaskRef{}, ports.ErrNotFound
}

type journeyReviewValidator struct{}

func (journeyReviewValidator) Submit(_ context.Context, sub agenttools.ReviewSubmission) (agenttools.ReviewResult, error) {
	out := agenttools.ReviewResult{}
	for _, input := range sub.Review.Findings {
		out.Findings = append(out.Findings, agenttools.ReviewFinding{Input: input, Tier: finding.TierE2})
	}
	return out, nil
}

func TestReviewGateConnectedLifecycleReplaysAndRequiresCurrentHeadReview(t *testing.T) {
	ctx := context.Background()
	cid := id.ChangeID("KAN-41")
	room := ports.RoomBinding{ChangeID: cid, RoomID: "room-1"}
	pr := ports.PR{Repo: "aarush323/PBL_TOS_RISK_ANALYZER", Number: 41, Author: "bob", BaseSHA: commandTestBase, HeadSHA: commandTestHead, State: ports.PRStateOpen}
	runtime, store, _, _ := setupCommandRuntime(t, cid, nil, pr)

	// Advance the seeded change through the minimum valid pre-review lifecycle, then feed the intake result. The
	// separate intake and command tests cover CODEOWNERS/person mapping and human reviewer selection.
	actor := change.Actor{Kind: change.ActorSystem, ID: "heimdall"}
	mkEvent := func(typ change.EventType, payload change.Payload, ref string) change.Event {
		event, eventErr := runtime.event(cid, actor, commands.SourceChat, typ, payload, ref)
		require.NoError(t, eventErr)
		return event
	}
	seed := []change.Event{
		mkEvent(change.EventRoomOpened, change.RoomOpened{RoomID: room.RoomID}, "seed-room"),
		mkEvent(change.EventBranchEnsured, change.BranchEnsured{Repo: pr.Repo, Branch: "KAN-41-tests", BaseSHA: commandTestBase}, "seed-branch"),
		mkEvent(change.EventTaskAssigned, change.TaskAssigned{TaskID: "tsk_01ARZ3NDEKTSV4RRFFQ69G5FAV", Agent: "test-author", Role: workflow.RoleTestAuthor,
			Deadline: time.Now().Add(time.Hour), BundleRef: workflow.BlobRef("blob:sha256:" + strings.Repeat("0", 64))}, "seed-author-task"),
		mkEvent(change.EventTestsSubmitted, change.TestsSubmitted{Refs: []string{"tests/refunds"}, ACMap: map[string][]string{"AC1": {"tests/refunds/refund_test.go"}},
			SHA: commandTestHead, TreeHash: commandTestHead}, "seed-tests-submitted"),
		mkEvent(change.EventTestsVerified, change.TestsVerified{RunIDs: []id.RunID{"run_01ARZ3NDEKTSV4RRFFQ69G5FAV"},
			SHA: commandTestHead, TreeHash: commandTestHead}, "seed-tests-verified"),
		mkEvent(change.EventTestsCommitted, change.TestsCommitted{SHA: commandTestHead, TreeHash: commandTestHead}, "seed-tests-committed"),
	}
	_, err := runtime.engine.Advance(ctx, cid, seed)
	require.NoError(t, err)
	opened, err := runtime.event(cid, change.Actor{Kind: change.ActorGitHub, ID: "github"}, commands.SourceGitHub, change.EventPROpened,
		change.PROpened{Repo: pr.Repo, Number: pr.Number, HeadSHA: pr.HeadSHA, Author: pr.Author, Ticket: cid,
			SuggestedReviewers: []change.ActorID{"ana-reviewer", "eli-reviewer"}}, "pr-opened")
	require.NoError(t, err)
	slop := mkEvent(change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}, "seed-slop")
	pick, err := runtime.event(cid, change.Actor{Kind: change.ActorHuman, ID: "alice"}, commands.SourceChat, change.EventReviewersPicked,
		change.ReviewersPicked{People: []change.ActorID{"ana-reviewer", "eli-reviewer"}, By: "alice", Method: change.PickCommand}, "journey-reviewers")
	require.NoError(t, err)
	_, err = runtime.engine.Advance(ctx, cid, []change.Event{opened, slop, pick})
	require.NoError(t, err)
	events, err := store.Load(ctx, cid)
	require.NoError(t, err)
	picked := lastPickedReviewers(t, events)
	require.Equal(t, []change.ActorID{"ana-reviewer", "eli-reviewer"}, picked.People)
	require.Equal(t, change.PickCommand, picked.Method)
	firstAssignments := reviewerAssignments(t, events, 0)
	require.Len(t, firstAssignments, 2)

	gen := ids.New()
	toolService, err := agenttools.New(agenttools.Config{
		Events: store, Index: journeyIndex{}, Blobs: newMemBlobStore(), Clock: clock.System{}, IDs: gen,
		Asker: &chatAsker{chat: runtime.chat, store: store, ids: gen}, ReviewRefs: reviewRefs{code: dummyVCS{}},
		ReviewValidator: journeyReviewValidator{},
	})
	require.NoError(t, err)
	submitJourneyReviews(ctx, t, runtime, toolService, cid, pr, firstAssignments, pr.HeadSHA, 0)

	// The human challenge changes F1 to disputed, opens a durable case, and the resolver fan-out scopes a task
	// to that CaseID. A resolver verdict is followed by the policy-authorized human uphold.
	challenge := ports.Message{ID: "journey-challenge", Room: room.RoomID, Sender: "bob", Text: "heimdall challenge F1 a failing regression test proves this", At: time.Now().UTC()}
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, challenge))
	events, err = store.Load(ctx, cid)
	require.NoError(t, err)
	resolverAssignment := resolverAssignmentForCase(t, events)
	require.NotEmpty(t, resolverAssignment.TaskID)
	require.NotEmpty(t, resolverAssignment.CaseID)
	resolver := agenttools.Assignment{ChangeID: cid, TaskID: resolverAssignment.TaskID, Agent: resolverAssignment.Agent, Role: workflow.RoleResolver,
		Task: workflow.TaskEnvelope{TaskID: resolverAssignment.TaskID, Role: workflow.RoleResolver, CaseID: resolverAssignment.CaseID}}
	verdict := deliberation.Verdict{CaseID: resolverAssignment.CaseID, Decision: deliberation.DecisionUphold,
		Rationale: "The regression confirms the vulnerable path.", Confidence: finding.ConfidenceHigh,
		Citations: []deliberation.Citation{{Kind: deliberation.CitationFinding, Ref: "F1", Quote: "the failing regression reproduces the issue"}}}
	require.NoError(t, toolService.SubmitVerdict(ctx, resolver, verdict, false))
	_, err = runtime.engine.Advance(ctx, cid, nil)
	require.NoError(t, err)
	uphold := ports.Message{ID: "journey-uphold", Room: room.RoomID, Sender: "alice", Text: "heimdall uphold F1", At: time.Now().UTC()}
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, uphold))

	// Exercise the other human outcome too: an independent challenge can be dismissed, which removes that finding
	// from the blocking fold while preserving the decision trail.
	dismissChallenge := ports.Message{ID: "journey-dismiss-challenge", Room: room.RoomID, Sender: "bob", Text: "heimdall challenge F2 a separate test disputes this finding", At: time.Now().UTC()}
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, dismissChallenge))
	events, err = store.Load(ctx, cid)
	require.NoError(t, err)
	dismissAssignment := resolverAssignmentForCase(t, events)
	dismissResolver := agenttools.Assignment{ChangeID: cid, TaskID: dismissAssignment.TaskID, Agent: dismissAssignment.Agent, Role: workflow.RoleResolver,
		Task: workflow.TaskEnvelope{TaskID: dismissAssignment.TaskID, Role: workflow.RoleResolver, CaseID: dismissAssignment.CaseID}}
	dismissVerdict := deliberation.Verdict{CaseID: dismissAssignment.CaseID, Decision: deliberation.DecisionDismiss,
		Rationale: "The independent test does not support this report.", Confidence: finding.ConfidenceHigh,
		Citations: []deliberation.Citation{{Kind: deliberation.CitationFinding, Ref: "F2", Quote: "the independent test contradicts this claim"}}}
	require.NoError(t, toolService.SubmitVerdict(ctx, dismissResolver, dismissVerdict, false))
	_, err = runtime.engine.Advance(ctx, cid, nil)
	require.NoError(t, err)
	dismiss := ports.Message{ID: "journey-dismiss", Room: room.RoomID, Sender: "alice", Text: "heimdall dismiss F2 the independent test contradicts this claim", At: time.Now().UTC()}
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, dismiss))

	events, err = store.Load(ctx, cid)
	require.NoError(t, err)
	factsBeforePush, err := gateuse.Fold(events)
	require.NoError(t, err)
	require.Equal(t, pr.HeadSHA, factsBeforePush.HeadSHA)
	require.Empty(t, factsBeforePush.HumanPending, "the human decision closes the challenged case")
	require.Len(t, factsBeforePush.Blocking, 1, "the dismissed finding no longer blocks; the upheld finding remains open")
	require.Len(t, factsBeforePush.ReviewRequests, 2)
	requireReviewerCoverage(t, factsBeforePush, true)

	// A new head invalidates the existing review coverage. The engine starts a new reviewer cycle, and submissions
	// from that cycle are the only ones that can satisfy the current-head review clause.
	pushed, err := runtime.event(cid, change.Actor{Kind: change.ActorGitHub, ID: "github"}, commands.SourceGitHub, change.EventPRPushed,
		change.PRPushed{Repo: pr.Repo, Number: pr.Number, HeadSHA: reviewJourneyHead2}, "pr-pushed-2")
	require.NoError(t, err)
	newHeadSlop := mkEvent(change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}, "new-head-slop")
	newHeadPick, err := runtime.event(cid, change.Actor{Kind: change.ActorHuman, ID: "alice"}, commands.SourceChat, change.EventReviewersPicked,
		change.ReviewersPicked{People: []change.ActorID{"ana-reviewer", "eli-reviewer"}, By: "alice", Method: change.PickCommand}, "journey-reviewers-new-head")
	require.NoError(t, err)
	_, err = runtime.engine.Advance(ctx, cid, []change.Event{pushed, newHeadSlop, newHeadPick})
	require.NoError(t, err)
	events, err = store.Load(ctx, cid)
	require.NoError(t, err)
	factsAfterPush, err := gateuse.Fold(events)
	require.NoError(t, err)
	require.Equal(t, reviewJourneyHead2, factsAfterPush.HeadSHA)
	requireReviewerCoverage(t, factsAfterPush, false)
	require.Contains(t, unmetClauses(gate.Evaluate(factsAfterPush)), gate.ClauseReviews,
		"the gate must reject old-SHA reviews after a push")

	newAssignments := reviewerAssignmentsAfterLatestPush(t, events)
	require.Len(t, newAssignments, 2, "each selected reviewer receives a fresh task for the new head")
	submitJourneyReviews(ctx, t, runtime, toolService, cid, pr, newAssignments, reviewJourneyHead2, 10)
	events, err = store.Load(ctx, cid)
	require.NoError(t, err)
	finalFacts, err := gateuse.Fold(events)
	require.NoError(t, err)
	require.Equal(t, reviewJourneyHead2, finalFacts.HeadSHA)
	requireReviewerCoverage(t, finalFacts, true)
	require.NotContains(t, unmetClauses(gate.Evaluate(finalFacts)), gate.ClauseReviews,
		"the gate review clause passes once all selected reviewers submit on the current SHA")
	// Replaying the same room commands must not duplicate decisions, cases, or responses.
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, challenge))
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, uphold))
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, dismissChallenge))
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, dismiss))
	replayed, err := store.Load(ctx, cid)
	require.NoError(t, err)
	require.Len(t, replayed, len(events), "command source references make replay idempotent")
}

func lastPickedReviewers(t *testing.T, events []change.Event) change.ReviewersPicked {
	t.Helper()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == change.EventReviewersPicked {
			picked, ok := mustPayload[change.ReviewersPicked](events[i])
			require.True(t, ok)
			return picked
		}
	}
	t.Fatal("reviewers.picked was not recorded")
	return change.ReviewersPicked{}
}

func resolverAssignmentForCase(t *testing.T, events []change.Event) change.TaskAssigned {
	t.Helper()
	var latest change.TaskAssigned
	for _, event := range events {
		if event.Type != change.EventTaskAssigned {
			continue
		}
		assignment, ok := mustPayload[change.TaskAssigned](event)
		if ok && assignment.Role == workflow.RoleResolver {
			latest = assignment
		}
	}
	require.NotEmpty(t, latest.TaskID, "resolver task was not assigned to the challenge case")
	return latest
}

func reviewerAssignmentsAfterLatestPush(t *testing.T, events []change.Event) []change.TaskAssigned {
	t.Helper()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == change.EventPRPushed {
			return reviewerAssignments(t, events, i+1)
		}
	}
	t.Fatal("pr.pushed was not recorded")
	return nil
}

func submitJourneyReviews(ctx context.Context, t *testing.T, runtime *commandRuntime, service *agenttools.Service, cid id.ChangeID,
	pr ports.PR, assignments []change.TaskAssigned, head id.SHA, variantOffset int) {
	t.Helper()
	for i, assigned := range assignments {
		assignment := journeyAssignment(cid, pr, assigned, head)
		_, err := service.SubmitReview(ctx, assignment, journeyReview(head, i+variantOffset), false)
		require.NoError(t, err)
		_, err = runtime.engine.Advance(ctx, cid, nil)
		require.NoError(t, err)
	}
}

func requireReviewerCoverage(t *testing.T, facts gate.Facts, covered bool) {
	t.Helper()
	require.Len(t, facts.ReviewRequests, 2)
	for _, request := range facts.ReviewRequests {
		require.Equal(t, covered, request.Covered)
	}
}

func unmetClauses(decision gate.Decision) []gate.ClauseID {
	clauses := make([]gate.ClauseID, len(decision.Unmet))
	for i, unmet := range decision.Unmet {
		clauses[i] = unmet.ID
	}
	return clauses
}

func reviewerAssignments(t *testing.T, events []change.Event, from int) []change.TaskAssigned {
	t.Helper()
	var assignments []change.TaskAssigned
	for _, event := range events[from:] {
		if event.Type != change.EventTaskAssigned {
			continue
		}
		payload, ok := mustPayload[change.TaskAssigned](event)
		if ok && payload.Role == workflow.RoleReviewer {
			assignments = append(assignments, payload)
		}
	}
	return assignments
}

func journeyAssignment(cid id.ChangeID, pr ports.PR, assigned change.TaskAssigned, head id.SHA) agenttools.Assignment {
	return agenttools.Assignment{ChangeID: cid, TaskID: assigned.TaskID, Agent: assigned.Agent, Role: assigned.Role,
		Task: workflow.TaskEnvelope{TaskID: assigned.TaskID, Role: assigned.Role,
			Change: workflow.ChangeRef{ID: cid, Repo: string(pr.Repo), BaseSHA: pr.BaseSHA, HeadSHA: head},
			Inputs: workflow.Inputs{FilesChanged: []string{"internal/refunds/refunds.go"}}}}
}

func journeyReview(head id.SHA, variant int) finding.Review {
	reason := "review evidence supports the reported risk"
	return finding.Review{HeadSHA: head, FilesReviewed: []string{"internal/refunds/refunds.go"}, Findings: []finding.Input{{
		Claim: "The refund limit can be bypassed by retrying the request " + string(rune('A'+variant)) + ".", Category: finding.CategorySecurity,
		Severity: finding.SeverityHigh, Confidence: finding.ConfidenceHigh,
		Location: finding.Location{File: "internal/refunds/refunds.go", Lines: [2]int{40, 41}},
		Evidence: []finding.Evidence{{Type: finding.EvidenceReasoning, Reasoning: &reason}},
	}}}
}
