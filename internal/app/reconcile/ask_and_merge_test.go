package reconcile

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

type mergePolicyInputs struct {
	fakeInputs
	mergeStatus string
}

func (m mergePolicyInputs) Policy(context.Context, change.Repo, id.SHA) (policy.Policy, error) {
	return policy.Policy{
		People:    map[string]policy.Person{"ann": {GitHub: "ann"}, "bob": {GitHub: "bob"}},
		Approvers: map[string][]string{"default": {"bob"}},
		Tracker:   policy.Tracker{MergeStatus: m.mergeStatus},
	}, nil
}

func TestAskIsPostedOnceNamingItsPeopleAndSecondRunPostsNothing(t *testing.T) {
	r := newRig(t)
	r.rec.cfg.Inputs = mergePolicyInputs{}
	r.trigger(t)
	r.room(t, tkt)
	r.branch(t)
	r.submit(t)
	r.add(t, tkt, change.EventTestsVerified, change.TestsVerified{
		RunIDs:       []id.RunID{"run_01HZX3K9QW7M2P5R8T0V4YB6CD"},
		SHA:          head1,
		TreeHash:     base,
		NeedsSignoff: true,
	})

	r.run(t, tkt)
	var posts []change.Effect
	for _, k := range r.store.order {
		fx := r.store.effects[k]
		if fx.Kind == change.EffectRoomPost {
			posts = append(posts, fx)
		}
	}
	require.Len(t, posts, 1, "one Ask produces one room_post job")
	require.Equal(t, "switch:room-1", posts[0].Target)
	var p change.RoomPostPayload
	require.NoError(t, json.Unmarshal(posts[0].Payload, &p))
	require.Contains(t, p.Text, "@bob")
	require.Contains(t, p.Text, "heimdall approve-tests KAN-5")

	// Second Run with the same Ask must not enqueue another post.
	countBefore := len(r.store.order)
	r.run(t, tkt)
	require.Len(t, r.store.order, countBefore, "second run posts nothing")
}

func TestAskRoutesToStandingChannelWhenConfigured(t *testing.T) {
	r := newRig(t)
	r.rec.cfg.Inputs = mergePolicyInputs{}
	r.rec.cfg.TestsRoom = "switch:tests-chan"
	r.rec.cfg.ReviewRoom = "switch:reviews-chan"
	r.pr(t)
	r.room(t, prc)
	// Inside the pick wait window, rowPick emits an Ask for the PR author.
	r.clock.set(t0.Add(10 * time.Second))

	r.run(t, prc)
	var found bool
	for _, k := range r.store.order {
		fx := r.store.effects[k]
		if fx.Kind == change.EffectRoomPost {
			found = true
			require.Equal(t, "switch:reviews-chan", fx.Target)
			var p change.RoomPostPayload
			require.NoError(t, json.Unmarshal(fx.Payload, &p))
			require.Contains(t, p.Text, "@ann")
			require.Contains(t, p.Text, "heimdall review")
		}
	}
	require.True(t, found, "pick Ask posted to standing review room")
}
