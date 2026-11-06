package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agenttools"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/gateuse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/intake"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/deliberation"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const reviewJourneyHead2 = id.SHA("a200000000000000000000000000000000000002")

type reviewJourneyIndex struct{}

func (reviewJourneyIndex) ActiveTask(context.Context, id.AgentName, workflow.Role) (ports.ActiveTaskRef, error) {
	return ports.ActiveTaskRef{}, ports.ErrNotFound
}

type reviewJourneyValidator struct{}

func (reviewJourneyValidator) Submit(_ context.Context, submission agenttools.ReviewSubmission) (agenttools.ReviewResult, error) {
	var result agenttools.ReviewResult
	for _, input := range submission.Review.Findings {
		// An E2 claim carries the tool result the server repeats to verify it (A2).
		input.Evidence = []finding.Evidence{{Type: finding.EvidenceToolResult, ToolResult: &finding.ToolResult{Tool: "go vet", Location: input.Location}}}
		result.Findings = append(result.Findings, agenttools.ReviewFinding{Input: input, Tier: finding.TierE2})
	}
	return result, nil
}

type reviewJourneyGateSource struct{}

func (reviewJourneyGateSource) Outside(context.Context, gateuse.Request) (gateuse.Outside, error) {
	pol := policy.Policy{
		Version: 1, Workflow: "default",
		Blocking: policy.Blocking{MinTier: finding.TierE1, MinSeverity: finding.SeverityHigh},
		Consensus: policy.Consensus{MinHighReporters: 2, BlockScore: 100,
			Confidence: policy.Confidence{High: 1, Medium: .5, Low: .2}},
	}
	return gateuse.Outside{
		HeadTestTreeHash: string(commandTestHead), TestDiffKnown: true,
		PolicyKnown: true, PolicyAtBase: pol, PolicyAtHead: pol,
	}, nil
}

type reviewJourneyProvider struct {
	pr         ports.PR
	files      []ports.ChangedFile
	policy     []byte
	codeowners []byte
}

