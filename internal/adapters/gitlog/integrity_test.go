package gitlog

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func logPath(c id.ChangeID) string  { return "changes/" + string(c) + ".jsonl" }
func headPath(c id.ChangeID) string { return "changes/" + string(c) + ".head" }
func effPath(c id.ChangeID) string  { return "effects/" + string(c) + ".jsonl" }

// requireIntegrity checks that err is a typed integrity failure that names the change and holds no secret.
func requireIntegrity(t *testing.T, err error, c id.ChangeID, reason string) {
	t.Helper()
	require.ErrorIs(t, err, ErrIntegrity)
	var ie *IntegrityError
	require.ErrorAs(t, err, &ie)
	require.Equal(t, c, ie.Change)
	require.Contains(t, ie.Reason, reason)
	requireNoSecrets(t, err.Error())
}

func requireNoSecrets(t *testing.T, text string) {
	t.Helper()
	for _, secret := range [][]byte{testKey, otherKey, []byte(testToken)} {
		for _, form := range []string{string(secret), hex.EncodeToString(secret), base64.StdEncoding.EncodeToString(secret)} {
			require.NotContains(t, text, form)
		}
	}
}

// TestTamperedLogIsRefused covers ACTION_MODE 2.7: a writer with push access edits the ref, and the gate must refuse
// the log instead of reading it. Each case edits the stored files after a valid append and expects Load and Append to
// fail with an IntegrityError, and Append to write nothing.
func TestTamperedLogIsRefused(t *testing.T) {
	ctx := t.Context()
	cid := prChange(t, 7)
	other := prChange(t, 8)

	setup := func(t *testing.T) (*fakeGit, *Store) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		fx := roomEffect(t, cid, 3, "a", `{"text":"hi"}`)
		_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-1", "room-2", "room-3"), []change.Effect{fx})
		require.NoError(t, err)
		seed(ctx, t, s, other, "other-1")
		return g, s
	}
	editHead := func(g *fakeGit, mutate func(h *headFile)) map[string][]byte {
		var h headFile
		require.NoError(t, json.Unmarshal(g.file(headPath(cid)), &h))
		mutate(&h)
		raw, err := json.Marshal(h)
		require.NoError(t, err)
		return map[string][]byte{headPath(cid): append(raw, '\n')}
	}
	lines := func(g *fakeGit) [][]byte {
		return bytes.SplitAfter(bytes.TrimSuffix(g.file(logPath(cid)), nil), []byte("\n"))
	}

	cases := []struct {
		name   string
		edit   func(g *fakeGit) map[string][]byte
		reason string
	}{
		{"an event payload is edited", func(g *fakeGit) map[string][]byte {
			return map[string][]byte{logPath(cid): bytes.Replace(g.file(logPath(cid)), []byte("room-2"), []byte("room-9"), 1)}
		}, "hash chain"},
		{"an event is removed from the middle", func(g *fakeGit) map[string][]byte {
			l := lines(g)
			return map[string][]byte{logPath(cid): bytes.Join([][]byte{l[0], l[2]}, nil)}
		}, "hash chain"},
		{"the tail is cut and the head kept", func(g *fakeGit) map[string][]byte {
			l := lines(g)
			return map[string][]byte{logPath(cid): bytes.Join(l[:2], nil)}
		}, "head does not match"},
		{"the tail is cut and the head forged without the key", func(g *fakeGit) map[string][]byte {
			l := lines(g)
			var ev change.Event
			require.NoError(t, json.Unmarshal(l[1], &ev))
			files := editHead(g, func(h *headFile) { h.Version, h.Hash = 2, ev.Hash })
			files[logPath(cid)] = bytes.Join(l[:2], nil)
			return files
		}, "mac"},
		{"a head signed with the key but for the wrong version", func(g *fakeGit) map[string][]byte {
			var h headFile
			require.NoError(t, json.Unmarshal(g.file(headPath(cid)), &h))
			raw, err := signHead(testKey, cid, 2, h.Hash, h.Effects)
			require.NoError(t, err)
			return map[string][]byte{headPath(cid): raw}
		}, "head does not match"},
		{"the head MAC is flipped", func(g *fakeGit) map[string][]byte {
			return editHead(g, func(h *headFile) { h.MAC = flipHex(h.MAC) })
		}, "mac"},
		{"the head MAC is cut short", func(g *fakeGit) map[string][]byte {
			return editHead(g, func(h *headFile) { h.MAC = h.MAC[:10] })
		}, "mac"},
		{"the head MAC is empty", func(g *fakeGit) map[string][]byte {
			return editHead(g, func(h *headFile) { h.MAC = "" })
		}, "mac"},
		{"the head version is raised", func(g *fakeGit) map[string][]byte {
			return editHead(g, func(h *headFile) { h.Version++ })
		}, "mac"},
		{"the head effects digest is changed", func(g *fakeGit) map[string][]byte {
			return editHead(g, func(h *headFile) { h.Effects = digest([]byte("x")) })
		}, "mac"},
		{"the head of another change is copied in", func(g *fakeGit) map[string][]byte {
			return map[string][]byte{headPath(cid): g.file(headPath(other))}
		}, "mac"},
		{"the head file is deleted", func(*fakeGit) map[string][]byte { return map[string][]byte{headPath(cid): nil} }, "missing"},
		{"the change file is deleted", func(*fakeGit) map[string][]byte { return map[string][]byte{logPath(cid): nil} }, "missing"},
		{"the change file is replaced by another change's", func(g *fakeGit) map[string][]byte {
			return map[string][]byte{logPath(cid): g.file(logPath(other))}
		}, ""},
		{"the head is not JSON", func(*fakeGit) map[string][]byte { return map[string][]byte{headPath(cid): []byte("hello")} }, "malformed"},
		{"the head has an unknown field", func(g *fakeGit) map[string][]byte {
			raw := bytes.Replace(g.file(headPath(cid)), []byte("{"), []byte(`{"extra":1,`), 1)
			return map[string][]byte{headPath(cid): raw}
		}, "malformed"},
		{"the change file loses its last newline", func(g *fakeGit) map[string][]byte {
			return map[string][]byte{logPath(cid): bytes.TrimSuffix(g.file(logPath(cid)), []byte("\n"))}
		}, "newline"},
		{"the change file has a junk line", func(g *fakeGit) map[string][]byte {
			return map[string][]byte{logPath(cid): append(g.file(logPath(cid)), []byte("junk\n")...)}
		}, "malformed"},
		{"the effects file is edited", func(g *fakeGit) map[string][]byte {
			return map[string][]byte{effPath(cid): bytes.Replace(g.file(effPath(cid)), []byte("hi"), []byte("evil"), 1)}
		}, "effects"},
		{"the change directory is a file", func(*fakeGit) map[string][]byte {
			return map[string][]byte{"changes": []byte("x")}
		}, "layout"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, s := setup(t)
			g.writeFiles(c.edit(g))
			tip := g.tip()
			fx := []change.Effect{roomEffect(t, cid, 4, "b", `{"text":"next"}`)}

			if !strings.Contains(c.name, "effects file") {
				_, err := s.Load(ctx, cid)
				requireIntegrity(t, err, cid, c.reason)
			}
			_, err := s.Append(ctx, cid, 3, roomEvents(cid, 3, "room-4"), fx)
			requireIntegrity(t, err, cid, c.reason)
			require.Equal(t, tip, g.tip(), "a refused log gets no new commit")
		})
	}
}

