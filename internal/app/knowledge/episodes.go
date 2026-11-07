package knowledge

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// EpisodeWorker delivers durable source records to Graphiti. A failed call stays pending for the next sweep.
type EpisodeWorker struct {
	queue  ports.EpisodeQueue
	memory ports.Memory
	clock  ports.Clock
	mu     sync.Mutex
}

// EpisodeDeleter removes the matching Graphiti source episode.
type EpisodeDeleter interface {
	ForgetEpisode(context.Context, string, string) error
}

// QueuedEpisodeLifecycle removes a memory source and tombstones its durable retry record.
type QueuedEpisodeLifecycle struct {
	deleter EpisodeDeleter
	queue   ports.EpisodeQueue
	clock   ports.Clock
}

// NewQueuedEpisodeLifecycle requires both Graphiti deletion and ledger tombstoning boundaries.
func NewQueuedEpisodeLifecycle(deleter EpisodeDeleter, queue ports.EpisodeQueue, clock ports.Clock) (*QueuedEpisodeLifecycle, error) {
	if deleter == nil || queue == nil || clock == nil {
		return nil, errors.New("queued episode lifecycle dependencies must not be nil")
	}
	return &QueuedEpisodeLifecycle{deleter: deleter, queue: queue, clock: clock}, nil
}

// ForgetEpisode deletes the external episode and then tombstones its durable source record.
func (l *QueuedEpisodeLifecycle) ForgetEpisode(ctx context.Context, key, group string) error {
	if err := l.deleter.ForgetEpisode(ctx, key, group); err != nil {
		return fmt.Errorf("delete Graphiti source episode: %w", err)
	}
	if err := l.queue.InvalidateEpisode(ctx, change.EffectKey(key), l.clock.Now()); err != nil {
		return fmt.Errorf("tombstone memory source episode: %w", err)
	}
	return nil
}

// NewEpisodeWorker requires a durable queue, Graphiti adapter, and injected clock.
func NewEpisodeWorker(queue ports.EpisodeQueue, memory ports.Memory, clock ports.Clock) (*EpisodeWorker, error) {
	if queue == nil || memory == nil || clock == nil {
		return nil, errors.New("memory episode worker dependencies must not be nil")
	}
	return &EpisodeWorker{queue: queue, memory: memory, clock: clock}, nil
}

// Enqueue validates and durably records source status before Graphiti can receive an episode.
func (w *EpisodeWorker) Enqueue(ctx context.Context, episode ports.Episode) error {
	if err := episode.Validate(); err != nil {
		return fmt.Errorf("validate source memory episode: %w", err)
	}
	if err := w.queue.EnqueueEpisode(ctx, episode); err != nil {
		return fmt.Errorf("persist source memory episode: %w", err)
	}
	return nil
}

// maxConsecutiveFailures ends a sweep when Graphiti looks down, so a long queue does not cost one timeout each.
const maxConsecutiveFailures = 3

// RunOnce delivers at most limit episodes. A failed episode stays pending, is pushed back by the queue's backoff and
// the sweep goes on, so one episode Graphiti rejects cannot block the rest. After maxConsecutiveFailures failures
// in a row the sweep ends, because Graphiti is probably down. Graphiti uses the stable key for idempotent retry.
// The error names the first failed episode.
func (w *EpisodeWorker) RunOnce(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		return 0, errors.New("memory episode batch limit must be 1 to 1000")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	items, err := w.queue.PendingEpisodes(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("load pending memory episodes: %w", err)
	}
	delivered, streak := 0, 0
	var first error
	for _, episode := range items {
		if err := ctx.Err(); err != nil {
			return delivered, err
		}
		if err := episode.Validate(); err != nil {
			return delivered, fmt.Errorf("pending episode %q is invalid: %w", episode.Key, err)
		}
		if err := w.memory.AddEpisode(ctx, episode); err != nil {
			if first == nil {
				first = fmt.Errorf("deliver memory episode %q to Graphiti: %w", episode.Key, err)
			}
			if err := w.queue.DeferEpisode(ctx, change.EffectKey(episode.Key), w.clock.Now()); err != nil {
				return delivered, fmt.Errorf("defer memory episode %q: %w", episode.Key, err)
			}
			if streak++; streak >= maxConsecutiveFailures {
				break
			}
			continue
		}
		streak = 0
		if err := w.queue.MarkEpisodeDelivered(ctx, change.EffectKey(episode.Key), w.clock.Now()); err != nil {
			return delivered, fmt.Errorf("acknowledge memory episode %q: %w", episode.Key, err)
		}
		delivered++
	}
	return delivered, first
}
