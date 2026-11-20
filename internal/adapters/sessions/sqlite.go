package sessions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	// The pure Go driver keeps the build CGO-free.
	_ "modernc.org/sqlite"
)

// maxCopyBytes bounds the copy made when an agent has the database open. Larger files are refused, not read half.
const maxCopyBytes = 4 << 30

// ErrTooLargeToCopy means the database has a live write-ahead log and is too big to copy safely.
var ErrTooLargeToCopy = errors.New("database is in use and too large to copy")

// DB is a read-only handle on an agent's SQLite file. Close it to release a scratch copy, if one was made.
type DB struct {
	*sql.DB
	dir string
}

// Close closes the handle and removes the scratch copy.
func (d *DB) Close() error {
	err := d.DB.Close()
	if d.dir != "" {
		err = errors.Join(err, os.RemoveAll(d.dir))
	}
	return err
}

// OpenReadOnly opens an agent database without any chance of changing it.
//
// A database nobody is writing is opened in place with mode=ro and immutable=1, so SQLite takes no lock and
// writes no journal, wal or shm file. When a non-empty -wal file exists, an agent is running and the newest rows
// live only in that log. An immutable open would hide them, and a normal open would join the agent's locking.
// So the database and its log are copied into a private temporary folder and the copy is opened. The original
// is only read.
func OpenReadOnly(ctx context.Context, path string) (*DB, error) {
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("open %s: not a regular file", path)
	}
	wal, err := os.Stat(path + "-wal")
	if err != nil || wal.Size() == 0 {
		return openInPlace(ctx, path)
	}
	if info.Size()+wal.Size() > maxCopyBytes {
		return nil, fmt.Errorf("open %s: %w", path, ErrTooLargeToCopy)
	}
	return openCopy(ctx, path)
}

func openInPlace(ctx context.Context, path string) (*DB, error) {
	u := url.URL{Scheme: "file", Path: path, RawQuery: url.Values{"mode": {"ro"}, "immutable": {"1"}}.Encode()}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return ping(ctx, &DB{DB: db}, path)
}

func openCopy(ctx context.Context, path string) (*DB, error) {
	dir, err := os.MkdirTemp("", "heimdall-sessions-")
	if err != nil {
		return nil, fmt.Errorf("make scratch folder: %w", err)
	}
	dst := filepath.Join(dir, "copy.db")
	for _, suffix := range []string{"", "-wal"} {
		if err := copyFile(path+suffix, dst+suffix); err != nil {
			_ = os.RemoveAll(dir)
			return nil, fmt.Errorf("copy %s: %w", path+suffix, err)
		}
	}
	// The copy is ours, so it may use the log. query_only still stops any write.
	u := url.URL{Scheme: "file", Path: dst, RawQuery: url.Values{"_pragma": {"query_only(1)"}}.Encode()}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("open copy of %s: %w", path, err)
	}
	return ping(ctx, &DB{DB: db, dir: dir}, path)
}

func ping(ctx context.Context, d *DB, path string) (*DB, error) {
	d.SetMaxOpenConns(1)
	var n int
	if err := d.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master`).Scan(&n); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return d, nil
}

// copyFile copies the file src to dst, both given as a folder and a name. Roots keep each side inside its folder.
func copyFile(src, dst string) (err error) {
	in, err := openIn(filepath.Dir(src), filepath.Base(src), os.O_RDONLY)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := openIn(filepath.Dir(dst), filepath.Base(dst), os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, out.Close()) }()
	_, err = io.Copy(out, in)
	return err
}

func openIn(dir, name string, flag int) (*os.File, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.OpenFile(name, flag, 0o600)
}
