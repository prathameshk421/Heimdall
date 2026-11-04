package change_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

func TestIdempotencyKeyGoldenVectors(t *testing.T) {
	k, err := change.IdempotencyKey("PAY-142", 7, change.EffectGitHubCheck, "acme/pay-demo:9f1c2ab:gate-3")
	require.NoError(t, err)
	require.Equal(t, change.EffectKey("github_check:PAY-142:7:acme/pay-demo:9f1c2ab:gate-3"), k)

	k, err = change.IdempotencyKey("pr-acme-pay-demo-7", 1, change.EffectRoomPost, "msg-1")
	require.NoError(t, err)
	require.Equal(t, change.EffectKey("room_post:pr-acme-pay-demo-7:1:msg-1"), k)
}

func TestIdempotencyKeyIsStableAndDependsOnEveryInput(t *testing.T) {
	base, err := change.IdempotencyKey("PAY-142", 7, change.EffectTrackerWrite, "comment")
	require.NoError(t, err)
	again, err := change.IdempotencyKey("PAY-142", 7, change.EffectTrackerWrite, "comment")
	require.NoError(t, err)
	require.Equal(t, base, again)

	others := []struct {
		name string
		key  func() (change.EffectKey, error)
	}{
		{"change", func() (change.EffectKey, error) {
			return change.IdempotencyKey("PAY-143", 7, change.EffectTrackerWrite, "comment")
		}},
		{"version", func() (change.EffectKey, error) {
			return change.IdempotencyKey("PAY-142", 8, change.EffectTrackerWrite, "comment")
		}},
		{"kind", func() (change.EffectKey, error) {
			return change.IdempotencyKey("PAY-142", 7, change.EffectMemoryEpisode, "comment")
		}},
		{"discriminator", func() (change.EffectKey, error) {
			return change.IdempotencyKey("PAY-142", 7, change.EffectTrackerWrite, "comment2")
		}},
	}
	for _, o := range others {
		k, err := o.key()
		require.NoError(t, err, o.name)
		require.NotEqual(t, base, k, "%s must change the key", o.name)
	}
}

func TestIdempotencyKeyIsInjectiveWhenTheDiscriminatorHoldsColons(t *testing.T) {
	// The first three fields hold no colon, so a colon inside the discriminator cannot move a boundary.
	a, err := change.IdempotencyKey("PAY-1", 1, change.EffectRoomPost, "x:1:y")
	require.NoError(t, err)
	b, err := change.IdempotencyKey("PAY-1", 1, change.EffectRoomPost, "x")
	require.NoError(t, err)
	require.NotEqual(t, a, b)
}

func TestIdempotencyKeyRefusesBadInputs(t *testing.T) {
	cases := map[string]func() (change.EffectKey, error){
		"colon in change id": func() (change.EffectKey, error) {
			return change.IdempotencyKey("PAY:142", 1, change.EffectRoomPost, "d")
		},
		"version zero": func() (change.EffectKey, error) {
			return change.IdempotencyKey("PAY-142", 0, change.EffectRoomPost, "d")
		},
		"unknown kind": func() (change.EffectKey, error) { return change.IdempotencyKey("PAY-142", 1, "smoke_signal", "d") },
		"blank discriminator": func() (change.EffectKey, error) {
			return change.IdempotencyKey("PAY-142", 1, change.EffectRoomPost, "")
		},
		"space in discriminator": func() (change.EffectKey, error) {
			return change.IdempotencyKey("PAY-142", 1, change.EffectRoomPost, "a b")
		},
	}
	for name, call := range cases {
		_, err := call()
		require.Error(t, err, name)
	}
}

func TestEffectKindsAreTheOutboxKindsOfTheSpec(t *testing.T) {
	for _, k := range []change.EffectKind{change.EffectGitHubCheck, change.EffectGitHubComment, change.EffectGitHubBranch,
		change.EffectRoomOpen, change.EffectRoomPost, change.EffectTrackerWrite, change.EffectMemoryEpisode} {
		require.True(t, k.Valid(), k)
	}
	require.False(t, change.EffectKind("email").Valid())
}

func TestEffectValidateNamesEveryField(t *testing.T) {
	good := change.Effect{
		Key:     "room_post:PAY-142:1:msg-1",
		Kind:    change.EffectRoomPost,
		Target:  "room-42",
		Payload: json.RawMessage(`{"text":"opened"}`),
	}
	require.NoError(t, good.Validate())

	bad := good
	bad.Key = "no colons here"
	bad.Kind = "email"
	bad.Target = ""
	bad.Payload = json.RawMessage(`[1]`)
	err := bad.Validate()
	require.Error(t, err)
	for _, field := range []string{"key", "kind", "target", "payload"} {
		require.Contains(t, err.Error(), field)
	}
}
