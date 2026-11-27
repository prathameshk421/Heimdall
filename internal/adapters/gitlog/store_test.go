package gitlog

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// TestEventStoreContract runs the behaviour every ports.EventStore must show. No shared suite exists in the repo, so
// it is written here against the fake Git Data API.
func TestEventStoreContract(t *testing.T) {
	ctx := t.Context()
	cid := prChange(t, 7)

	t.Run("a change with no log loads as nothing", func(t *testing.T) {
		s := newFakeGit(t).store(t, testKey)
		got, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("append writes a hash chain that Load returns, and leaves the caller's events alone", func(t *testing.T) {
		s := newFakeGit(t).store(t, testKey)
		evs := roomEvents(cid, 0, "room-1", "room-2")
		n, err := s.Append(ctx, cid, 0, evs, nil)
		require.NoError(t, err)
		require.Equal(t, int64(2), n)
		require.Equal(t, change.Hash{}, evs[0].Hash, "Append must not write into the caller's events")

		got, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.NoError(t, change.VerifyChain(got))
		require.Nil(t, got[0].PrevHash)
		require.Equal(t, got[0].Hash, *got[1].PrevHash)
		require.Equal(t, evs[1].ID, got[1].ID)
		require.JSONEq(t, string(evs[1].Payload), string(got[1].Payload))
	})

	t.Run("a second append continues the chain", func(t *testing.T) {
		s := newFakeGit(t).store(t, testKey)
		seed(ctx, t, s, cid, "room-1")
		n, err := s.Append(ctx, cid, 1, roomEvents(cid, 1, "room-2", "room-3"), nil)
		require.NoError(t, err)
		require.Equal(t, int64(3), n)
		got, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Len(t, got, 3)
		require.NoError(t, change.VerifyChain(got))
	})

	t.Run("a stale or ahead expected version is a conflict and writes nothing", func(t *testing.T) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		seed(ctx, t, s, cid, "room-1")
		tip := g.tip()

		_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "late"), nil)
		require.ErrorIs(t, err, ports.ErrVersionConflict)
		_, err = s.Append(ctx, cid, 3, roomEvents(cid, 3, "gap"), nil)
		require.ErrorIs(t, err, ports.ErrVersionConflict)
		require.Equal(t, tip, g.tip(), "a conflicting append moves no ref")
	})

	t.Run("a bad batch is refused before any call", func(t *testing.T) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		other := prChange(t, 8)
		bad := roomEvents(cid, 0, "room-1")
		bad[0].Payload = json.RawMessage(`{"nope":1}`)
		cases := map[string]struct {
			expected int64
			evs      []change.Event
		}{
			"no events":         {0, nil},
			"negative expected": {-1, roomEvents(cid, -1, "a")},
			"version gap":       {0, []change.Event{roomEvent(cid, 2, "a")}},
			"another change":    {0, roomEvents(other, 0, "a")},
			"invalid payload":   {0, bad},
			"non consecutive":   {0, []change.Event{roomEvent(cid, 1, "a"), roomEvent(cid, 3, "b")}},
		}
		for name, c := range cases {
			_, err := s.Append(ctx, cid, c.expected, c.evs, nil)
			require.Error(t, err, name)
			require.NotErrorIs(t, err, ports.ErrVersionConflict, name)
		}
		require.Zero(t, g.callCount("GET"), "a bad batch makes no call")
	})

	t.Run("an event id already in the log is refused", func(t *testing.T) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		first := roomEvents(cid, 0, "a")
		_, err := s.Append(ctx, cid, 0, first, nil)
		require.NoError(t, err)
		tip := g.tip()
		again := roomEvents(cid, 1, "b")
		again[0].ID = first[0].ID
		_, err = s.Append(ctx, cid, 1, again, nil)
		require.Error(t, err)
		require.NotErrorIs(t, err, ports.ErrVersionConflict)
		in := roomEvents(cid, 1, "b", "c")
		in[1].ID = in[0].ID
		_, err = s.Append(ctx, cid, 1, in, nil)
		require.Error(t, err)
		require.Equal(t, tip, g.tip())
	})

	t.Run("changes are independent", func(t *testing.T) {
		s := newFakeGit(t).store(t, testKey)
		a, b := prChange(t, 1), id.ChangeID("PAY-142")
		seed(ctx, t, s, a, "a1")
		seed(ctx, t, s, b, "b1")
		gotA, err := s.Load(ctx, a)
		require.NoError(t, err)
		gotB, err := s.Load(ctx, b)
		require.NoError(t, err)
		require.Len(t, gotA, 1)
		require.Len(t, gotB, 1)
		require.Equal(t, a, gotA[0].ChangeID)
	})

	t.Run("occurred_at is cut to microseconds so a later import keeps the hash", func(t *testing.T) {
		s := newFakeGit(t).store(t, testKey)
		ev := roomEvent(cid, 1, "room-1")
		ev.OccurredAt = base.Add(123456789 * time.Nanosecond)
		_, err := s.Append(ctx, cid, 0, []change.Event{ev}, nil)
		require.NoError(t, err)
		got, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Equal(t, base.Add(123456*time.Microsecond), got[0].OccurredAt)
		require.NoError(t, change.VerifyChain(got))
	})
}

