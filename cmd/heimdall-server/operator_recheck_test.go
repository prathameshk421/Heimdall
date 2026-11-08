package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The operator asks for a recheck with the coordinator key; a wrong key and a body without a reason are refused
// before anything is recorded.
func TestOperatorRecheckNeedsTheKeyAFindingAndAReason(t *testing.T) {
	called := 0
	h := operatorRecheck{
		recheck: func(context.Context, id.ChangeID, string, string) error {
			called++
			return nil
		},
		secrets: fixedSecret{value: "key"},
		keyName: "switch_api_key",
	}
	post := func(auth, body string) int {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/recheck", strings.NewReader(body))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	require.Equal(t, http.StatusUnauthorized, post("Bearer wrong", `{"change":"KAN-23","finding":"F4","reason":"busy"}`))
	require.Equal(t, http.StatusBadRequest, post("Bearer key", `{"change":"KAN-23","finding":"F4"}`))
	require.Zero(t, called)
	require.Equal(t, http.StatusAccepted, post("Bearer key", `{"change":"KAN-23","finding":"F4","reason":"the runner was busy"}`))
	require.Equal(t, 1, called)
}

// A recheck is recorded once per request for a finding whose evidence the server can repeat, even after a downgrade
// to E4. A finding filed as E4, an unknown finding and an unknown change are refused.
func TestOperatorRecheckRecordsARequestForAProvableFindingOnly(t *testing.T) {
	ctx := context.Background()
	cid := id.ChangeID("KAN-23")
	pr := ports.PR{Repo: "aarush323/heimdall-fixture-pay", Number: 11, Author: "bob", BaseSHA: commandTestBase, HeadSHA: commandTestHead, State: ports.PRStateOpen}
	runtime, store, _, _ := setupCommandRuntime(t, cid, nil, pr)
	agent := change.Actor{Kind: change.ActorAgent, ID: "reviewer-1"}
	opened, err := runtime.event(cid, change.Actor{Kind: change.ActorGitHub, ID: "bob"}, commands.SourceGitHub, change.EventPROpened,
		change.PROpened{Repo: pr.Repo, Number: pr.Number, HeadSHA: pr.HeadSHA, Author: pr.Author}, "open-pr")
	require.NoError(t, err)
	e1 := id.FindingID("fnd_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	e4 := id.FindingID("fnd_01ARZ3NDEKTSV4RRFFQ69G5FAW")
	filedE1, err := runtime.event(cid, agent, commands.SourceGitHub, change.EventFindingFiled,
		change.FindingFiled{FindingID: e1, Ref: id.FindingRef(4), Tier: "E1", Severity: "high", State: "open", By: "reviewer-1"}, "f4")
	require.NoError(t, err)
	downgraded, err := runtime.event(cid, change.Actor{Kind: change.ActorSystem, ID: "heimdall"}, commands.SourceGitHub, change.EventFindingStateChanged,
		change.FindingStateChanged{FindingID: e1, Ref: id.FindingRef(4), Tier: "E4", Severity: "high", State: "unverified", By: "heimdall"}, "f4-down")
	require.NoError(t, err)
	filedE4, err := runtime.event(cid, agent, commands.SourceGitHub, change.EventFindingFiled,
		change.FindingFiled{FindingID: e4, Ref: id.FindingRef(7), Tier: "E4", Severity: "low", State: "unverified", By: "reviewer-1"}, "f7")
	require.NoError(t, err)
	_, err = runtime.engine.Advance(ctx, cid, []change.Event{opened, filedE1, downgraded, filedE4})
	require.NoError(t, err)

	require.NoError(t, runtime.OperatorRecheck(ctx, cid, "F4", "the runner was busy"))
	events, err := store.Load(ctx, cid)
	require.NoError(t, err)
	var got []change.FindingRecheckRequested
	for _, ev := range events {
		if ev.Type == change.EventFindingRecheckRequested {
			payload, ok := mustPayload[change.FindingRecheckRequested](ev)
			require.True(t, ok)
			got = append(got, payload)
		}
	}
	require.Equal(t, []change.FindingRecheckRequested{{FindingID: e1, Ref: id.FindingRef(4), Reason: "the runner was busy", By: "operator"}}, got)

	require.ErrorContains(t, runtime.OperatorRecheck(ctx, cid, "F7", "x"), "filed as E4")
	require.ErrorContains(t, runtime.OperatorRecheck(ctx, cid, "F9", "x"), "no finding F9")
	require.ErrorContains(t, runtime.OperatorRecheck(ctx, "KAN-99", "F4", "x"), "no change")
}
