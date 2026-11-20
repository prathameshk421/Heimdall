package opencode

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/sessions"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/cardgen"
)

// Agent is the name this reader gives its sessions.
const Agent = "opencode"

// maxPartRows bounds the text and tool rows read from one session.
const maxPartRows = 50000

// ErrUnsupportedSchema means the database lacks a table or column this reader needs.
var ErrUnsupportedSchema = errors.New("unsupported opencode database schema")

// DefaultPath is where OpenCode keeps its database: XDG data home, on every platform OpenCode supports.
func DefaultPath(home string) string {
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db")
}

// required lists, per table, the columns the reader selects.
func required() map[string][]string {
	return map[string][]string{
		"session": {"id", "parent_id", "directory", "time_updated"},
		"message": {"id", "session_id", "time_created", "data"},
		"part":    {"id", "message_id", "session_id", "data"},
	}
}

type sessionRow struct {
	id, parent, dir string
	updated         int64
}

// Read returns the sessions of the repository at root from the OpenCode database at dbPath, newest first.
// A session belongs to the repository when its directory is the root, below it, or in a git worktree of it.
// Sessions without a user or assistant text are left out.
//
// A session that cannot be read is skipped and named in the returned error. The sessions returned are valid even
// when the error is not nil. A database that cannot be opened or has an unknown schema returns no sessions.
func Read(ctx context.Context, dbPath, root string, lim sessions.Limits) ([]sessions.Session, error) {
	db, err := sessions.OpenReadOnly(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	if err := checkSchema(ctx, db); err != nil {
		return nil, err
	}
	rows, err := candidates(ctx, db, lim.Since)
	if err != nil {
		return nil, err
	}
	match := sessions.NewMatcher(root)
	var out []sessions.Session
	var errs []error
	for _, r := range rows {
		if len(out) >= lim.MaxSessions {
			break
		}
		if !match.Match(r.dir) {
			continue
		}
		turns, err := readTurns(ctx, db, r, root)
		if err != nil {
			errs = append(errs, fmt.Errorf("read opencode session %s: %w", r.id, err))
			continue
		}
		if len(turns) == 0 {
			continue
		}
		out = append(out, sessions.Session{Agent: Agent, ID: r.id, Updated: time.UnixMilli(r.updated), Turns: turns})
	}
	return out, errors.Join(errs...)
}

func checkSchema(ctx context.Context, db *sessions.DB) error {
	for table, cols := range required() {
		have, err := columns(ctx, db, table)
		if err != nil {
			return err
		}
		for _, c := range cols {
			if !slices.Contains(have, c) {
				return fmt.Errorf("%w: table %q has no column %q", ErrUnsupportedSchema, table, c)
			}
		}
	}
	return nil
}

func columns(ctx context.Context, db *sessions.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, fmt.Errorf("inspect table %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("inspect table %s: %w", table, err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect table %s: %w", table, err)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%w: no table %q", ErrUnsupportedSchema, table)
	}
	return names, nil
}

