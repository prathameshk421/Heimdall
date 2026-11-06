package profile

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

type memoryProfiles struct {
	records map[string]ports.ProfileProposal
	active  map[string]string
}

func (m *memoryProfiles) PutProfile(_ context.Context, p ports.ProfileProposal) (bool, error) {
	if old, ok := m.records[p.ID]; ok {
		if string(old.Artifact) != string(p.Artifact) {
			return false, errors.New("conflict")
		}
		return false, nil
	}
	m.records[p.ID] = p
	return true, nil
}
func (m *memoryProfiles) Profile(_ context.Context, key string) (ports.ProfileProposal, bool, error) {
	p, ok := m.records[key]
	return p, ok, nil
}
func (m *memoryProfiles) ApproveProfile(_ context.Context, key, actor string, at time.Time) (bool, error) {
	p, ok := m.records[key]
	if !ok || p.State != ports.ProfileProposed {
		return false, nil
	}
	p.State = ports.ProfileApproved
	p.Approver = actor
	p.ApprovedAt = at
	m.records[key] = p
	return true, nil
}
func (m *memoryProfiles) RecordProfileTrial(_ context.Context, key string, t ports.ProfileTrial, _ time.Time) (bool, error) {
	p, ok := m.records[key]
	if !ok || p.State != ports.ProfileApproved {
		return false, nil
	}
	p.Trial = t
	m.records[key] = p
	return true, nil
}
func (m *memoryProfiles) PromoteProfile(_ context.Context, key string, at time.Time) (bool, error) {
	p, ok := m.records[key]
	if !ok || p.State != ports.ProfileApproved || p.Trial.Outcome != ports.TrialPassed {
		return false, nil
	}
	p.State = ports.ProfilePromoted
	p.PromotedAt = at
	m.records[key] = p
	m.active[p.Repo] = key
	return true, nil
}
func (m *memoryProfiles) ActiveProfile(_ context.Context, repo string) (ports.ProfileProposal, bool, error) {
	key, ok := m.active[repo]
	if !ok {
		return ports.ProfileProposal{}, false, nil
	}
	return m.records[key], true, nil
}
func (m *memoryProfiles) PromotedProfiles(context.Context) ([]ports.ProfileProposal, error) {
	out := []ports.ProfileProposal{}
	for _, p := range m.records {
		if p.State == ports.ProfilePromoted {
			out = append(out, p)
		}
	}
	return out, nil
}

type imagePolicy struct{}

func (imagePolicy) AllowedImages(context.Context) ([]string, error) {
	return []string{"registry.example/go@sha256:" + strings.Repeat("a", 64)}, nil
}

type profileParser struct{}

func (profileParser) Parse([]byte, []string) (ports.ProfileSpec, error) {
	return ports.ProfileSpec{Languages: []string{"go"}, Image: "registry.example/go@sha256:" + strings.Repeat("a", 64), Acceptance: "go test {dir}", Seam: "api", ACTag: "// ac", TestsDir: "tests/heimdall"}, nil
}

type profileAuth struct{ err error }

func (a profileAuth) AuthorizeProfile(context.Context, string, change.Repo, string) error {
	return a.err
}

type profileTrial struct {
	result ports.SandboxResult
	job    ports.ProfileTrialJob
}

func (t *profileTrial) Trial(_ context.Context, j ports.ProfileTrialJob) (ports.SandboxResult, error) {
	t.job = j
	return t.result, nil
}

type profileSync struct{ names []string }

func (s *profileSync) Profiles(context.Context) ([]string, error) { return s.names, nil }
func (s *profileSync) SyncProfile(_ context.Context, n string, _ []byte) error {
	s.names = append(s.names, n)
	return nil
}

type profileWarm struct{ request ports.ProfileWarmRequest }

