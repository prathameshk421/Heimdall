package cursor_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/sessions"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/sessions/cursor"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/cardgen"
)

// Fixtures follow the layout cursor-history (MIT) documents for Cursor's state.vscdb files: a key-value table
// ItemTable per workspace with key composer.composerData, and a global cursorDiskKV table with composerData:<id>
// and bubbleId:<composer>:<bubble>. Every row here is invented.
var base = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

type store struct {
	user   string
	global *sql.DB
	n      int
}

func openKV(t *testing.T, path, table string) *sql.DB {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = ctxExec(db, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB)`, table))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newStore(t *testing.T) *store {
	t.Helper()
	user := filepath.Join(t.TempDir(), "Cursor", "User")
	return &store{user: user, global: openKV(t, filepath.Join(user, "globalStorage", "state.vscdb"), "cursorDiskKV")}
}

func put(t *testing.T, db *sql.DB, table, key string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	_, err = ctxExec(db, fmt.Sprintf(`INSERT INTO %s (key, value) VALUES (?, ?)`, table), key, data)
	require.NoError(t, err)
}

type bubble struct {
	kind   int
	text   string
	params string
	at     time.Time
}

// composer writes the global rows of one chat and returns its index entry.
func (s *store) composer(t *testing.T, id string, updated time.Time, bubbles ...bubble) map[string]any {
	t.Helper()
	var heads []map[string]any
	for _, b := range bubbles {
		s.n++
		bid := fmt.Sprintf("bub-%d", s.n)
		heads = append(heads, map[string]any{"bubbleId": bid, "type": b.kind})
		row := map[string]any{"bubbleId": bid, "type": b.kind, "text": b.text, "createdAt": b.at.Format("2006-01-02T15:04:05.000Z")}
		if b.params != "" {
			row["toolFormerData"] = map[string]any{"name": "edit_file_v2", "params": b.params, "result": "SECRET-TOOL-RESULT", "status": "completed"}
		}
		put(t, s.global, "cursorDiskKV", "bubbleId:"+id+":"+bid, row)
	}
	put(t, s.global, "cursorDiskKV", "composerData:"+id, map[string]any{"composerId": id, "fullConversationHeadersOnly": heads})
	return map[string]any{"composerId": id, "name": "chat " + id, "createdAt": updated.UnixMilli() - 5000, "lastUpdatedAt": updated.UnixMilli()}
}

// workspace registers a workspace folder and lists composers in its state.vscdb.
func (s *store) workspace(t *testing.T, hash, folderURI string, index any) {
	t.Helper()
	dir := filepath.Join(s.user, "workspaceStorage", hash)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	wj, err := json.Marshal(map[string]string{"folder": folderURI})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workspace.json"), wj, 0o600))
	db := openKV(t, filepath.Join(dir, "state.vscdb"), "ItemTable")
	put(t, db, "ItemTable", "composer.composerData", index)
	require.NoError(t, db.Close())
}

func fileURI(p string) string { return (&url.URL{Scheme: "file", Path: p}).String() }

func read(t *testing.T, s *store, root string, lim sessions.Limits) []sessions.Session {
	t.Helper()
	got, err := cursor.Read(context.Background(), s.user, root, lim)
	require.NoError(t, err)
	return got
}

var wide = sessions.Limits{Since: base.AddDate(0, 0, -30), MaxSessions: 50}

func TestReadKeepsTextAndFilePathsOnly(t *testing.T) {
	root := filepath.Join(t.TempDir(), "my repo") // a space exercises the URI decoding
	require.NoError(t, os.MkdirAll(root, 0o750))
	s := newStore(t)
	c := s.composer(t, "comp-1", base.Add(time.Hour),
		bubble{kind: 1, text: "Always run make fast before you commit.", at: base},
		bubble{kind: 2, text: "Sure.", at: base.Add(time.Minute)},
		bubble{kind: 2, text: "", params: `{"relativeWorkspacePath":"internal/a.go"}`, at: base.Add(2 * time.Minute)},
		bubble{kind: 2, text: "", params: `{"targetFile":"` + filepath.Join(root, "docs", "b.md") + `"}`, at: base.Add(3 * time.Minute)},
		bubble{kind: 2, text: "", params: `{"targetFile":"/etc/hosts"}`, at: base.Add(4 * time.Minute)},
		bubble{kind: 1, text: "That was wrong, use the Makefile.", at: base.Add(5 * time.Minute)},
		bubble{kind: 7, text: "unknown bubble type", at: base.Add(6 * time.Minute)},
	)
	s.workspace(t, "ws1", fileURI(root), map[string]any{"allComposers": []any{c}})

	got := read(t, s, root, wide)
	require.Len(t, got, 1)
	require.Equal(t, "cursor", got[0].Agent)
	require.Equal(t, "comp-1", got[0].ID)
	require.Equal(t, base.Add(time.Hour), got[0].Updated.UTC())
	require.Equal(t, []cardgen.Turn{
		{Role: "user", Text: "Always run make fast before you commit.", Session: "comp-1", Date: "2026-11-10"},
		{Role: "assistant", Text: "Sure.", Files: []string{"internal/a.go", "docs/b.md"}, Session: "comp-1", Date: "2026-11-10"},
		{Role: "user", Text: "That was wrong, use the Makefile.", Session: "comp-1", Date: "2026-11-10"},
	}, got[0].Turns)
	for _, turn := range got[0].Turns {
		require.NotContains(t, turn.Text, "SECRET")
	}
}

func TestReadScopesByWorkspaceFolderAndWorktree(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o750))
	wt := filepath.Join(parent, "repo-feature")
	admin := filepath.Join(root, ".git", "worktrees", "repo-feature")
	require.NoError(t, os.MkdirAll(admin, 0o750))
	require.NoError(t, os.MkdirAll(wt, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+admin+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(admin, "commondir"), []byte("../..\n"), 0o600))
	elsewhere := filepath.Join(parent, "elsewhere")
	require.NoError(t, os.MkdirAll(filepath.Join(elsewhere, ".git"), 0o750))

	s := newStore(t)
	say := func(id string) map[string]any {
		return s.composer(t, id, base, bubble{kind: 1, text: "hello " + id, at: base})
	}
	s.workspace(t, "a", fileURI(root), map[string]any{"allComposers": []any{say("c-root")}})
	s.workspace(t, "b", fileURI(wt), map[string]any{"allComposers": []any{say("c-wt")}})
	s.workspace(t, "c", fileURI(elsewhere), map[string]any{"allComposers": []any{say("c-other")}})
	s.workspace(t, "d", "vscode-remote://ssh-remote+host"+root, map[string]any{"allComposers": []any{say("c-remote")}})

	var ids []string
	for _, x := range read(t, s, root, wide) {
		ids = append(ids, x.ID)
	}
	require.ElementsMatch(t, []string{"c-root", "c-wt"}, ids)
}

func TestReadAcceptsABareComposerArrayAndMergesDuplicates(t *testing.T) {
	root := t.TempDir()
	s := newStore(t)
	c := s.composer(t, "c1", base, bubble{kind: 1, text: "one", at: base})
	s.workspace(t, "a", fileURI(root), []any{c})
	s.workspace(t, "b", fileURI(root), map[string]any{"allComposers": []any{c}})

	require.Len(t, read(t, s, root, wide), 1, "the same chat in two workspace stores is read once")
}

func TestReadSkipsChatsWithoutGlobalRowsAndOldChats(t *testing.T) {
	root := t.TempDir()
	s := newStore(t)
	fresh := s.composer(t, "fresh", base, bubble{kind: 1, text: "fresh", at: base})
	old := s.composer(t, "old", base.AddDate(0, 0, -90), bubble{kind: 1, text: "old", at: base.AddDate(0, 0, -90)})
	ghost := map[string]any{"composerId": "ghost", "lastUpdatedAt": base.UnixMilli()}
	s.workspace(t, "a", fileURI(root), map[string]any{"allComposers": []any{fresh, old, ghost}})

	got := read(t, s, root, wide)
	require.Len(t, got, 1)
	require.Equal(t, "fresh", got[0].ID)
}

func TestReadLimitNewestFirst(t *testing.T) {
	root := t.TempDir()
	s := newStore(t)
	var index []any
	for i := range 4 {
		index = append(index, s.composer(t, fmt.Sprintf("c%d", i), base.Add(time.Duration(i)*time.Hour), bubble{kind: 1, text: "x", at: base}))
	}
	s.workspace(t, "a", fileURI(root), map[string]any{"allComposers": index})

	got := read(t, s, root, sessions.Limits{Since: wide.Since, MaxSessions: 2})
	require.Len(t, got, 2)
	require.Equal(t, "c3", got[0].ID)
	require.Equal(t, "c2", got[1].ID)
}

func TestReadToleratesAWorkspaceStoreWithoutChats(t *testing.T) {
	root := t.TempDir()
	s := newStore(t)
	dir := filepath.Join(s.user, "workspaceStorage", "empty")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	wj, err := json.Marshal(map[string]string{"folder": fileURI(root)})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workspace.json"), wj, 0o600))
	db, err := sql.Open("sqlite", filepath.Join(dir, "state.vscdb"))
	require.NoError(t, err)
	_, err = ctxExec(db, `CREATE TABLE other (k TEXT)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.NoError(t, os.MkdirAll(filepath.Join(s.user, "workspaceStorage", "nofile"), 0o750))

	require.Empty(t, read(t, s, root, wide))
}

