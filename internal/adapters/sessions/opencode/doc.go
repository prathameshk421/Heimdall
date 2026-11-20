// Package opencode reads OpenCode sessions from its SQLite database.
//
// # Purpose
//
// It turns the sessions of one repository into cardgen turns for `heimdall cards scan`. It keeps user and
// assistant text and the file paths of tool calls. It never keeps tool output, reasoning, synthetic text parts
// (the text OpenCode injects for tool calls) or ignored parts.
//
// # Schema matched
//
// OpenCode v1.19.6 (tag read on GitHub, anomalyco/opencode): packages/core/src/session/sql.ts. Tables
// `session` (id, parent_id, directory, time_updated in ms), `message` (id, session_id, time_created, data JSON
// with role) and `part` (id, message_id, session_id, data JSON). The part JSON is defined in
// packages/schema/src/v1/session.ts: type "text" has text, synthetic and ignored; type "tool" has
// state.input with the tool arguments (filePath for the file tools). The default path is
// ~/.local/share/opencode/opencode.db. Newer OpenCode source also has session_v2 tables; this reader does not
// use them, and reads the v1 tables that v1.19.6 still writes.
//
// # API
//
// Read(ctx, dbPath, root, limits) returns sessions newest first. DefaultPath(home) names the database.
// ErrUnsupportedSchema means a table or column is missing; the error names it.
//
// # Invariants
//
// The SQL extracts only the needed JSON fields, so tool output never reaches memory. Consecutive assistant
// messages form one turn, as in the Claude Code reader. A session with a parent_id is a sub-agent session and its
// turns have Sidechain set. A session whose directory is outside the repository and its worktrees is skipped.
//
// # Failure modes
//
// The OPENCODE_DB override of OpenCode is not read, because the environment may only be read in
// internal/platform/config. A changed schema fails with ErrUnsupportedSchema, not with partial data.
//
// # Tests
//
// opencode_test.go builds a synthetic database in the v1.19.6 shape: text and file paths only, synthetic parts,
// repository and worktree scope, sub-agent sessions, limits, a live wal database and an unknown schema.
package opencode
