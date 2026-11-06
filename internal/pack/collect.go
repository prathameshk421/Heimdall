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
	"os/exec"
	"path/filepath"
	"strings"
)

const maxSourceBytes = 4 << 20

// CollectMarkdown reads explicitly selected markdown files beneath repoRoot. Symlinks and paths outside the root
// are refused; secret files are still passed through the mandatory first redaction pass later.
func CollectMarkdown(repoRoot string, paths []string) ([]SourceDocument, error) {
	root, err := os.OpenRoot(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("open pack source root: %w", err)
	}
	defer func() { _ = root.Close() }()
	documents := make([]SourceDocument, 0, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		if clean == "." || filepath.IsAbs(path) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.Ext(clean) != ".md" {
			return nil, fmt.Errorf("pack markdown path %q must be a relative .md file", path)
		}
		file, err := root.Open(clean)
		if err != nil {
			return nil, fmt.Errorf("open pack markdown %q: %w", clean, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(file, maxSourceBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read pack markdown %q: %w", clean, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close pack markdown %q: %w", clean, closeErr)
		}
		if len(body) > maxSourceBytes {
			return nil, fmt.Errorf("pack markdown %q exceeds %d bytes", clean, maxSourceBytes)
		}
		documents = append(documents, SourceDocument{Provenance: "[doc:" + filepath.ToSlash(clean) + "]", Raw: string(body)})
	}
	return documents, nil
}

// CollectGit obtains authored commit summaries and ownership hints without collecting patches or file contents.
func CollectGit(ctx context.Context, repoRoot, author string, maxCommits int) ([]SourceDocument, error) {
	if author == "" || maxCommits < 1 || maxCommits > 500 {
		return nil, errors.New("git pack collection needs an author and a 1 to 500 commit bound")
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "log", "--max-count="+fmt.Sprint(maxCommits), "--author="+author, "--format=%h%x09%ad%x09%s", "--date=short") // #nosec G204 -- fixed git command; dynamic values are individual argv fields, never shell text
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("collect authored git history: %w", err)
	}
	if len(out) > maxSourceBytes {
		return nil, fmt.Errorf("git history exceeds %d bytes", maxSourceBytes)
	}
	return []SourceDocument{{Provenance: "[git:history]", Raw: string(out)}}, nil
}

// CollectClaude reads Claude Code JSONL sessions from explicitly supplied directories. Only user and assistant
// text whose session cwd equals repoRoot is retained; tool results, tool calls and other repositories are dropped.
func CollectClaude(ctx context.Context, repoRoot string, sessionDirs []string) ([]SourceDocument, error) {
	var docs []SourceDocument
	for _, dir := range sessionDirs {
		collected, err := collectClaudeDir(ctx, repoRoot, dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		docs = append(docs, collected...)
	}
	return docs, nil
}

func collectClaudeDir(ctx context.Context, repoRoot, dir string) ([]SourceDocument, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open claude transcript directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	directory, err := root.Open(".")
	if err != nil {
		return nil, fmt.Errorf("open claude transcript directory: %w", err)
	}
	entries, err := directory.ReadDir(-1)
	closeErr := directory.Close()
	if err != nil {
		return nil, fmt.Errorf("list claude transcript directory: %w", err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close claude transcript directory: %w", closeErr)
	}
	var docs []SourceDocument
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		doc, keep, err := collectClaudeEntry(ctx, root, repoRoot, entry)
		if err != nil {
			return nil, err
		}
		if keep {
			docs = append(docs, doc)
		}
	}
	return docs, nil
}

func collectClaudeEntry(ctx context.Context, root *os.Root, repoRoot string, entry os.DirEntry) (SourceDocument, bool, error) {
	if err := ctx.Err(); err != nil {
		return SourceDocument{}, false, err
	}
	file, err := root.Open(entry.Name())
	if err != nil {
		return SourceDocument{}, false, fmt.Errorf("open Claude transcript: %w", err)
	}
	body, readErr := extractClaude(file, repoRoot)
	closeErr := file.Close()
	if readErr != nil {
		return SourceDocument{}, false, fmt.Errorf("read Claude transcript: %w", readErr)
	}
	if closeErr != nil {
		return SourceDocument{}, false, fmt.Errorf("close Claude transcript: %w", closeErr)
	}
	if body == "" {
		return SourceDocument{}, false, nil
	}
	return SourceDocument{Provenance: "[transcript:" + strings.TrimSuffix(entry.Name(), ".jsonl") + "]", Raw: body}, true, nil
}

func extractClaude(r io.Reader, repoRoot string) (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), maxSourceBytes)
	var out bytes.Buffer
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		cwd, _ := record["cwd"].(string)
		if filepath.Clean(cwd) != filepath.Clean(repoRoot) {
			continue
		}
		typ, _ := record["type"].(string)
		if typ != "user" && typ != "assistant" {
			continue
		}
		text := claudeText(record["message"])
		if text == "" {
			continue
		}
		_, _ = fmt.Fprintf(&out, "%s: %s\n", typ, text)
		if out.Len() > maxSourceBytes {
			return "", fmt.Errorf("claude transcript exceeds %d bytes", maxSourceBytes)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan Claude JSONL: %w", err)
	}
	return out.String(), nil
}

func claudeText(value any) string {
	message, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	switch content := message["content"].(type) {
	case string:
		return content
	case []any:
		var parts []string
		for _, item := range content {
			part, ok := item.(map[string]any)
			if !ok || part["type"] != "text" {
				continue
			}
			if text, ok := part["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}
