package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type fixedSecret struct{ value string }

func (f fixedSecret) Secret(string) (string, error) { return f.value, nil }

func rejectRequest(auth, body string) *http.Request {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/reject", strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	return req
}

// The operator rejects a change with the coordinator key. A wrong key, a wrong method and an empty reason are
// refused before anything is recorded.
func TestOperatorRejectNeedsTheKeyAndAReason(t *testing.T) {
	var gotChange id.ChangeID
	var gotReason string
	h := operatorReject{
		reject: func(_ context.Context, cid id.ChangeID, reason string) error {
			gotChange, gotReason = cid, reason
			return nil
		},
		secrets: fixedSecret{value: "key"},
		keyName: "switch_api_key",
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, rejectRequest("Bearer nope", `{"change":"KAN-9","reason":"stuck"}`))
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, rejectRequest("Bearer key", `{"change":"KAN-9","reason":""}`))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, gotChange)

	rec = httptest.NewRecorder()
	get := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/reject", nil)
	get.Header.Set("Authorization", "Bearer key")
	h.ServeHTTP(rec, get)
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, rejectRequest("Bearer key", `{"change":"KAN-9","reason":"stuck on the old base"}`))
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, id.ChangeID("KAN-9"), gotChange)
	require.Equal(t, "stuck on the old base", gotReason)
}

// The operator's rejection is recorded once as a decision by the actor "operator", with the reason. A second call,
// and a change that does not exist, are refused.
func TestOperatorRejectRecordsOneDecisionAndRefusesARepeat(t *testing.T) {
	cid := id.ChangeID("KAN-1")
	runtime, store, _, _ := setupCommandRuntime(t, cid, commandTestTickets{}, ports.PR{})

	require.NoError(t, runtime.OperatorReject(context.Background(), cid, "cut from the old base"))
	events, err := store.Load(context.Background(), cid)
	require.NoError(t, err)
	decision := findDecision(t, events, "operator:reject:KAN-1")
	require.Equal(t, change.DecisionReject, decision.Kind)
	require.Equal(t, "cut from the old base", decision.Reason)
	require.Equal(t, change.ActorID("operator"), decision.Login)

	require.ErrorContains(t, runtime.OperatorReject(context.Background(), cid, "again"), "already rejected")
	require.ErrorContains(t, runtime.OperatorReject(context.Background(), "KAN-99", "x"), "no change")
}
