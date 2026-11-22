package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/dashboard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type fakeOpsReader struct{ snap ports.OpsSnapshot }

func (f fakeOpsReader) OpsSnapshot(context.Context) (ports.OpsSnapshot, error) { return f.snap, nil }

type staticSecrets map[string]string

func (s staticSecrets) Secret(name string) (string, error) { return s[name], nil }

// O6 + R6: one dead job and one queued job appear on /admin/ops with their kinds, alongside the per-role scorecard.
func TestOperatorOpsReportsQueuedAndDeadJobsByKindAndScorecard(t *testing.T) {
	h := operatorOps{
		ops: fakeOpsReader{snap: ports.OpsSnapshot{
			QueuedByKind:        map[change.EffectKind]int{change.EffectTaskAssign: 1},
			OldestQueuedSeconds: map[change.EffectKind]int64{change.EffectTaskAssign: 12},
			DeadByKind:          map[change.EffectKind]int{change.EffectRunChecks: 1},
			DeadCount:           1,
			DeadMessages:        2,
			OpenChanges:         3,
		}},
		scorecard: func(context.Context) (dashboard.RoleScorecard, error) {
			return dashboard.RoleScorecard{
				TestAuthor: dashboard.TestAuthorScore{Submitted: 4, Verified: 3, RedFirstRate: 0.75},
				Reviewer:   dashboard.ReviewerScore{Reviews: 6, Filed: 8, Verified: 5},
			}, nil
		},
		secrets: staticSecrets{"op_key": "tok-ops"},
		keyName: "op_key",
	}

	// Unauthenticated request is refused.
	unauth := httptest.NewRecorder()
	h.ServeHTTP(unauth, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/ops", nil))
	require.Equal(t, http.StatusUnauthorized, unauth.Code)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/ops", nil)
	req.Header.Set("Authorization", "Bearer tok-ops")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var got opsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, 1, got.Outbox.QueuedByKind[change.EffectTaskAssign])
	require.Equal(t, 1, got.Outbox.DeadByKind[change.EffectRunChecks])
	require.Equal(t, 1, got.Outbox.DeadCount)
	require.Equal(t, 2, got.Outbox.DeadMessages)
	require.Equal(t, 3, got.Outbox.OpenChanges)
	require.Equal(t, 4, got.Scorecard.TestAuthor.Submitted)
	require.Equal(t, 5, got.Scorecard.Reviewer.Verified)
}
