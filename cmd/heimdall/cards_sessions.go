package main

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/cardgen"
)

// Bounds for reading session files from the developer's disk.
const (
	maxSessionFileBytes = 64 << 20
	maxSessionFiles     = 200
	maxTurnChars        = 4000
)

// sessionFile is one transcript on disk.
type sessionFile struct {
	Agent string
	Path  string
	Mod   time.Time
}

// parsedSession is what the scan keeps of one transcript: ordered turns and the pull request it produced, if any.
type parsedSession struct {
	Turns []cardgen.Turn
	Links []cardgen.PRLink
}

// encodeProjectPath gives the folder name Claude Code uses for a project root: every "/", "_" and "." becomes "-".
func encodeProjectPath(root string) string {
	return strings.NewReplacer("/", "-", "_", "-", ".", "-").Replace(root)
}

// claudeSessionFiles lists the Claude transcripts of the repository. The trailing "*" also matches the folders of
// worktrees, which Claude Code stores as separate projects named like the root plus "--claude-worktrees-<name>".
func claudeSessionFiles(home, root string) []sessionFile {
	pattern := filepath.Join(home, ".claude", "projects", encodeProjectPath(root)+"*", "*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return statSessions("claude", matches)
}

// codexSessionFiles lists the Codex rollout files. Whether one belongs to the repository is known only from its
// session_meta record, so the parser decides.
func codexSessionFiles(home string) []sessionFile {
	var paths []string
	base := filepath.Join(home, ".codex", "sessions")
	_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() && strings.HasPrefix(d.Name(), "rollout-") && filepath.Ext(d.Name()) == ".jsonl" {
			paths = append(paths, path)
		}
		return nil
	})
	return statSessions("codex", paths)
}

func statSessions(agent string, paths []string) []sessionFile {
	var out []sessionFile
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, sessionFile{Agent: agent, Path: p, Mod: info.ModTime()})
	}
	return out
}

// newestSessions keeps files changed since cutoff, newest first, at most limit.
func newestSessions(files []sessionFile, cutoff time.Time, limit int) []sessionFile {
	kept := slices.DeleteFunc(slices.Clone(files), func(f sessionFile) bool { return f.Mod.Before(cutoff) })
	slices.SortFunc(kept, func(a, b sessionFile) int { return b.Mod.Compare(a.Mod) })
	if len(kept) > limit {
		kept = kept[:limit]
	}
	return kept
}

// readSession opens one transcript and parses it with the parser of its agent. A file over the size bound is read
// only up to the bound; a truncated last line is dropped.
func readSession(f sessionFile, root string) (parsedSession, error) {
	file, err := os.Open(f.Path)
	if err != nil {
		return parsedSession{}, err
	}
	defer func() { _ = file.Close() }()
	id := strings.TrimSuffix(filepath.Base(f.Path), ".jsonl")
	limited := io.LimitReader(file, maxSessionFileBytes)
	if f.Agent == "codex" {
		return parseCodexSession(limited, root, id), nil
	}
	return parseClaudeSession(limited, root, id), nil
}

type claudeRecord struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	SessionID   string `json:"sessionId"`
	Timestamp   string `json:"timestamp"`
	Cwd         string `json:"cwd"`
	PRNumber    int    `json:"prNumber"`
	PRURL       string `json:"prUrl"`
	Message     struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type claudeBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Input struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	} `json:"input"`
}

// parseClaudeSession reads a Claude Code transcript. It keeps user and assistant text, and the file paths of tool
// calls, never tool output and never thinking. Consecutive assistant records form one turn, so the files of a whole
// tool loop belong to the turn the developer then answers. A session whose cwd is outside root yields nothing.
func parseClaudeSession(r io.Reader, root, fallbackID string) parsedSession {
	dec := json.NewDecoder(r)
	sb := &claudeBuilder{root: root, fallbackID: fallbackID}
	for {
		var rec claudeRecord
		if err := dec.Decode(&rec); err != nil {
			break
		}
		if rec.Cwd != "" && !underRoot(rec.Cwd, root) {
			return parsedSession{}
		}
		sb.add(rec)
	}
	sb.flush()
	return sb.res
}

type claudeBuilder struct {
	root, fallbackID string
	res              parsedSession
	cur              *cardgen.Turn
}

func (sb *claudeBuilder) flush() {
	if sb.cur != nil && (sb.cur.Text != "" || len(sb.cur.Files) > 0) {
		sb.res.Turns = append(sb.res.Turns, *sb.cur)
	}
	sb.cur = nil
}

