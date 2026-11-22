package github

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWriterResolvesPerRepoInstallationOn422AndCachesIt(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	lookupCalls := 0
	otherInstallCalls := 0

	// Default installation 42 rejects repos owned by another user with 422.
	env.f.route(http.MethodPost, "/app/installations/42/access_tokens", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"There is at least one repository that does not exist or is not accessible to the parent installation."}`))
	})
	env.f.route(http.MethodGet, "/repos/otherorg/t3code/installation", func(w http.ResponseWriter, _ *http.Request) {
		lookupCalls++
		writeJSON(w, http.StatusOK, map[string]any{"id": 999})
	})
	env.f.route(http.MethodPost, "/app/installations/999/access_tokens", func(w http.ResponseWriter, _ *http.Request) {
		otherInstallCalls++
		writeJSON(w, http.StatusCreated, map[string]any{
			"token":      "ghs_other_org_token",
			"expires_at": time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		})
	})

	tok, err := env.a.Tokens().Writer(context.Background(), OpCommentWrite, "otherorg/t3code")
	require.NoError(t, err)
	require.Equal(t, "ghs_other_org_token", tok)
	require.Equal(t, 1, lookupCalls)
	require.Equal(t, 1, otherInstallCalls)

	// A different write op on the same repo reuses cached installation 999 without calling 42 or lookup again.
	tok2, err := env.a.Tokens().Writer(context.Background(), OpCheckWrite, "otherorg/t3code")
	require.NoError(t, err)
	require.Equal(t, "ghs_other_org_token", tok2)
	require.Equal(t, 1, lookupCalls)
	require.Equal(t, 2, otherInstallCalls)
}
