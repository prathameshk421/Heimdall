// Package knowledgepostgres stores knowledge intake provenance independently from the event-store schema.
package knowledgepostgres

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Repository is the durable knowledge provenance store.
type Repository struct{ pool *pgxpool.Pool }

var _ ports.KnowledgeRepository = (*Repository)(nil)

// New constructs a repository using the application's shared PostgreSQL pool.
func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("knowledge postgres: nil pool")
	}
	return &Repository{pool: pool}, nil
}

// Migrate applies only this adapter's migrations; it does not modify the event-store migration sequence.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("knowledge postgres: nil pool")
	}
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("knowledge migrations: %w", err)
	}
	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, sub, goose.WithTableName("knowledge_goose_db_version"))
	if err != nil {
		return fmt.Errorf("knowledge migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply knowledge migrations: %w", err)
	}
	return nil
}

// Find returns the record for the scoped idempotency key.
func (r *Repository) Find(ctx context.Context, team, key string) (ports.KnowledgeRecord, bool, error) {
	record, err := scanRecord(r.pool.QueryRow(ctx, selectRecord+` WHERE team=$1 AND idempotency_key=$2`, team, key))
	if errors.Is(err, sql.ErrNoRows) {
		return ports.KnowledgeRecord{}, false, nil
	}
	if err != nil {
		return ports.KnowledgeRecord{}, false, fmt.Errorf("find knowledge record: %w", err)
	}
	return record, true, nil
}

// Begin atomically inserts a pending record or returns the record that won the replay race.
func (r *Repository) Begin(ctx context.Context, rec ports.KnowledgeRecord) (ports.KnowledgeRecord, error) {
	_, err := r.pool.Exec(ctx, `INSERT INTO knowledge_records
		(team,idempotency_key,source_path,source_digest,content_digest,approved_by,subject,redacted_content,state)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'pending') ON CONFLICT (team,idempotency_key) DO NOTHING`,
		rec.Team, rec.IdempotencyKey, rec.SourcePath, rec.SourceDigest, rec.ContentDigest, rec.ApprovedBy, rec.Subject, rec.Content)
	if err != nil {
		return ports.KnowledgeRecord{}, fmt.Errorf("insert pending knowledge record: %w", err)
	}
	stored, found, err := r.Find(ctx, rec.Team, rec.IdempotencyKey)
	if err != nil {
		return ports.KnowledgeRecord{}, err
	}
	if !found {
		return ports.KnowledgeRecord{}, errors.New("inserted knowledge record was not visible")
	}
	return stored, nil
}

// Complete marks the record complete only after the external memory write succeeds.
func (r *Repository) Complete(ctx context.Context, team, key string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE knowledge_records SET state='completed', completed_at=now()
		WHERE team=$1 AND idempotency_key=$2 AND state='pending'`, team, key)
	if err != nil {
		return fmt.Errorf("complete knowledge record: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var state string
		if err := r.pool.QueryRow(ctx, `SELECT state FROM knowledge_records WHERE team=$1 AND idempotency_key=$2`, team, key).Scan(&state); err != nil {
			return fmt.Errorf("verify knowledge completion: %w", err)
		}
		if state != string(ports.KnowledgeComplete) {
			return errors.New("knowledge record is not pending")
		}
	}
	return nil
}

const selectRecord = `SELECT team,source_path,source_digest,content_digest,idempotency_key,approved_by,subject,redacted_content,state FROM knowledge_records`

type rowScanner interface{ Scan(...any) error }

func scanRecord(row rowScanner) (ports.KnowledgeRecord, error) {
	var rec ports.KnowledgeRecord
	var state string
	err := row.Scan(&rec.Team, &rec.SourcePath, &rec.SourceDigest, &rec.ContentDigest, &rec.IdempotencyKey, &rec.ApprovedBy, &rec.Subject, &rec.Content, &state)
	rec.State = ports.KnowledgeState(state)
	return rec, err
}
