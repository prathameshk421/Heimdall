package cursor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/sessions"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/cardgen"
)

// Agent is the name this reader gives its sessions.
const Agent = "cursor"

// UserDirs lists where Cursor keeps its "User" folder on macOS, Linux and Windows. The caller reads the ones that
// exist.
func UserDirs(home string) []string {
	return []string{
		filepath.Join(home, "Library", "Application Support", "Cursor", "User"),
		filepath.Join(home, ".config", "Cursor", "User"),
		filepath.Join(home, "AppData", "Roaming", "Cursor", "User"),
	}
}

// pathKeys are the tool parameters that name a file, in the order tried.
func pathKeys() []string {
	return []string{"relativeWorkspacePath", "targetFile", "file_path", "filePath", "path", "file"}
}

type composerHead struct {
	ID      string
	Dir     string // the workspace folder the chat was opened in; relative tool paths start here
	Updated time.Time
}

// Read returns the chats of the repository at root from the Cursor "User" folder userDir, newest first.
//
// Cursor splits its state. Each workspaceStorage/<hash>/ folder has a workspace.json naming the opened folder and a
// state.vscdb whose ItemTable key composer.composerData lists the chats opened there. The chat bodies live in
// globalStorage/state.vscdb, table cursorDiskKV: composerData:<id> holds the ordered bubble ids and
// bubbleId:<id>:<bubble> holds each message. A chat belongs to the repository when its workspace folder is the
// root, below it, or a git worktree of it. Local file folders only: remote workspaces are skipped.
//
// A store or chat that cannot be read is skipped and named in the returned error. The sessions returned are valid
// even when the error is not nil.
func Read(ctx context.Context, userDir, root string, lim sessions.Limits) ([]sessions.Session, error) {
	heads, errs := repoComposers(ctx, userDir, root, lim.Since)
	if len(heads) == 0 {
		return nil, errors.Join(errs...)
	}
	global, err := sessions.OpenReadOnly(ctx, filepath.Join(userDir, "globalStorage", "state.vscdb"))
	if err != nil {
		return nil, errors.Join(append(errs, err)...)
	}
	defer func() { _ = global.Close() }()
	var out []sessions.Session
	for _, h := range heads {
		if len(out) >= lim.MaxSessions {
			break
		}
		turns, err := readTurns(ctx, global, h, root)
		if err != nil {
			errs = append(errs, fmt.Errorf("read cursor chat %s: %w", h.ID, err))
			continue
		}
		if len(turns) > 0 {
			out = append(out, sessions.Session{Agent: Agent, ID: h.ID, Updated: h.Updated, Turns: turns})
		}
	}
	return out, errors.Join(errs...)
}

// repoComposers lists the chats of every workspace store of the repository, newest first, merged by id.
func repoComposers(ctx context.Context, userDir, root string, since time.Time) ([]composerHead, []error) {
	dirs, err := filepath.Glob(filepath.Join(userDir, "workspaceStorage", "*"))
	if err != nil {
		return nil, []error{fmt.Errorf("list workspace stores: %w", err)}
	}
	var errs []error
	match := sessions.NewMatcher(root)
	byID := map[string]composerHead{}
	for _, dir := range dirs {
		folder, ok := workspaceFolder(dir)
		if !ok || !match.Match(folder) {
			continue
		}
		heads, err := workspaceComposers(ctx, filepath.Join(dir, "state.vscdb"))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, h := range heads {
			h.Dir = folder
			if !h.Updated.Before(since) && h.Updated.After(byID[h.ID].Updated) {
				byID[h.ID] = h
			}
		}
	}
	out := make([]composerHead, 0, len(byID))
	for _, h := range byID {
		out = append(out, h)
	}
	slices.SortFunc(out, func(a, b composerHead) int {
		if c := b.Updated.Compare(a.Updated); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, errs
}

// workspaceFolder reads the folder a workspace store belongs to. Only file: URIs are local paths.
func workspaceFolder(dir string) (string, bool) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", false
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open("workspace.json")
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, 1<<16))
	if err != nil {
		return "", false
	}
	var w struct {
		Folder string `json:"folder"`
	}
	if json.Unmarshal(data, &w) != nil || w.Folder == "" {
		return "", false
	}
	u, err := url.Parse(w.Folder)
	if err != nil || u.Scheme != "file" || u.Path == "" {
		return "", false
	}
	return filepath.FromSlash(u.Path), true
}

