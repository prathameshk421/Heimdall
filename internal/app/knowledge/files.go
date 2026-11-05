package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const maxKnowledgeFileBytes = ports.MaxUntrustedText - 1024

// FileState is the human approval and retention state of one knowledge source.
type FileState string

const (
	// FilePending is staged but not ingested until owner approval.
	FilePending FileState = "pending"
	// FileApproved has been authorized and ingested into team memory.
	FileApproved FileState = "approved"
)

// FileDoc is the metadata record exchanged with persistence adapters.
type FileDoc = ports.KnowledgeFile

// FileCatalog persists only metadata; document bytes live in the content-addressed blob store.
type FileCatalog = ports.KnowledgeFiles

// FileApproval authorizes a mapped policy owner or group approver for one document.
type FileApproval interface {
	ApproveKnowledgeFile(context.Context, string, FileDoc) error
}

// EpisodeLifecycle removes/invalidates Graphiti episodes by their stable source key.
type EpisodeLifecycle interface {
	ForgetEpisode(context.Context, string, string) error
}

// EpisodeEnqueuer persists approved knowledge for asynchronous, retryable Graphiti delivery.
type EpisodeEnqueuer interface {
	Enqueue(context.Context, ports.Episode) error
}

// FileScanner redacts source bytes before they can be stored or ingested.
type FileScanner interface {
	Scan(string) (string, []RedactionSummary, error)
}

// RedactionSummary describes removed material without retaining matched values.
type RedactionSummary struct {
	Kind  string
	Count int
}

// FileService stages default-branch markdown, enforces approval, ingests only approved sources, and expires files.
type FileService struct {
	head      ports.RepoReader
	code      ports.CodeReader
	blobs     ports.BlobStore
	deleter   ports.BlobDeleter
	catalog   FileCatalog
	approval  FileApproval
	episodes  EpisodeEnqueuer
	lifecycle EpisodeLifecycle
	scanner   FileScanner
	clock     ports.Clock
}

// NewFileService requires every safety and persistence boundary.
func NewFileService(head ports.RepoReader, code ports.CodeReader, blobs ports.BlobStore, deleter ports.BlobDeleter, catalog FileCatalog, approval FileApproval, episodes EpisodeEnqueuer, lifecycle EpisodeLifecycle, scanner FileScanner, clock ports.Clock) (*FileService, error) {
	if head == nil || code == nil || blobs == nil || deleter == nil || catalog == nil || approval == nil || episodes == nil || lifecycle == nil || scanner == nil || clock == nil {
		return nil, errors.New("knowledge file service dependencies must not be nil")
	}
	return &FileService{head: head, code: code, blobs: blobs, deleter: deleter, catalog: catalog, approval: approval, episodes: episodes, lifecycle: lifecycle, scanner: scanner, clock: clock}, nil
}

// Stage reads one markdown file from the current default branch, scans it, stores the redacted bytes and creates a
// pending record. It does not send the document to Graphiti before human approval.
func (s *FileService) Stage(ctx context.Context, actor string, repo change.Repo, filePath, group, kind, sourceURL string, retentionDays int) (FileDoc, []string, error) {
	clean, safeURL, err := validateFileRequest(actor, group, kind, filePath, sourceURL, retentionDays)
	if err != nil {
		return FileDoc{}, nil, err
	}
	sha, err := s.head.DefaultHead(ctx, repo)
	if err != nil {
		return FileDoc{}, nil, fmt.Errorf("read repository default head: %w", err)
	}
	doc, hits, err := s.readAndStore(ctx, repo, sha, clean, group, kind, safeURL, retentionDays)
	if err != nil {
		return FileDoc{}, nil, err
	}
	if err := s.catalog.Save(ctx, doc); err != nil {
		return FileDoc{}, nil, fmt.Errorf("save pending knowledge file: %w", err)
	}
	removed := make([]string, 0, len(hits))
	for _, hit := range hits {
		removed = append(removed, fmt.Sprintf("%s (%d)", hit.Kind, hit.Count))
	}
	return doc, removed, nil
}

