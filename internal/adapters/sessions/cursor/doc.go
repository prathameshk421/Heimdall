// Package cursor reads Cursor chats from its state.vscdb SQLite files.
//
// # Purpose
//
// It turns the chats of one repository into cardgen turns for `heimdall cards scan`. It keeps user and assistant
// text and the file paths of tool calls. It never keeps tool results. Cursor is closed source and changes its
// store without notice, so a rule must never depend on this source alone.
//
// # Layout matched
//
// Public sources: the cursor-history project (github.com/S2thend/cursor-history, MIT, v0.18.0, src/core/storage.ts
// and src/core/parser.ts) and the Cursor forum threads linked in docs/product/research/09-context-packs.md.
// Folder User under ~/Library/Application Support/Cursor (macOS), ~/.config/Cursor (Linux) or
// ~/AppData/Roaming/Cursor (Windows).
//
//   - workspaceStorage/<hash>/workspace.json: {"folder": "file:///path"}. Only file: folders are read.
//   - workspaceStorage/<hash>/state.vscdb, table ItemTable, key composer.composerData: {"allComposers":
//     [{"composerId", "createdAt", "lastUpdatedAt"}]}, or the same list as a bare array.
//   - globalStorage/state.vscdb, table cursorDiskKV: composerData:<id> holds fullConversationHeadersOnly, the
//     ordered [{"bubbleId", "type"}]; bubbleId:<id>:<bubbleId> holds {"type": 1 user or 2 assistant, "text",
//     "createdAt", "toolFormerData": {"params" or "rawArgs"}}.
//
// Not read: multi-root .code-workspace files, remote workspaces, the Cursor CLI store.db agent files, and the
// older inline conversation format, because none was verified against a public source.
//
// # API
//
// Read(ctx, userDir, root, limits) returns chats newest first. UserDirs(home) lists the candidate folders.
//
// # Invariants
//
// A chat belongs to the repository when its workspace folder is the root, below it, or in a worktree of it. The
// same chat in two workspace stores is read once, with its newest update time. Relative tool paths start at the
// workspace folder of the chat. The SQL extracts only the needed fields.
//
// # Failure modes
//
// A store or chat that cannot be read is skipped and named in the returned error; the sessions returned are still
// valid. A store without ItemTable or without the composer key has no chats. A bubble that is not JSON is skipped.
// A missing global store is an error that wraps os.ErrNotExist.
//
// # Tests
//
// cursor_test.go builds synthetic workspace and global stores: text and file paths only, scope by folder and by
// worktree, remote folders, array and duplicate indexes, old chats, limits, empty stores, corrupt bubbles and
// stores, and proof that no file is changed.
package cursor
