package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// Found live on KAN-9: a test_quality effect runs two sandbox jobs of up to 3 minutes each, but every effect got the same
// 20 s, so the outbox cancelled it mid-run and each retry started another container. A kind may carry its own timeout.
func TestAKindTimeoutReplacesTheDefaultForThatKindOnly(t *testing.T) {
	ctx := context.Background()
	long := effectOf(change.EffectRoomPost, 1)
	short := effectOf(change.EffectGitHubCheck, 2)
	box := newMemOutbox(long, short)
	deadlines := map[change.EffectKind]time.Duration{}
	ex := &fakeExec{fn: func(ctx context.Context, fx change.Effect) error {
		dl, ok := ctx.Deadline()
		require.True(t, ok)
		deadlines[fx.Kind] = time.Until(dl)
		return nil
	}}
	cfg := config(box, allKinds(ex))
	cfg.KindTimeouts = map[change.EffectKind]time.Duration{change.EffectRoomPost: 5 * time.Second}
	cfg.Lease = 2 * time.Minute
	d := mustNew(t, cfg)

	_, err := d.RunOnce(ctx)
	require.NoError(t, err)
	require.Greater(t, deadlines[change.EffectRoomPost], 4*time.Second, "the kind timeout applies")
	require.LessOrEqual(t, deadlines[change.EffectGitHubCheck], time.Second, "other kinds keep the default")
}

// The lease covers a whole batch run back to back, so it must exceed the batch size times the longest timeout.
func TestTheLeaseMustCoverABatchOfTheLongestKind(t *testing.T) {
	cfg := config(newMemOutbox(), allKinds(&fakeExec{}))
	cfg.KindTimeouts = map[change.EffectKind]time.Duration{change.EffectRoomPost: 10 * time.Second}
	_, err := New(cfg) // batch 10 x 10 s = 100 s, lease 1 min
	require.ErrorContains(t, err, "lease")

	cfg.KindTimeouts = map[change.EffectKind]time.Duration{change.EffectRoomPost: -time.Second}
	cfg.Lease = time.Hour
	_, err = New(cfg)
	require.ErrorContains(t, err, "must be positive")
}