func validateFileRequest(actor, group, kind, filePath, sourceURL string, retentionDays int) (string, string, error) {
	if actor == "" || group == "" || (kind != "adr" && kind != "document") || retentionDays < 1 || retentionDays > 3650 {
		return "", "", errors.New("knowledge file request is invalid")
	}
	clean := path.Clean(filePath)
	if !strings.HasPrefix(clean, "docs/") || path.Ext(clean) != ".md" || strings.HasPrefix(clean, "../") || len(clean) > 512 {
		return "", "", errors.New("knowledge files must be markdown under docs/")
	}
	safeURL, err := validateSourceURL(sourceURL)
	if err != nil {
		return "", "", err
	}
	return clean, safeURL, nil
}

func validateSourceURL(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || !u.IsAbs() || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.ForceQuery {
		return "", errors.New("knowledge provenance URL must be an HTTP(S) URL without credentials or query data")
	}
	return u.String(), nil
}

func (s *FileService) readAndStore(ctx context.Context, repo change.Repo, sha id.SHA, filePath, group, kind, sourceURL string, retentionDays int) (FileDoc, []RedactionSummary, error) {
	content, err := s.code.ReadFile(ctx, repo, sha, filePath)
	if err != nil {
		return FileDoc{}, nil, fmt.Errorf("read knowledge file at default head: %w", err)
	}
	if len(content) > maxKnowledgeFileBytes {
		return FileDoc{}, nil, fmt.Errorf("knowledge file exceeds %d bytes", maxKnowledgeFileBytes)
	}
	redacted, hits, err := s.scanner.Scan(string(content))
	if err != nil {
		return FileDoc{}, nil, fmt.Errorf("scan knowledge file: %w", err)
	}
	blob, err := s.blobs.Put(ctx, ports.BlobKnowledge, strings.NewReader(redacted))
	if err != nil {
		return FileDoc{}, nil, fmt.Errorf("store redacted knowledge file: %w", err)
	}
	now := s.clock.Now()
	doc := FileDoc{Ref: knowledgeFileRef(repo, filePath, sha), Repository: repo, Path: filePath, SHA: sha, Group: group, SourceURL: sourceURL, Blob: blob, Kind: kind, State: string(FilePending), CreatedAt: now, ExpiresAt: now.Add(time.Duration(retentionDays) * 24 * time.Hour)}
	return doc, hits, nil
}

// Approve authorizes the person before any episode can enter shared or team memory.
func (s *FileService) Approve(ctx context.Context, actor, ref string) error {
	doc, err := s.catalog.Get(ctx, ref)
	if err != nil {
		return fmt.Errorf("load knowledge file: %w", err)
	}
	if doc.State != string(FilePending) {
		return errors.New("knowledge file is not pending approval")
	}
	if err := s.approval.ApproveKnowledgeFile(ctx, actor, doc); err != nil {
		return fmt.Errorf("authorize knowledge approval: %w", err)
	}
	raw, err := s.blobs.Get(ctx, doc.Blob)
	if err != nil {
		return fmt.Errorf("read staged knowledge content: %w", err)
	}
	body, readErr := io.ReadAll(io.LimitReader(raw, maxKnowledgeFileBytes+1))
	closeErr := raw.Close()
	if readErr != nil {
		return fmt.Errorf("read staged knowledge content: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close staged knowledge content: %w", closeErr)
	}
	if len(body) > maxKnowledgeFileBytes {
		return errors.New("staged knowledge content exceeds size limit")
	}
	episodes, err := knowledgeEpisodes(doc, body)
	if err != nil {
		return fmt.Errorf("build knowledge episodes: %w", err)
	}
	for _, episode := range episodes {
		if err := s.episodes.Enqueue(ctx, episode); err != nil {
			return fmt.Errorf("ingest approved knowledge file: %w", err)
		}
	}
	doc.State = string(FileApproved)
	doc.ApprovedBy = actor
	if err := s.catalog.Save(ctx, doc); err != nil {
		return fmt.Errorf("record knowledge approval: %w", err)
	}
	return nil
}