func candidates(ctx context.Context, db *sessions.DB, since time.Time) ([]sessionRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, COALESCE(parent_id, ''), COALESCE(directory, ''), time_updated FROM session WHERE time_updated >= ? ORDER BY time_updated DESC, id`,
		since.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("list opencode sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []sessionRow
	for rows.Next() {
		var r sessionRow
		if err := rows.Scan(&r.id, &r.parent, &r.dir, &r.updated); err != nil {
			return nil, fmt.Errorf("list opencode sessions: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type messageRow struct {
	id, role string
	created  int64
}

type partRow struct {
	messageID, kind, text, file string
	skip                        bool
}

// readTurns builds the turns of one session. Only text parts and the file paths of tool calls are read: the SQL
// extracts those fields, so tool output, reasoning and file contents never reach memory.
func readTurns(ctx context.Context, db *sessions.DB, s sessionRow, root string) ([]cardgen.Turn, error) {
	msgs, err := messages(ctx, db, s.id)
	if err != nil {
		return nil, err
	}
	parts, err := parts(ctx, db, s.id)
	if err != nil {
		return nil, err
	}
	b := &builder{session: s, root: root}
	for _, m := range msgs {
		b.add(m, parts[m.id])
	}
	b.flush()
	return b.turns, nil
}

func messages(ctx context.Context, db *sessions.DB, session string) ([]messageRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, COALESCE(json_extract(data, '$.role'), ''), time_created FROM message WHERE session_id = ? ORDER BY time_created, id LIMIT ?`,
		session, sessions.MaxTurnsPerSession)
	if err != nil {
		return nil, fmt.Errorf("read messages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []messageRow
	for rows.Next() {
		var m messageRow
		if err := rows.Scan(&m.id, &m.role, &m.created); err != nil {
			return nil, fmt.Errorf("read messages: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func parts(ctx context.Context, db *sessions.DB, session string) (map[string][]partRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT message_id,
  json_extract(data, '$.type'),
  COALESCE(json_extract(data, '$.text'), ''),
  COALESCE(json_extract(data, '$.synthetic'), 0) OR COALESCE(json_extract(data, '$.ignored'), 0),
  COALESCE(json_extract(data, '$.state.input.filePath'), json_extract(data, '$.state.input.path'), json_extract(data, '$.state.input.file_path'), '')
FROM part WHERE session_id = ? AND json_extract(data, '$.type') IN ('text', 'tool') ORDER BY message_id, id LIMIT ?`,
		session, maxPartRows)
	if err != nil {
		return nil, fmt.Errorf("read parts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]partRow{}
	for rows.Next() {
		var p partRow
		if err := rows.Scan(&p.messageID, &p.kind, &p.text, &p.skip, &p.file); err != nil {
			return nil, fmt.Errorf("read parts: %w", err)
		}
		out[p.messageID] = append(out[p.messageID], p)
	}
	return out, rows.Err()
}

// builder joins consecutive assistant messages into one turn, as the Claude Code reader does, so the files of a
// whole tool loop belong to the turn the developer answers next.
type builder struct {
	session sessionRow
	root    string
	turns   []cardgen.Turn
	cur     *cardgen.Turn
}

func (b *builder) flush() {
	if b.cur != nil && (b.cur.Text != "" || len(b.cur.Files) > 0) {
		b.turns = append(b.turns, *b.cur)
	}
	b.cur = nil
}

func (b *builder) add(m messageRow, ps []partRow) {
	if m.role != "user" && m.role != "assistant" {
		return
	}
	text, files := b.textAndFiles(ps)
	date := sessions.DateOfMillis(m.created)
	sidechain := b.session.parent != ""
	if m.role == "user" {
		if text == "" {
			return
		}
		b.flush()
		b.turns = append(b.turns, cardgen.Turn{Role: "user", Text: sessions.CapText(text), Session: b.session.id, Date: date, Sidechain: sidechain})
		return
	}
	if b.cur == nil {
		b.cur = &cardgen.Turn{Role: "assistant", Session: b.session.id, Date: date, Sidechain: sidechain}
	}
	b.cur.Text = sessions.CapText(strings.TrimSpace(b.cur.Text + "\n" + text))
	for _, f := range files {
		if !slices.Contains(b.cur.Files, f) {
			b.cur.Files = append(b.cur.Files, f)
		}
	}
}

// textAndFiles returns the visible text of a message and the repository relative files its tool calls name.
func (b *builder) textAndFiles(ps []partRow) (string, []string) {
	var texts, files []string
	for _, p := range ps {
		switch {
		case p.kind == "text" && !p.skip && strings.TrimSpace(p.text) != "":
			texts = append(texts, strings.TrimSpace(p.text))
		case p.kind == "tool":
			if rel := sessions.RepoRelative(p.file, b.session.dir, b.root); rel != "" && !slices.Contains(files, rel) {
				files = append(files, rel)
			}
		}
	}
	return strings.Join(texts, "\n"), files
}
