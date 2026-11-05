package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// memRow is one outbox row of the in-memory outbox.
type memRow struct {
	fx       change.Effect
	done     bool
	leased   bool
	attempts int
	lastErr  string
}

// memOutbox is an in-memory ports.Outbox. A lease is a flag, not a clock: lease expiry is tested against PostgreSQL in
// the component test, so these tests check what the drainer decides.
type memOutbox struct {
	mu       sync.Mutex
	rows     []*memRow
	limits   []int
	leases   []time.Duration
	claimErr error
	doneErr  func(change.EffectKey) error
}

var _ ports.Outbox = (*memOutbox)(nil)

func newMemOutbox(fxs ...change.Effect) *memOutbox {
	m := &memOutbox{}
	m.add(fxs...)
	return m
}

func (m *memOutbox) add(fxs ...change.Effect) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, fx := range fxs {
		m.rows = append(m.rows, &memRow{fx: fx})
	}
}

func (m *memOutbox) row(key change.EffectKey) *memRow {
	for _, r := range m.rows {
		if r.fx.Key == key {
			return r
		}
	}
	return nil
}

func (m *memOutbox) failNextClaim(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.claimErr = err
}

func (m *memOutbox) state(key change.EffectKey) memRow {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *m.row(key)
}

func (m *memOutbox) Pending(_ context.Context, limit int) ([]change.Effect, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []change.Effect
	for _, r := range m.rows {
		if !r.done && r.attempts < 8 && len(out) < limit {
			out = append(out, r.fx)
		}
	}
	return out, nil
}

func (m *memOutbox) Claim(_ context.Context, limit int, lease time.Duration) ([]change.Effect, error) {
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
		if !r.done && !r.leased && r.attempts < 8 && len(out) < limit {
			r.leased = true
			out = append(out, r.fx)
		}
	}
	return out, nil
}

func (m *memOutbox) ClaimEffect(_ context.Context, key change.EffectKey, lease time.Duration) (*change.Effect, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leases = append(m.leases, lease)
	for _, r := range m.rows {
		if r.fx.Key != key {
			continue
		}
		if r.done || r.attempts >= 8 {
			return nil, nil
		}
		if r.leased {
			return nil, ports.ErrEffectLeased
		}
		r.leased = true
		fx := r.fx
		return &fx, nil
	}
	return nil, nil
}

func (m *memOutbox) Done(ctx context.Context, key change.EffectKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("mark %s done: %w", key, err)
	}
	if m.doneErr != nil {
		if err := m.doneErr(key); err != nil {
			return err
		}
	}
	r := m.row(key)
	if r == nil {
		return fmt.Errorf("mark %s done: %w", key, ports.ErrNotFound)
	}
	r.done = true
	r.leased = false
	return nil
}

func (m *memOutbox) Fail(ctx context.Context, key change.EffectKey, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("record failure of %s: %w", key, err)
	}
	r := m.row(key)
	if r == nil {
		return fmt.Errorf("record failure of %s: %w", key, ports.ErrNotFound)
	}
	if r.done {
		return fmt.Errorf("record failure of %s: the effect is already done", key)
	}
	r.attempts++
	r.lastErr = reason
	r.leased = false
	return nil
}

// fakeExec is an executor that records the effects it is given. fn, when set, decides the result.
type fakeExec struct {
	mu   sync.Mutex
	seen []change.Effect
	fn   func(context.Context, change.Effect) error
}

func (e *fakeExec) Execute(ctx context.Context, fx change.Effect) error {
	e.mu.Lock()
	e.seen = append(e.seen, fx)
	fn := e.fn
	e.mu.Unlock()
	if fn != nil {
		return fn(ctx, fx)
	}
	return nil
}

func (e *fakeExec) keys() []change.EffectKey {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]change.EffectKey, 0, len(e.seen))
	for _, fx := range e.seen {
		out = append(out, fx.Key)
	}
	return out
}

func effectOf(kind change.EffectKind, n int) change.Effect {
	return change.Effect{
		Key:     change.EffectKey(fmt.Sprintf("%s:PAY-142:%d:d", kind, n)),
		Kind:    kind,
		Target:  "room-42",
		Payload: json.RawMessage(fmt.Sprintf(`{"n":%d}`, n)),
	}
}

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

// allKinds maps every kind to one executor.
func allKinds(ex Executor) map[change.EffectKind]Executor {
	return map[change.EffectKind]Executor{
		change.EffectGitHubCheck:   ex,
		change.EffectGitHubComment: ex,
		change.EffectRoomPost:      ex,
	}
}

// config is a valid configuration: batch 10, a minute of lease, a second per effect.
func config(box ports.Outbox, execs map[change.EffectKind]Executor) Config {
	return Config{
		Outbox:        box,
		Executors:     execs,
		Logger:        discard(),
		Batch:         10,
		Lease:         time.Minute,
		EffectTimeout: time.Second,
	}
}

func mustNew(t *testing.T, cfg Config) *Drainer {
	t.Helper()
	d, err := New(cfg)
	require.NoError(t, err)
	return d
}

