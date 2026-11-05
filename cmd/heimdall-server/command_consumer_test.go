package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/intake"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
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
	runtime, err := newCommandRuntime(store, ad, eng, gen, clock.System{}, nil)
	require.NoError(t, err)
	runtime.comments = comments
	return runtime, store, chat, comments
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

func TestServerCommandRuntimeRoutesGitHubCommentChallengeAndReplies(t *testing.T) {
	cid := id.ChangeID("KAN-3")
	pr := ports.PR{Repo: "aarush323/PBL_TOS_RISK_ANALYZER", Number: 9, Title: "Fix", Branch: "KAN-3-fix", Author: "bob", BaseSHA: commandTestBase, HeadSHA: commandTestHead, State: ports.PRStateOpen}
	runtime, store, _, comments := setupCommandRuntime(t, cid, nil, pr)
	runtime.intake = prIntake{in: commandTestIntake{stream: cid}}
	created := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	comment := ports.IssueComment{ID: 41, Login: "bob", UserType: "User", Body: "/heimdall challenge F1 the regression test reproduces it", CreatedAt: created, UpdatedAt: created}
	err := runtime.HandleGitHubComment(context.Background(), pr.Repo, pr, comment)
	require.NoError(t, err)
	events, err := store.Load(context.Background(), cid)
	require.NoError(t, err)
	found := false
	for _, ev := range events {
		if ev.Type == change.EventChallengeFiled {
			found = true
			payload, ok := mustPayload[change.ChallengeFiled](ev)
			require.True(t, ok)
			require.Equal(t, "F1", payload.Finding.String())
			require.Equal(t, 1, payload.Round)
		}
	}
	require.True(t, found, "challenge command must append challenge.filed")
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