// Forget invalidates the matching memory episode and removes the file metadata. Expiry uses the same operation.
func (s *FileService) Forget(ctx context.Context, actor, ref string) error {
	doc, err := s.catalog.Get(ctx, ref)
	if err != nil {
		return fmt.Errorf("load knowledge file: %w", err)
	}
	if err := s.approval.ApproveKnowledgeFile(ctx, actor, doc); err != nil {
		return fmt.Errorf("authorize knowledge deletion: %w", err)
	}
	if doc.State == string(FileApproved) {
		if err := s.forgetEpisodes(ctx, doc); err != nil {
			return err
		}
	}
	last, err := s.catalog.Delete(ctx, doc.Ref)
	if err != nil {
		return fmt.Errorf("delete knowledge file metadata: %w", err)
	}
	if last {
		if err := s.deleter.Delete(ctx, ports.BlobKnowledge, doc.Blob); err != nil {
			return fmt.Errorf("delete knowledge file blob: %w", err)
		}
	}
	return nil
}

// Expire removes files whose retention has elapsed and returns how many were removed.
func (s *FileService) Expire(ctx context.Context) (int, error) {
	docs, err := s.catalog.Expired(ctx, s.clock.Now())
	if err != nil {
		return 0, fmt.Errorf("list expired knowledge files: %w", err)
	}
	for _, doc := range docs {
		if doc.State == string(FileApproved) {
			if err := s.forgetEpisodes(ctx, doc); err != nil {
				return 0, err
			}
		}
		last, err := s.catalog.Delete(ctx, doc.Ref)
		if err != nil {
			return 0, fmt.Errorf("delete expired knowledge metadata: %w", err)
		}
		if last {
			if err := s.deleter.Delete(ctx, ports.BlobKnowledge, doc.Blob); err != nil {
				return 0, fmt.Errorf("delete expired knowledge blob: %w", err)
			}
		}
	}
	return len(docs), nil
}

func knowledgeEpisodes(doc FileDoc, body []byte) ([]ports.Episode, error) {
	status := ports.EpisodeObserved
	if doc.Kind == "adr" {
		status = ports.EpisodeConfirmed
	}
	content, err := json.Marshal(map[string]string{"claim": "Accepted knowledge from " + doc.Path, "source_url": doc.SourceURL, "repository": string(doc.Repository), "path": doc.Path, "sha": string(doc.SHA), "content": string(body)})
	if err != nil {
		return nil, fmt.Errorf("encode episode body: %w", err)
	}
	base := "episode:knowledge:" + doc.Ref
	episodes := []ports.Episode{{Key: change.EffectKey(base), Source: "knowledge_file", Status: status, GroupID: doc.Group, Body: string(content)}}
	if doc.Kind == "adr" && doc.Group != "shared" {
		episodes = append(episodes, ports.Episode{Key: change.EffectKey(base + ":shared"), Source: "knowledge_file", Status: status, GroupID: "shared", Body: string(content)})
	}
	return episodes, nil
}

func (s *FileService) forgetEpisodes(ctx context.Context, doc FileDoc) error {
	base := "episode:knowledge:" + doc.Ref
	if err := s.lifecycle.ForgetEpisode(ctx, base, doc.Group); err != nil {
		return fmt.Errorf("invalidate knowledge episode: %w", err)
	}
	if doc.Kind == "adr" && doc.Group != "shared" {
		if err := s.lifecycle.ForgetEpisode(ctx, base+":shared", "shared"); err != nil {
			return fmt.Errorf("invalidate shared knowledge episode: %w", err)
		}
	}
	return nil
}

func knowledgeFileRef(repo change.Repo, path string, sha id.SHA) string {
	sum := sha256.Sum256([]byte(string(repo) + "\n" + path + "\n" + string(sha)))
	return "knowledge:" + hex.EncodeToString(sum[:])
}
