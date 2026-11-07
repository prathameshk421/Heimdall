package effects_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

type mockBlobStore struct {
	puts    []mockPut
	putErr  error
	getErr  error
	content map[workflow.BlobRef][]byte
}

type mockPut struct {
	class ports.BlobClass
	data  []byte
}

func (m *mockBlobStore) Put(_ context.Context, class ports.BlobClass, r io.Reader) (workflow.BlobRef, error) {
	if m.putErr != nil {
		return "", m.putErr
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	m.puts = append(m.puts, mockPut{class: class, data: data})
	sum := sha256.Sum256(data)
	ref := workflow.BlobRef(fmt.Sprintf("blob:sha256:%s", hex.EncodeToString(sum[:])))
	if m.content == nil {
		m.content = make(map[workflow.BlobRef][]byte)
	}
	m.content[ref] = data
	return ref, nil
}

func (m *mockBlobStore) Get(_ context.Context, ref workflow.BlobRef) (io.ReadCloser, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	data, ok := m.content[ref]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

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

func makeTaskAssignEffect(t *testing.T, target string, bundleBytes []byte) change.Effect {
	t.Helper()
	payload := change.TaskAssignPayload{
		Agent:  id.AgentName("bob-reviewer"),
		Role:   workflow.RoleReviewer,
		TaskID: id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB001"),
		RoomID: change.RoomID("room_142"),
		Bundle: bundleBytes,
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	key, err := change.IdempotencyKey("PAY-142", 1, change.EffectTaskAssign, "tsk_01HZX3K9QW7M2P5R8T0V4YB001")
	require.NoError(t, err)

	return change.Effect{
		Key:     key,
		Kind:    change.EffectTaskAssign,
		Target:  target,
		Payload: raw,
	}
}

func TestTaskAssignSuccess(t *testing.T) {
	blobs := &mockBlobStore{}
	chat := &mockChat{}
	inviter := &mockInviter{chat: chat}

	exec, err := effects.NewTaskAssign(effects.TaskAssignDeps{
		Blobs:   blobs,
		Chat:    chat,
		Inviter: inviter,
	})
	require.NoError(t, err)

	bundle := []byte(`{"ticket":"PAY-142","repo":"acme/pay"}`)
	sum := sha256.Sum256(bundle)
	ref := fmt.Sprintf("blob:sha256:%s", hex.EncodeToString(sum[:]))

	fx := makeTaskAssignEffect(t, ref, bundle)

	err = exec.Execute(context.Background(), fx)
	require.NoError(t, err)

	// Blob stored
	require.Len(t, blobs.puts, 1)
	require.Equal(t, ports.BlobBundle, blobs.puts[0].class)
	require.Equal(t, bundle, blobs.puts[0].data)

	// Chat message posted
	require.Len(t, chat.posts, 1)
	require.Equal(t, change.RoomID("room_142"), chat.posts[0].room)
	require.Equal(t, fx.Key, chat.posts[0].key)
	// The room is read by people too (Discord, Telegram): the line says what the agent will do, then the agent's cue.
	require.Equal(t, "@bob-reviewer PAY-142 is yours: review the pull request. Start with heimdall_get_task, it has your full instructions.", chat.posts[0].text)
}

func TestTaskAssignLineNamesTheWorkOfEachRole(t *testing.T) {
	require.Equal(t, "@test-author PAY-142 is yours: write failing tests for the acceptance criteria. Start with heimdall_get_task, it has your full instructions.",
		effects.TaskAssignText("test-author", "PAY-142", workflow.RoleTestAuthor))
	require.Equal(t, "@resolver PAY-142 is yours: settle the challenged findings. Start with heimdall_get_task, it has your full instructions.",
		effects.TaskAssignText("resolver", "PAY-142", workflow.RoleResolver))
}

// The agent must be in the room before it is addressed there: a room opens with only the coordinator in it.
func TestTaskAssignInvitesTheAgentIntoTheRoomBeforePosting(t *testing.T) {
	chat := &mockChat{}
	inviter := &mockInviter{chat: chat}
	exec, err := effects.NewTaskAssign(effects.TaskAssignDeps{Blobs: &mockBlobStore{}, Chat: chat, Inviter: inviter})
	require.NoError(t, err)

	bundle := []byte(`{"ticket":"PAY-142"}`)
	sum := sha256.Sum256(bundle)
	require.NoError(t, exec.Execute(context.Background(), makeTaskAssignEffect(t, fmt.Sprintf("blob:sha256:%s", hex.EncodeToString(sum[:])), bundle)))

	require.Equal(t, []mockInvite{{room: "room_142", agent: "bob-reviewer"}}, inviter.invites)
	require.False(t, inviter.postedFirst, "the invitation comes before the message")
	require.Len(t, chat.posts, 1)
}

// A failed invitation is an error, and nothing is posted to a room the agent is not in.
func TestTaskAssignInviteFailureStopsBeforeThePost(t *testing.T) {
	chat := &mockChat{}
	inviter := &mockInviter{chat: chat, err: errors.New("switch refused")}
	exec, err := effects.NewTaskAssign(effects.TaskAssignDeps{Blobs: &mockBlobStore{}, Chat: chat, Inviter: inviter})
	require.NoError(t, err)

	bundle := []byte(`{"ticket":"PAY-142"}`)
	sum := sha256.Sum256(bundle)
	err = exec.Execute(context.Background(), makeTaskAssignEffect(t, fmt.Sprintf("blob:sha256:%s", hex.EncodeToString(sum[:])), bundle))
	require.ErrorContains(t, err, "invite")
	require.Empty(t, chat.posts)
}

func TestTaskAssignHashMismatchRefused(t *testing.T) {
	blobs := &mockBlobStore{}
	chat := &mockChat{}
	inviter := &mockInviter{chat: chat}

	exec, err := effects.NewTaskAssign(effects.TaskAssignDeps{
		Blobs:   blobs,
		Chat:    chat,
		Inviter: inviter,
	})
	require.NoError(t, err)

	bundle := []byte(`{"ticket":"PAY-142","repo":"acme/pay"}`)
	mismatchedRef := "blob:sha256:0000000000000000000000000000000000000000000000000000000000000000"

	fx := makeTaskAssignEffect(t, mismatchedRef, bundle)

	err = exec.Execute(context.Background(), fx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "hash mismatch")

	// Neither blob store nor chat should have been called
	require.Empty(t, blobs.puts)
	require.Empty(t, chat.posts)
}

func TestTaskAssignIdempotent(t *testing.T) {
	blobs := &mockBlobStore{}
	chat := &mockChat{}
	inviter := &mockInviter{chat: chat}

	exec, err := effects.NewTaskAssign(effects.TaskAssignDeps{
		Blobs:   blobs,
		Chat:    chat,
		Inviter: inviter,
	})
	require.NoError(t, err)

	bundle := []byte(`{"ticket":"PAY-142"}`)
	sum := sha256.Sum256(bundle)
	ref := fmt.Sprintf("blob:sha256:%s", hex.EncodeToString(sum[:]))

	fx := makeTaskAssignEffect(t, ref, bundle)

	// First call
	err = exec.Execute(context.Background(), fx)
	require.NoError(t, err)
	require.Len(t, chat.posts, 1)

	// Second call with same key
	err = exec.Execute(context.Background(), fx)
	require.NoError(t, err)
	// Chat dedupes: still exactly 1 post
	require.Len(t, chat.posts, 1)
}

func TestTaskAssignNoSecretInErrors(t *testing.T) {
	secretMsg := "top-secret-api-key-12345"
	blobs := &mockBlobStore{putErr: errors.New(secretMsg)}
	chat := &mockChat{}
	inviter := &mockInviter{chat: chat}

	exec, err := effects.NewTaskAssign(effects.TaskAssignDeps{
		Blobs:   blobs,
		Chat:    chat,
		Inviter: inviter,
	})
	require.NoError(t, err)

	bundle := []byte(`{"ticket":"PAY-142"}`)
	sum := sha256.Sum256(bundle)
	ref := fmt.Sprintf("blob:sha256:%s", hex.EncodeToString(sum[:]))

	fx := makeTaskAssignEffect(t, ref, bundle)

	err = exec.Execute(context.Background(), fx)
	require.Error(t, err)
	// The Error() message must NOT contain the secret
	require.NotContains(t, err.Error(), secretMsg)
	// But Unwrap() preserves the cause for errors.Is
	require.ErrorIs(t, err, blobs.putErr)
}

func TestNewTaskAssignMissingDeps(t *testing.T) {
	_, err := effects.NewTaskAssign(effects.TaskAssignDeps{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing blobs")
}
