package reconcile

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// changingProfile answers what the profile store holds now, as a later write would change it.
type changingProfile struct {
	fakeInputs
	now change.Profile
}

func (c *changingProfile) Profile(context.Context, change.Repo) (change.Profile, error) {
	return c.now, nil
}

// The profile row assigns a paid task when it sees no proposal. Live (Nov 21) a 60-second cache hid a proposal the
// profiler had just made, so a second profile task ran and buried the approved one. The profile is read fresh.
func TestTheProfileIsReadFreshOnEveryRun(t *testing.T) {
	clock := &fakeClock{now: t0}
	cache := newInputCache(clock)
	src := &changingProfile{}

	first, err := cache.profile(context.Background(), src, "acme/pay")
	require.NoError(t, err)
	require.Nil(t, first.Latest)

	src.now = change.Profile{Latest: &change.ProfileProposal{ID: "p1", State: "proposed"}}
	clock.now = t0.Add(5 * time.Second)
	second, err := cache.profile(context.Background(), src, "acme/pay")
	require.NoError(t, err)
	require.NotNil(t, second.Latest, "a proposal made a moment ago is seen on the next run")
	require.Equal(t, "proposed", second.Latest.State)
}
