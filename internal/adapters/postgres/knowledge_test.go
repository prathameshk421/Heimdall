//go:build component

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeFileCatalogPersistsApprovalRetentionAndBlobReferences(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	db := srv.database(ctx, t)
	store := NewStore(db.pool)
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	file := ports.KnowledgeFile{Ref: "knowledge:one", Repository: change.Repo("acme/pay"), Path: "docs/adr/1.md", SHA: id.SHA("abc1234"), Group: "payments", SourceURL: "https://example.test/adr/1", Blob: workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), Kind: "adr", State: "pending", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	require.NoError(t, store.Save(ctx, file))
	got, err := store.Get(ctx, file.Ref)
	require.NoError(t, err)
	require.Equal(t, file.Ref, got.Ref)
	require.Equal(t, file.Repository, got.Repository)
	require.Equal(t, file.Path, got.Path)
	require.Equal(t, file.SHA, got.SHA)
	require.Equal(t, file.Group, got.Group)
	require.Equal(t, file.SourceURL, got.SourceURL)
	require.Equal(t, file.Blob, got.Blob)
	require.Equal(t, file.Kind, got.Kind)
	require.Equal(t, file.State, got.State)
	require.True(t, file.CreatedAt.Equal(got.CreatedAt))
	require.True(t, file.ExpiresAt.Equal(got.ExpiresAt))

	approved := file
	approved.State, approved.ApprovedBy = "approved", "owner"
	require.NoError(t, store.Save(ctx, approved))
	got, err = store.Get(ctx, file.Ref)
	require.NoError(t, err)
	require.Equal(t, "owner", got.ApprovedBy)

	second := file
	second.Ref, second.Path, second.Group = "knowledge:two", "docs/adr/1.md", "shared"
	require.NoError(t, store.Save(ctx, second))
	expired, err := store.Expired(ctx, now.Add(48*time.Hour))
	require.NoError(t, err)
	require.Len(t, expired, 2)
	last, err := store.Delete(ctx, file.Ref)
	require.NoError(t, err)
	require.False(t, last, "the shared blob must remain while another source references it")
	last, err = store.Delete(ctx, second.Ref)
	require.NoError(t, err)
	require.True(t, last, "the blob is eligible for deletion after its final reference")
	_, err = store.Get(ctx, file.Ref)
	require.ErrorIs(t, err, ports.ErrNotFound)
}

func TestKnowledgeFileCatalogRejectsApprovalDowngradeAndImmutableMetadataChanges(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	db := srv.database(ctx, t)
	store := NewStore(db.pool)
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	file := ports.KnowledgeFile{Ref: "knowledge:one", Repository: change.Repo("acme/pay"), Path: "docs/guide.md", SHA: id.SHA("abc1234"), Group: "payments", Blob: workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), Kind: "document", State: "pending", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	require.NoError(t, store.Save(ctx, file))
	approved := file
	approved.State, approved.ApprovedBy = "approved", "owner"
	require.NoError(t, store.Save(ctx, approved))
	require.Error(t, store.Save(ctx, file), "approval must not be downgraded")
	changed := approved
	changed.SourceURL = "https://attacker.invalid/changed"
	require.Error(t, store.Save(ctx, changed), "provenance is immutable after staging")
}
