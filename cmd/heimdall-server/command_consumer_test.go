package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/intake"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
	"github.com/stretchr/testify/require"
)

const commandTestBase = id.SHA("b100000000000000000000000000000000000001")
const commandTestHead = id.SHA("a100000000000000000000000000000000000001")

type commandTestVCS struct {
	dummyVCS
	policy []byte
	pr     ports.PR
}

func (v commandTestVCS) ReadFile(context.Context, change.Repo, id.SHA, string) ([]byte, error) {
	return v.policy, nil
}
func (v commandTestVCS) DefaultHead(context.Context, change.Repo) (id.SHA, error) {
	return commandTestBase, nil
}
func (v commandTestVCS) PR(context.Context, change.Repo, int) (ports.PR, error) { return v.pr, nil }

type commandTestChat struct{ posts []string }

func (commandTestChat) OpenRoom(context.Context, ports.RoomRequest) (change.RoomID, error) {
	return "", nil
}
func (f *commandTestChat) Post(_ context.Context, _ change.RoomID, _ change.EffectKey, body string) error {
	f.posts = append(f.posts, body)
	return nil
}
func (commandTestChat) Read(context.Context, change.RoomID, ports.Cursor) ([]ports.Message, ports.Cursor, error) {
	return nil, "", nil
}

type commandTestComments struct{ bodies []string }

func (f *commandTestComments) Comment(_ context.Context, _ change.Repo, _ int, _ change.EffectKey, body string) error {
	f.bodies = append(f.bodies, body)
	return nil
}

type commandTestTickets struct{ dummyTickets }

func (commandTestTickets) Ticket(_ context.Context, key id.ChangeID) (ports.Ticket, error) {
	return ports.Ticket{Key: key, Title: "Manual start", Assignee: "jira-assignee", Status: "To Do"}, nil
}

type commandTestIntake struct{ stream id.ChangeID }

func (f commandTestIntake) HandlePR(context.Context, change.Repo, int) (intake.Result, error) {
	return intake.Result{Stream: f.stream}, nil
}

func setupCommandRuntime(t *testing.T, cid id.ChangeID, tickets ports.TicketReader, pr ports.PR) (*commandRuntime, *memStore, *commandTestChat, *commandTestComments) {
	t.Helper()
	store := newMemStore()
	gen := ids.New()
	eng := newSliceEngine(t, store)
	first := triggeredEvent(t, string(cid))
	first.ChangeID = cid
	first.Payload, _ = json.Marshal(change.Triggered{Ticket: cid, Repo: "aarush323/PBL_TOS_RISK_ANALYZER", Assignee: "jira-assignee", Source: change.TriggerJira})
	require.NoError(t, first.Validate())
	_, err := eng.Start(context.Background(), first)
	require.NoError(t, err)
	policyBytes, err := os.ReadFile("../../internal/domain/policy/testdata/policy.golden.yaml")
	require.NoError(t, err)
	vcs := commandTestVCS{policy: policyBytes, pr: pr}
	chat, comments := &commandTestChat{}, &commandTestComments{}
	if tickets == nil {
		tickets = commandTestTickets{}
	}
	ad := adapters{tickets: tickets, vcs: vcs, chat: chat, projects: projectRepos{"KAN": "aarush323/PBL_TOS_RISK_ANALYZER"}}
	runtime, err := newCommandRuntime(store, ad, eng, gen, clock.System{}, nil, nil)
	require.NoError(t, err)
	runtime.comments = comments
	return runtime, store, chat, comments
}

type commandProfileFake struct{ approvals, promotions []string }

func (f *commandProfileFake) Propose(context.Context, mcp.Principal, string, []byte) (ports.ProfileProposal, bool, error) {
	return ports.ProfileProposal{}, false, nil
}
func (f *commandProfileFake) Approve(_ context.Context, p mcp.Principal, _ string) error {
	f.approvals = append(f.approvals, p.Person)
	return nil
}
func (f *commandProfileFake) Trial(context.Context, mcp.Principal, string, id.SHA) (ports.ProfileTrial, error) {
	return ports.ProfileTrial{}, nil
}
func (f *commandProfileFake) Promote(_ context.Context, p mcp.Principal, _ string) error {
	f.promotions = append(f.promotions, p.Person)
	return nil
}

