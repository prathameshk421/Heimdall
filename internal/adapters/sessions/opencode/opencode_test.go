package opencode_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/sessions"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/sessions/opencode"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/cardgen"
)

// The tables below have the columns of OpenCode v1.19.6 (packages/core/src/session/sql.ts) that the reader uses
// and a few it ignores. Rows are invented; no real session data is here.
const schema = `
CREATE TABLE session (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, parent_id TEXT, slug TEXT NOT NULL,
  directory TEXT NOT NULL, title TEXT NOT NULL, version TEXT NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL);
CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL);
CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT NOT NULL, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL);
`

var base = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

type fixture struct {
	db  *sql.DB
	n   int
	dir string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "opencode.db"))
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = ctxExec(db, schema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return &fixture{db: db, dir: dir}
}

func (f *fixture) path() string { return filepath.Join(f.dir, "opencode.db") }

func (f *fixture) session(t *testing.T, id, parent, dir string, updated time.Time) {
	t.Helper()
	_, err := ctxExec(f.db, `INSERT INTO session (id, project_id, parent_id, slug, directory, title, version, time_created, time_updated) VALUES (?,?,?,?,?,?,?,?,?)`,
		id, "prj", nullable(parent), "slug-"+id, dir, "title", "1.19.6", updated.UnixMilli()-1000, updated.UnixMilli())
	require.NoError(t, err)
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (f *fixture) message(t *testing.T, session, role string, at time.Time, parts ...map[string]any) {
	t.Helper()
	f.n++
	id := fmt.Sprintf("msg_%04d", f.n)
	data, err := json.Marshal(map[string]any{"role": role, "time": map[string]any{"created": at.UnixMilli()}})
	require.NoError(t, err)
	_, err = ctxExec(f.db, `INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES (?,?,?,?,?)`, id, session, at.UnixMilli(), at.UnixMilli(), string(data))
	require.NoError(t, err)
	for i, p := range parts {
		pd, err := json.Marshal(p)
		require.NoError(t, err)
		_, err = ctxExec(f.db, `INSERT INTO part (id, message_id, session_id, time_created, time_updated, data) VALUES (?,?,?,?,?,?)`,
			fmt.Sprintf("prt_%04d_%02d", f.n, i), id, session, at.UnixMilli(), at.UnixMilli(), string(pd))
		require.NoError(t, err)
	}
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

func tool(name, file string) map[string]any {
	return map[string]any{"type": "tool", "tool": name, "callID": "c1", "state": map[string]any{
		"status": "completed", "input": map[string]any{"filePath": file}, "output": "SECRET-TOOL-OUTPUT", "title": "t",
	}}
}

func read(t *testing.T, f *fixture, root string) []sessions.Session {
	t.Helper()
	got, err := opencode.Read(context.Background(), f.path(), root, sessions.Limits{Since: base.AddDate(0, 0, -30), MaxSessions: 50})
	require.NoError(t, err)
	return got
}

func TestReadKeepsTextAndFilePathsOnly(t *testing.T) {
	root := t.TempDir()
	f := newFixture(t)
	f.session(t, "ses_1", "", root, base.Add(time.Hour))
	f.message(t, "ses_1", "user", base, text("Never edit generated files by hand."))
	f.message(t, "ses_1", "assistant", base.Add(time.Minute),
		map[string]any{"type": "reasoning", "text": "SECRET-THINKING"},
		text("I will edit it."),
		tool("edit", filepath.Join(root, "internal", "a.go")),
		tool("read", "/etc/hosts"),
		map[string]any{"type": "step-finish", "reason": "stop"})
	f.message(t, "ses_1", "assistant", base.Add(2*time.Minute), text("Done."), tool("write", "docs/b.md"))
	f.message(t, "ses_1", "user", base.Add(3*time.Minute), text("No, use the generator."))

	got := read(t, f, root)
	require.Len(t, got, 1)
	require.Equal(t, "opencode", got[0].Agent)
	require.Equal(t, "ses_1", got[0].ID)
	require.Equal(t, base.Add(time.Hour), got[0].Updated.UTC())
	require.Equal(t, []cardgen.Turn{
		{Role: "user", Text: "Never edit generated files by hand.", Session: "ses_1", Date: "2026-11-10"},
		{Role: "assistant", Text: "I will edit it.\nDone.", Files: []string{"internal/a.go", "docs/b.md"}, Session: "ses_1", Date: "2026-11-10"},
		{Role: "user", Text: "No, use the generator.", Session: "ses_1", Date: "2026-11-10"},
	}, got[0].Turns)
	for _, turn := range got[0].Turns {
		require.NotContains(t, turn.Text, "SECRET")
	}
}

func TestReadSkipsSyntheticAndIgnoredParts(t *testing.T) {
	root := t.TempDir()
	f := newFixture(t)
	f.session(t, "ses_1", "", root, base)
	f.message(t, "ses_1", "user", base,
		map[string]any{"type": "text", "text": "Called the Read tool with input {...} and file body", "synthetic": true},
		map[string]any{"type": "text", "text": "hidden from the model", "ignored": true},
		text("Please fix the build."))

	got := read(t, f, root)
	require.Len(t, got, 1)
	require.Len(t, got[0].Turns, 1)
	require.Equal(t, "Please fix the build.", got[0].Turns[0].Text)
}

func TestReadScopesSessionsToTheRepository(t *testing.T) {
	base2 := t.TempDir()
	root := filepath.Join(base2, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o750))
	wt := filepath.Join(base2, "repo-feature")
	admin := filepath.Join(root, ".git", "worktrees", "repo-feature")
	require.NoError(t, os.MkdirAll(admin, 0o750))
	require.NoError(t, os.MkdirAll(wt, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+admin+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(admin, "commondir"), []byte("../..\n"), 0o600))
	elsewhere := filepath.Join(base2, "elsewhere")
	require.NoError(t, os.MkdirAll(filepath.Join(elsewhere, ".git"), 0o750))

	f := newFixture(t)
	for id, dir := range map[string]string{"ses_root": root, "ses_wt": wt, "ses_other": elsewhere, "ses_empty": ""} {
		f.session(t, id, "", dir, base)
		f.message(t, id, "user", base, text("hello from "+id))
	}

	got := read(t, f, root)
	ids := []string{}
	for _, s := range got {
		ids = append(ids, s.ID)
	}
	require.ElementsMatch(t, []string{"ses_root", "ses_wt"}, ids)
}

func TestReadMarksSubagentSessionsAsSidechain(t *testing.T) {
	root := t.TempDir()
	f := newFixture(t)
	f.session(t, "ses_parent", "", root, base)
	f.session(t, "ses_child", "ses_parent", root, base)
	f.message(t, "ses_child", "user", base, text("sub task"))

	for _, s := range read(t, f, root) {
		for _, turn := range s.Turns {
			require.Equal(t, s.ID == "ses_child", turn.Sidechain, s.ID)
		}
	}
}

func TestReadLimits(t *testing.T) {
	root := t.TempDir()
	f := newFixture(t)
	f.session(t, "ses_old", "", root, base.AddDate(0, 0, -90))
	f.message(t, "ses_old", "user", base.AddDate(0, 0, -90), text("old"))
	for i := range 3 {
		id := fmt.Sprintf("ses_%d", i)
		f.session(t, id, "", root, base.Add(time.Duration(i)*time.Hour))
		f.message(t, id, "user", base, text("hello "+id))
	}
	f.session(t, "ses_silent", "", root, base.Add(10*time.Hour)) // no messages: nothing to keep

	got, err := opencode.Read(context.Background(), f.path(), root, sessions.Limits{Since: base.AddDate(0, 0, -30), MaxSessions: 2})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "ses_2", got[0].ID, "newest first, empty sessions skipped, old ones cut")
	require.Equal(t, "ses_1", got[1].ID)
}

