package gitlog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// TestCreateOutcomes covers the first append, which creates the ref with POST /git/refs.
func TestCreateOutcomes(t *testing.T) {
	ctx := t.Context()
	cid := prChange(t, 7)

	t.Run("a create refused while the ref is still absent is retried", func(t *testing.T) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		var refused atomic.Bool
		g.intercept = func(w http.ResponseWriter, r *http.Request) bool {
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/refs") && refused.CompareAndSwap(false, true) {
				reply(w, http.StatusBadGateway, map[string]string{"message": "Server Error"})
				return true
			}
			return false
		}
		n, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-1"), nil)
		require.NoError(t, err)
		require.Equal(t, int64(1), n)
		require.Equal(t, []time.Duration{time.Second}, g.waits)
	})

	t.Run("a create that landed but lost its answer is a success", func(t *testing.T) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		var lost atomic.Bool
		g.intercept = func(w http.ResponseWriter, r *http.Request) bool {
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/refs") && lost.CompareAndSwap(false, true) {
				g.serve(httptest.NewRecorder(), r)
				reply(w, http.StatusBadGateway, map[string]string{"message": "Server Error"})
				return true
			}
			return false
		}
		_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-1"), nil)
		require.NoError(t, err)
		require.Equal(t, 1, g.callCount("POST refs"))
		require.Empty(t, g.waits)
	})
}

// TestTwoWritersOnOneChange covers ACTION_MODE 2.4 and S1 1.2: two gate runs read the same tip and append to the same
// change. Exactly one wins. The other gets ErrVersionConflict, and its events are not in the log.
func TestTwoWritersOnOneChange(t *testing.T) {
	ctx := t.Context()
	cid := prChange(t, 7)
	g := newFakeGit(t)
	seed(ctx, t, g.store(t, testKey), cid, "room-1")

	// Both writers read the tip before either swaps: each waits at its swap until the other arrives too, or for a
	// short timeout when the other already stopped.
	var mu sync.Mutex
	arrived, release := 0, make(chan struct{})
	g.hookBeforeRefWrite = func(n int) {
		if n < 2 {
			return
		}
		mu.Lock()
		arrived++
		if arrived == 2 {
			close(release)
		}
		mu.Unlock()
		select {
		case <-release:
		case <-time.After(2 * time.Second):
		}
	}
	stores := []*Store{g.store(t, testKey), g.store(t, testKey)}
	errs := make([]error, 2)
	var wg sync.WaitGroup
	evs := [][]string{{"writer-a"}, {"writer-b"}}
	for i := range stores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = stores[i].Append(ctx, cid, 1, roomEvents(cid, 1, evs[i]...), nil)
		}()
	}
	wg.Wait()

	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
			continue
		}
		require.ErrorIs(t, err, ports.ErrVersionConflict)
	}
	require.Equal(t, 1, wins, "exactly one writer wins: %v", errs)
	got, err := stores[0].Load(ctx, cid)
	require.NoError(t, err)
	require.Len(t, got, 2, "the loser's events are not in the log")
	require.Equal(t, 2, g.callCount("POST graphql"), "both writers offered a commit on the same tip")
	require.Zero(t, g.callCount("PATCH"))
}

// TestManyWritersOnOneChange races several writers on one change with no ordering help. Exactly one wins.
func TestManyWritersOnOneChange(t *testing.T) {
	ctx := t.Context()
	cid := prChange(t, 7)
	g := newFakeGit(t)
	seed(ctx, t, g.store(t, testKey), cid, "room-1")
	const writers = 6
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		s := g.store(t, testKey)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.Append(ctx, cid, 1, roomEvents(cid, 1, "w"), nil)
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
			continue
		}
		require.ErrorIs(t, err, ports.ErrVersionConflict)
	}
	require.Equal(t, 1, wins)
	got, err := g.store(t, testKey).Load(ctx, cid)
	require.NoError(t, err)
	require.Len(t, got, 2)
}
