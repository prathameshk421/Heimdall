package gitlog

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
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

func TestAppendCommitShape(t *testing.T) {
	ctx := t.Context()
	g := newFakeGit(t)
	s := g.store(t, testKey)
	cid := prChange(t, 7)

	seed(ctx, t, s, cid, "room-1")
	require.Equal(t, 1, g.callCount("POST refs"), "the first append creates the ref")
	var created struct{ Ref, SHA string }
	require.NoError(t, json.Unmarshal(g.bodies["POST refs"][0], &created))
	require.Equal(t, testRef, created.Ref)
	var commit struct{ Parents []string }
	require.NoError(t, json.Unmarshal(g.bodies["POST commits"][0], &commit))
	require.Empty(t, commit.Parents, "the first commit has no parent")
	require.Equal(t, created.SHA, g.tip())
	firstTip := g.tip()

	_, err := s.Append(ctx, cid, 1, roomEvents(cid, 1, "room-2"), nil)
	require.NoError(t, err)
	require.Equal(t, 1, g.callCount("PATCH refs/heimdall/state"))
	var upd struct {
		SHA   string
		Force *bool
	}
	require.NoError(t, json.Unmarshal(g.bodies["PATCH refs/heimdall/state"][0], &upd))
	require.NotNil(t, upd.Force)
	require.False(t, *upd.Force, "the ref update is a compare-and-swap, never a force")
	require.Equal(t, g.tip(), upd.SHA)
	require.NoError(t, json.Unmarshal(g.bodies["POST commits"][1], &commit))
	require.Equal(t, []string{firstTip}, commit.Parents)
	var tree struct {
		BaseTree string `json:"base_tree"`
	}
	require.NoError(t, json.Unmarshal(g.bodies["POST trees"][1], &tree))
	require.NotEmpty(t, tree.BaseTree, "later commits build on the tip's tree")
}

func TestStoredLayout(t *testing.T) {
	ctx := t.Context()
	g := newFakeGit(t)
	s := g.store(t, testKey)
	cid := prChange(t, 7)
	fx := roomEffect(t, cid, 2, "a", `{"text":"hi"}`)
	_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-1", "room-2"), []change.Effect{fx})
	require.NoError(t, err)

	logData := g.file("changes/" + string(cid) + ".jsonl")
	require.Equal(t, 2, strings.Count(string(logData), "\n"), "one event per line")
	var head headFile
	require.NoError(t, json.Unmarshal(g.file("changes/"+string(cid)+".head"), &head))
	require.Equal(t, int64(2), head.Version)
	require.Equal(t, digest(g.file("effects/"+string(cid)+".jsonl")), head.Effects)
	require.Equal(t, hex.EncodeToString(computeMAC(testKey, cid, 2, head.Hash, head.Effects)), head.MAC)

	rows := strings.Split(strings.TrimSpace(string(g.file("effects/"+string(cid)+".jsonl"))), "\n")
	require.Len(t, rows, 1)
	var row effectLine
	require.NoError(t, json.Unmarshal([]byte(rows[0]), &row))
	events, err := s.Load(ctx, cid)
	require.NoError(t, err)
	require.Equal(t, events[1].ID, row.EventID, "the effect is caused by the last event of the batch")
	require.Equal(t, fx.Key, row.Effect.Key)
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
		require.Equal(t, 2, g.callCount("PATCH"), "one refused update, one that won")
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
		require.Equal(t, 1, g.callCount("POST refs"))
		require.Equal(t, 1, g.callCount("PATCH"))
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
		before := g.callCount("PATCH")
		_, err := s.Append(ctx, cid, 1, roomEvents(cid, 1, "room-2"), nil)
		require.ErrorIs(t, err, ports.ErrUnavailable)
		require.NotErrorIs(t, err, ports.ErrVersionConflict)
		require.Equal(t, maxRebuilds, g.callCount("PATCH")-before)
	})

	t.Run("a refusal that is not a moved ref is returned and not retried", func(t *testing.T) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		seed(ctx, t, s, cid, "room-1")
		g.intercept = func(w http.ResponseWriter, r *http.Request) bool {
			if r.Method != http.MethodPatch {
				return false
			}
			reply(w, http.StatusUnprocessableEntity, map[string]string{"message": "Required status check is expected"})
			return true
		}
		_, err := s.Append(ctx, cid, 1, roomEvents(cid, 1, "room-2"), nil)
		require.Error(t, err)
		require.ErrorIs(t, err, errRefused)
		require.NotErrorIs(t, err, ports.ErrVersionConflict)
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
	ok := Config{Repo: testRepo, Token: testToken, Key: testKey}
	cases := map[string]func(*Config){
		"short key":   func(c *Config) { c.Key = []byte("short") },
		"no key":      func(c *Config) { c.Key = nil },
		"no token":    func(c *Config) { c.Token = "" },
		"bad repo":    func(c *Config) { c.Repo = "not-a-repo" },
		"branch name": func(c *Config) { c.Ref = "heimdall/state" },
		"dotdot ref":  func(c *Config) { c.Ref = "refs/heimdall/../state" },
		"bad url":     func(c *Config) { c.BaseURL = "ftp://example.com" },
	}
	for name, mutate := range cases {
		c := ok
		mutate(&c)
		_, err := New(c)
		require.Error(t, err, name)
		require.NotContains(t, err.Error(), testToken, name)
	}
	s, err := New(ok)
	require.NoError(t, err)
	require.Equal(t, DefaultRef, s.api.ref)
	require.Equal(t, DefaultBaseURL, s.api.base)
	_, err = New(Config{Repo: testRepo, Token: testToken, Key: testKey, Ref: "refs/heads/heimdall/state"})
	require.NoError(t, err, "the owner may pick a branch if S1 refuses the custom ref")
}

func TestErrorsKeepTheirKind(t *testing.T) {
	ctx := t.Context()
	cid := prChange(t, 7)
	g := newFakeGit(t)
	s := g.store(t, testKey)

	cases := map[int]error{
		http.StatusUnauthorized:        ports.ErrUnauthorized,
		http.StatusForbidden:           ports.ErrUnauthorized,
		http.StatusTooManyRequests:     ports.ErrUnavailable,
		http.StatusInternalServerError: ports.ErrUnavailable,
	}
	for status, want := range cases {
		g.intercept = func(w http.ResponseWriter, _ *http.Request) bool {
			reply(w, status, map[string]string{"message": "x"})
			return true
		}
		_, err := s.Load(ctx, cid)
		require.ErrorIs(t, err, want, "status %d", status)
		require.NotErrorIs(t, err, ErrIntegrity)
	}
}