func TestReadCapsTurnText(t *testing.T) {
	root := t.TempDir()
	f := newFixture(t)
	f.session(t, "ses_1", "", root, base)
	long := make([]byte, sessions.MaxTurnChars*3)
	for i := range long {
		long[i] = 'x'
	}
	f.message(t, "ses_1", "user", base, text(string(long)))
	require.LessOrEqual(t, len(read(t, f, root)[0].Turns[0].Text), sessions.MaxTurnChars)
}

func TestReadFromALiveDatabase(t *testing.T) {
	root := t.TempDir()
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=wal_autocheckpoint(0)")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = ctxExec(db, schema)
	require.NoError(t, err)
	f := &fixture{db: db, dir: dir}
	f.session(t, "ses_live", "", root, base)
	f.message(t, "ses_live", "user", base, text("typed a second ago"))

	got, err := opencode.Read(context.Background(), path, root, sessions.Limits{Since: base.AddDate(0, 0, -1), MaxSessions: 5})
	require.NoError(t, err)
	require.Len(t, got, 1, "rows that live only in the wal file are read")
}

func TestReadRejectsAnUnknownSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = ctxExec(db, `CREATE TABLE session (id TEXT); CREATE TABLE message (id TEXT)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = opencode.Read(context.Background(), path, t.TempDir(), sessions.Limits{Since: base, MaxSessions: 5})
	require.ErrorIs(t, err, opencode.ErrUnsupportedSchema)
}

func TestReadMissingDatabase(t *testing.T) {
	_, err := opencode.Read(context.Background(), filepath.Join(t.TempDir(), "none.db"), t.TempDir(), sessions.Limits{Since: base, MaxSessions: 5})
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestDefaultPath(t *testing.T) {
	require.Equal(t, filepath.Join("/home/dev", ".local", "share", "opencode", "opencode.db"), opencode.DefaultPath("/home/dev"))
}

func ctxExec(db *sql.DB, q string, args ...any) (sql.Result, error) {
	return db.ExecContext(context.Background(), q, args...)
}
