package bundle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestBuildReplayAndInputSensitivity(t *testing.T) {
	req := testRequest()
	first, err := Build(req)
	require.NoError(t, err)
	second, err := Build(req)
	require.NoError(t, err)
	require.Equal(t, first.Bytes, second.Bytes)
	require.Equal(t, Hash(first), Hash(second))

	changed := req
	changed.Criteria = []string{"AC2 changed"}
	criteria, err := Build(changed)
	require.NoError(t, err)
	require.NotEqual(t, Hash(first), Hash(criteria))
	changed = req
	changed.Diff = "new diff"
	diff, err := Build(changed)
	require.NoError(t, err)
	require.NotEqual(t, Hash(first), Hash(diff))
	changed = req
	changed.Pack = "other pack"
	pack, err := Build(changed)
	require.NoError(t, err)
	require.NotEqual(t, Hash(first), Hash(pack))
}

func TestBuildCanonicalizesUnorderedListsWithoutHTMLEscaping(t *testing.T) {
	req := testRequest()
	req.Diff = "<change & review>"
	req.FilesChanged = []string{"z.go", "a.go"}
	req.Criteria = []string{"AC2", "AC1"}
	req.CI = []string{"z:success", "a:failure"}
	first, err := Build(req)
	require.NoError(t, err)
	req.FilesChanged = []string{"a.go", "z.go"}
	req.Criteria = []string{"AC1", "AC2"}
	req.CI = []string{"a:failure", "z:success"}
	second, err := Build(req)
	require.NoError(t, err)
	require.Equal(t, first.Bytes, second.Bytes)
	require.Contains(t, string(first.Bytes), "<change & review>")
	require.NotContains(t, string(first.Bytes), `\\u003c`)
}

func TestBuildRefusesRoleCapOverflowWithNumbers(t *testing.T) {
	req := testRequest()
	req.Diff = strings.Repeat("x", 160004)
	req.BudgetTokens = 50000
	_, err := Build(req)
	require.ErrorContains(t, err, "test-author")
	require.ErrorContains(t, err, "40000")
	require.ErrorContains(t, err, "exceeds budget")
}

func TestBuildRejectsRoleBudgetAndKeepsUntrustedAsReference(t *testing.T) {
	req := testRequest()
	req.Role = workflow.RoleReviewer
	req.BudgetTokens = 1
	_, err := Build(req)
	require.ErrorContains(t, err, "reviewer")
	require.ErrorContains(t, err, "1")

	req = testRequest()
	built, err := Build(req)
	require.NoError(t, err)
	require.Contains(t, string(built.Bytes), string(req.Untrusted[0].Ref))
	require.NotContains(t, string(built.Bytes), "text")
}

func TestStoreRoundTripAndMissingBlob(t *testing.T) {
	built, err := Build(testRequest())
	require.NoError(t, err)
	store := newMemoryBlobs()
	ref, err := Store(context.Background(), store, ports.BlobBundle, built)
	require.NoError(t, err)
	require.Equal(t, Hash(built), ref)
	raw, err := store.Get(context.Background(), ref)
	require.NoError(t, err)
	contents, err := io.ReadAll(raw)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.Equal(t, built.Bytes, contents)

	_, err = store.Get(context.Background(), workflow.BlobRef("blob:sha256:"+strings.Repeat("0", 64)))
	require.ErrorIs(t, err, ports.ErrNotFound)
}

func testRequest() Request {
	return Request{
		Role: workflow.RoleTestAuthor, Change: "PAY-142", BaseSHA: "base", HeadSHA: "head",
		Diff: "diff", FilesChanged: []string{"b.go", "a.go"}, Criteria: []string{"AC1"},
		CI: []string{"acceptance:success"}, Untrusted: []UntrustedRef{{Source: "pr_body", Ref: workflow.BlobRef("blob:sha256:" + strings.Repeat("a", 64))}},
		Pack: "pack", Memory: "memory", BudgetTokens: 40000,
	}
}

type memoryBlobs struct{ data map[workflow.BlobRef][]byte }

func newMemoryBlobs() *memoryBlobs { return &memoryBlobs{data: make(map[workflow.BlobRef][]byte)} }

func (m *memoryBlobs) Put(_ context.Context, _ ports.BlobClass, r io.Reader) (workflow.BlobRef, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	ref, err := workflow.ParseBlobRef("blob:sha256:" + hex.EncodeToString(sum[:]))
	if err != nil {
		return "", err
	}
	m.data[ref] = b
	return ref, nil
}

func (m *memoryBlobs) Get(_ context.Context, ref workflow.BlobRef) (io.ReadCloser, error) {
	b, ok := m.data[ref]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

var _ ports.BlobStore = (*memoryBlobs)(nil)