func (sb *claudeBuilder) add(rec claudeRecord) {
	id := firstNonEmpty(rec.SessionID, sb.fallbackID)
	if rec.Type == "pr-link" && rec.PRNumber > 0 {
		sb.res.Links = addLink(sb.res.Links, cardgen.PRLink{Session: id, Number: rec.PRNumber, URL: rec.PRURL})
		return
	}
	if rec.IsMeta || (rec.Type != "user" && rec.Type != "assistant") {
		return
	}
	text, files, onlyToolResult := claudeContent(rec.Message.Content, rec.Cwd, sb.root)
	if rec.Type == "user" {
		if onlyToolResult || text == "" {
			return
		}
		sb.flush()
		sb.res.Turns = append(sb.res.Turns, cardgen.Turn{Role: "user", Text: capText(text), Session: id, Date: dateOf(rec.Timestamp), Sidechain: rec.IsSidechain})
		return
	}
	if sb.cur != nil && sb.cur.Sidechain != rec.IsSidechain {
		sb.flush()
	}
	if sb.cur == nil {
		sb.cur = &cardgen.Turn{Role: "assistant", Session: id, Date: dateOf(rec.Timestamp), Sidechain: rec.IsSidechain}
	}
	sb.cur.Text = capText(strings.TrimSpace(sb.cur.Text + "\n" + text))
	sb.cur.Files = appendUnique(sb.cur.Files, files)
}

// claudeContent returns the text and relative file paths of one message. A message of tool results only is flagged.
func claudeContent(raw json.RawMessage, cwd, root string) (text string, files []string, onlyToolResult bool) {
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return strings.TrimSpace(plain), nil, false
	}
	var blocks []claudeBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return "", nil, false
	}
	var parts []string
	results := 0
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "tool_use":
			if rel := repoRelative(firstNonEmpty(b.Input.FilePath, b.Input.NotebookPath), cwd, root); rel != "" {
				files = append(files, rel)
			}
		case "tool_result":
			results++
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n")), files, results > 0 && len(parts) == 0
}

type codexRecord struct {
	Type    string `json:"type"`
	Payload struct {
		Type    string          `json:"type"`
		Role    string          `json:"role"`
		Cwd     string          `json:"cwd"`
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
	} `json:"payload"`
	Timestamp string `json:"timestamp"`
}

// parseCodexSession reads a Codex rollout file. The session_meta record names the working directory; a session
// outside root yields nothing. Only user and assistant message text is kept.
func parseCodexSession(r io.Reader, root, fallbackID string) parsedSession {
	dec := json.NewDecoder(r)
	var res parsedSession
	id, inside := fallbackID, false
	for {
		var rec codexRecord
		if err := dec.Decode(&rec); err != nil {
			break
		}
		if rec.Type == "session_meta" {
			inside = underRoot(rec.Payload.Cwd, root)
			id = firstNonEmpty(rec.Payload.ID, fallbackID)
			continue
		}
		if !inside || rec.Type != "response_item" || rec.Payload.Type != "message" {
			continue
		}
		if rec.Payload.Role != "user" && rec.Payload.Role != "assistant" {
			continue
		}
		text := codexText(rec.Payload.Content)
		if text == "" {
			continue
		}
		res.Turns = appendCodexTurn(res.Turns, cardgen.Turn{Role: rec.Payload.Role, Text: capText(text), Session: id, Date: dateOf(rec.Timestamp)})
	}
	return res
}

func appendCodexTurn(turns []cardgen.Turn, t cardgen.Turn) []cardgen.Turn {
	if n := len(turns); n > 0 && t.Role == "assistant" && turns[n-1].Role == "assistant" {
		turns[n-1].Text = capText(turns[n-1].Text + "\n" + t.Text)
		return turns
	}
	return append(turns, t)
}

func codexText(raw json.RawMessage) string {
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return strings.TrimSpace(plain)
	}
	var blocks []claudeBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if (b.Type == "input_text" || b.Type == "output_text" || b.Type == "text") && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func underRoot(dir, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(dir))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// repoRelative turns a tool path into a path relative to the working tree of the record, or to the root. A path
// outside both is dropped, so the output never names files of other projects.
func repoRelative(p, cwd, root string) string {
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	for _, base := range []string{cwd, root} {
		if base != "" && underRoot(p, base) {
			if rel, err := filepath.Rel(base, p); err == nil && rel != "." {
				return filepath.ToSlash(rel)
			}
		}
	}
	return ""
}

func capText(s string) string {
	if len(s) <= maxTurnChars {
		return s
	}
	return string([]rune(s[:maxTurnChars]))
}

func dateOf(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func appendUnique(dst, src []string) []string {
	for _, s := range src {
		if !slices.Contains(dst, s) {
			dst = append(dst, s)
		}
	}
	return dst
}

func addLink(links []cardgen.PRLink, l cardgen.PRLink) []cardgen.PRLink {
	if slices.ContainsFunc(links, func(x cardgen.PRLink) bool { return x.Session == l.Session && x.Number == l.Number }) {
		return links
	}
	return append(links, l)
}
