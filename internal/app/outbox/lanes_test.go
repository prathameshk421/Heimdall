package outbox

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.KindClaimer = (*memOutbox)(nil)

func (m *memOutbox) ClaimKinds(_ context.Context, kinds []change.EffectKind, limit int, lease time.Duration) ([]change.Effect, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.limits = append(m.limits, limit)
	m.leases = append(m.leases, lease)
	if m.claimErr != nil {
		err := m.claimErr
		m.claimErr = nil
		return nil, err
	}
	var out []change.Effect
	for _, r := range m.rows {
		if !r.done && !r.leased && !r.started && r.attempts < 8 && slices.Contains(kinds, r.fx.Kind) && len(out) < limit {
			r.leased = true
			out = append(out, r.fx)
		}
	}
	return out, nil
}

type blockingExec struct {
	started chan change.EffectKey
	release chan struct{}
}

func (b *blockingExec) Execute(ctx context.Context, fx change.Effect) error {
	b.started <- fx.Key
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type quickExec struct {
	done chan change.EffectKey
}

func (q *quickExec) Execute(_ context.Context, fx change.Effect) error {
	q.done <- fx.Key
	return nil
}

// O2: three long sandbox effects in flight must not block a task_assign claimed after them.
func TestSlowSandboxEffectsDoNotBlockQuickTaskAssign(t *testing.T) {
	t.Parallel()
	box := newMemOutbox(
		effectOf(change.EffectRunChecks, 1),
		effectOf(change.EffectTestQuality, 2),
		effectOf(change.EffectVerifyEvidence, 3),
		effectOf(change.EffectTaskAssign, 4),
	)
	slowEx := &blockingExec{started: make(chan change.EffectKey, 3), release: make(chan struct{})}
	fastEx := &quickExec{done: make(chan change.EffectKey, 1)}
	execs := map[change.EffectKind]Executor{
		change.EffectRunChecks:      slowEx,
		change.EffectTestQuality:    slowEx,
		change.EffectVerifyEvidence: slowEx,
		change.EffectTaskAssign:     fastEx,
	}

	slowDrainer, err := New(Config{
		Outbox: box, Executors: execs, Logger: slog.New(slog.DiscardHandler),
		Batch: 1, Lease: time.Minute, EffectTimeout: 5 * time.Second,
		Kinds: []change.EffectKind{change.EffectRunChecks, change.EffectTestQuality, change.EffectVerifyEvidence},
	})
	require.NoError(t, err)
	quickDrainer, err := New(Config{
		Outbox: box, Executors: execs, Logger: slog.New(slog.DiscardHandler),
		Batch: 1, Lease: time.Minute, EffectTimeout: 5 * time.Second,
		Kinds: []change.EffectKind{change.EffectTaskAssign},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start 3 slow workers; all 3 block in flight on the 3 sandbox effects.
	for range 3 {
		go func() {
			_, _ = slowDrainer.RunOnce(ctx)
		}()
	}
	for range 3 {
		select {
		case <-slowEx.started:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for slow sandbox effects to start")
		}
	}

	// While all 3 slow effects are still in flight, the quick lane runs task_assign immediately.
	res, err := quickDrainer.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, DrainResult{Claimed: 1, Done: 1}, res)
	select {
	case key := <-fastEx.done:
		require.Equal(t, effectOf(change.EffectTaskAssign, 4).Key, key)
	case <-time.After(time.Second):
		t.Fatal("quick task_assign did not execute while slow lane was full")
	}

	close(slowEx.release)
}
