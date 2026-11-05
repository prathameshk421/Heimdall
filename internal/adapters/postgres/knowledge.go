package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.KnowledgeFiles = (*Store)(nil)

const knowledgeFileColumns = `ref, repository, path, sha, group_id, source_url, blob_ref, kind, state, created_at, expires_at, approved_by`

// Save persists knowledge-file provenance. Immutable source metadata cannot be changed by an approval update.
func (s *Store) Save(ctx context.Context, file ports.KnowledgeFile) error {
	if !validKnowledgeFile(file) {
		return errors.New("knowledge file metadata is invalid")
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO knowledge_file (`+knowledgeFileColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (ref) DO UPDATE SET state = EXCLUDED.state, approved_by = EXCLUDED.approved_by
		WHERE knowledge_file.repository = EXCLUDED.repository AND knowledge_file.path = EXCLUDED.path
		  AND knowledge_file.sha = EXCLUDED.sha AND knowledge_file.group_id = EXCLUDED.group_id
		  AND knowledge_file.source_url = EXCLUDED.source_url AND knowledge_file.blob_ref = EXCLUDED.blob_ref
		  AND knowledge_file.kind = EXCLUDED.kind AND knowledge_file.created_at = EXCLUDED.created_at
		  AND knowledge_file.expires_at = EXCLUDED.expires_at
		  AND (knowledge_file.state = 'pending' OR EXCLUDED.state = 'approved')`,
		file.Ref, string(file.Repository), file.Path, string(file.SHA), file.Group, file.SourceURL, string(file.Blob),
		file.Kind, file.State, file.CreatedAt, file.ExpiresAt, file.ApprovedBy)
	if err != nil {
		return fmt.Errorf("save knowledge file %q: %w", file.Ref, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("knowledge file %q conflicts with immutable provenance or approval state", file.Ref)
	}
	return nil
}

func validKnowledgeFile(file ports.KnowledgeFile) bool {
	return validKnowledgeIdentity(file) && validKnowledgeState(file) && validKnowledgeRetention(file)
}

func validKnowledgeIdentity(file ports.KnowledgeFile) bool {
	return file.Ref != "" && file.Repository != "" && file.Path != "" && file.SHA != "" && file.Group != "" && file.Blob != "" &&
		(file.Kind == "adr" || file.Kind == "document")
}

func validKnowledgeState(file ports.KnowledgeFile) bool {
	return (file.State == "pending" && file.ApprovedBy == "") || (file.State == "approved" && file.ApprovedBy != "")
}

func validKnowledgeRetention(file ports.KnowledgeFile) bool {
	return !file.CreatedAt.IsZero() && file.ExpiresAt.After(file.CreatedAt)
}

// Get returns provenance for one stable knowledge reference.
func (s *Store) Get(ctx context.Context, ref string) (ports.KnowledgeFile, error) {
	file, err := scanKnowledgeFile(s.pool.QueryRow(ctx, `SELECT `+knowledgeFileColumns+` FROM knowledge_file WHERE ref = $1`, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.KnowledgeFile{}, fmt.Errorf("knowledge file %q: %w", ref, ports.ErrNotFound)
	}
	if err != nil {
		return ports.KnowledgeFile{}, fmt.Errorf("load knowledge file %q: %w", ref, err)
	}
	return file, nil
}

// Expired returns files due for deletion in stable reference order.
func (s *Store) Expired(ctx context.Context, at time.Time) ([]ports.KnowledgeFile, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+knowledgeFileColumns+` FROM knowledge_file WHERE expires_at <= $1 ORDER BY expires_at, ref`, at)
	if err != nil {
		return nil, fmt.Errorf("list expired knowledge files: %w", err)
	}
	defer rows.Close()
	files := make([]ports.KnowledgeFile, 0)
	for rows.Next() {
		file, err := scanKnowledgeFile(rows)
		if err != nil {
			return nil, fmt.Errorf("scan expired knowledge file: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired knowledge files: %w", err)
	}
	return files, nil
}

// Delete removes one metadata row and reports whether its content-addressed blob has no remaining references.
func (s *Store) Delete(ctx context.Context, ref string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin knowledge-file deletion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var blob string
	err = tx.QueryRow(ctx, `SELECT blob_ref FROM knowledge_file WHERE ref = $1 FOR UPDATE`, ref).Scan(&blob)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("knowledge file %q: %w", ref, ports.ErrNotFound)
	}
	if err != nil {
		return false, fmt.Errorf("lock knowledge file %q: %w", ref, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM knowledge_file WHERE ref = $1`, ref); err != nil {
		return false, fmt.Errorf("delete knowledge file %q: %w", ref, err)
	}
	var refs int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM knowledge_file WHERE blob_ref = $1`, blob).Scan(&refs); err != nil {
		return false, fmt.Errorf("count knowledge blob references: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit knowledge-file deletion: %w", err)
	}
	return refs == 0, nil
}

type knowledgeRow interface {
	Scan(...any) error
}

func scanKnowledgeFile(row knowledgeRow) (ports.KnowledgeFile, error) {
	var file ports.KnowledgeFile
	var repo, sha, blob string
	err := row.Scan(&file.Ref, &repo, &file.Path, &sha, &file.Group, &file.SourceURL, &blob, &file.Kind, &file.State,
		&file.CreatedAt, &file.ExpiresAt, &file.ApprovedBy)
	if err != nil {
		return ports.KnowledgeFile{}, err
	}
	file.Repository, file.SHA, file.Blob = change.Repo(repo), id.SHA(sha), workflow.BlobRef(blob)
	return file, nil
}
