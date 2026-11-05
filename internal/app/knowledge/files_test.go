package knowledge

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeFileWaitsForApprovalScansAndKeepsProvenance(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	code := &fileCode{content: []byte("decision with secret")}
	blobs := &fileBlobs{data: map[workflow.BlobRef][]byte{}}
	catalog := &fileCatalog{docs: map[string]FileDoc{}}
	memory := &fileMemory{}
	approval := &fileApproval{}
	life := &fileLifecycle{}
	svc, err := NewFileService(fileHead{}, code, blobs, blobs, catalog, approval, memory, life, fileScanner{}, knowledgeClock{now})
	require.NoError(t, err)
	doc, removed, err := svc.Stage(ctx, "alice", "acme/pay", "docs/adr/004.md", "payments", "adr", "https://git.example/adr/004", 30)
	require.NoError(t, err)
	require.Equal(t, string(FilePending), doc.State)
	require.Equal(t, []string{"cloud_key (1)"}, removed)
	require.Equal(t, "base-head", string(doc.SHA))
	require.Equal(t, "docs/adr/004.md", doc.Path)
	require.Zero(t, memory.adds)
	stored := blobs.data[doc.Blob]
	require.Equal(t, "decision with redacted", string(stored))
	require.NoError(t, svc.Approve(ctx, "alice", doc.Ref))
	require.Equal(t, 2, memory.adds, "accepted ADRs are shared with the team and shared group")
	require.Equal(t, ports.EpisodeConfirmed, memory.last.Status)
	require.Equal(t, "shared", memory.last.GroupID)
	require.Contains(t, memory.last.Body, "docs/adr/004.md")
	require.Equal(t, "alice", approval.actor)
	require.NoError(t, svc.Forget(ctx, "owner", doc.Ref))
	require.Equal(t, 2, life.forgot)
	require.NotContains(t, catalog.docs, doc.Ref)
}

func TestKnowledgeFileRequiresMarkdownUnderDocsAndExpiresPendingRecord(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	catalog := &fileCatalog{docs: map[string]FileDoc{}}
	blobs := &fileBlobs{data: map[workflow.BlobRef][]byte{}}
	svc, err := NewFileService(fileHead{}, &fileCode{content: []byte("doc")}, blobs, blobs, catalog, &fileApproval{}, &fileMemory{}, &fileLifecycle{}, fileScanner{}, knowledgeClock{now})
	require.NoError(t, err)
	_, _, err = svc.Stage(context.Background(), "owner", "acme/pay", "README.md", "payments", "document", "", 30)
	require.ErrorContains(t, err, "under docs")
	doc, _, err := svc.Stage(context.Background(), "owner", "acme/pay", "docs/note.md", "payments", "document", "", 1)
	require.NoError(t, err)
	catalog.expired = []FileDoc{doc}
	count, err := svc.Expire(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.NotContains(t, catalog.docs, doc.Ref)
}

func TestKnowledgeFileRejectsUnsafeProvenanceBeforeReadingOrStoring(t *testing.T) {
	catalog := &fileCatalog{docs: map[string]FileDoc{}}
	blobs := &fileBlobs{data: map[workflow.BlobRef][]byte{}}
	svc, err := NewFileService(fileHead{}, &fileCode{content: []byte("safe")}, blobs, blobs, catalog, &fileApproval{}, &fileMemory{}, &fileLifecycle{}, fileScanner{}, knowledgeClock{time.Now()})
	require.NoError(t, err)
	_, _, err = svc.Stage(context.Background(), "owner", "acme/pay", "docs/guide.md", "payments", "document", "javascript:alert(1)", 30)
	require.ErrorContains(t, err, "provenance URL")
	require.Empty(t, catalog.docs)
	require.Empty(t, blobs.data)
	_, _, err = svc.Stage(context.Background(), "owner", "acme/pay", "docs/guide.md", "payments", "document", "https://example.test/guide?token=credential", 30)
	require.ErrorContains(t, err, "provenance URL")
	require.Empty(t, catalog.docs)
	require.Empty(t, blobs.data)
}

type fileHead struct{}

func (fileHead) DefaultHead(context.Context, change.Repo) (id.SHA, error) {
	return id.SHA("base-head"), nil
}

type fileCode struct{ content []byte }

func (c *fileCode) ReadFile(context.Context, change.Repo, id.SHA, string) ([]byte, error) {
	return c.content, nil
}
func (*fileCode) TreeHash(context.Context, change.Repo, id.SHA, string) (id.SHA, error) {
	return "", nil
}
func (*fileCode) Tarball(context.Context, change.Repo, id.SHA) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

type fileBlobs struct{ data map[workflow.BlobRef][]byte }

func (b *fileBlobs) Put(_ context.Context, _ ports.BlobClass, r io.Reader) (workflow.BlobRef, error) {
	body, _ := io.ReadAll(r)
	ref := workflow.BlobRef("blob:sha256:" + strings.Repeat("a", 64))
	b.data[ref] = body
	return ref, nil
}
func (b *fileBlobs) Get(_ context.Context, ref workflow.BlobRef) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(b.data[ref]))), nil
}
func (b *fileBlobs) Delete(_ context.Context, _ ports.BlobClass, ref workflow.BlobRef) error {
	delete(b.data, ref)
	return nil
}

type fileCatalog struct {
	docs    map[string]FileDoc
	expired []FileDoc
}

func (c *fileCatalog) Save(_ context.Context, d FileDoc) error { c.docs[d.Ref] = d; return nil }
func (c *fileCatalog) Get(_ context.Context, ref string) (FileDoc, error) {
	d, ok := c.docs[ref]
	if !ok {
		return FileDoc{}, ports.ErrNotFound
	}
	return d, nil
}
func (c *fileCatalog) Expired(context.Context, time.Time) ([]FileDoc, error) {
	return c.expired, nil
}
func (c *fileCatalog) Delete(_ context.Context, ref string) (bool, error) {
	delete(c.docs, ref)
	return true, nil
}

type fileApproval struct{ actor string }

func (a *fileApproval) ApproveKnowledgeFile(_ context.Context, actor string, _ FileDoc) error {
	a.actor = actor
	return nil
}

type fileMemory struct {
	adds int
	last ports.Episode
}

func (m *fileMemory) Enqueue(_ context.Context, ep ports.Episode) error {
	m.adds++
	m.last = ep
	return nil
}
func (*fileMemory) Search(context.Context, ports.MemoryQuery) ([]ports.MemoryItem, error) {
	return nil, nil
}

type fileLifecycle struct{ forgot int }

func (l *fileLifecycle) ForgetEpisode(context.Context, string, string) error { l.forgot++; return nil }

type fileScanner struct{}

func (fileScanner) Scan(s string) (string, []RedactionSummary, error) {
	return strings.ReplaceAll(s, "secret", "redacted"), []RedactionSummary{{Kind: "cloud_key", Count: 1}}, nil
}