// workspaceComposers reads composer.composerData of one workspace store. A store with no chat table, no such key
// or a changed shape yields nothing, because Cursor changes this file without notice.
func workspaceComposers(ctx context.Context, path string) ([]composerHead, error) {
	_, statErr := os.Stat(path)
	if errors.Is(statErr, fs.ErrNotExist) {
		return nil, nil // a workspace folder without state.vscdb has no chats
	}
	if statErr != nil {
		return nil, statErr
	}
	db, err := sessions.OpenReadOnly(ctx, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	var hasTable int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'ItemTable'`).Scan(&hasTable); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if hasTable == 0 {
		return nil, nil // a store that predates chats has no ItemTable
	}
	var raw []byte
	err = db.QueryRowContext(ctx, `SELECT CAST(value AS BLOB) FROM ItemTable WHERE key = 'composer.composerData'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return parseIndex(raw), nil
}

type indexEntry struct {
	ID        string  `json:"composerId"`
	CreatedAt float64 `json:"createdAt"`
	UpdatedAt float64 `json:"lastUpdatedAt"`
}

func parseIndex(raw []byte) []composerHead {
	var entries []indexEntry
	var wrapped struct {
		All []indexEntry `json:"allComposers"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.All != nil {
		entries = wrapped.All
	} else if json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	var out []composerHead
	for _, e := range entries {
		ms := e.UpdatedAt
		if ms == 0 {
			ms = e.CreatedAt
		}
		if e.ID != "" && ms > 0 {
			out = append(out, composerHead{ID: e.ID, Updated: time.UnixMilli(int64(ms))})
		}
	}
	return out
}

// readTurns builds the turns of one chat from the global store. The SQL extracts only the text and tool
// parameter fields, so tool results and file contents never reach memory.
func readTurns(ctx context.Context, db *sessions.DB, h composerHead, root string) ([]cardgen.Turn, error) {
	var raw []byte
	err := db.QueryRowContext(ctx, `SELECT CAST(value AS BLOB) FROM cursorDiskKV WHERE key = ?`, "composerData:"+h.ID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var data struct {
		Headers []struct {
			BubbleID string `json:"bubbleId"`
		} `json:"fullConversationHeadersOnly"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("composerData is not the expected JSON: %w", err)
	}
	b := &builder{id: h.ID, dir: h.Dir, root: root, fallback: sessions.DateOfMillis(h.Updated.UnixMilli())}
	for i, head := range data.Headers {
		if i >= sessions.MaxTurnsPerSession {
			break
		}
		msg, ok, err := bubbleAt(ctx, db, h.ID, head.BubbleID)
		if err != nil {
			return nil, err
		}
		if ok {
			b.add(msg)
		}
	}
	b.flush()
	return b.turns, nil
}

type bubbleRow struct {
	kind              int
	text, at          string
	params, rawParams string
}

func bubbleAt(ctx context.Context, db *sessions.DB, composer, bubble string) (bubbleRow, bool, error) {
	var m bubbleRow
	var kind sql.NullInt64
	var text, at, params, rawParams sql.NullString
	err := db.QueryRowContext(ctx, `SELECT json_extract(CAST(value AS TEXT), '$.type'), json_extract(CAST(value AS TEXT), '$.text'), json_extract(CAST(value AS TEXT), '$.createdAt'),
  json_extract(CAST(value AS TEXT), '$.toolFormerData.params'), json_extract(CAST(value AS TEXT), '$.toolFormerData.rawArgs')
FROM cursorDiskKV WHERE key = ?`, "bubbleId:"+composer+":"+bubble).Scan(&kind, &text, &at, &params, &rawParams)
	if errors.Is(err, sql.ErrNoRows) {
		return m, false, nil
	}
	if err != nil {
		// A bubble that is not JSON is one corrupt row, not a reason to drop the chat.
		if strings.Contains(err.Error(), "malformed JSON") {
			return m, false, nil
		}
		return m, false, err
	}
	return bubbleRow{kind: int(kind.Int64), text: text.String, at: at.String, params: params.String, rawParams: rawParams.String}, true, nil
}

// builder joins consecutive assistant bubbles into one turn, as the Claude Code reader does.
type builder struct {
	id, dir, root, fallback string
	turns                   []cardgen.Turn
	cur                     *cardgen.Turn
}

func (b *builder) flush() {
	if b.cur != nil && (b.cur.Text != "" || len(b.cur.Files) > 0) {
		b.turns = append(b.turns, *b.cur)
	}
	b.cur = nil
}

func (b *builder) add(m bubbleRow) {
	text := strings.TrimSpace(m.text)
	date := b.fallback
	if len(m.at) >= 10 {
		date = m.at[:10]
	}
	switch m.kind {
	case 1:
		if text == "" {
			return
		}
		b.flush()
		b.turns = append(b.turns, cardgen.Turn{Role: "user", Text: sessions.CapText(text), Session: b.id, Date: date})
	case 2:
		if b.cur == nil {
			b.cur = &cardgen.Turn{Role: "assistant", Session: b.id, Date: date}
		}
		b.cur.Text = sessions.CapText(strings.TrimSpace(b.cur.Text + "\n" + text))
		if rel := sessions.RepoRelative(toolFile(m), b.dir, b.root); rel != "" && !slices.Contains(b.cur.Files, rel) {
			b.cur.Files = append(b.cur.Files, rel)
		}
	}
}

// toolFile returns the file a tool call names, from its params or, when those are empty, its raw arguments.
func toolFile(m bubbleRow) string {
	for _, raw := range []string{m.params, m.rawParams} {
		var p map[string]any
		if raw == "" || json.Unmarshal([]byte(raw), &p) != nil {
			continue
		}
		for _, k := range pathKeys() {
			if s, ok := p[k].(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}