func TestNewRefusesBadConfiguration(t *testing.T) {
	ex := &fakeExec{}
	box := newMemOutbox()

	require.NoError(t, func() error { _, err := New(config(box, allKinds(ex))); return err }(),
		"the base configuration is valid, so each case below changes one thing")

	tests := []struct {
		name   string
		change func(*Config)
		want   string
	}{
		{"an empty executor map", func(c *Config) { c.Executors = map[change.EffectKind]Executor{} }, "no executors"},
		{"a nil executor for a kind", func(c *Config) {
			c.Executors = map[change.EffectKind]Executor{change.EffectRoomPost: nil}
		}, `executor for kind "room_post" is nil`},
		{"a kind that is not an effect kind", func(c *Config) {
			c.Executors = map[change.EffectKind]Executor{change.EffectKind("carrier_pigeon"): ex}
		}, `"carrier_pigeon": not an effect kind`},
		{"a nil outbox", func(c *Config) { c.Outbox = nil }, "outbox is nil"},
		{"a nil logger", func(c *Config) { c.Logger = nil }, "logger is nil"},
		{"a batch of zero", func(c *Config) { c.Batch = 0 }, "batch 0"},
		{"a zero effect timeout", func(c *Config) { c.EffectTimeout = 0 }, "effect timeout"},
		{"a zero lease", func(c *Config) { c.Lease = 0 }, "lease"},
		{"a lease that does not cover the batch", func(c *Config) {
			c.Batch = 10
			c.EffectTimeout = time.Second
			c.Lease = 5 * time.Second
		}, "must exceed batch 10 times the effect timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config(box, allKinds(ex))
			tt.change(&cfg)
			d, err := New(cfg)
			require.Nil(t, d)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestRunOnceExecutesEveryEffectAndMarksItDone(t *testing.T) {
	ctx := context.Background()
	fx := []change.Effect{
		effectOf(change.EffectRoomPost, 1),
		effectOf(change.EffectGitHubCheck, 2),
		effectOf(change.EffectGitHubComment, 3),
	}
	box := newMemOutbox(fx...)
	ex := &fakeExec{}
	d := mustNew(t, config(box, allKinds(ex)))

	res, err := d.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, DrainResult{Claimed: 3, Done: 3}, res)
	require.Equal(t, []change.EffectKey{fx[0].Key, fx[1].Key, fx[2].Key}, ex.keys(), "executed in claim order")
	for _, f := range fx {
		require.True(t, box.state(f.Key).done, f.Key)
	}
	pending, err := box.Pending(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, pending)
}

func TestExecutorErrorIsRecordedWithItsTextAndRetried(t *testing.T) {
	ctx := context.Background()
	fx := effectOf(change.EffectRoomPost, 1)
	box := newMemOutbox(fx)
	ex := &fakeExec{fn: func(context.Context, change.Effect) error { return errors.New("switch 503: room busy") }}
	d := mustNew(t, config(box, allKinds(ex)))

	res, err := d.RunOnce(ctx)
	require.NoError(t, err, "an executor error is an outcome, not a drainer error")
	require.Equal(t, DrainResult{Claimed: 1, Failed: 1}, res)

	st := box.state(fx.Key)
	require.False(t, st.done)
	require.Equal(t, 1, st.attempts)
	require.Equal(t, "switch 503: room busy", st.lastErr, "the reason is the executor's error text and nothing else")
	require.False(t, st.leased, "Fail releases the lease, so the effect is claimable again")

	ex.fn = nil
	res, err = d.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, DrainResult{Claimed: 1, Done: 1}, res)
	require.True(t, box.state(fx.Key).done)
}

func TestEffectWithoutExecutorIsFailedWithAClearReason(t *testing.T) {
	ctx := context.Background()
	fx := effectOf(change.EffectRoomPost, 1)
	box := newMemOutbox(fx)
	github := &fakeExec{}
	d := mustNew(t, config(box, map[change.EffectKind]Executor{change.EffectGitHubCheck: github}))

	res, err := d.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, DrainResult{Claimed: 1, Failed: 1}, res)
	require.Equal(t, `no executor for effect kind "room_post"`, box.state(fx.Key).lastErr)
	require.Empty(t, github.keys(), "an executor for another kind is never called")
	require.False(t, box.state(fx.Key).done)
}

func TestBatchSizeIsHonoured(t *testing.T) {
	ctx := context.Background()
	var fx []change.Effect
	for i := range 7 {
		fx = append(fx, effectOf(change.EffectRoomPost, i+1))
	}
	box := newMemOutbox(fx...)
	ex := &fakeExec{}
	cfg := config(box, allKinds(ex))
	cfg.Batch = 3
	d := mustNew(t, cfg)

	var claimed []int
	for range 3 {
		res, err := d.RunOnce(ctx)
		require.NoError(t, err)
		claimed = append(claimed, res.Claimed)
	}
	require.Equal(t, []int{3, 3, 1}, claimed)
	require.Equal(t, []int{3, 3, 3}, box.limits, "every claim asks for the configured batch size")
	require.Equal(t, []time.Duration{time.Minute, time.Minute, time.Minute}, box.leases, "and the configured lease")
}

