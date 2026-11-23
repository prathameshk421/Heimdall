package reviewlearn_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reviewlearn"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
)

type outcomesHistory struct {
	streams map[id.ChangeID][]change.Event
}

func (h outcomesHistory) ChangeIDs(context.Context) ([]id.ChangeID, error) {
	ids := make([]id.ChangeID, 0, len(h.streams))
	for k := range h.streams {
		ids = append(ids, k)
	}
	return ids, nil
}

func (h outcomesHistory) Load(_ context.Context, cid id.ChangeID) ([]change.Event, error) {
	return h.streams[cid], nil
}

type staticClock struct{ t time.Time }

func (c staticClock) Now() time.Time { return c.t }

func TestCardOutcomesTracksOverridesProofAndIsolatesOwners(t *testing.T) {
	s := newStream(t, "CHG-1")
	s.file(1, rhea, finding.CategorySecurity, "internal/auth/jwt.go", "auth.md")
	v := record(1, rhea, finding.StateUpheld)
	v.Tier = finding.TierE1
	s.add(change.EventFindingVerified, change.ActorSystem, "heimdall", change.FindingVerified(v))

	s.file(2, rhea, finding.CategorySecurity, "internal/auth/jwt.go", "auth.md")
	s.decide(change.DecisionOverride, ref(2).String(), "waived for hotfix")

	// Different reviewer agent's dismissal of the same card name must never leak into rhea's outcomes.
	s.file(3, noah, finding.CategorySecurity, "internal/auth/jwt.go", "auth.md")
	s.move(3, noah, finding.StateDismissed)

	svc, err := reviewlearn.NewService(outcomesHistory{streams: map[id.ChangeID][]change.Event{"CHG-1": s.events}}, staticClock{t: t0}, 0)
	require.NoError(t, err)
	snap, err := svc.Snapshot(context.Background())
	require.NoError(t, err)

	got := snap.CardOutcomes(rhea, []rulecard.Card{{Name: "auth.md", Status: rulecard.StatusActive, Body: "b"}})
	require.Len(t, got, 1)
	require.Equal(t, 1, got[0].Kept)
	require.Equal(t, 1, got[0].Proven)
	require.Equal(t, 1, got[0].Overridden)
	require.Equal(t, 0, got[0].Dismissed)
	require.Equal(t, []string{"waived for hotfix"}, got[0].DismissalReasons)
}