func TestProfileHumanCommandsRequireMappedPolicyOwnerAndUseMappedIdentity(t *testing.T) {
	cid := id.ChangeID("KAN-42")
	runtime, _, chat, _ := setupCommandRuntime(t, cid, nil, ports.PR{})
	profiles := &commandProfileFake{}
	runtime.profiles = profiles
	room := ports.RoomBinding{ChangeID: cid, RoomID: "profile-room"}
	proposalID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	denied := ports.Message{ID: "profile-denied", Room: room.RoomID, Sender: "bob", Text: "heimdall approve-profile " + proposalID}
	require.NoError(t, runtime.processor.HandleRoomMessage(context.Background(), room, denied))
	require.Empty(t, profiles.approvals)
	require.Contains(t, chat.posts[len(chat.posts)-1], "not authorized")

	approved := ports.Message{ID: "profile-approved", Room: room.RoomID, Sender: "ana", Text: "heimdall approve-profile " + proposalID}
	require.NoError(t, runtime.processor.HandleRoomMessage(context.Background(), room, approved))
	require.Equal(t, []string{"ana"}, profiles.approvals)

	promoted := ports.Message{ID: "profile-promoted", Room: room.RoomID, Sender: "ana", Text: "heimdall promote-profile " + proposalID}
	require.NoError(t, runtime.processor.HandleRoomMessage(context.Background(), room, promoted))
	require.Equal(t, []string{"ana"}, profiles.promotions)
}

type commandKnowledgeFiles struct{ approvals, forgets []string }

func (*commandKnowledgeFiles) Stage(context.Context, mcp.Principal, mcp.Assignment, string, string, string, string, int) (mcp.KnowledgeFileInfo, error) {
	return mcp.KnowledgeFileInfo{}, nil
}
func (f *commandKnowledgeFiles) Approve(_ context.Context, p mcp.Principal, ref string) error {
	f.approvals = append(f.approvals, p.Person+":"+ref)
	return nil
}
func (f *commandKnowledgeFiles) Forget(_ context.Context, p mcp.Principal, ref string) error {
	f.forgets = append(f.forgets, p.Person+":"+ref)
	return nil
}

func TestKnowledgeApprovalAndForgetCommandsUseMappedHumanAndPolicyBoundary(t *testing.T) {
	cid := id.ChangeID("KAN-43")
	runtime, _, chat, _ := setupCommandRuntime(t, cid, nil, ports.PR{})
	files := &commandKnowledgeFiles{}
	runtime.files = files
	room := ports.RoomBinding{ChangeID: cid, RoomID: "knowledge-room"}
	ref := "knowledge:" + strings.Repeat("a", 64)
	denied := ports.Message{ID: "knowledge-denied", Room: room.RoomID, Sender: "dana", Text: "heimdall approve-knowledge " + strings.Repeat("a", 64)}
	require.NoError(t, runtime.processor.HandleRoomMessage(context.Background(), room, denied))
	require.Empty(t, files.approvals)
	require.Contains(t, chat.posts[len(chat.posts)-1], "not authorized")

	approved := ports.Message{ID: "knowledge-approved", Room: room.RoomID, Sender: "ana", Text: "heimdall approve-knowledge " + strings.Repeat("a", 64)}
	require.NoError(t, runtime.processor.HandleRoomMessage(context.Background(), room, approved))
	require.Equal(t, []string{"ana:" + ref}, files.approvals)

	forgot := ports.Message{ID: "knowledge-forgot", Room: room.RoomID, Sender: "ana", Text: "heimdall forget " + ref + " poisoned source should be removed"}
	require.NoError(t, runtime.processor.HandleRoomMessage(context.Background(), room, forgot))
	require.Equal(t, []string{"ana:" + ref}, files.forgets)
}