func TestCancellationStopsTheBatchButKeepsEarlierDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fx := []change.Effect{effectOf(change.EffectRoomPost, 1), effectOf(change.EffectRoomPost, 2), effectOf(change.EffectRoomPost, 3)}
	box := newMemOutbox(fx...)
	ex := &fakeExec{fn: func(context.Context, change.Effect) error {
		cancel() // the shutdown arrives while the first effect runs
		return nil
	}}
	d := mustNew(t, config(box, allKinds(ex)))

	res, err := d.RunOnce(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, DrainResult{Claimed: 3, Done: 1}, res)
	require.True(t, box.state(fx[0].Key).done, "an executed effect is still recorded as done after cancellation")
	require.Equal(t, []change.EffectKey{fx[0].Key}, ex.keys(), "the rest of the batch is not executed")
	require.False(t, box.state(fx[1].Key).done)
	require.False(t, box.state(fx[2].Key).done)
}

func TestFailedDoneIsReturnedAndTheEffectStaysLeased(t *testing.T) {
	ctx := context.Background()
	fx := effectOf(change.EffectRoomPost, 1)
	box := newMemOutbox(fx)
	box.doneErr = func(change.EffectKey) error { return errors.New("connection reset") }
	ex := &fakeExec{}
	d := mustNew(t, config(box, allKinds(ex)))

	res, err := d.RunOnce(ctx)
	require.ErrorContains(t, err, "connection reset")
	require.Equal(t, DrainResult{Claimed: 1}, res, "the effect ran, but it is not counted as done")
	require.Equal(t, []change.EffectKey{fx.Key}, ex.keys())
	require.False(t, box.state(fx.Key).done)
	require.True(t, box.state(fx.Key).leased, "the lease is left to expire, so the effect is retried later")
}

func TestEffectDeadlineBoundsTheExecutor(t *testing.T) {
	ctx := context.Background()
	fx := effectOf(change.EffectRoomPost, 1)
	box := newMemOutbox(fx)
	ex := &fakeExec{fn: func(ctx context.Context, _ change.Effect) error {
		<-ctx.Done() // an executor that hangs until its deadline
		return ctx.Err()
	}}
	cfg := config(box, allKinds(ex))
	cfg.EffectTimeout = 50 * time.Millisecond
	d := mustNew(t, cfg)

	res, err := d.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, DrainResult{Claimed: 1, Failed: 1}, res)
	require.Contains(t, box.state(fx.Key).lastErr, "context deadline exceeded")
}

func TestRunDoesWorkOnWakeAndTickAndStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := effectOf(change.EffectRoomPost, 1)
	box := newMemOutbox(first)
	ex := &fakeExec{}
	d := mustNew(t, config(box, allKinds(ex)))

	wake := make(chan struct{})
	tick := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx, wake, tick) }()

	require.Eventually(t, func() bool { return box.state(first.Key).done }, 10*time.Second, 10*time.Millisecond,
		"the first batch runs without waiting")

	second := effectOf(change.EffectGitHubCheck, 2)
	box.add(second)
	sendWake(t, wake)
	require.Eventually(t, func() bool { return box.state(second.Key).done }, 10*time.Second, 10*time.Millisecond,
		"a wake starts a batch")

	third := effectOf(change.EffectGitHubComment, 3)
	box.add(third)
	sendTick(t, tick)
	require.Eventually(t, func() bool { return box.state(third.Key).done }, 10*time.Second, 10*time.Millisecond,
		"a tick starts a batch")

	cancel()
	require.NoError(t, awaitRun(t, done), "Run returns nil when its context is cancelled")
}

// sendWake and sendTick hand a signal to Run, failing the test rather than hanging when Run never receives it.
func sendWake(t *testing.T, wake chan<- struct{}) {
	t.Helper()
	select {
	case wake <- struct{}{}:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not receive a wake")
	}
}

func sendTick(t *testing.T, tick chan<- time.Time) {
	t.Helper()
	select {
	case tick <- time.Now():
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not receive a tick")
	}
}

// awaitRun waits for Run to return, failing the test rather than hanging.
func awaitRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancellation")
		return nil
	}
}

func TestRunKeepsGoingAfterAFailedClaim(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fx := effectOf(change.EffectRoomPost, 1)
	box := newMemOutbox(fx)
	box.failNextClaim(errors.New("claim: database unavailable"))
	ex := &fakeExec{}
	d := mustNew(t, config(box, allKinds(ex)))

	wake := make(chan struct{})
	tick := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx, wake, tick) }()

	// The first batch fails to claim, so nothing has run; the next tick finds the database again.
	require.Empty(t, ex.keys())
	sendTick(t, tick)
	require.Eventually(t, func() bool { return box.state(fx.Key).done }, 10*time.Second, 10*time.Millisecond,
		"a failed claim is logged and the drainer carries on")

	cancel()
	require.NoError(t, awaitRun(t, done))
}
