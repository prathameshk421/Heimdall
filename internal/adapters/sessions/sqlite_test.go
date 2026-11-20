package sessions_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/sessions"
)

func newDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func count(t *testing.T, db *sessions.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT count(*) FROM t`).Scan(&n))
	return n
}

func fileHash(t *testing.T, path string) [32]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return sha256.Sum256(data)
}

func TestOpenReadOnlyLeavesTheFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	w := newDB(t, path)
	_, err := ctxExec(w, `CREATE TABLE t (v TEXT); INSERT INTO t VALUES ('a'), ('b')`)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	before := fileHash(t, path)
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)

	db, err := sessions.OpenReadOnly(context.Background(), path)
	require.NoError(t, err)
	require.Equal(t, 2, count(t, db))
	_, err = db.ExecContext(context.Background(), `INSERT INTO t VALUES ('c')`)
	require.Error(t, err, "the handle must refuse writes")
	require.NoError(t, db.Close())

	require.Equal(t, before, fileHash(t, path), "the file bytes did not change")
	after, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, after, len(entries), "no journal, wal or shm file was left beside the database")
}

func TestOpenReadOnlySeesTheWriteAheadLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live.db")
	w := newDB(t, "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=wal_autocheckpoint(0)")
	_, err := ctxExec(w, `CREATE TABLE t (v TEXT); INSERT INTO t VALUES ('a')`)
	require.NoError(t, err)
	// The writer stays open, so the newest rows live only in the -wal file.
	info, err := os.Stat(path + "-wal")
	require.NoError(t, err)
	require.Positive(t, info.Size())
	before := fileHash(t, path)
	walBefore := fileHash(t, path+"-wal")

	db, err := sessions.OpenReadOnly(context.Background(), path)
	require.NoError(t, err)
	require.Equal(t, 1, count(t, db), "a row that is only in the wal file is read")
	require.NoError(t, db.Close())

	require.Equal(t, before, fileHash(t, path))
	require.Equal(t, walBefore, fileHash(t, path+"-wal"), "the live wal file is untouched")
	_, err = ctxExec(w, `INSERT INTO t VALUES ('b')`)
	require.NoError(t, err, "the real writer still works after our read")
}

func TestOpenReadOnlyCleansTheCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live.db")
	w := newDB(t, "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=wal_autocheckpoint(0)")
	_, err := ctxExec(w, `CREATE TABLE t (v TEXT); INSERT INTO t VALUES ('a')`)
	require.NoError(t, err)

	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	db, err := sessions.OpenReadOnly(context.Background(), path)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	left, err := os.ReadDir(tmp)
	require.NoError(t, err)
	require.Empty(t, left, "the scratch copy is removed on close")
}

func TestOpenReadOnlyRefusesAHugeLiveDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.db")
	w := newDB(t, path)
	_, err := ctxExec(w, `CREATE TABLE t (v TEXT)`)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	// A sparse log file takes no disk space but reports 5 GiB.
	require.NoError(t, os.WriteFile(path+"-wal", []byte("x"), 0o600))
	require.NoError(t, os.Truncate(path+"-wal", 5<<30))

	_, err = sessions.OpenReadOnly(context.Background(), path)
	require.ErrorIs(t, err, sessions.ErrTooLargeToCopy)
}

func TestOpenReadOnlyFollowsASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real", "app.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o750))
	w := newDB(t, target)
	_, err := ctxExec(w, `CREATE TABLE t (v TEXT); INSERT INTO t VALUES ('a')`)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	link := filepath.Join(dir, "link.db")
	require.NoError(t, os.Symlink(target, link))

	db, err := sessions.OpenReadOnly(context.Background(), link)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.Equal(t, 1, count(t, db))
}

func TestOpenReadOnlyErrors(t *testing.T) {
	_, err := sessions.OpenReadOnly(context.Background(), filepath.Join(t.TempDir(), "missing.db"))
	require.ErrorIs(t, err, os.ErrNotExist)

	notDB := filepath.Join(t.TempDir(), "x.db")
	require.NoError(t, os.WriteFile(notDB, []byte("this is not sqlite at all, only text"), 0o600))
	db, err := sessions.OpenReadOnly(context.Background(), notDB)
	if err == nil {
		defer func() { _ = db.Close() }()
		_, err = db.QueryContext(context.Background(), `SELECT 1 FROM sqlite_master`)
	}
	require.Error(t, err)
}

func ctxExec(db *sql.DB, q string) (sql.Result, error) {
	return db.ExecContext(context.Background(), q)
}