func TestServerCommandRuntimeExecutesMappedRoomRememberAndPostsReply(t *testing.T) {
	cid := id.ChangeID("KAN-1")
	room := ports.RoomBinding{ChangeID: cid, RoomID: "room-1"}
	runtime, store, chat, _ := setupCommandRuntime(t, cid, nil, ports.PR{})
	msg := ports.Message{ID: "switch-message-31", Room: room.RoomID, Sender: "bob", Text: "heimdall remember refunds are capped at 500", At: time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, runtime.processor.HandleRoomMessage(context.Background(), room, msg))
	events, err := store.Load(context.Background(), cid)
	require.NoError(t, err)
	recorded := findDecision(t, events, "switch-message-31")
	require.Equal(t, change.DecisionRemember, recorded.Kind)
	require.Equal(t, "refunds are capped at 500", recorded.Reason)
	require.Equal(t, []string{"I recorded that for this change."}, chat.posts)
}

func TestServerCommandRuntimeStartsTicketThroughTheEngineAndRecordsSource(t *testing.T) {
	cid := id.ChangeID("KAN-1")
	room := ports.RoomBinding{ChangeID: cid, RoomID: "room-1"}
	runtime, store, chat, _ := setupCommandRuntime(t, cid, commandTestTickets{}, ports.PR{})
	msg := ports.Message{ID: "switch-start-42", Room: room.RoomID, Sender: "bob", Text: "heimdall start KAN-2", At: time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, runtime.processor.HandleRoomMessage(context.Background(), room, msg))
	events, err := store.Load(context.Background(), "KAN-2")
	require.NoError(t, err)
	require.NotEmpty(t, events)
	require.Equal(t, change.EventChangeTriggered, events[0].Type)
	triggered, ok := mustPayload[change.Triggered](events[0])
	require.True(t, ok)
	require.Equal(t, change.TriggerManual, triggered.Source)
	decision := findDecision(t, events, "switch-start-42")
	require.Equal(t, change.DecisionStart, decision.Kind)
	require.Equal(t, []string{"Started."}, chat.posts)
}

func TestServerCommandRuntimeSelectsReviewersForFanout(t *testing.T) {
	cid := id.ChangeID("KAN-4")
	room := ports.RoomBinding{ChangeID: cid, RoomID: "room-1"}
	pr := ports.PR{Repo: "aarush323/PBL_TOS_RISK_ANALYZER", Number: 9, Author: "alice", BaseSHA: commandTestBase, HeadSHA: commandTestHead, State: ports.PRStateOpen}
	runtime, store, _, _ := setupCommandRuntime(t, cid, nil, pr)
	opened, err := runtime.event(cid, change.Actor{Kind: change.ActorGitHub, ID: "github"}, commands.SourceGitHub, change.EventPROpened,
		change.PROpened{Repo: pr.Repo, Number: pr.Number, HeadSHA: pr.HeadSHA, Author: pr.Author}, "open-pr")
	require.NoError(t, err)
	_, err = runtime.engine.Advance(context.Background(), cid, []change.Event{opened})
	require.NoError(t, err)
	msg := ports.Message{ID: "switch-review-43", Room: room.RoomID, Sender: "alice", Text: "heimdall review @ana @eli", At: time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, runtime.processor.HandleRoomMessage(context.Background(), room, msg))
	events, err := store.Load(context.Background(), cid)
	require.NoError(t, err)
	var picked *change.ReviewersPicked
	assigned := make(map[string]bool)
	for _, ev := range events {
		switch ev.Type {
		case change.EventReviewersPicked:
			payload, ok := mustPayload[change.ReviewersPicked](ev)
			require.True(t, ok)
			picked = &payload
		case change.EventTaskAssigned:
			payload, ok := mustPayload[change.TaskAssigned](ev)
			require.True(t, ok)
			assigned[string(payload.Agent)] = true
		case change.EventChangeTriggered, change.EventRoomOpened, change.EventBranchEnsured,
			change.EventTaskAcked, change.EventTaskTimedOut, change.EventTaskFailed, change.EventTaskProgress,
			change.EventStepRetried, change.EventStepEscalated, change.EventQuestionAsked, change.EventQuestionAnswered,
			change.EventTestsSubmitted, change.EventTestsVerified, change.EventTestsRejected, change.EventTestsCommitted,
			change.EventTestsApproved, change.EventTicketChanged, change.EventPROpened, change.EventPRPushed,
			change.EventPRLinked, change.EventPRClosed, change.EventSlopEvaluated, change.EventReviewSubmitted,
			change.EventReviewUnavailable, change.EventReviewWaived, change.EventFindingFiled, change.EventFindingVerified,
			change.EventFindingStateChanged, change.EventChallengeFiled, change.EventCaseOpened,
			change.EventVerdictSubmitted, change.EventVerdictApplied, change.EventVerdictEscalated,
			change.EventDecisionRecorded, change.EventApprovalRecorded, change.EventApprovalInvalidated,
			change.EventCIReported, change.EventGateEvaluated, change.EventChangeRejected, change.EventChangeMerged:
			// These event types are intentionally outside the selection and task-assignment assertions.
		}
	}
	require.NotNil(t, picked, "review command must append the event that starts reviewer fan-out")
	require.Equal(t, []change.ActorID{"ana-reviewer", "eli-reviewer"}, picked.People)
	require.Equal(t, change.ActorID("alice"), picked.By)
	require.Equal(t, change.PickCommand, picked.Method)
	require.Equal(t, map[string]bool{"ana-reviewer": true, "eli-reviewer": true}, assigned,
		"reviewer selection must wake exactly the selected agents with assigned tasks")
}