func (p reviewJourneyProvider) PR(context.Context, change.Repo, int) (ports.PR, error) {
	return p.pr, nil
}
func (p reviewJourneyProvider) Files(context.Context, change.Repo, int) ([]ports.ChangedFile, error) {
	return p.files, nil
}
func (p reviewJourneyProvider) OpenPRs(context.Context, change.Repo) ([]ports.PR, error) {
	return []ports.PR{p.pr}, nil
}
func (p reviewJourneyProvider) ReadFile(_ context.Context, _ change.Repo, _ id.SHA, name string) ([]byte, error) {
	switch name {
	case "heimdall.yaml":
		return p.policy, nil
	case ".github/CODEOWNERS":
		return p.codeowners, nil
	case "internal/refunds/refunds.go":
		return []byte(strings.Repeat("source line\n", 60)), nil
	default:
		return nil, ports.ErrNotFound
	}
}
func (reviewJourneyProvider) TreeHash(context.Context, change.Repo, id.SHA, string) (id.SHA, error) {
	return commandTestHead, nil
}
func (reviewJourneyProvider) Tarball(context.Context, change.Repo, id.SHA) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func TestReviewGateConnectedLifecycleAndCurrentHeadConsensus(t *testing.T) {
	ctx := context.Background()
	cid := id.ChangeID("KAN-41")
	room := ports.RoomBinding{ChangeID: cid, RoomID: "room-1"}
	pr := ports.PR{Repo: "aarush323/PBL_TOS_RISK_ANALYZER", Number: 41, Author: "bob", BaseSHA: commandTestBase, HeadSHA: commandTestHead, State: ports.PRStateOpen}
	runtime, store, _, _ := setupCommandRuntime(t, cid, nil, pr)
	actor := change.Actor{Kind: change.ActorSystem, ID: "heimdall"}
	makeEvent := func(kind change.EventType, payload change.Payload, ref string) change.Event {
		event, err := runtime.event(cid, actor, commands.SourceChat, kind, payload, ref)
		require.NoError(t, err)
		return event
	}

	// Drive the seeded ticket through the product's real reducer states. The actual intake app reads CODEOWNERS at the
	// PR base, persists mapped suggestions, and the real picker timeout consumes them before fan-out.
	seed := []change.Event{
		makeEvent(change.EventRoomOpened, change.RoomOpened{RoomID: room.RoomID}, "review-seed-room"),
		makeEvent(change.EventBranchEnsured, change.BranchEnsured{Repo: pr.Repo, Branch: "KAN-41-tests", BaseSHA: commandTestBase}, "review-seed-branch"),
		makeEvent(change.EventTaskAssigned, change.TaskAssigned{TaskID: "tsk_01ARZ3NDEKTSV4RRFFQ69G5FAV", Agent: "test-author", Role: workflow.RoleTestAuthor,
			Deadline: time.Now().Add(time.Hour), BundleRef: workflow.BlobRef("blob:sha256:" + strings.Repeat("0", 64))}, "review-seed-author"),
		makeEvent(change.EventTestsSubmitted, change.TestsSubmitted{Refs: []string{"tests/refunds"}, ACMap: map[string][]string{"AC1": {"tests/refunds/refund_test.go"}},
			SHA: commandTestHead, TreeHash: commandTestHead}, "review-seed-tests"),
		makeEvent(change.EventTestsVerified, change.TestsVerified{RunIDs: []id.RunID{"run_01ARZ3NDEKTSV4RRFFQ69G5FAV"},
			SHA: commandTestHead, TreeHash: commandTestHead}, "review-seed-verified"),
		makeEvent(change.EventTestsCommitted, change.TestsCommitted{SHA: commandTestHead, TreeHash: commandTestHead}, "review-seed-committed"),
	}
	_, err := runtime.engine.Advance(ctx, cid, seed)
	require.NoError(t, err)
	policyBytes, err := os.ReadFile("../../internal/domain/policy/testdata/policy.golden.yaml")
	require.NoError(t, err)
	provider := reviewJourneyProvider{
		pr: ports.PR{Repo: pr.Repo, Number: pr.Number, Title: "KAN-41 refund safety", Body: "## Justification\n\nImplementation for the requested ticket.",
			Branch: "feature/KAN-41-refund", Author: pr.Author, BaseSHA: pr.BaseSHA, HeadSHA: pr.HeadSHA, State: ports.PRStateOpen},
		files: []ports.ChangedFile{
			{Path: "internal/refunds/refunds.go", Status: ports.FileModified, Additions: 12, Deletions: 4},
			{Path: "tests/heimdall/KAN-41/refund_test.go", Status: ports.FileAdded, Additions: 18},
		},
		policy: policyBytes, codeowners: []byte("internal/refunds/** @ana @eli\n"),
	}
	decodePolicy := func(raw []byte) (policy.Policy, error) {
		var parsed policy.Policy
		decoder := yaml.NewDecoder(bytes.NewReader(raw))
		decoder.KnownFields(true)
		if err := decoder.Decode(&parsed); err != nil {
			return policy.Policy{}, err
		}
		return parsed, nil
	}
	intakeService, err := intake.New(intake.Config{Events: store, PRs: provider, Code: provider, Clock: clock.System{}, IDs: ids.New(), Policy: decodePolicy})
	require.NoError(t, err)
	runtime.intake = prIntake{in: intakeService}
	intakeResult, err := intakeService.HandlePR(ctx, pr.Repo, pr.Number)
	require.NoError(t, err)
	require.Equal(t, cid, intakeResult.Stream)
	require.True(t, intakeResult.Pass)
	_, err = runtime.engine.Advance(ctx, cid, nil)
	require.NoError(t, err)
	_, err = runtime.engine.Timeout(ctx, cid, "pick")
	require.NoError(t, err)

	events, err := store.Load(ctx, cid)
	require.NoError(t, err)
	assignments := reviewerAssignments(t, events, 0)
	require.Len(t, assignments, 2, "one task per mapped reviewer")
	require.Equal(t, []id.AgentName{"ana-reviewer", "eli-reviewer"}, []id.AgentName{assignments[0].Agent, assignments[1].Agent})

	gen := ids.New()
	reviewService, err := agenttools.New(agenttools.Config{
		Events: store, Index: reviewJourneyIndex{}, Blobs: newMemBlobStore(), Clock: clock.System{}, IDs: gen,
		Asker: &chatAsker{chat: runtime.chat, store: store, ids: gen}, ReviewRefs: reviewRefs{code: commandTestVCS{dummyVCS: dummyVCS{}}},
		ReviewValidator: reviewJourneyValidator{},
		Advance: func(ctx context.Context, cid id.ChangeID, evs []change.Event) error {
			_, err := runtime.engine.Advance(ctx, cid, evs)
			return err
		},
	})
	require.NoError(t, err)
	gateEvaluator, err := gateuse.New(gateuse.Config{Events: store, Source: reviewJourneyGateSource{}, Clock: clock.System{}, IDs: gen})
	require.NoError(t, err)

	// A single high-confidence report stays below the base policy threshold. The second independent report shares its
	// fingerprint and must create a consensus hold even though each report is below the normal tier threshold.
	submitJourneyReview(ctx, t, runtime, reviewService, cid, pr, assignments[0], pr.HeadSHA)
	oneReport, err := gateEvaluator.Evaluate(ctx, cid)
	require.NoError(t, err)
	require.False(t, hasReasonPrefix(oneReport.Reasons, "blocking:"))
	submitJourneyReview(ctx, t, runtime, reviewService, cid, pr, assignments[1], pr.HeadSHA)
	twoReports, err := gateEvaluator.Evaluate(ctx, cid)
	require.NoError(t, err)
	require.True(t, hasReasonPrefix(twoReports.Reasons, "blocking:"), "two high-confidence reviewers independently report the same fingerprint")

	// A challenge opens a durable, case-scoped resolver task; resolver evidence is followed by the human decision.
	challenge := ports.Message{ID: "review-challenge-f1", Room: room.RoomID, Sender: "bob", Text: "heimdall challenge F1 a failing regression test proves this", At: time.Now().UTC()}
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, challenge))
	events, err = store.Load(ctx, cid)
	require.NoError(t, err)
	resolverTask := resolverAssignmentForJourney(t, events)
	resolver := agenttools.Assignment{ChangeID: cid, TaskID: resolverTask.TaskID, Agent: resolverTask.Agent, Role: workflow.RoleResolver,
		Task: workflow.TaskEnvelope{TaskID: resolverTask.TaskID, Role: workflow.RoleResolver, CaseID: resolverTask.CaseID}}
	verdict := deliberation.Verdict{CaseID: resolverTask.CaseID, Decision: deliberation.DecisionUphold,
		Rationale: "The regression confirms the reported path.", Confidence: finding.ConfidenceHigh,
		Citations: []deliberation.Citation{{Kind: deliberation.CitationFinding, Ref: "F1", Quote: "the regression reproduces the report"}}}
	require.NoError(t, reviewService.SubmitVerdict(ctx, resolver, verdict, false))
	_, err = runtime.engine.Advance(ctx, cid, nil)
	require.NoError(t, err)
	uphold := ports.Message{ID: "review-uphold-f1", Room: room.RoomID, Sender: "alice", Text: "heimdall uphold F1", At: time.Now().UTC()}
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, uphold))

	// Dismiss the other report through a second fully assigned case. The remaining upheld report is no longer a
	// two-reviewer consensus, so the consensus-created blocking hold is released.
	dismissChallenge := ports.Message{ID: "review-challenge-f2", Room: room.RoomID, Sender: "bob", Text: "heimdall challenge F2 a separate test disputes this finding", At: time.Now().UTC()}
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, dismissChallenge))
	events, err = store.Load(ctx, cid)
	require.NoError(t, err)
	dismissTask := resolverAssignmentForJourney(t, events)
	dismissResolver := agenttools.Assignment{ChangeID: cid, TaskID: dismissTask.TaskID, Agent: dismissTask.Agent, Role: workflow.RoleResolver,
		Task: workflow.TaskEnvelope{TaskID: dismissTask.TaskID, Role: workflow.RoleResolver, CaseID: dismissTask.CaseID}}
	dismissVerdict := deliberation.Verdict{CaseID: dismissTask.CaseID, Decision: deliberation.DecisionDismiss,
		Rationale: "The independent test contradicts this report.", Confidence: finding.ConfidenceHigh,
		Citations: []deliberation.Citation{{Kind: deliberation.CitationFinding, Ref: "F2", Quote: "the independent test contradicts the claim"}}}
	require.NoError(t, reviewService.SubmitVerdict(ctx, dismissResolver, dismissVerdict, false))
	_, err = runtime.engine.Advance(ctx, cid, nil)
	require.NoError(t, err)
	dismiss := ports.Message{ID: "review-dismiss-f2", Room: room.RoomID, Sender: "alice", Text: "heimdall dismiss F2 the independent test contradicts this claim", At: time.Now().UTC()}
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, dismiss))
	oneSurvivor, err := gateEvaluator.Evaluate(ctx, cid)
	require.NoError(t, err)
	require.False(t, hasReasonPrefix(oneSurvivor.Reasons, "blocking:"), "dismissal removes that reporter from consensus")

	// A new SHA invalidates old review coverage and starts a fresh selection/fan-out cycle.
	newHead, err := runtime.event(cid, change.Actor{Kind: change.ActorGitHub, ID: "github"}, commands.SourceGitHub, change.EventPRPushed,
		change.PRPushed{Repo: pr.Repo, Number: pr.Number, HeadSHA: reviewJourneyHead2}, "review-pr-pushed")
	require.NoError(t, err)
	newSlop := makeEvent(change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}, "review-new-slop")
	newPick := makeEvent(change.EventReviewersPicked, change.ReviewersPicked{
		People: []change.ActorID{"ana-reviewer", "eli-reviewer"}, By: "alice", Method: change.PickCommand,
	}, "review-new-pick")
	_, err = runtime.engine.Advance(ctx, cid, []change.Event{newHead, newSlop, newPick})
	require.NoError(t, err)
	events, err = store.Load(ctx, cid)
	require.NoError(t, err)
	facts, err := gateuse.Fold(events)
	require.NoError(t, err)
	require.Equal(t, reviewJourneyHead2, facts.HeadSHA)
	requireReviewCoverage(t, facts, false)
	stale, err := gateEvaluator.Evaluate(ctx, cid)
	require.NoError(t, err)
	require.True(t, hasReasonPrefix(stale.Reasons, "reviews:"), "the gate rejects review coverage from the previous SHA")

	newAssignments := reviewerAssignmentsAfterLatestPush(t, events)
	require.Len(t, newAssignments, 2)
	for _, assignment := range newAssignments {
		submitJourneyReview(ctx, t, runtime, reviewService, cid, pr, assignment, reviewJourneyHead2)
	}
	current, err := gateEvaluator.Evaluate(ctx, cid)
	require.NoError(t, err)
	require.False(t, hasReasonPrefix(current.Reasons, "reviews:"), "fresh submissions cover the current head")
	require.True(t, hasReasonPrefix(current.Reasons, "blocking:"), "new current-head reports restore the independent consensus hold")

	// Replaying the command source IDs must not add duplicate cases or decisions.
	beforeReplay, err := store.Load(ctx, cid)
	require.NoError(t, err)
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, challenge))
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, uphold))
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, dismissChallenge))
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, dismiss))
	afterReplay, err := store.Load(ctx, cid)
	require.NoError(t, err)
	require.Len(t, afterReplay, len(beforeReplay))
}