func TestEffects(t *testing.T) {
	ctx := t.Context()
	cid := prChange(t, 7)

	t.Run("the same key and content is a no-op, other content is refused", func(t *testing.T) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		fx := roomEffect(t, cid, 1, "a", `{"text":"hi"}`)
		_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-1"), []change.Effect{fx})
		require.NoError(t, err)
		before := string(g.file("effects/" + string(cid) + ".jsonl"))

		same := fx
		same.Payload = json.RawMessage(`{ "text" : "hi" }`)
		_, err = s.Append(ctx, cid, 1, roomEvents(cid, 1, "room-2"), []change.Effect{same, same})
		require.NoError(t, err)
		require.Equal(t, before, string(g.file("effects/"+string(cid)+".jsonl")))

		tip := g.tip()
		changed := fx
		changed.Payload = json.RawMessage(`{"text":"other"}`)
		_, err = s.Append(ctx, cid, 2, roomEvents(cid, 2, "room-3"), []change.Effect{changed})
		require.ErrorIs(t, err, ErrEffectKeyReused)
		require.Equal(t, tip, g.tip(), "a refused append writes nothing")
	})

	t.Run("an invalid effect is refused", func(t *testing.T) {
		s := newFakeGit(t).store(t, testKey)
		_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-1"), []change.Effect{{Key: "x"}})
		require.Error(t, err)
	})
}