func TestRefusalNamesTheCauseOfABrokenChain(t *testing.T) {
	ctx := t.Context()
	g := newFakeGit(t)
	s := g.store(t, testKey)
	cid := prChange(t, 7)
	seed(ctx, t, s, cid, "room-1", "room-2")
	g.writeFiles(map[string][]byte{logPath(cid): bytes.Replace(g.file(logPath(cid)), []byte("room-2"), []byte("room-9"), 1)})
	_, err := s.Load(t.Context(), cid)
	require.ErrorIs(t, err, change.ErrChainBroken)
	require.ErrorIs(t, err, ErrIntegrity)
}

func TestWrongKeyIsRefused(t *testing.T) {
	ctx := t.Context()
	g := newFakeGit(t)
	cid := prChange(t, 7)
	seed(ctx, t, g.store(t, testKey), cid, "room-1")
	wrong := g.store(t, otherKey)

	_, err := wrong.Load(t.Context(), cid)
	requireIntegrity(t, err, cid, "mac")
	_, err = wrong.Append(t.Context(), cid, 1, roomEvents(cid, 1, "room-2"), nil)
	requireIntegrity(t, err, cid, "mac")
}

// TestChangesAreRefusedOneByOne checks that a bad log of one change does not stop the others: the PR with the broken
// log is reset by the gate (state.reset, ACTION_MODE 2.7) and the rest of the ref stays usable.
func TestChangesAreRefusedOneByOne(t *testing.T) {
	ctx := t.Context()
	g := newFakeGit(t)
	s := g.store(t, testKey)
	bad, good := prChange(t, 1), prChange(t, 2)
	seed(ctx, t, s, bad, "a")
	seed(ctx, t, s, good, "b")
	g.writeFiles(map[string][]byte{headPath(bad): nil})

	_, err := s.Load(t.Context(), bad)
	require.ErrorIs(t, err, ErrIntegrity)
	got, err := s.Load(t.Context(), good)
	require.NoError(t, err)
	require.Len(t, got, 1)
	_, err = s.Append(t.Context(), good, 1, roomEvents(good, 1, "c"), nil)
	require.NoError(t, err)
}

