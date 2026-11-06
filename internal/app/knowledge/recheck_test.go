package knowledge

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func recheckService(t *testing.T, mem *memorySpy, now time.Time) *SearchService {
	t.Helper()
	s, err := NewSearchService(mem, scopeSpy{"payments"}, &auditSpy{}, knowledgeClock{now})
	require.NoError(t, err)
	return s
}

// The re-query reports whether the cited reference is found, confirmed and superseded, using the citing agent's
// groups and skipping the answer cache.
func TestRecheckReportsFoundConfirmedAndSuperseded(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	cases := map[string]struct {
		item ports.MemoryItem
		want verify.MemoryState
	}{
		"confirmed":        {ports.MemoryItem{Ref: "decision:x", Text: "t", Label: ports.LabelConfirmed}, verify.MemoryState{Found: true, Confirmed: true}},
		"observed":         {ports.MemoryItem{Ref: "decision:x", Text: "t", Label: ports.LabelObserved}, verify.MemoryState{Found: true}},
		"label superseded": {ports.MemoryItem{Ref: "decision:x", Text: "t", Label: ports.LabelSuperseded}, verify.MemoryState{Found: true, Superseded: true}},
		"invalidated":      {ports.MemoryItem{Ref: "decision:x", Text: "t", Label: ports.LabelConfirmed, InvalidAt: &past}, verify.MemoryState{Found: true, Confirmed: true, Superseded: true}},
		"invalid later":    {ports.MemoryItem{Ref: "decision:x", Text: "t", Label: ports.LabelConfirmed, InvalidAt: &future}, verify.MemoryState{Found: true, Confirmed: true}},
		"another ref":      {ports.MemoryItem{Ref: "decision:other", Text: "t", Label: ports.LabelConfirmed}, verify.MemoryState{}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			mem := &memorySpy{items: []ports.MemoryItem{c.item}}
			got, err := recheckService(t, mem, now).Recheck(context.Background(), "bob-reviewer", "acme/pay", "a01b2c3", "decision:x")
			require.NoError(t, err)
			require.Equal(t, c.want, got)
			require.Equal(t, []string{"payments", "shared"}, mem.query.Groups, "the groups come from policy for the citing agent")
		})
	}
}

func TestRecheckDoesNotUseTheAnswerCache(t *testing.T) {
	mem := &memorySpy{items: []ports.MemoryItem{{Ref: "decision:x", Text: "t", Label: ports.LabelConfirmed}}}
	s := recheckService(t, mem, time.Now())
	for i := 0; i < 2; i++ {
		_, err := s.Recheck(context.Background(), "bob-reviewer", "acme/pay", "a01b2c3", "decision:x")
		require.NoError(t, err)
	}
	require.Equal(t, 2, mem.calls, "a re-query must see the current memory")
}

func TestRecheckRefusesABlankAgentOrReference(t *testing.T) {
	s := recheckService(t, &memorySpy{}, time.Now())
	_, err := s.Recheck(context.Background(), "", "acme/pay", "a01b2c3", "decision:x")
	require.Error(t, err)
	_, err = s.Recheck(context.Background(), "bob-reviewer", "acme/pay", "a01b2c3", "  ")
	require.Error(t, err)
}