// TestRefConflicts covers ACTION_MODE 2.4: a moved ref for another change rebuilds, for the same change conflicts.
func TestRefConflicts(t *testing.T) {
	ctx := t.Context()
	cid := prChange(t, 7)

	t.Run("a ref that moved for another change is rebuilt and retried", func(t *testing.T) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		seed(ctx, t, s, cid, "room-1")
		g.hookBeforeRefWrite = func(n int) {
			if n == 2 { // the second ref write of the test is our append below
				moveOtherFile(g, n)
			}
		}
		n, err := s.Append(ctx, cid, 1, roomEvents(cid, 1, "room-2"), nil)
		require.NoError(t, err)
		require.Equal(t, int64(2), n)
		require.Equal(t, 2, g.callCount("POST graphql"), "one refused swap, one that won")
		got, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.Equal(t, "write 2", string(g.file("other/note.txt")), "the other writer's file is kept")
	})

	t.Run("a log of another change moved is rebuilt and both changes are kept", func(t *testing.T) {
		g := newFakeGit(t)
		s, rival := g.store(t, testKey), g.store(t, testKey)
		other := prChange(t, 8)
		g.hookBeforeRefWrite = func(n int) {
			if n == 1 {
				seed(ctx, t, rival, other, "rival-1")
			}
		}
		n, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-1"), nil)
		require.NoError(t, err)
		require.Equal(t, int64(1), n)
		for _, c := range []id.ChangeID{cid, other} {
			got, err := s.Load(ctx, c)
			require.NoError(t, err)
			require.Len(t, got, 1, string(c))
		}
	})

	t.Run("a ref created by another writer first is retried", func(t *testing.T) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		g.hookBeforeRefWrite = func(n int) {
			if n == 1 {
				moveOtherFile(g, n)
			}
		}
		_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-1"), nil)
		require.NoError(t, err)
		require.Equal(t, 1, g.callCount("POST refs"), "the create is refused with 422 and not repeated")
		require.Equal(t, 1, g.callCount("POST graphql"), "the rebuilt commit is swapped onto the other writer's tip")
	})

	t.Run("the same change moved is a version conflict and writes nothing of ours", func(t *testing.T) {
		g := newFakeGit(t)
		s, rival := g.store(t, testKey), g.store(t, testKey)
		seed(ctx, t, s, cid, "room-1")
		g.hookBeforeRefWrite = func(n int) {
			if n == 2 {
				_, err := rival.Append(ctx, cid, 1, roomEvents(cid, 1, "rival"), nil)
				require.NoError(t, err)
			}
		}
		_, err := s.Append(ctx, cid, 1, roomEvents(cid, 1, "ours"), nil)
		require.ErrorIs(t, err, ports.ErrVersionConflict)
		got, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.JSONEq(t, `{"room_id":"rival"}`, string(got[1].Payload))
	})

	t.Run("the same change created by another writer first is a version conflict", func(t *testing.T) {
		g := newFakeGit(t)
		s, rival := g.store(t, testKey), g.store(t, testKey)
		g.hookBeforeRefWrite = func(n int) {
			if n == 1 {
				seed(ctx, t, rival, cid, "rival")
			}
		}
		_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "ours"), nil)
		require.ErrorIs(t, err, ports.ErrVersionConflict)
	})

	t.Run("a ref that never stops moving ends in an unavailable error after a bounded number of tries", func(t *testing.T) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		seed(ctx, t, s, cid, "room-1")
		g.hookBeforeRefWrite = func(n int) { moveOtherFile(g, n) }
		before := g.callCount("POST graphql")
		_, err := s.Append(ctx, cid, 1, roomEvents(cid, 1, "room-2"), nil)
		require.ErrorIs(t, err, ports.ErrUnavailable)
		require.NotErrorIs(t, err, ports.ErrVersionConflict)
		require.Equal(t, maxRebuilds, g.callCount("POST graphql")-before)
	})

	t.Run("a cancelled context stops the retry", func(t *testing.T) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		seed(ctx, t, s, cid, "room-1")
		cctx, cancel := context.WithCancel(ctx)
		g.hookBeforeRefWrite = func(n int) { cancel(); moveOtherFile(g, n) }
		_, err := s.Append(cctx, cid, 1, roomEvents(cid, 1, "room-2"), nil)
		require.Error(t, err)
	})
}

func TestConcurrentAppendsToDifferentChanges(t *testing.T) {
	ctx := t.Context()
	g := newFakeGit(t)
	const writers = 6
	stores := make([]*Store, writers)
	changes := make([]id.ChangeID, writers)
	for i := range writers {
		stores[i], changes[i] = g.store(t, testKey), prChange(t, 100+i)
	}
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = stores[i].Append(ctx, changes[i], 0, roomEvents(changes[i], 0, "r"), nil)
		}()
	}
	wg.Wait()
	for i := range writers {
		require.NoError(t, errs[i])
		got, err := stores[i].Load(ctx, changes[i])
		require.NoError(t, err)
		require.Len(t, got, 1)
	}
}

func TestNewRefusesBadConfig(t *testing.T) {
	g := newFakeGit(t)
	ok := Config{Repo: testRepo, Git: g.api(t, testToken), Key: testKey, Wait: g.wait}
	cases := map[string]func(*Config){
		"short key":   func(c *Config) { c.Key = []byte("short") },
		"no key":      func(c *Config) { c.Key = nil },
		"no git":      func(c *Config) { c.Git = nil },
		"no wait":     func(c *Config) { c.Wait = nil },
		"bad repo":    func(c *Config) { c.Repo = "not-a-repo" },
		"branch name": func(c *Config) { c.Ref = "heimdall/state" },
		"dotdot ref":  func(c *Config) { c.Ref = "refs/heimdall/../state" },
	}
	for name, mutate := range cases {
		c := ok
		mutate(&c)
		_, err := New(c)
		require.Error(t, err, name)
		requireNoSecrets(t, err.Error())
	}
	s, err := New(ok)
	require.NoError(t, err)
	require.Equal(t, DefaultRef, s.ref)
}
