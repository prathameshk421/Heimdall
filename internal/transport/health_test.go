package transport

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func healthy(context.Context) error { return nil }

func get(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), method, target, nil))
	return rec
}

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func mustRouter(t *testing.T, checkers ...Checker) http.Handler {
	t.Helper()
	h, err := NewRouter(discard(), checkers...)
	require.NoError(t, err)
	return h
}

func TestHealthzWithNoCheckersIsOK(t *testing.T) {
	rec := get(t, mustRouter(t), http.MethodGet, "/healthz")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestHealthzWithAllChecksPassingIsOK(t *testing.T) {
	h := mustRouter(t, Checker{Name: "db", Check: healthy}, Checker{Name: "jira", Check: healthy})
	rec := get(t, h, http.MethodGet, "/healthz")
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestHealthzReportsFailingChecksByName(t *testing.T) {
	const secret = "pg-password-hunter2-SECRET"
	failDB := func(context.Context) error { return errors.New("dial postgres://heimdall:" + secret + "@db") }
	h := mustRouter(t,
		Checker{Name: "db", Check: failDB},
		Checker{Name: "github", Check: healthy},
		Checker{Name: "openrouter", Check: func(context.Context) error { return errors.New("401 token expired") }},
	)
	rec := get(t, h, http.MethodGet, "/healthz")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var body struct {
		Status  string   `json:"status"`
		Failing []string `json:"failing"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "unavailable", body.Status)
	require.Equal(t, []string{"db", "openrouter"}, body.Failing, "failing names in registration order, healthy ones omitted")
	require.NotContains(t, rec.Body.String(), secret, "a checker's error text must never reach the client")
	require.NotContains(t, rec.Body.String(), "401", "a checker's error text must never reach the client")
}

func TestHealthzRejectsOtherMethods(t *testing.T) {
	rec := get(t, mustRouter(t), http.MethodPost, "/healthz")
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestHealthzUnknownPathIsNotFound(t *testing.T) {
	rec := get(t, mustRouter(t), http.MethodGet, "/nope")
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestNewRouterRefusesBadCheckers(t *testing.T) {
	cases := []struct {
		name     string
		checkers []Checker
		wantErr  string
	}{
		{"empty name", []Checker{{Name: "", Check: healthy}}, "name"},
		{"duplicate name", []Checker{{Name: "db", Check: healthy}, {Name: "db", Check: healthy}}, "duplicate"},
		{"nil check", []Checker{{Name: "db", Check: nil}}, "nil"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := NewRouter(discard(), tc.checkers...)
			require.ErrorContains(t, err, tc.wantErr)
			require.Nil(t, h)
		})
	}
}

func TestNewRouterRefusesNilLogger(t *testing.T) {
	h, err := NewRouter(nil)
	require.ErrorContains(t, err, "logger")
	require.Nil(t, h)
}
