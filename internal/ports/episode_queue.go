package ports

import (
	"context"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// EpisodeQueue is the durable source/trust ledger and retry queue for Graphiti ingestion.
type EpisodeQueue interface {
	EnqueueEpisode(context.Context, Episode) error
	PendingEpisodes(context.Context, int) ([]Episode, error)
	MarkEpisodeDelivered(context.Context, change.EffectKey, time.Time) error
	InvalidateEpisode(context.Context, change.EffectKey, time.Time) error
	// DeferEpisode records a failed delivery: the episode stays pending but is not offered again until a backoff
	// that doubles with each failure (30 s, capped at 1 h) has passed, so one episode Graphiti rejects cannot
	// block the ones behind it.
	DeferEpisode(context.Context, change.EffectKey, time.Time) error
}

// EpisodeSourceTrust returns the durable status and tombstone state for one Graphiti source key.
type EpisodeSourceTrust interface {
	EpisodeTrust(context.Context, change.EffectKey) (EpisodeStatus, bool, error)
}
