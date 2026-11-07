package pack

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// GitCommit is the privacy-limited portion of a commit retained in a pack.
type GitCommit struct{ Hash, Author, Subject string }

// GitOwnership is a path's author-attributed line count.
type GitOwnership struct {
	Path, Author string
	Lines        int
}

// GitSource supplies only git metadata; implementations must not return patches or file contents.
type GitSource interface {
	Log(context.Context, string) ([]GitCommit, error)
	Blame(context.Context, string, string) ([]GitOwnership, error)
}

// GitCollector samples commits and blame through an injected read-only source.
type GitCollector struct {
	Source     GitSource
	MaxCommits int
}

// Collect returns commit messages and ownership summaries with stable provenance.
func (c GitCollector) Collect(ctx context.Context, repository, author string, paths []string) ([]Document, error) {
	if c.Source == nil || repository == "" || author == "" || c.MaxCommits < 1 {
		return nil, fmt.Errorf("invalid git collector configuration")
	}
	commits, err := c.Source.Log(ctx, author)
	if err != nil {
		return nil, fmt.Errorf("collect git history: %w", err)
	}
	if len(commits) > c.MaxCommits {
		commits = commits[:c.MaxCommits]
	}
	logContent, err := formatCommits(commits)
	if err != nil {
		return nil, err
	}
	documents := []Document{makeDocument("git:log", logContent)}
	for _, file := range paths {
		document, err := c.collectBlame(ctx, repository, author, file)
		if err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	return documents, nil
}

func formatCommits(commits []GitCommit) (string, error) {
	var out strings.Builder
	for _, commit := range commits {
		if commit.Hash == "" || commit.Author == "" {
			return "", fmt.Errorf("git source returned incomplete commit")
		}
		fmt.Fprintf(&out, "[%s] %s: %s\n", commit.Hash, commit.Author, commit.Subject)
	}
	return out.String(), nil
}

func (c GitCollector) collectBlame(ctx context.Context, repository, author, file string) (Document, error) {
	if err := validateSource(file); err != nil {
		return Document{}, err
	}
	ownership, err := c.Source.Blame(ctx, repository, file)
	if err != nil {
		return Document{}, fmt.Errorf("collect git blame %q: %w", file, err)
	}
	var out strings.Builder
	for _, item := range ownership {
		if item.Path != file || item.Author == "" || item.Lines < 0 {
			return Document{}, fmt.Errorf("git source returned invalid blame for %q", file)
		}
		if item.Author == author {
			fmt.Fprintf(&out, "%s: %d lines\n", file, item.Lines)
		}
	}
	return makeDocument("git:blame:"+file, out.String()), nil
}

type transcriptEnvelope struct {
	Type    string `json:"type"`
	Cwd     string `json:"cwd"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Content json.RawMessage `json:"content"`
}

// TranscriptCollector extracts user and assistant text from repo sessions and drops tool data.
type TranscriptCollector struct {
	Root     Root
	RepoPath string
	Limits   Limits
}

// Collect parses bounded JSONL transcripts from explicitly selected root-relative paths.
func (c TranscriptCollector) Collect(sources []string) ([]Document, error) {
	if c.Root == nil || c.RepoPath == "" || c.Limits.MaxFiles <= 0 || c.Limits.MaxBytes <= 0 {
		return nil, fmt.Errorf("invalid transcript collector configuration")
	}
	if len(sources) > c.Limits.MaxFiles {
		return nil, fmt.Errorf("%w: too many transcript files", ErrCollectionLimit)
	}
	seen := map[string]bool{}
	var total int64
	var result []Document
	for _, source := range sources {
		if seen[source] {
			continue
		}
		seen[source] = true
		document, consumed, err := c.collectTranscript(source, c.Limits.MaxBytes-total)
		if err != nil {
			return nil, err
		}
		total += consumed
		result = append(result, document)
	}
	return result, nil
}

func (c TranscriptCollector) collectTranscript(source string, remaining int64) (Document, int64, error) {
	if err := validateSource(source); err != nil {
		return Document{}, 0, err
	}
	file, err := c.Root.Open(source)
	if err != nil {
		return Document{}, 0, fmt.Errorf("open transcript %q: %w", source, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, remaining+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return Document{}, 0, fmt.Errorf("read transcript %q: %w", source, errors.Join(readErr, closeErr))
	}
	if int64(len(data)) > remaining {
		return Document{}, 0, fmt.Errorf("%w: transcript byte limit", ErrCollectionLimit)
	}
	content, err := extractTranscript(data, source, c.RepoPath, remaining)
	if err != nil {
		return Document{}, 0, err
	}
	return makeDocument("transcript:"+path.Base(source), content), int64(len(data)), nil
}

func extractTranscript(data []byte, source, repoPath string, maxBytes int64) (string, error) {
	var out strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 4096), int(maxBytes))
	for scanner.Scan() {
		var event transcriptEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return "", fmt.Errorf("parse transcript %q: %w", source, err)
		}
		if event.Cwd != repoPath || (event.Type != "user" && event.Type != "assistant") {
			continue
		}
		content := event.Message.Content
		if len(content) == 0 {
			content = event.Content
		}
		text, ok := transcriptText(content)
		if ok {
			fmt.Fprintf(&out, "%s: %s\n", event.Type, text)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan transcript %q: %w", source, err)
	}
	return out.String(), nil
}

func transcriptText(raw json.RawMessage) (string, bool) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, true
	}
	var parts []struct{ Type, Text string }
	if json.Unmarshal(raw, &parts) != nil {
		return "", false
	}
	var out strings.Builder
	for _, part := range parts {
		if part.Type == "text" {
			out.WriteString(part.Text)
		}
	}
	return out.String(), out.Len() > 0
}

func makeDocument(provenance, content string) Document {
	return Document{Path: provenance, Provenance: provenance, Content: content, SHA256: sha256.Sum256([]byte(content))}
}