func submitJourneyReview(ctx context.Context, t *testing.T, runtime *commandRuntime, service *agenttools.Service, cid id.ChangeID,
	pr ports.PR, assigned change.TaskAssigned, head id.SHA) {
	t.Helper()
	reason := "source evidence supports this independent security report"
	review := finding.Review{HeadSHA: head, FilesReviewed: []string{"internal/refunds/refunds.go"}, Findings: []finding.Input{{
		Claim: "The refund limit can be bypassed by retrying the request.", Category: finding.CategorySecurity,
		Severity: finding.SeverityHigh, Confidence: finding.ConfidenceHigh,
		Location: finding.Location{File: "internal/refunds/refunds.go", Lines: [2]int{40, 41}},
		Evidence: []finding.Evidence{{Type: finding.EvidenceReasoning, Reasoning: &reason}},
	}}}
	assignment := agenttools.Assignment{ChangeID: cid, TaskID: assigned.TaskID, Agent: assigned.Agent, Role: assigned.Role,
		Task: workflow.TaskEnvelope{TaskID: assigned.TaskID, Role: assigned.Role,
			Change: workflow.ChangeRef{ID: cid, Repo: string(pr.Repo), BaseSHA: pr.BaseSHA, HeadSHA: head},
			Inputs: workflow.Inputs{FilesChanged: []string{"internal/refunds/refunds.go"}}}}
	_, err := service.SubmitReview(ctx, assignment, review, false)
	require.NoError(t, err)
	runJourneyVerification(ctx, t, runtime, cid)
	_, err = runtime.engine.Advance(ctx, cid, nil)
	require.NoError(t, err)
}

