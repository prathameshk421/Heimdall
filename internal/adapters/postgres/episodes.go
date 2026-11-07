package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.EpisodeQueue = (*Store)(nil)
var _ ports.EpisodeSourceTrust = (*Store)(nil)

// EnqueueEpisode stores source status before Graphiti delivery and deduplicates exact source versions.
func (s *Store) EnqueueEpisode(ctx context.Context, episode ports.Episode) error {
	if err := episode.Validate(); err != nil {
		return fmt.Errorf("validate queued memory episode: %w", err)
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO memory_episode_queue (episode_key, source, status, group_id, body)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (episode_key) DO UPDATE SET episode_key=EXCLUDED.episode_key
		WHERE memory_episode_queue.source=EXCLUDED.source AND memory_episode_queue.status=EXCLUDED.status
		AND memory_episode_queue.group_id=EXCLUDED.group_id AND memory_episode_queue.body=EXCLUDED.body
		AND memory_episode_queue.invalidated_at IS NULL`,
		string(episode.Key), episode.Source, string(episode.Status), episode.GroupID, episode.Body)
	if err != nil {
		return fmt.Errorf("enqueue memory episode: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("memory episode key %q already has different provenance or content", episode.Key)
	}
	return nil
}

// PendingEpisodes returns retryable source records in insertion order.
func (s *Store) PendingEpisodes(ctx context.Context, limit int) ([]ports.Episode, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("memory episode batch limit must be 1 to 1000")
	}
	rows, err := s.pool.Query(ctx, `SELECT episode_key, source, status, group_id, body FROM memory_episode_queue
		WHERE delivered_at IS NULL AND invalidated_at IS NULL ORDER BY created_at, episode_key LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending memory episodes: %w", err)
	}
	defer rows.Close()
	var episodes []ports.Episode
	for rows.Next() {
		var e ports.Episode
		var key, status string
		if err := rows.Scan(&key, &e.Source, &status, &e.GroupID, &e.Body); err != nil {
			return nil, fmt.Errorf("scan queued memory episode: %w", err)
		}
		e.Key, e.Status = change.EffectKey(key), ports.EpisodeStatus(status)
		episodes = append(episodes, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending memory episodes: %w", err)
	}
	return episodes, nil
}

// MarkEpisodeDelivered records successful Graphiti ingestion. Repeating the acknowledgement is harmless.
func (s *Store) MarkEpisodeDelivered(ctx context.Context, key change.EffectKey, at time.Time) error {
	if key == "" || at.IsZero() {
		return errors.New("memory episode delivery acknowledgement is invalid")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE memory_episode_queue SET delivered_at=COALESCE(delivered_at,$2) WHERE episode_key=$1`, string(key), at)
	if err != nil {
		return fmt.Errorf("mark memory episode delivered: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("memory episode %q: %w", key, ports.ErrNotFound)
	}
	return nil
}

// InvalidateEpisode tombstones a source so pending retries and later duplicate events cannot reingest deleted memory.
func (s *Store) InvalidateEpisode(ctx context.Context, key change.EffectKey, at time.Time) error {
	if key == "" || at.IsZero() {
		return errors.New("memory episode invalidation is invalid")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE memory_episode_queue SET invalidated_at=COALESCE(invalidated_at,$2) WHERE episode_key=$1`, string(key), at)
	if err != nil {
		return fmt.Errorf("invalidate memory episode: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("memory episode %q: %w", key, ports.ErrNotFound)
	}
	return nil
}

// EpisodeDeliveryTime reports when the source episode was accepted by Graphiti. Used by source-status audits.
func (s *Store) EpisodeDeliveryTime(ctx context.Context, key change.EffectKey) (time.Time, error) {
	var delivered time.Time
	err := s.pool.QueryRow(ctx, `SELECT delivered_at FROM memory_episode_queue WHERE episode_key=$1 AND delivered_at IS NOT NULL`, string(key)).Scan(&delivered)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, fmt.Errorf("memory episode %q is not delivered: %w", key, ports.ErrNotFound)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("read memory episode delivery: %w", err)
	}
	return delivered, nil
}

// EpisodeTrust reads source trust from the durable ledger, never from a model-generated Graphiti label.
func (s *Store) EpisodeTrust(ctx context.Context, key change.EffectKey) (ports.EpisodeStatus, bool, error) {
	if key == "" {
		return "", false, errors.New("memory source key is required")
	}
	var status string
	var invalidated bool
	err := s.pool.QueryRow(ctx, `SELECT status, invalidated_at IS NOT NULL FROM memory_episode_queue WHERE episode_key=$1`, string(key)).Scan(&status, &invalidated)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("memory source %q: %w", key, ports.ErrNotFound)
	}
	if err != nil {
		return "", false, fmt.Errorf("read memory source trust: %w", err)
	}
	return ports.EpisodeStatus(status), invalidated, nil
}
