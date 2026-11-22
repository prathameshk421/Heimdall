package profile

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

func (m *memoryProfiles) ProfilesByPrefix(_ context.Context, prefix string) ([]ports.ProfileProposal, error) {
	var out []ports.ProfileProposal
	for id, p := range m.records {
		if strings.HasPrefix(id, prefix) {
			out = append(out, p)
		}
	}
	return out, nil
}

type prefixClock struct{}

func (prefixClock) Now() time.Time { return time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC) }

type prefixWarmer struct{}

func (prefixWarmer) Warm(context.Context, ports.ProfileWarmRequest) (ports.ProfileWarmResult, error) {
	return ports.ProfileWarmResult{}, nil
}

func TestApproveAndPromoteAcceptUniqueShortPrefixAndRefuseAmbiguous(t *testing.T) {
	t.Parallel()
	id1 := "49423b5" + strings.Repeat("1", 57)
	id2 := "49423b5" + strings.Repeat("2", 57)
	id3 := "abcdef0" + strings.Repeat("3", 57)
	store := &memoryProfiles{
		records: map[string]ports.ProfileProposal{
			id1: {ID: id1, Repo: "acme/pay", State: ports.ProfileProposed, Author: "profiler"},
			id2: {ID: id2, Repo: "acme/pay", State: ports.ProfileProposed, Author: "profiler"},
			id3: {ID: id3, Repo: "acme/pay", State: ports.ProfileProposed, Author: "profiler"},
		},
		active: map[string]string{},
	}
	svc, err := New(Config{
		Store:      store,
		Parser:     profileParser{},
		Policy:     imagePolicy{},
		Trials:     &profileTrial{},
		Syncer:     &profileSync{},
		Warmer:     prefixWarmer{},
		Authorizer: profileAuth{},
		Clock:      prefixClock{},
	})
	require.NoError(t, err)

	// Unique 7-char prefix approves id3.
	require.NoError(t, svc.Approve(context.Background(), "alice", "abcdef0"))
	require.Equal(t, ports.ProfileApproved, store.records[id3].State)

	// Ambiguous 7-char prefix is refused naming the matches.
	err = svc.Approve(context.Background(), "alice", "49423b5")
	require.Error(t, err)
	var refused StepRefusedError
	require.ErrorAs(t, err, &refused)
	require.Contains(t, refused.Reason, "ambiguous")
}