func TestServerCommandRuntimeUpheldFindingChangesGateState(t *testing.T) {
	cid := id.ChangeID("KAN-5")
	room := ports.RoomBinding{ChangeID: cid, RoomID: "room-1"}
	pr := ports.PR{Repo: "aarush323/PBL_TOS_RISK_ANALYZER", Number: 10, Author: "bob", BaseSHA: commandTestBase, HeadSHA: commandTestHead, State: ports.PRStateOpen}
	runtime, store, _, _ := setupCommandRuntime(t, cid, nil, pr)
	opened, err := runtime.event(cid, change.Actor{Kind: change.ActorGitHub, ID: "bob"}, commands.SourceGitHub, change.EventPROpened,
		change.PROpened{Repo: pr.Repo, Number: pr.Number, HeadSHA: pr.HeadSHA, Author: pr.Author}, "open-pr")
	require.NoError(t, err)
	findingID := id.FindingID("fnd_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	filed, err := runtime.event(cid, change.Actor{Kind: change.ActorAgent, ID: "ana-reviewer"}, commands.SourceGitHub, change.EventFindingFiled,
		change.FindingFiled{FindingID: findingID, Ref: id.FindingRef(1), Tier: "E3", State: "open", By: "ana-reviewer"}, "finding")
	require.NoError(t, err)
	caseOpened, err := runtime.event(cid, change.Actor{Kind: change.ActorHuman, ID: "bob"}, commands.SourceChat, change.EventCaseOpened,
		change.CaseOpened{CaseID: id.CaseID("case_01ARZ3NDEKTSV4RRFFQ69G5FAV"), Finding: id.FindingRef(1)}, "case")
	require.NoError(t, err)
	_, err = runtime.engine.Advance(context.Background(), cid, []change.Event{opened, filed, caseOpened})
	require.NoError(t, err)
	msg := ports.Message{ID: "switch-uphold-44", Room: room.RoomID, Sender: "alice", Text: "heimdall uphold F1", At: time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, runtime.processor.HandleRoomMessage(context.Background(), room, msg))
	events, err := store.Load(context.Background(), cid)
	require.NoError(t, err)
	var changed *change.FindingStateChanged
	var applied *change.VerdictApplied
	for _, ev := range events {
		if ev.Type == change.EventFindingStateChanged {
			payload, ok := mustPayload[change.FindingStateChanged](ev)
			require.True(t, ok)
			changed = &payload
		}
		if ev.Type == change.EventVerdictApplied {
			payload, ok := mustPayload[change.VerdictApplied](ev)
			require.True(t, ok)
			applied = &payload
		}
	}
	require.NotNil(t, changed, "human uphold must transition the finding state consumed by the gate")
	require.Equal(t, findingID, changed.FindingID)
	require.Equal(t, "upheld", string(changed.State))
	require.NotNil(t, applied, "human decision must settle the challenged case for gate evaluation")
	require.Equal(t, id.CaseID("case_01ARZ3NDEKTSV4RRFFQ69G5FAV"), applied.CaseID)
}

