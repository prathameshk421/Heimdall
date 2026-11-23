package effects_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

type mockChat struct {
	posts   []mockPost
	postErr error
	posted  map[change.EffectKey]bool
}

type mockPost struct {
	room change.RoomID
	key  change.EffectKey
	text string
}

func (m *mockChat) OpenRoom(_ context.Context, _ ports.RoomRequest) (change.RoomID, error) {
	return "", errors.New("not implemented")
}

func (m *mockChat) Post(_ context.Context, room change.RoomID, key change.EffectKey, text string) error {
	if m.postErr != nil {
		return m.postErr
	}
	if m.posted == nil {
		m.posted = make(map[change.EffectKey]bool)
	}
	if m.posted[key] {
		// Deduped
		return nil
	}
	m.posted[key] = true
	m.posts = append(m.posts, mockPost{room: room, key: key, text: text})
	return nil
}

func (m *mockChat) Read(_ context.Context, _ change.RoomID, _ ports.Cursor) ([]ports.Message, ports.Cursor, error) {
	return nil, "", errors.New("not implemented")
}

type mockInvite struct {
	room  change.RoomID
	agent string
}

// mockInviter records invitations and whether the room had already been posted to when each one arrived.
type mockInviter struct {
	chat        *mockChat
	invites     []mockInvite
	postedFirst bool
	err         error
}

func (m *mockInviter) InviteAgent(_ context.Context, room change.RoomID, agent string) error {
	if m.err != nil {
		return m.err
	}
	if m.chat != nil && len(m.chat.posts) > 0 {
		m.postedFirst = true
	}
	m.invites = append(m.invites, mockInvite{room: room, agent: agent})
	return nil
}

func makeTaskAssignEffect(t *testing.T) change.Effect {
	t.Helper()
	payload := change.TaskAssignPayload{
		Agent:  id.AgentName("bob-reviewer"),
		Role:   workflow.RoleReviewer,
		TaskID: id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB001"),
		RoomID: change.RoomID("room_142"),
		Change: "PAY-142",
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	key, err := change.IdempotencyKey("PAY-142", 1, change.EffectTaskAssign, "tsk_01HZX3K9QW7M2P5R8T0V4YB001")
	require.NoError(t, err)
	return change.Effect{Key: key, Kind: change.EffectTaskAssign, Target: "tsk_01HZX3K9QW7M2P5R8T0V4YB001", Payload: raw}
}

func TestTaskAssignInvitesThenPostsTheWakeLineOnce(t *testing.T) {
	t.Parallel()
	chat := &mockChat{}
	inviter := &mockInviter{chat: chat}
	exec, err := effects.NewTaskAssign(effects.TaskAssignDeps{Chat: chat, Inviter: inviter})
	require.NoError(t, err)

	fx := makeTaskAssignEffect(t)
	require.NoError(t, exec.Execute(context.Background(), fx))

	// The agent must be in the room before it is addressed there: a room opens with only the coordinator in it.
	require.Equal(t, []mockInvite{{room: "room_142", agent: "bob-reviewer"}}, inviter.invites)
	require.False(t, inviter.postedFirst, "the invitation comes before the message")
	require.Len(t, chat.posts, 1)
	require.Equal(t, change.RoomID("room_142"), chat.posts[0].room)
	require.Equal(t, fx.Key, chat.posts[0].key)
	// The room is read by people too (Discord, Telegram): the line says what the agent will do, then the agent's cue.
	require.Equal(t, "@bob-reviewer PAY-142 is yours: review the pull request. Start with heimdall_get_task, it has your full instructions.", chat.posts[0].text)

	require.NoError(t, exec.Execute(context.Background(), fx))
	require.Len(t, chat.posts, 1, "chat dedupes by key")
}

// A failed invitation is an error, and nothing is posted to a room the agent is not in.
func TestTaskAssignInviteFailureStopsBeforeThePost(t *testing.T) {
	t.Parallel()
	chat := &mockChat{}
	inviter := &mockInviter{chat: chat, err: errors.New("switch refused")}
	exec, err := effects.NewTaskAssign(effects.TaskAssignDeps{Chat: chat, Inviter: inviter})
	require.NoError(t, err)

	err = exec.Execute(context.Background(), makeTaskAssignEffect(t))
	require.ErrorContains(t, err, "invite")
	require.Empty(t, chat.posts)
}

func TestTaskAssignNoSecretInErrors(t *testing.T) {
	t.Parallel()
	secretMsg := "top-secret-api-key-12345"
	chat := &mockChat{postErr: errors.New(secretMsg)}
	exec, err := effects.NewTaskAssign(effects.TaskAssignDeps{Chat: chat, Inviter: &mockInviter{chat: chat}})
	require.NoError(t, err)

	err = exec.Execute(context.Background(), makeTaskAssignEffect(t))
	require.Error(t, err)
	require.NotContains(t, err.Error(), secretMsg, "the error text carries no cause text")
	require.ErrorIs(t, err, chat.postErr, "the cause stays reachable for errors.Is")
}
