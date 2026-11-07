package pack

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// CollectCodex reads Codex rollout JSONL files and keeps only user/assistant text from sessions in repoRoot.
// Tool calls, tool output and sessions from other working directories are discarded.
func CollectCodex(ctx context.Context, repoRoot string, sessionDirs []string) ([]SourceDocument, error) {
	var docs []SourceDocument
	for _, dir := range sessionDirs {
		root, err := os.OpenRoot(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("open Codex transcript directory: %w", err)
		}
		collected, collectErr := collectCodexDir(ctx, root, repoRoot)
		if collectErr != nil {
			_ = root.Close()
			return nil, collectErr
		}
		docs = append(docs, collected...)
		if err := root.Close(); err != nil {
			return nil, fmt.Errorf("close Codex transcript directory: %w", err)
		}
	}
	return docs, nil
}

func collectCodexDir(ctx context.Context, root *os.Root, repoRoot string) ([]SourceDocument, error) {
	entries, err := readRootDir(root, ".")
	if err != nil {
		return nil, fmt.Errorf("list Codex transcript directory: %w", err)
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	var docs []SourceDocument
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" || isSymlink(entry) {
			continue
		}
		body, err := readRootFile(root, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read Codex transcript: %w", err)
		}
		text, err := extractCodex(bytes.NewReader(body), repoRoot)
		if err != nil {
			return nil, fmt.Errorf("parse Codex transcript: %w", err)
		}
		if text != "" {
			docs = append(docs, SourceDocument{Provenance: "[transcript:codex:" + strings.TrimSuffix(entry.Name(), ".jsonl") + "]", Raw: text})
		}
	}
	return docs, nil
}

func extractCodex(r io.Reader, repoRoot string) (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), maxSourceBytes)
	cwd := ""
	var out bytes.Buffer
	for scanner.Scan() {
		var record map[string]any
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			continue
		}
		payload, _ := record["payload"].(map[string]any)
		typ, _ := record["type"].(string)
		if typ == "session_meta" {
			cwd = stringField(payload, "cwd")
			if cwd == "" {
				cwd = stringField(record, "cwd")
			}
			continue
		}
		if filepath.Clean(cwd) != filepath.Clean(repoRoot) {
			continue
		}
		role, content := codexMessage(record, payload, typ)
		if role != "user" && role != "assistant" {
			continue
		}
		text := codexText(content)
		if text == "" {
			continue
		}
		_, _ = fmt.Fprintf(&out, "%s: %s\n", role, text)
		if out.Len() > maxSourceBytes {
			return "", fmt.Errorf("codex transcript exceeds %d bytes", maxSourceBytes)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan Codex JSONL: %w", err)
	}
	return out.String(), nil
}

func codexMessage(record, payload map[string]any, typ string) (string, any) {
	if typ == "response_item" {
		if stringField(payload, "type") != "message" {
			return "", nil
		}
		return stringField(payload, "role"), payload["content"]
	}
	if typ == "message" {
		message, _ := record["message"].(map[string]any)
		return stringField(message, "role"), message["content"]
	}
	return "", nil
}

