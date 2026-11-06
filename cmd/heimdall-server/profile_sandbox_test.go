package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

type selectedProfileStore struct {
	proposal ports.ProfileProposal
	found    bool
	err      error
}

func (s selectedProfileStore) PutProfile(context.Context, ports.ProfileProposal) (bool, error) {
	return false, nil
}
func (s selectedProfileStore) Profile(context.Context, string) (ports.ProfileProposal, bool, error) {
	return ports.ProfileProposal{}, false, nil
}
func (s selectedProfileStore) ApproveProfile(context.Context, string, string, time.Time) (bool, error) {
	return false, nil
}
func (s selectedProfileStore) RecordProfileTrial(context.Context, string, ports.ProfileTrial, time.Time) (bool, error) {
	return false, nil
}
func (s selectedProfileStore) PromoteProfile(context.Context, string, time.Time) (bool, error) {
	return false, nil
}
func (s selectedProfileStore) ActiveProfile(context.Context, string) (ports.ProfileProposal, bool, error) {
	return s.proposal, s.found, s.err
}
func (s selectedProfileStore) PromotedProfiles(context.Context) ([]ports.ProfileProposal, error) {
	return nil, nil
}

type selectedSandbox struct {
	job      ports.SandboxJob
	overlays []ports.File
}

func (s *selectedSandbox) Run(_ context.Context, j ports.SandboxJob) (ports.SandboxResult, error) {
	s.job = j
	return ports.SandboxResult{}, nil
}
func (s *selectedSandbox) RunOverlay(_ context.Context, j ports.SandboxJob, files []ports.File) (ports.SandboxResult, error) {
	s.job = j
	s.overlays = files
	if len(files) == 0 {
		return ports.SandboxResult{}, errors.New("overlay required")
	}
	return ports.SandboxResult{Passed: len(files)}, nil
}

func TestProfileSandboxSelectsRepositoryPromotionForEveryProductionRun(t *testing.T) {
	underlying := &selectedSandbox{}
	selector := profileSandbox{Sandbox: underlying, Profiles: selectedProfileStore{proposal: ports.ProfileProposal{Repo: "Acme/Pay"}, found: true}}
	job := ports.SandboxJob{Kind: ports.SandboxRepro, Repo: change.Repo("Acme/Pay"), SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Dir: "tests/heimdall/PAY-1", Timeout: time.Minute}
	_, err := selector.Run(context.Background(), job)
	require.NoError(t, err)
	require.Equal(t, "acme-pay", underlying.job.Profile)
	files := []ports.File{{Path: "tests/heimdall/PAY-1/repro_test.go", Content: []byte("test")}}
	_, err = selector.RunOverlay(context.Background(), job, files)
	require.NoError(t, err)
	require.Equal(t, "acme-pay", underlying.job.Profile)
	require.Equal(t, files, underlying.overlays)
}

func TestProfileSandboxFailsClosedOnDurableSelectionErrors(t *testing.T) {
	underlying := &selectedSandbox{}
	selector := profileSandbox{Sandbox: underlying, Profiles: selectedProfileStore{err: errors.New("database unavailable")}}
	_, err := selector.Run(context.Background(), ports.SandboxJob{Repo: "acme/pay"})
	require.ErrorContains(t, err, "resolve active profile")
	require.Empty(t, underlying.job.Profile)
}