func (w *profileWarm) Warm(_ context.Context, r ports.ProfileWarmRequest) (ports.ProfileWarmResult, error) {
	w.request = r
	return ports.ProfileWarmResult{Modules: []string{"example.test/mod"}}, nil
}

type fixedProfileClock struct{ now time.Time }

func (c fixedProfileClock) Now() time.Time { return c.now }

func newProfileService(t *testing.T, trial ports.SandboxResult) (*Service, *memoryProfiles, *profileTrial, *profileSync, *profileWarm) {
	t.Helper()
	store := &memoryProfiles{records: map[string]ports.ProfileProposal{}, active: map[string]string{}}
	runner := &profileTrial{result: trial}
	syncer := &profileSync{}
	warmer := &profileWarm{}
	svc, err := New(Config{Store: store, Parser: profileParser{}, Policy: imagePolicy{}, Trials: runner, Syncer: syncer, Warmer: warmer, Authorizer: profileAuth{}, Clock: fixedProfileClock{now: time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)}})
	require.NoError(t, err)
	return svc, store, runner, syncer, warmer
}

func TestLifecycleRequiresAuthorizedApprovalPassingTrialAndPromotion(t *testing.T) {
	logRef := workflow.BlobRef("blob:sha256:" + strings.Repeat("c", 64))
	sha := id.SHA(strings.Repeat("d", 40))
	svc, store, runner, syncer, warmer := newProfileService(t, ports.SandboxResult{Passed: 2, LogRef: logRef})
	proposal, created, err := svc.Propose(context.Background(), "acme/pay", "profiler", []byte("artifact"))
	require.NoError(t, err)
	require.True(t, created)
	replay, created, err := svc.Propose(context.Background(), "acme/pay", "profiler", []byte("artifact"))
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, proposal.ID, replay.ID)
	require.Error(t, svc.Promote(context.Background(), "owner", proposal.ID), "a proposal without approval and a passing trial cannot be promoted")
	require.NoError(t, svc.Approve(context.Background(), "owner", proposal.ID))
	trial, err := svc.Trial(context.Background(), proposal.ID, sha)
	require.NoError(t, err)
	require.Equal(t, ports.TrialPassed, trial.Outcome)
	require.Equal(t, "acme-pay", runner.job.Name)
	require.NoError(t, svc.Promote(context.Background(), "owner", proposal.ID))
	require.Equal(t, []string{"acme-pay"}, syncer.names)
	_, err = svc.Warm(context.Background(), "acme/pay", sha)
	require.NoError(t, err)
	require.Equal(t, "acme-pay", warmer.request.Profile)
	active, found, err := store.ActiveProfile(context.Background(), "acme/pay")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, ports.ProfilePromoted, active.State)
}

func TestLifecycleRefusesUnauthorizedAndFailedTrialPromotion(t *testing.T) {
	svc, store, _, syncer, _ := newProfileService(t, ports.SandboxResult{Failed: 1, LogRef: workflow.BlobRef("blob:sha256:" + strings.Repeat("c", 64))})
	proposal, _, err := svc.Propose(context.Background(), "acme/pay", "profiler", []byte("artifact"))
	require.NoError(t, err)
	svc.cfg.Authorizer = profileAuth{err: errors.New("not an owner")}
	require.Error(t, svc.Approve(context.Background(), "intruder", proposal.ID))
	p, _, err := store.Profile(context.Background(), proposal.ID)
	require.NoError(t, err)
	require.Equal(t, ports.ProfileProposed, p.State)
	svc.cfg.Authorizer = profileAuth{}
	require.NoError(t, svc.Approve(context.Background(), "owner", proposal.ID))
	trial, err := svc.Trial(context.Background(), proposal.ID, id.SHA(strings.Repeat("d", 40)))
	require.NoError(t, err)
	require.Equal(t, ports.TrialFailed, trial.Outcome)
	require.Error(t, svc.Promote(context.Background(), "owner", proposal.ID))
	require.Empty(t, syncer.names)
}
