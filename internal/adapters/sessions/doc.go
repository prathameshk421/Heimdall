// Package sessions holds what the OpenCode and Cursor session readers share: the Session result type, a
// read-only SQLite opener, and a repository matcher that knows git worktrees.
//
// # Purpose
//
// `heimdall cards scan` mines the developer's past agent sessions for rule cards. Claude Code and Codex keep
// JSONL files and are read in cmd/heimdall. OpenCode and Cursor keep SQLite databases. This package and its
// sub-packages read those, and return the same turns (cardgen.Turn) as the JSONL readers. It does not redact, it
// does not change the card format, and it never writes to an agent's files.
//
// # API
//
// Session and Limits are the result and the bounds of every reader. OpenReadOnly opens an agent database.
// NewMatcher(root).Match(dir) says whether a session directory belongs to the repository. RepoRelative, CapText
// and DateOfMillis are small helpers with the same rules as the JSONL readers. CommonGitDir and UnderRoot are the
// building blocks of the matcher.
//
// # Invariants
//
// A database is never opened for writing. With no live write-ahead log it is opened in place with mode=ro and
// immutable=1, so SQLite writes no journal, wal or shm file. With a live log (an agent is running) the database
// and its log are copied into a private temporary folder, the copy is read, and the folder is removed on Close.
// Copies over 4 GiB are refused with ErrTooLargeToCopy.
//
// A directory belongs to the repository when it lies under the root, or when its git common dir equals the one of
// the root. The common dir is found through the .git pointer file and its commondir file, so a linked worktree
// outside the root matches. A directory that does not exist matches only by path, never by guess.
//
// Pointer files are read through os.Root with a 4 KiB bound. The driver is modernc.org/sqlite, which needs no cgo,
// so CGO_ENABLED=0 builds keep working.
//
// # Failure modes
//
// A missing file returns an error that wraps os.ErrNotExist. A file that is not SQLite returns an error from the
// first query. Readers skip one broken session and report it in the returned error; the sessions they return are
// still valid.
//
// # Tests
//
// repo_test.go builds worktree layouts by hand. sqlite_test.go proves a read leaves the file bytes, the live wal
// file and the folder unchanged, and that rows only in the wal file are seen. Fixtures are built in each test.
// No test reads a real home directory.
package sessions
