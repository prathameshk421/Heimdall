package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// The identifiers of the step tests start with room or branch, so they do not clash with the other tests of this
// package.
const (
	roomTicketKey = id.ChangeID("KAN-7")
	roomRepoName  = change.Repo("aarush323/heimdall-fixture-pay")
	roomBaseSHA   = id.SHA("0123456789abcdef0123456789abcdef01234567")
	roomDefaults  = `{"invite":["role:test-author","person:$assignee"]}`
)

var (
	roomAt        = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	roomJiraActor = change.Actor{Kind: change.ActorJira, ID: "jira"}
	roomSwitchAct = change.Actor{Kind: change.ActorSwitch, ID: "switch"}
	roomGitHubAct = change.Actor{Kind: change.ActorGitHub, ID: "github"}
)

// Compile-time checks: both steps are engine steps.
var (
	_ engine.Step = engine.OpenRoom{}
	_ engine.Step = engine.EnsureBranch{}
)

// roomEvent builds a stored event of the given version, with a payload encoded from v.
func roomEvent(t *testing.T, version int64, typ change.EventType, actor change.Actor, v any) change.Event {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return change.Event{
		ID:            change.EventID(fmt.Sprintf("00000000-0000-4000-8000-%012d", version)),
		ChangeID:      roomTicketKey,
		Version:       version,
		Type:          typ,
		Actor:         actor,
		OccurredAt:    roomAt,
		CorrelationID: "corr-1",
		Payload:       raw,
	}
}

// roomTriggered is change.triggered at version 1, assigned to assignee.
func roomTriggered(t *testing.T) change.Event {
	t.Helper()
	return roomEvent(t, 1, change.EventChangeTriggered, roomJiraActor, change.Triggered{
		Ticket: roomTicketKey, Repo: roomRepoName, Assignee: "ana", Source: change.TriggerJira,
	})
}

// roomOpenedEvt is room.opened at version 2.
func roomOpenedEvt(t *testing.T) change.Event {
	t.Helper()
	return roomEvent(t, 2, change.EventRoomOpened, roomSwitchAct, change.RoomOpened{RoomID: "room-7"})
}

// roomBranchEnsuredEvt is branch.ensured at version 3, for the Done checks.
func roomBranchEnsuredEvt(t *testing.T) change.Event {
	t.Helper()
	return roomEvent(t, 3, change.EventBranchEnsured, roomGitHubAct, change.BranchEnsured{
		Repo: roomRepoName, Branch: "KAN-7-add-refunds", BaseSHA: roomBaseSHA,
	})
}

// roomView returns the view of the change with the given events.
func roomView(evs ...change.Event) engine.ChangeView {
	return engine.ChangeView{ID: roomTicketKey, Events: evs}
}

// roomOpenStep builds the room step from a with object and fails the test when the with is refused.
func roomOpenStep(t *testing.T) engine.OpenRoom {
	t.Helper()
	s, err := engine.NewOpenRoom(json.RawMessage(roomDefaults))
	require.NoError(t, err)
	return s
}

func TestOpenRoomTypeAndDone(t *testing.T) {
	s := roomOpenStep(t)
	require.Equal(t, "switch.open_room", s.Type())

	trig := roomTriggered(t)
	require.False(t, s.Done(roomView(trig)), "not done before room.opened is stored")
	require.True(t, s.Done(roomView(trig, roomOpenedEvt(t))), "done once room.opened is in the view")
}

func TestOpenRoomStartAndTimeoutReturnNothing(t *testing.T) {
	s := roomOpenStep(t)
	res, err := s.Start(context.Background(), roomView(roomTriggered(t)), json.RawMessage(roomDefaults))
	require.NoError(t, err)
	require.Equal(t, engine.Result{}, res)

	res, err = s.OnTimeout(context.Background(), roomView(roomTriggered(t)))
	require.NoError(t, err)
	require.Equal(t, engine.Result{}, res)
}