// TestMACCompareIsConstantTime pins the one property a behavioural test cannot see: the MAC is compared with
// hmac.Equal and with no ordinary string or byte comparison.
func TestMACCompareIsConstantTime(t *testing.T) {
	src, err := os.ReadFile("logfile.go")
	require.NoError(t, err)
	text := string(src)
	require.Contains(t, text, "hmac.Equal(")
	for _, banned := range []string{"bytes.Equal(", "== h.MAC", "!= h.MAC", "subtle.", "strings.EqualFold("} {
		require.NotContains(t, text, banned)
	}
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	for _, f := range files {
		if f == "logfile.go" || strings.HasSuffix(f, "_test.go") {
			continue
		}
		other, err := os.ReadFile(f)
		require.NoError(t, err)
		require.NotContains(t, string(other), ".MAC", f+" must not compare a MAC")
	}
}

func TestSizeLimits(t *testing.T) {
	ctx := t.Context()
	cid := prChange(t, 7)

	t.Run("an event over 64 KiB is refused with a typed error and nothing is written", func(t *testing.T) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		_, err := s.Append(ctx, cid, 0, []change.Event{noteEvent(cid, 1, MaxEventBytes+1)}, nil)
		require.ErrorIs(t, err, ErrTooLarge)
		var le *LimitError
		require.ErrorAs(t, err, &le)
		require.Equal(t, int64(MaxEventBytes), le.Max)
		require.Zero(t, g.callCount("POST"))
	})

	t.Run("an event whose line passes 64 KiB with its envelope is refused", func(t *testing.T) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		_, err := s.Append(ctx, cid, 0, []change.Event{noteEvent(cid, 1, MaxEventBytes-100)}, nil)
		require.ErrorIs(t, err, ErrTooLarge)
		require.Zero(t, g.callCount("POST"))
	})

	t.Run("an event just under the limit is kept", func(t *testing.T) {
		s := newFakeGit(t).store(t, testKey)
		_, err := s.Append(ctx, cid, 0, []change.Event{noteEvent(cid, 1, MaxEventBytes-2048)}, nil)
		require.NoError(t, err)
		got, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Len(t, got, 1)
	})

	t.Run("a change file over 4 MiB is refused with a typed error and nothing is written", func(t *testing.T) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		evs := make([]change.Event, 70) // 70 events of about 62 KiB are over 4 MiB
		for i := range evs {
			evs[i] = noteEvent(cid, int64(i+1), MaxEventBytes-2048)
		}
		_, err := s.Append(ctx, cid, 0, evs, nil)
		require.ErrorIs(t, err, ErrTooLarge)
		var le *LimitError
		require.ErrorAs(t, err, &le)
		require.Equal(t, int64(MaxFileBytes), le.Max)
		require.Zero(t, g.callCount("POST"))
	})

	t.Run("a stored change file over 4 MiB is refused on read", func(t *testing.T) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		seed(ctx, t, s, cid, "room-1")
		g.writeFiles(map[string][]byte{logPath(cid): bytes.Repeat([]byte("x"), MaxFileBytes+1)})
		_, err := s.Load(ctx, cid)
		require.ErrorIs(t, err, ErrTooLarge)
	})

	t.Run("an effects file over 4 MiB is refused", func(t *testing.T) {
		s := newFakeGit(t).store(t, testKey)
		var fx []change.Effect
		for i := range 50 {
			fx = append(fx, roomEffect(t, cid, 1, fmt.Sprint("k", i), `{"text":"`+strings.Repeat("a", 90<<10)+`"}`))
		}
		_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-1"), fx)
		require.ErrorIs(t, err, ErrTooLarge)
	})
}

