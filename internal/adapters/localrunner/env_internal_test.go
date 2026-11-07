package localrunner

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func envValue(env []string, key string) (string, bool) {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v, true
		}
	}
	return "", false
}

// The build cache must not live in the temporary HOME: it would be empty on every run and each run would compile the
// standard library again (found in CI: the package timed out).
func TestTheGoBuildCacheIsNotInsideTheTemporaryHome(t *testing.T) {
	r := New(nil, WithGoCache("/var/cache/heimdall-go"))
	env := r.buildEnv("/tmp/run-1", Job{})
	home, _ := envValue(env, "HOME")
	cache, ok := envValue(env, "GOCACHE")
	require.True(t, ok, "GOCACHE must be set")
	require.Equal(t, "/var/cache/heimdall-go", cache)
	require.Equal(t, "/tmp/run-1", home)
	require.False(t, strings.HasPrefix(cache, home), "the cache must outlive the run")
}

func TestTheDefaultGoCacheIsUnderTheUserCacheDirectory(t *testing.T) {
	r := New(nil)
	env := r.buildEnv("/tmp/run-2", Job{})
	cache, ok := envValue(env, "GOCACHE")
	require.True(t, ok)
	require.Contains(t, cache, "heimdall-runner")
	require.False(t, strings.HasPrefix(cache, "/tmp/run-2"))
}
