package switchchat

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestNewRefusesMissingDependencies(t *testing.T) {
	base := Config{
		BaseURL: "https://switch.example.test", AgentID: testAgentID, Secrets: fakeSecrets{},
		APIKeySecret: testKeySecret, RegistrationSecret: testRegSecret, HTTPClient: http.DefaultClient,
		Clock: &fakeClock{}, Wait: immediateWait,
	}
	_, err := New(base)
	require.NoError(t, err)

	noClock := base
	noClock.Clock = nil
	_, err = New(noClock)
	require.Error(t, err)

	noWait := base
	noWait.Wait = nil
	_, err = New(noWait)
	require.Error(t, err)

	relative := base
	relative.BaseURL = "/relative"
	_, err = New(relative)
	require.Error(t, err)
}

func TestCallSendsTheKeyFromTheSecretFileAsBearer(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	_, err := c.OpenRoom(context.Background(), ports.RoomRequest{ChangeID: "PAY-142", Title: "PAY-142 refunds", Key: "k1"})
	require.NoError(t, err)
	for _, r := range f.requests() {
		require.Equal(t, testAPIKey, r.Auth, "every call after registration uses the key file")
	}
}

func TestUnauthorisedIsPortsUnauthorisedAndNeverCarriesTheKey(t *testing.T) {
	f := newFakeSwitch(t)
	secrets := fakeSecrets{testKeySecret: "wrong-key-value"}
	c, _ := newTestClient(t, f)
	c.secrets = secrets
	_, err := c.OpenRoom(context.Background(), ports.RoomRequest{ChangeID: "PAY-142", Title: "PAY-142 refunds", Key: "k1"})
	require.ErrorIs(t, err, ports.ErrUnauthorized)
	require.NotContains(t, err.Error(), "wrong-key-value")
	require.NotContains(t, err.Error(), testAPIKey)
}

func TestMissingSecretFileIsWrappedAndNamesTheFileOnly(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	c.secrets = fakeSecrets{}
	_, err := c.OpenRoom(context.Background(), ports.RoomRequest{ChangeID: "PAY-142", Title: "PAY-142 refunds", Key: "k1"})
	require.Error(t, err)
	require.Contains(t, err.Error(), testKeySecret)
	require.Empty(t, f.requests(), "no request is sent without a key")
}

func TestServerErrorsAreUnavailableAndClientErrorsAreNot(t *testing.T) {
	f := newFakeSwitch(t)
	f.ops["create_room"] = func(map[string]any) (int, any) { return http.StatusBadGateway, map[string]any{"detail": "x"} }
	c, _ := newTestClient(t, f)
	_, err := c.OpenRoom(context.Background(), ports.RoomRequest{ChangeID: "PAY-142", Title: "PAY-142 refunds", Key: "k1"})
	require.ErrorIs(t, err, ports.ErrUnavailable)

	f.ops["create_room"] = func(map[string]any) (int, any) { return http.StatusBadRequest, map[string]any{"detail": "x"} }
	_, err = c.OpenRoom(context.Background(), ports.RoomRequest{ChangeID: "PAY-143", Title: "PAY-143 other", Key: "k2"})
	require.Error(t, err)
	require.NotErrorIs(t, err, ports.ErrUnavailable)
	status, ok := StatusOf(err)
	require.True(t, ok)
	require.Equal(t, http.StatusBadRequest, status)
}

func TestNotFoundIsPortsNotFound(t *testing.T) {
	f := newFakeSwitch(t)
	f.ops["invite_agent_to_room"] = func(map[string]any) (int, any) { return http.StatusNotFound, nil }
	c, _ := newTestClient(t, f)
	err := c.InviteAgent(context.Background(), roomID(t, testRoomUUID), "test-author")
	require.ErrorIs(t, err, ports.ErrNotFound)
}

func TestBreakerOpensAfterFiveUnavailableAndProbesAfterCooldown(t *testing.T) {
	f := newFakeSwitch(t)
	f.ops["create_room"] = func(map[string]any) (int, any) { return http.StatusServiceUnavailable, nil }
	c, clock := newTestClient(t, f)
	ctx := context.Background()
	req := ports.RoomRequest{ChangeID: "PAY-142", Title: "PAY-142 refunds", Key: "k1"}

	for i := 0; i < 5; i++ {
		_, err := c.OpenRoom(ctx, req)
		require.ErrorIs(t, err, ports.ErrUnavailable)
	}
	before := len(f.requests())
	_, err := c.OpenRoom(ctx, req)
	require.ErrorIs(t, err, ports.ErrUnavailable)
	require.Contains(t, err.Error(), "circuit open")
	require.Len(t, f.requests(), before, "an open breaker sends nothing")

	clock.advance(31 * time.Second)
	f.ops["create_room"] = func(map[string]any) (int, any) { return http.StatusOK, map[string]any{"id": testRoomUUID} }
	_, err = c.OpenRoom(ctx, req)
	require.NoError(t, err, "the first call after the cooldown is a probe that closes the breaker")
	require.Greater(t, len(f.requests()), before)
}

func TestClientErrorsDoNotOpenTheBreaker(t *testing.T) {
	f := newFakeSwitch(t)
	f.ops["create_room"] = func(map[string]any) (int, any) { return http.StatusBadRequest, nil }
	c, _ := newTestClient(t, f)
	req := ports.RoomRequest{ChangeID: "PAY-142", Title: "PAY-142 refunds", Key: "k1"}
	for i := 0; i < 8; i++ {
		_, err := c.OpenRoom(context.Background(), req)
		require.Error(t, err)
	}
	f.ops["create_room"] = func(map[string]any) (int, any) { return http.StatusOK, map[string]any{"id": testRoomUUID} }
	_, err := c.OpenRoom(context.Background(), req)
	require.NoError(t, err, "400s are neutral to the breaker")
}

func TestDeadlineIsTenSecondsAndCancellationStops(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.OpenRoom(ctx, ports.RoomRequest{ChangeID: "PAY-142", Title: "PAY-142 refunds", Key: "k1"})
	require.ErrorIs(t, err, context.Canceled, "a cancelled caller gets context.Canceled")
	require.NotContains(t, err.Error(), testAPIKey)
	require.Equal(t, 10*time.Second, requestDeadline)
}

func TestACancelledCallerIsNotASwitchFailure(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	req := ports.RoomRequest{ChangeID: "PAY-142", Title: "PAY-142 refunds", Key: "k1"}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 8; i++ {
		_, err := c.OpenRoom(cancelled, req)
		require.ErrorIs(t, err, context.Canceled)
		require.NotErrorIs(t, err, ports.ErrUnavailable, "the caller stopped, so Switch did not fail")
	}
	_, err := c.OpenRoom(context.Background(), req)
	require.NoError(t, err, "eight cancelled calls do not open the breaker")
}
