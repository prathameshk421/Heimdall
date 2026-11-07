package knowledge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestEpisodeWorkerLeavesFailedGraphitiWritePendingAndAcknowledgesSuccess(t *testing.T) {
	ctx := context.Background()
	queue := &episodeQueueSpy{pending: []ports.Episode{{Key: change.EffectKey("episode:decision:one"), Source: "decision", Status: ports.EpisodeConfirmed, GroupID: "payments", Body: `{"claim":"safe"}`}}}
	memory := &episodeMemorySpy{fail: true}
	worker, err := NewEpisodeWorker(queue, memory, knowledgeClock{now: time.Now()})
	require.NoError(t, err)
	count, err := worker.RunOnce(ctx, 10)
	require.Zero(t, count)
	require.ErrorContains(t, err, "Graphiti")
	require.Empty(t, queue.delivered, "failed Graphiti writes remain retryable")

	memory.fail = false
	count, err = worker.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, []change.EffectKey{queue.pending[0].Key}, queue.delivered)
	require.Equal(t, 2, memory.adds)
}

type episodeQueueSpy struct {
	pending     []ports.Episode
	delivered   []change.EffectKey
	invalidated []change.EffectKey
	deferred    []change.EffectKey
}

func (q *episodeQueueSpy) DeferEpisode(_ context.Context, key change.EffectKey, _ time.Time) error {
	q.deferred = append(q.deferred, key)
	return nil
}

func (q *episodeQueueSpy) EnqueueEpisode(context.Context, ports.Episode) error { return nil }
func (q *episodeQueueSpy) PendingEpisodes(context.Context, int) ([]ports.Episode, error) {
	return append([]ports.Episode(nil), q.pending...), nil
}
func (q *episodeQueueSpy) MarkEpisodeDelivered(_ context.Context, key change.EffectKey, _ time.Time) error {
	q.delivered = append(q.delivered, key)
	return nil
}
func (q *episodeQueueSpy) InvalidateEpisode(_ context.Context, key change.EffectKey, _ time.Time) error {
	q.invalidated = append(q.invalidated, key)
	return nil
}

type episodeMemorySpy struct {
	adds     int
	fail     bool
	failKeys map[change.EffectKey]bool
}

func (m *episodeMemorySpy) AddEpisode(_ context.Context, e ports.Episode) error {
	m.adds++
	if m.fail || m.failKeys[e.Key] {
		return errors.New("graphiti unavailable")
	}
	return nil
}
func (*episodeMemorySpy) Search(context.Context, ports.MemoryQuery) ([]ports.MemoryItem, error) {
	return nil, nil
}

var _ ports.EpisodeQueue = (*episodeQueueSpy)(nil)
var _ ports.Memory = (*episodeMemorySpy)(nil)

type episodeDeleteSpy struct {
	err  error
	keys []string
}

func (d *episodeDeleteSpy) ForgetEpisode(_ context.Context, key, group string) error {
	d.keys = append(d.keys, key+"@"+group)
	return d.err
}

func TestQueuedEpisodeLifecycleTombstonesOnlyAfterGraphitiDelete(t *testing.T) {
	queue := &episodeQueueSpy{}
	deleter := &episodeDeleteSpy{err: errors.New("Graphiti unavailable")}
	lifecycle, err := NewQueuedEpisodeLifecycle(deleter, queue, knowledgeClock{now: time.Now()})
	require.NoError(t, err)
	err = lifecycle.ForgetEpisode(context.Background(), "episode:knowledge:one", "payments")
	require.ErrorContains(t, err, "Graphiti")
	require.Empty(t, queue.invalidated)

	deleter.err = nil
	require.NoError(t, lifecycle.ForgetEpisode(context.Background(), "episode:knowledge:one", "payments"))
	require.Equal(t, []change.EffectKey{"episode:knowledge:one"}, queue.invalidated)
	require.Equal(t, []string{"episode:knowledge:one@payments", "episode:knowledge:one@payments"}, deleter.keys)
}

func episodesNamed(keys ...string) []ports.Episode {
	var out []ports.Episode
	for _, k := range keys {
		out = append(out, ports.Episode{Key: change.EffectKey(k), Source: "decision", Status: ports.EpisodeConfirmed, GroupID: "payments", Body: `{"claim":"safe"}`})
	}
	return out
}

func TestEpisodeWorkerSkipsAnEpisodeGraphitiRejectsSoLaterOnesStillArrive(t *testing.T) {
	queue := &episodeQueueSpy{pending: episodesNamed("episode:a", "episode:poison", "episode:c")}
	memory := &episodeMemorySpy{failKeys: map[change.EffectKey]bool{"episode:poison": true}}
	worker, err := NewEpisodeWorker(queue, memory, knowledgeClock{now: time.Now()})
	require.NoError(t, err)
	count, err := worker.RunOnce(context.Background(), 10)
	require.Equal(t, 2, count)
	require.ErrorContains(t, err, "episode:poison")
	require.Equal(t, []change.EffectKey{"episode:a", "episode:c"}, queue.delivered)
	require.Equal(t, []change.EffectKey{"episode:poison"}, queue.deferred, "the failed episode is pushed back, not retried at once")
}

func TestEpisodeWorkerStopsASweepAfterThreeFailuresInARow(t *testing.T) {
	queue := &episodeQueueSpy{pending: episodesNamed("episode:1", "episode:2", "episode:3", "episode:4", "episode:5")}
	memory := &episodeMemorySpy{fail: true}
	worker, err := NewEpisodeWorker(queue, memory, knowledgeClock{now: time.Now()})
	require.NoError(t, err)
	count, err := worker.RunOnce(context.Background(), 10)
	require.Zero(t, count)
	require.Error(t, err)
	require.Equal(t, 3, memory.adds, "Graphiti is down: do not spend a timeout on every queued episode")
	require.Len(t, queue.deferred, 3)
}
