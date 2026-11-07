package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.PackCatalog = (*Store)(nil)

// SavePack inserts immutable metadata for one owner/version. Repeating identical uploads is idempotent.
func (s *Store) SavePack(ctx context.Context, owner string, version int, blob workflow.BlobRef, manifest []byte, createdAt time.Time) error {
	if owner == "" || version < 1 || blob == "" || len(manifest) == 0 || createdAt.IsZero() {
		return errors.New("review pack metadata is invalid")
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO review_pack (owner, version, blob_ref, manifest, created_at)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (owner, version) DO UPDATE SET blob_ref=EXCLUDED.blob_ref
		WHERE review_pack.blob_ref=EXCLUDED.blob_ref AND review_pack.manifest=EXCLUDED.manifest
		AND review_pack.created_at=EXCLUDED.created_at`, owner, version, string(blob), manifest, createdAt)
	if err != nil {
		return fmt.Errorf("save review pack: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("review pack owner %q version %d already has different content", owner, version)
	}
	return nil
}

// LatestPack returns the newest signed manifest for the exact owner.
func (s *Store) LatestPack(ctx context.Context, owner string) (ports.PackRecord, error) {
	var record ports.PackRecord
	var blob string
	err := s.pool.QueryRow(ctx, `SELECT owner, version, blob_ref, manifest, created_at FROM review_pack WHERE owner=$1 ORDER BY version DESC LIMIT 1`, owner).
		Scan(&record.Owner, &record.Version, &blob, &record.Manifest, &record.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.PackRecord{}, fmt.Errorf("review pack for %q: %w", owner, ports.ErrNotFound)
	}
	if err != nil {
		return ports.PackRecord{}, fmt.Errorf("load latest review pack: %w", err)
	}
	record.Blob = workflow.BlobRef(blob)
	return record, nil
}

// DeletePacks deletes every version for an owner and returns only blob refs no other owner still references.
func (s *Store) DeletePacks(ctx context.Context, owner string) ([]workflow.BlobRef, error) {
	if owner == "" {
		return nil, errors.New("review pack owner is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin review pack revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `DELETE FROM review_pack WHERE owner=$1 RETURNING blob_ref`, owner)
	if err != nil {
		return nil, fmt.Errorf("delete review pack metadata: %w", err)
	}
	seen := make(map[string]struct{})
	var candidates []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan revoked pack blob: %w", err)
		}
		if _, ok := seen[ref]; !ok {
			candidates = append(candidates, ref)
			seen[ref] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate revoked packs: %w", err)
	}
	rows.Close()
	var unused []workflow.BlobRef
	for _, ref := range candidates {
		var referenced bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM review_pack WHERE blob_ref=$1)`, ref).Scan(&referenced); err != nil {
			return nil, fmt.Errorf("check remaining pack blob references: %w", err)
		}
		if !referenced {
			unused = append(unused, workflow.BlobRef(ref))
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit review pack revocation: %w", err)
	}
	return unused, nil
}