// TestReplyBounds checks that nothing from the API is trusted for its size.
func TestReplyBounds(t *testing.T) {
	ctx := t.Context()
	cid := prChange(t, 7)

	setup := func(t *testing.T) (*fakeGit, *Store) {
		g := newFakeGit(t)
		s := g.store(t, testKey)
		seed(ctx, t, s, cid, "room-1")
		return g, s
	}

	t.Run("a blob that states a huge size is refused before it is decoded", func(t *testing.T) {
		g, s := setup(t)
		huge := int64(1 << 40)
		g.blobSizeLie = &huge
		_, err := s.Load(ctx, cid)
		require.ErrorIs(t, err, ErrTooLarge)
	})

	t.Run("a blob whose size does not match its content is refused", func(t *testing.T) {
		g, s := setup(t)
		small := int64(3)
		g.blobSizeLie = &small
		_, err := s.Load(ctx, cid)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrIntegrity)
	})

	t.Run("a truncated tree listing is refused", func(t *testing.T) {
		g, s := setup(t)
		g.truncateTrees = true
		_, err := s.Load(ctx, cid)
		require.ErrorIs(t, err, ErrTooLarge)
	})

	t.Run("a reply body larger than the client's cap is refused", func(t *testing.T) {
		g, s := setup(t)
		g.intercept = func(w http.ResponseWriter, r *http.Request) bool {
			if !strings.Contains(r.URL.Path, "/git/ref/") {
				return false
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(bytes.Repeat([]byte(" "), 8<<20+10))
			return true
		}
		_, err := s.Load(ctx, cid)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrIntegrity)
	})

	t.Run("a reply with a bad object id or the wrong shape is refused", func(t *testing.T) {
		g, s := setup(t)
		for _, body := range []string{
			`{"object":{"sha":"zz","type":"commit"}}`,
			`{"object":{"sha":"` + strings.Repeat("a", 40) + `","type":"tag"}}`,
			`[{"object":{"sha":"` + strings.Repeat("a", 40) + `","type":"commit"}}]`,
			`not json`,
		} {
			g.intercept = func(w http.ResponseWriter, r *http.Request) bool {
				if !strings.Contains(r.URL.Path, "/git/ref/") {
					return false
				}
				_, _ = w.Write([]byte(body))
				return true
			}
			_, err := s.Load(ctx, cid)
			require.Error(t, err, body)
			require.NotErrorIs(t, err, ErrIntegrity, body)
		}
	})

	t.Run("a tree entry with a bad object id is refused", func(t *testing.T) {
		g, s := setup(t)
		g.intercept = func(w http.ResponseWriter, r *http.Request) bool {
			if !strings.Contains(r.URL.Path, "/git/trees/") {
				return false
			}
			reply(w, http.StatusOK, map[string]any{"tree": []map[string]string{{"path": "changes", "mode": "040000", "type": "tree", "sha": "../../etc"}}})
			return true
		}
		_, err := s.Load(ctx, cid)
		require.Error(t, err)
	})
}

// TestNoCredentialInErrorsOrPrints checks that neither the MAC key nor the token shows up in any error or any print.
func TestNoCredentialInErrorsOrPrints(t *testing.T) {
	ctx := t.Context()
	cid := prChange(t, 7)
	g := newFakeGit(t)
	s := g.store(t, testKey)
	seed(ctx, t, s, cid, "room-1")

	errs := map[string]error{}
	_, errs["conflict"] = s.Append(ctx, cid, 0, roomEvents(cid, 0, "x"), nil)
	_, errs["bad batch"] = s.Append(ctx, cid, 0, nil, nil)
	_, errs["wrong key"] = g.store(t, otherKey).Load(ctx, cid)
	_, errs["too large"] = s.Append(ctx, cid, 1, roomEvents(cid, 1, strings.Repeat("a", MaxEventBytes)), nil)

	badToken, err := New(Config{Repo: testRepo, Git: g.api(t, "ghs_WRONGTOKEN987654321"), Key: testKey, Wait: g.wait})
	require.NoError(t, err)
	_, errs["unauthorized"] = badToken.Load(ctx, cid)
	require.ErrorIs(t, errs["unauthorized"], ports.ErrUnauthorized)
	require.NotContains(t, errs["unauthorized"].Error(), "ghs_WRONGTOKEN987654321")

	g.intercept = func(w http.ResponseWriter, r *http.Request) bool {
		// A server that echoes the request headers back in its body must not reach our errors.
		reply(w, http.StatusInternalServerError, map[string]string{"echo": r.Header.Get("Authorization")})
		return true
	}
	_, errs["server error"] = s.Load(ctx, cid)
	require.ErrorIs(t, errs["server error"], ports.ErrUnavailable)

	for name, err := range errs {
		require.Error(t, err, name)
		requireNoSecrets(t, err.Error())
		require.NotContains(t, err.Error(), "Bearer", name)
		var ie *IntegrityError
		if errors.As(err, &ie) {
			requireNoSecrets(t, ie.Reason)
		}
	}

	cfg := Config{Repo: testRepo, Git: g.api(t, testToken), Key: testKey, Wait: g.wait}
	for _, v := range []any{s, cfg, &cfg} {
		requireNoSecrets(t, fmt.Sprintf("%v|%+v|%#v|%s", v, v, v, v))
	}
}