func TestReadNeverChangesTheStores(t *testing.T) {
	root := t.TempDir()
	s := newStore(t)
	c := s.composer(t, "c1", base, bubble{kind: 1, text: "one", at: base})
	s.workspace(t, "a", fileURI(root), map[string]any{"allComposers": []any{c}})
	require.NoError(t, s.global.Close())
	before := snapshot(t, s.user)

	_ = read(t, s, root, wide)

	require.Equal(t, before, snapshot(t, s.user), "no file was changed, added or removed")
}

func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		require.NoError(t, err)
		if d.Type().IsRegular() {
			data, err := os.ReadFile(p)
			require.NoError(t, err)
			out[p] = fmt.Sprintf("%d:%x", len(data), data)
		}
		return nil
	}))
	return out
}

func TestReadMissingGlobalStore(t *testing.T) {
	root := t.TempDir()
	user := t.TempDir()
	dir := filepath.Join(user, "workspaceStorage", "a")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	wj, err := json.Marshal(map[string]string{"folder": fileURI(root)})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workspace.json"), wj, 0o600))
	db := openKV(t, filepath.Join(dir, "state.vscdb"), "ItemTable")
	put(t, db, "ItemTable", "composer.composerData", map[string]any{"allComposers": []any{map[string]any{"composerId": "c1", "lastUpdatedAt": base.UnixMilli()}}})
	require.NoError(t, db.Close())

	_, err = cursor.Read(context.Background(), user, root, wide)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestUserDirs(t *testing.T) {
	require.Equal(t, []string{
		filepath.Join("/h", "Library", "Application Support", "Cursor", "User"),
		filepath.Join("/h", ".config", "Cursor", "User"),
		filepath.Join("/h", "AppData", "Roaming", "Cursor", "User"),
	}, cursor.UserDirs("/h"))
}

func TestReadSurvivesACorruptBubbleAndTextValues(t *testing.T) {
	root := t.TempDir()
	s := newStore(t)
	c := s.composer(t, "c1", base, bubble{kind: 1, text: "kept", at: base}, bubble{kind: 1, text: "will be corrupted", at: base})
	// One bubble is not JSON at all; another row stores its value as TEXT, not BLOB.
	_, err := ctxExec(s.global, `UPDATE cursorDiskKV SET value = 'not json {' WHERE key = 'bubbleId:c1:bub-2'`)
	require.NoError(t, err)
	_, err = ctxExec(s.global, `UPDATE cursorDiskKV SET value = CAST(value AS TEXT) WHERE key = 'bubbleId:c1:bub-1'`)
	require.NoError(t, err)
	s.workspace(t, "a", fileURI(root), map[string]any{"allComposers": []any{c}})

	got := read(t, s, root, wide)
	require.Len(t, got, 1)
	require.Len(t, got[0].Turns, 1)
	require.Equal(t, "kept", got[0].Turns[0].Text)
}

func TestReadReturnsGoodChatsAndTheErrorOfABrokenStore(t *testing.T) {
	root := t.TempDir()
	s := newStore(t)
	c := s.composer(t, "c1", base, bubble{kind: 1, text: "fine", at: base})
	s.workspace(t, "good", fileURI(root), map[string]any{"allComposers": []any{c}})
	broken := filepath.Join(s.user, "workspaceStorage", "broken")
	require.NoError(t, os.MkdirAll(broken, 0o750))
	wj, err := json.Marshal(map[string]string{"folder": fileURI(root)})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(broken, "workspace.json"), wj, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(broken, "state.vscdb"), []byte("this is not a sqlite file, only text"), 0o600))

	got, err := cursor.Read(context.Background(), s.user, root, wide)
	require.Error(t, err, "the broken store is reported")
	require.Len(t, got, 1, "the good chat is still returned")
}

func ctxExec(db *sql.DB, q string, args ...any) (sql.Result, error) {
	return db.ExecContext(context.Background(), q, args...)
}
