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
}
