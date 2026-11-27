package github

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// A changed resource answers 200 with a new ETag, and the new body replaces the cached one.
func TestAChangedResourceReplacesTheCachedBody(t *testing.T) {
	env := newTestEnv(t)
	path := "/repos/" + string(testRepo) + "/pulls/5"
	version := 1
	env.f.route("GET", path, func(w http.ResponseWriter, r *http.Request) {
		pr := samplePR(5)
		if version == 2 {
			pr = samplePR(6)
		}
		etag := `"pr5-v` + string(rune('0'+version)) + `"`
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		writeJSON(w, http.StatusOK, pr)
	})
	ctx := context.Background()
	_, err := env.a.PR(ctx, testRepo, 5)
	require.NoError(t, err)
	version = 2
	changed, err := env.a.PR(ctx, testRepo, 5)
	require.NoError(t, err)
	require.Equal(t, 6, changed.Number, "a 200 after a change is not hidden by the cache")
	again, err := env.a.PR(ctx, testRepo, 5)
	require.NoError(t, err)
	require.Equal(t, 6, again.Number, "the 304 now serves the new body")
}
