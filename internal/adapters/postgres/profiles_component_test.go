//go:build component

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestProfileStoreLifecycleReplayAndAtomicActiveSelection(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	db := srv.database(ctx, t)
	store := NewStore(db.pool)
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	p := ports.ProfileProposal{
		ID: strings.Repeat("a", 64), Repo: "acme/pay", State: ports.ProfileProposed, Author: "author",
		Artifact: []byte("profile: valid"), CreatedAt: now,
		Spec: ports.ProfileSpec{Languages: []string{"go"}, Image: "registry.example/go@sha256:" + strings.Repeat("b", 64), Acceptance: "go test {dir}", Seam: "test", ACTag: "// tag", TestsDir: "tests/heimdall"},
	}
	inserted, err := store.PutProfile(ctx, p)
	require.NoError(t, err)
	require.True(t, inserted)
	inserted, err = store.PutProfile(ctx, p)
	require.NoError(t, err)
	require.False(t, inserted, "exact proposal replay is idempotent")

	_, found, err := store.ActiveProfile(ctx, p.Repo)
	require.NoError(t, err)
	require.False(t, found)

	approved, err := store.ApproveProfile(ctx, p.ID, "reviewer", now.Add(time.Minute))
	require.NoError(t, err)
	require.True(t, approved)
	approved, err = store.ApproveProfile(ctx, p.ID, "reviewer", now.Add(time.Minute))
	require.NoError(t, err)
	require.False(t, approved, "approval replay cannot advance state twice")

	trial := ports.ProfileTrial{Outcome: ports.TrialFailed, Failed: 1, SHA: id.SHA(strings.Repeat("c", 40)), LogRef: workflow.BlobRef("blob:sha256:" + strings.Repeat("d", 64))}
	recorded, err := store.RecordProfileTrial(ctx, p.ID, trial, now.Add(2*time.Minute))
	require.NoError(t, err)
	require.True(t, recorded)
	promoted, err := store.PromoteProfile(ctx, p.ID, now.Add(3*time.Minute))
	require.NoError(t, err)
	require.False(t, promoted, "failed trial cannot be promoted")

	trial.Outcome, trial.Failed, trial.Passed = ports.TrialPassed, 0, 3
	recorded, err = store.RecordProfileTrial(ctx, p.ID, trial, now.Add(4*time.Minute))
	require.NoError(t, err)
	require.True(t, recorded)
	promoted, err = store.PromoteProfile(ctx, p.ID, now.Add(5*time.Minute))
	require.NoError(t, err)
	require.True(t, promoted)
	promoted, err = store.PromoteProfile(ctx, p.ID, now.Add(6*time.Minute))
	require.NoError(t, err)
	require.False(t, promoted, "promotion replay cannot advance state twice")

	active, found, err := store.ActiveProfile(ctx, p.Repo)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, ports.ProfilePromoted, active.State)
	require.Equal(t, ports.TrialPassed, active.Trial.Outcome)
	require.Equal(t, p.ID, active.ID)
	all, err := store.PromotedProfiles(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{p.ID}, []string{all[0].ID})
}