func codexText(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []any:
		var parts []string
		for _, item := range value {
			part, ok := item.(map[string]any)
			if !ok {
				continue
			}
			typ := stringField(part, "type")
			if typ != "input_text" && typ != "output_text" && typ != "text" {
				continue
			}
			if text := stringField(part, "text"); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// CollectOpenCode reads OpenCode's session/message/part JSON store. It joins only text parts for user and assistant
// messages in sessions whose recorded directory is exactly repoRoot.
func CollectOpenCode(ctx context.Context, storageRoot, repoRoot string) ([]SourceDocument, error) {
	root, err := os.OpenRoot(storageRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open OpenCode storage: %w", err)
	}
	defer func() { _ = root.Close() }()
	sessions, err := readRootDir(root, "session")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list OpenCode sessions: %w", err)
	}
	docs, err := collectOpenCodeSessions(ctx, root, sessions, repoRoot)
	if err != nil {
		return nil, err
	}
	if err := boundDocuments(docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func collectOpenCodeSessions(ctx context.Context, root *os.Root, sessions []os.DirEntry, repoRoot string) ([]SourceDocument, error) {
	slices.SortFunc(sessions, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	var docs []SourceDocument
	for _, session := range sessions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if session.IsDir() || filepath.Ext(session.Name()) != ".json" || isSymlink(session) {
			continue
		}
		id, matches, err := openCodeSessionID(root, session, repoRoot)
		if err != nil {
			return nil, err
		}
		if !matches {
			continue
		}
		items, err := collectOpenCodeMessages(root, id)
		if err != nil {
			return nil, err
		}
		docs = append(docs, items...)
	}
	return docs, nil
}

func openCodeSessionID(root *os.Root, session os.DirEntry, repoRoot string) (string, bool, error) {
	data, err := readRootFile(root, filepath.ToSlash(filepath.Join("session", session.Name())))
	if err != nil {
		return "", false, fmt.Errorf("read OpenCode session: %w", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil {
		return "", false, fmt.Errorf("decode OpenCode session: %w", err)
	}
	if filepath.Clean(stringField(metadata, "directory")) != filepath.Clean(repoRoot) {
		return "", false, nil
	}
	return strings.TrimSuffix(session.Name(), filepath.Ext(session.Name())), true, nil
}

func collectOpenCodeMessages(root *os.Root, sessionID string) ([]SourceDocument, error) {
	messages, err := readRootDir(root, filepath.ToSlash(filepath.Join("message", sessionID)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list OpenCode messages: %w", err)
	}
	slices.SortFunc(messages, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	var docs []SourceDocument
	for _, message := range messages {
		if message.IsDir() || filepath.Ext(message.Name()) != ".json" || isSymlink(message) {
			continue
		}
		item, err := collectOpenCodeMessage(root, sessionID, message)
		if err != nil {
			return nil, err
		}
		if item.Raw != "" {
			docs = append(docs, item)
		}
	}
	return docs, nil
}

func collectOpenCodeMessage(root *os.Root, sessionID string, message os.DirEntry) (SourceDocument, error) {
	messageID := strings.TrimSuffix(message.Name(), filepath.Ext(message.Name()))
	path := filepath.ToSlash(filepath.Join("message", sessionID, message.Name()))
	data, err := readRootFile(root, path)
	if err != nil {
		return SourceDocument{}, fmt.Errorf("read OpenCode message: %w", err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		return SourceDocument{}, fmt.Errorf("decode OpenCode message: %w", err)
	}
	role := stringField(record, "role")
	if role != "user" && role != "assistant" {
		return SourceDocument{}, nil
	}
	text, err := readOpenCodeParts(root, messageID)
	if err != nil {
		return SourceDocument{}, err
	}
	return SourceDocument{Provenance: "[transcript:opencode:" + messageID + "]", Raw: role + ": " + text}, nil
}

func readOpenCodeParts(root *os.Root, messageID string) (string, error) {
	entries, err := readRootDir(root, filepath.ToSlash(filepath.Join("part", messageID)))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("list OpenCode message parts: %w", err)
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	var parts []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || isSymlink(entry) {
			continue
		}
		body, err := readRootFile(root, filepath.ToSlash(filepath.Join("part", messageID, entry.Name())))
		if err != nil {
			return "", fmt.Errorf("read OpenCode message part: %w", err)
		}
		var part map[string]any
		if json.Unmarshal(body, &part) != nil || stringField(part, "type") != "text" {
			continue
		}
		if text := stringField(part, "text"); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

func readRootDir(root *os.Root, name string) ([]os.DirEntry, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return file.ReadDir(-1)
}

func readRootFile(root *os.Root, name string) ([]byte, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	body, err := io.ReadAll(io.LimitReader(file, maxSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxSourceBytes {
		return nil, fmt.Errorf("source file exceeds %d bytes", maxSourceBytes)
	}
	return body, nil
}

func isSymlink(entry os.DirEntry) bool {
	info, err := entry.Info()
	return err != nil || info.Mode()&os.ModeSymlink != 0
}

func stringField(record map[string]any, key string) string {
	value, _ := record[key].(string)
	return value
}

func boundDocuments(docs []SourceDocument) error {
	total := 0
	for _, doc := range docs {
		total += len(doc.Raw)
		if total > maxSourceBytes {
			return fmt.Errorf("transcript collection exceeds %d bytes", maxSourceBytes)
		}
	}
	return nil
}