func TestOpenRoomOnEventIgnoresOtherEvents(t *testing.T) {
	s := roomOpenStep(t)
	pr := roomEvent(t, 2, change.EventPROpened, roomGitHubAct, change.PROpened{
		Repo: roomRepoName, Number: 4, HeadSHA: roomBaseSHA, Author: "aarush323", Ticket: roomTicketKey,
	})
	res, err := s.OnEvent(context.Background(), roomView(roomTriggered(t), pr), pr)
	require.NoError(t, err)
	require.Empty(t, res.Effects)
	require.Empty(t, res.Events)
}

func TestOpenRoomEmitsOneRoomOpenEffect(t *testing.T) {
	s := roomOpenStep(t)
	trig := roomTriggered(t)
	res, err := s.OnEvent(context.Background(), roomView(trig), trig)
	require.NoError(t, err)
	require.Empty(t, res.Events, "the effect attaches to the incoming event, so no event is returned")
	require.Empty(t, res.Timers)
	require.Len(t, res.Effects, 1)

	fx := res.Effects[0]
	require.Equal(t, change.EffectRoomOpen, fx.Kind)
	require.Equal(t, string(roomTicketKey), fx.Target)
	wantKey, err := change.IdempotencyKey(roomTicketKey, trig.Version, change.EffectRoomOpen, "room")
	require.NoError(t, err)
	require.Equal(t, wantKey, fx.Key)
	require.Equal(t, change.EffectKey("room_open:KAN-7:1:room"), fx.Key)
}

func TestOpenRoomPayloadReplacesAssignee(t *testing.T) {
	s := roomOpenStep(t)
	trig := roomTriggered(t)
	res, err := s.OnEvent(context.Background(), roomView(trig), trig)
	require.NoError(t, err)
	require.Len(t, res.Effects, 1)

	p, err := change.DecodeRoomOpenPayload(res.Effects[0].Payload)
	require.NoError(t, err)
	require.Equal(t, change.RoomOpenPayload{
		Ticket: roomTicketKey,
		Invite: []string{"role:test-author", "person:ana"},
	}, p)
	require.NotContains(t, string(res.Effects[0].Payload), "$assignee")
}

func TestOpenRoomEffectIsStableAcrossCalls(t *testing.T) {
	s := roomOpenStep(t)
	trig := roomTriggered(t)
	first, err := s.OnEvent(context.Background(), roomView(trig), trig)
	require.NoError(t, err)
	second, err := s.OnEvent(context.Background(), roomView(trig), trig)
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func TestOpenRoomKeyChangesWithTheTriggerVersion(t *testing.T) {
	s := roomOpenStep(t)
	trig := roomTriggered(t)
	later := trig
	later.Version = 5
	a, err := s.OnEvent(context.Background(), roomView(trig), trig)
	require.NoError(t, err)
	b, err := s.OnEvent(context.Background(), roomView(later), later)
	require.NoError(t, err)
	require.Len(t, a.Effects, 1)
	require.Len(t, b.Effects, 1)
	require.NotEqual(t, a.Effects[0].Key, b.Effects[0].Key)
}

func TestOpenRoomRefusesTriggerForAnotherTicket(t *testing.T) {
	s := roomOpenStep(t)
	trig := roomEvent(t, 1, change.EventChangeTriggered, roomJiraActor, change.Triggered{
		Ticket: "KAN-8", Repo: roomRepoName, Assignee: "ana", Source: change.TriggerJira,
	})
	_, err := s.OnEvent(context.Background(), roomView(trig), trig)
	require.Error(t, err)
}

func TestOpenRoomRefusesBadWith(t *testing.T) {
	for name, with := range map[string]string{
		"no invite key":    `{}`,
		"unknown key":      `{"invite":[],"title":"x"}`,
		"role with space":  `{"invite":["role:test author"]}`,
		"unknown prefix":   `{"invite":["admin:root"]}`,
		"invite not array": `{"invite":"role:test-author"}`,
		"null invite":      `{"invite":null}`,
		"empty with":       ``,
		"not an object":    `["role:test-author"]`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := engine.NewOpenRoom(json.RawMessage(with))
			require.Error(t, err)
		})
	}
}

func TestOpenRoomAcceptsEmptyInviteList(t *testing.T) {
	_, err := engine.NewOpenRoom(json.RawMessage(`{"invite":[]}`))
	require.NoError(t, err)
}
