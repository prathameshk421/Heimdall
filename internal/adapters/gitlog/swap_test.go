package gitlog

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// TestSwapOutcomes covers ACTION_MODE_SPIKE.md 1.7 point 5: after a refused updateRefs the store reads the tip again
// over REST, and the tip decides between a lost answer, a transient failure and a conflict.
func TestSwapOutcomes(t *testing.T) {
	ctx := t.Context()
	cid := prChange(t, 7)

	setup := func(t *testing.T) (*fakeGit, *Store) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		seed(ctx, t, s, cid, "room-1")
		return g, s
	}

	for name, mode := range map[string]swapMode{"opaque error": swapLostAnswer, "bad gateway": swapLostGateway} {
		t.Run("a swap that landed but lost its answer ("+name+") is a success", func(t *testing.T) {
			g, s := setup(t)
			g.swapHook = func(int) swapMode { return mode }
			n, err := s.Append(ctx, cid, 1, roomEvents(cid, 1, "room-2"), nil)
			require.NoError(t, err)
			require.Equal(t, int64(2), n)
			require.Equal(t, 1, g.callCount("POST graphql"), "the store does not offer a commit that landed again")
			require.Empty(t, g.waits)
			got, err := s.Load(ctx, cid)
			require.NoError(t, err)
			require.Len(t, got, 2)
		})
	}

	t.Run("a lost answer with another writer on top is still our append", func(t *testing.T) {
		g, s := setup(t)
		g.swapHook = func(int) swapMode { return swapLostAnswer }
		var moved atomic.Bool
		g.intercept = func(_ http.ResponseWriter, r *http.Request) bool {
			if g.callCount("POST graphql") == 1 && strings.Contains(r.URL.Path, "/git/ref/") && moved.CompareAndSwap(false, true) {
				moveOtherFile(g, 99)
			}
			return false
		}
		n, err := s.Append(ctx, cid, 1, roomEvents(cid, 1, "room-2"), nil)
		require.NoError(t, err)
		require.Equal(t, int64(2), n)
		require.Equal(t, 1, g.callCount("POST graphql"))
		got, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.Equal(t, "write 99", string(g.file("other/note.txt")))
	})

}

// TestSwapRetries covers the transient case of S1 1.7 point 5 (the tip is still beforeOid) and FORBIDDEN.
func TestSwapRetries(t *testing.T) {
	ctx := t.Context()
	cid := prChange(t, 7)
	setup := func(t *testing.T) (*fakeGit, *Store) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		seed(ctx, t, s, cid, "room-1")
		return g, s
	}
	refReads := func(g *fakeGit) int { return g.callCount("GET ref/") }

	t.Run("a transient refusal with the tip unchanged is retried with backoff and then succeeds", func(t *testing.T) {
		g, s := setup(t)
		g.swapHook = func(n int) swapMode {
			if n <= 2 {
				return swapTransient
			}
			return swapNormal
		}
		before := g.tip()
		n, err := s.Append(ctx, cid, 1, roomEvents(cid, 1, "room-2"), nil)
		require.NoError(t, err)
		require.Equal(t, int64(2), n)
		require.Equal(t, 3, g.callCount("POST graphql"))
		require.Equal(t, []time.Duration{time.Second, 2 * time.Second}, g.waits)
		require.NotEqual(t, before, g.tip())
		got, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Len(t, got, 2)
	})

	t.Run("a refusal that never clears is unavailable after a bounded number of tries", func(t *testing.T) {
		g, s := setup(t)
		g.swapHook = func(int) swapMode { return swapTransient }
		before := g.tip()
		_, err := s.Append(ctx, cid, 1, roomEvents(cid, 1, "room-2"), nil)
		require.ErrorIs(t, err, ports.ErrUnavailable)
		require.ErrorIs(t, err, ports.ErrRefSwapUnconfirmed)
		require.NotErrorIs(t, err, ports.ErrVersionConflict)
		require.Equal(t, maxSwapAttempts, g.callCount("POST graphql"))
		require.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}, g.waits)
		require.Equal(t, before, g.tip())
	})

	t.Run("FORBIDDEN fails closed with no re-read and no retry", func(t *testing.T) {
		g, s := setup(t)
		g.swapHook = func(int) swapMode { return swapForbidden }
		before, reads := g.tip(), refReads(g)
		_, err := s.Append(ctx, cid, 1, roomEvents(cid, 1, "room-2"), nil)
		require.ErrorIs(t, err, ports.ErrUnauthorized)
		require.NotErrorIs(t, err, ports.ErrRefSwapUnconfirmed)
		require.Equal(t, 1, g.callCount("POST graphql"))
		require.Equal(t, reads+1, refReads(g), "only the read before the write")
		require.Empty(t, g.waits)
		require.Equal(t, before, g.tip())
		require.NotContains(t, err.Error(), "Resource not accessible")
	})

	t.Run("a cancelled context during the backoff stops the append", func(t *testing.T) {
		g, s := setup(t)
		g.swapHook = func(int) swapMode { return swapTransient }
		cctx, cancel := context.WithCancel(ctx)
		defer cancel()
		g.intercept = func(_ http.ResponseWriter, r *http.Request) bool {
			if r.URL.Path == "/graphql" {
				cancel()
			}
			return false
		}
		_, err := s.Append(cctx, cid, 1, roomEvents(cid, 1, "room-2"), nil)
		require.Error(t, err)
		require.ErrorIs(t, err, context.Canceled)
	})

}
