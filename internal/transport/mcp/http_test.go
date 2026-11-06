package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestHashTokenIsTheLowercaseSHA256Hex(t *testing.T) {
	sum := sha256.Sum256([]byte(testToken))
	require.Equal(t, hex.EncodeToString(sum[:]), HashToken(testToken))
}

func TestTheStoreIsAskedForTheHashNeverTheToken(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	rep := r.post("Bearer "+testToken, initBody)
	require.Equal(t, http.StatusOK, rep.status, rep.body)
	require.Equal(t, []string{HashToken(testToken)}, r.tokens.seen)
}

func TestRequestsWithoutAUsableBearerTokenGet401BeforeTheStoreIsAsked(t *testing.T) {
	cases := map[string]string{
		"no header":       "",
		"another scheme":  "Basic dGVzdDp0ZXN0",
		"an empty bearer": "Bearer ",
		"a bare token":    testToken,
		"two tokens":      "Bearer " + testToken + " extra",
	}
	for name, auth := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, workflow.RoleReviewer)
			rep := r.post(auth, initBody)
			require.Equal(t, http.StatusUnauthorized, rep.status)
			require.True(t, strings.HasPrefix(rep.header.Get("WWW-Authenticate"), "Bearer"))
			require.Empty(t, r.tokens.seen)
		})
	}
}

func TestAnUnknownTokenGets401(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	rep := r.post("Bearer not-a-token", initBody)
	require.Equal(t, http.StatusUnauthorized, rep.status)
	require.True(t, strings.HasPrefix(rep.header.Get("WWW-Authenticate"), "Bearer"))
}

func TestARevokedTokenGets401(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	r.tokens.err = ports.ErrUnauthorized
	rep := r.post("Bearer "+testToken, initBody)
	require.Equal(t, http.StatusUnauthorized, rep.status)
}

func TestAStoreThatCannotBeReachedGets503WithoutItsDetail(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	r.tokens.err = errors.New("pg: connection refused at db.internal:5432")
	rep := r.post("Bearer "+testToken, initBody)
	require.Equal(t, http.StatusServiceUnavailable, rep.status)
	require.NotContains(t, rep.body, "db.internal")
}

func TestAMethodOtherThanPOSTIsNotAllowed(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/mcp", nil))
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestTheRealTokenIsKeptOutOfRefusalsAndLogsWhenItIsRevokedOrTheStoreFails(t *testing.T) {
	cases := map[string]error{
		"revoked":    ports.ErrUnauthorized,
		"store down": errors.New("pg: connection refused"),
	}
	for name, storeErr := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, workflow.RoleReviewer)
			r.tokens.err = storeErr
			rep := r.post("Bearer "+testToken, initBody)
			require.NotContains(t, rep.body, testToken)
			for _, values := range rep.header {
				for _, v := range values {
					require.NotContains(t, v, testToken)
				}
			}
			require.NotContains(t, r.logs.String(), testToken)
		})
	}
}

func TestABodyOverEightMiBIsRefused(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	body := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` + strings.Repeat("a", 8<<20) + `"}}`
	rep := r.post("Bearer "+testToken, body)
	require.Equal(t, http.StatusRequestEntityTooLarge, rep.status)
}

func TestTheTokenNeverAppearsInAnyAnswerOrLog(t *testing.T) {
	r := newRig(t, workflow.RoleTestAuthor)
	r.tasks.currentErr = errors.New("switch refused the call")
	bodies := []reply{
		r.post("", initBody),
		r.post("Bearer nope", initBody),
		r.post("Bearer "+testToken, `{not json`),
		r.post("Bearer "+testToken, `{"jsonrpc":"2.0","id":2,"method":"no/such"}`),
		r.post("Bearer "+testToken, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_task","arguments":{}}}`),
		r.post("Bearer "+testToken, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_task","arguments":{}}}`),
	}
	for i, rep := range bodies {
		require.NotContains(t, rep.body, testToken, "reply %d", i)
		for _, values := range rep.header {
			for _, v := range values {
				require.NotContains(t, v, testToken, "reply %d header", i)
			}
		}
	}
	require.NotContains(t, r.logs.String(), testToken)
	require.NotContains(t, r.logs.String(), HashToken(testToken))
}

func TestLogsCarryToolAgentRoleAndOutcomeButNeverArguments(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	res := r.call("report_progress", `{"text":"secret-argument-marker"}`)
	require.False(t, res.IsError, res.text())
	logs := r.logs.String()
	require.Contains(t, logs, "report_progress")
	require.Contains(t, logs, string(testAgent))
	require.Contains(t, logs, "reviewer")
	require.Contains(t, logs, "outcome")
	require.NotContains(t, logs, "secret-argument-marker")
}