func TestServerCommandRuntimeRoutesGitHubCommentChallengeAndReplies(t *testing.T) {
	cid := id.ChangeID("KAN-3")
	pr := ports.PR{Repo: "aarush323/PBL_TOS_RISK_ANALYZER", Number: 9, Title: "Fix", Branch: "KAN-3-fix", Author: "bob", BaseSHA: commandTestBase, HeadSHA: commandTestHead, State: ports.PRStateOpen}
	runtime, store, _, comments := setupCommandRuntime(t, cid, nil, pr)
	runtime.intake = prIntake{in: commandTestIntake{stream: cid}}
	filed, err := runtime.event(cid, change.Actor{Kind: change.ActorAgent, ID: "ana-reviewer"}, commands.SourceGitHub, change.EventFindingFiled,
		change.FindingFiled{FindingID: id.FindingID("fnd_01ARZ3NDEKTSV4RRFFQ69G5FAV"), Ref: id.FindingRef(1), Tier: "E3", State: "open", By: "ana-reviewer"}, "finding")
	require.NoError(t, err)
	_, err = runtime.engine.Advance(context.Background(), cid, []change.Event{filed})
	require.NoError(t, err)
	created := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	comment := ports.IssueComment{ID: 41, Login: "bob", UserType: "User", Body: "/heimdall challenge F1 the regression test reproduces it", CreatedAt: created, UpdatedAt: created}
	err = runtime.HandleGitHubComment(context.Background(), pr.Repo, pr, comment)
	require.NoError(t, err)
	events, err := store.Load(context.Background(), cid)
	require.NoError(t, err)
	found := false
	foundState := false
	foundCase := false
	for _, ev := range events {
		if ev.Type == change.EventChallengeFiled {
			found = true
			payload, ok := mustPayload[change.ChallengeFiled](ev)
			require.True(t, ok)
			require.Equal(t, "F1", payload.Finding.String())
			require.Equal(t, 1, payload.Round)
		}
		if ev.Type == change.EventFindingStateChanged {
			payload, ok := mustPayload[change.FindingStateChanged](ev)
			require.True(t, ok)
			require.Equal(t, "disputed", string(payload.State))
			foundState = true
		}
		if ev.Type == change.EventCaseOpened {
			payload, ok := mustPayload[change.CaseOpened](ev)
			require.True(t, ok)
			require.Equal(t, "F1", payload.Finding.String())
			foundCase = true
		}
	}
	require.True(t, found, "challenge command must append challenge.filed")
	require.True(t, foundState, "challenge must move the finding into disputed state for the gate")
	require.True(t, foundCase, "challenge must create a durable resolver case for fan-out")
	require.Equal(t, []string{"Challenge recorded."}, comments.bodies)
	comment.ID = 42
	comment.UserType = "Bot"
	require.NoError(t, runtime.HandleGitHubComment(context.Background(), pr.Repo, pr, comment))
	comment.UserType = "User"
	comment.UpdatedAt = created.Add(time.Minute)
	require.NoError(t, runtime.HandleGitHubComment(context.Background(), pr.Repo, pr, comment))
	comment.UpdatedAt = created
	comment.Body = "ordinary comment"
	require.NoError(t, runtime.HandleGitHubComment(context.Background(), pr.Repo, pr, comment))
	require.Len(t, comments.bodies, 1, "bots, edits, and ordinary comments receive no command reply")
}

func findDecision(t *testing.T, events []change.Event, source string) change.DecisionRecorded {
	t.Helper()
	for _, ev := range events {
		if ev.Type == change.EventDecisionRecorded {
			p, ok := mustPayload[change.DecisionRecorded](ev)
			if ok && p.SourceRef == source {
				return p
			}
		}
	}
	t.Fatalf("decision source %q missing", source)
	return change.DecisionRecorded{}
}