type journeySource struct{}

func (journeySource) Fetch(context.Context, change.Repo, id.SHA) error { return nil }

type journeySandbox struct{}

// Run reports the located vet failure the E2 findings of the journey cite (internal/refunds/refunds.go, lines 40 to 41).
func (journeySandbox) Run(context.Context, ports.SandboxJob) (ports.SandboxResult, error) {
	return ports.SandboxResult{Failed: 1, Output: "./internal/refunds/refunds.go:40:2: unchecked error\n"}, nil
}

// runJourneyVerification runs the verify_evidence effects the engine committed, through the real executor with a
// sandbox that confirms the cited failure. The executor skips a finding it already evaluated for the current head, so
// running every committed effect again is safe.
func runJourneyVerification(ctx context.Context, t *testing.T, runtime *commandRuntime, cid id.ChangeID) {
	t.Helper()
	mem, ok := runtime.events.(*memStore)
	require.True(t, ok)
	exec, err := effects.NewVerifyEvidence(effects.VerifyEvidenceDeps{
		Source: journeySource{}, Sandbox: journeySandbox{}, Store: mem, Ingest: ingestor{eng: runtime.engine}, IDs: ids.New(), Clock: clock.System{},
	})
	require.NoError(t, err)
	mem.mu.Lock()
	pending := append([]change.Effect(nil), mem.fx...)
	mem.mu.Unlock()
	for _, fx := range pending {
		if fx.Kind == change.EffectVerifyEvidence && fx.Target == string(cid) {
			require.NoError(t, exec.Execute(ctx, fx))
		}
	}
}

func resolverAssignmentForJourney(t *testing.T, events []change.Event) change.TaskAssigned {
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
	require.NotEmpty(t, latest.TaskID)
	require.NotEmpty(t, latest.CaseID)
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

func reviewerAssignments(t *testing.T, events []change.Event, from int) []change.TaskAssigned {
	t.Helper()
	var assignments []change.TaskAssigned
	for _, event := range events[from:] {
		if event.Type != change.EventTaskAssigned {
			continue
		}
		assignment, ok := mustPayload[change.TaskAssigned](event)
		if ok && assignment.Role == workflow.RoleReviewer {
			assignments = append(assignments, assignment)
		}
	}
	return assignments
}

func requireReviewCoverage(t *testing.T, facts gate.Facts, covered bool) {
	t.Helper()
	require.Len(t, facts.ReviewRequests, 2)
	for _, request := range facts.ReviewRequests {
		require.Equal(t, covered, request.Covered)
	}
}

func hasReasonPrefix(reasons []string, prefix string) bool {
	for _, reason := range reasons {
		if strings.HasPrefix(reason, prefix) {
			return true
		}
	}
	return false
}
