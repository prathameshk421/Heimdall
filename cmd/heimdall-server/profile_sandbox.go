package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// profileSandbox resolves a repository's durable active profile before handing the job to the
// isolated runner. An unknown repository uses the configured built-in profile; a store error fails closed.
type profileSandbox struct {
	Sandbox  ports.Sandbox
	Profiles ports.ProfileStore
}

func (s profileSandbox) Run(ctx context.Context, job ports.SandboxJob) (ports.SandboxResult, error) {
	selected, err := s.selectProfile(ctx, job)
	if err != nil {
		return ports.SandboxResult{}, err
	}
	return s.Sandbox.Run(ctx, selected)
}

func (s profileSandbox) RunOverlay(ctx context.Context, job ports.SandboxJob, files []ports.File) (ports.SandboxResult, error) {
	selected, err := s.selectProfile(ctx, job)
	if err != nil {
		return ports.SandboxResult{}, err
	}
	if overlay, ok := s.Sandbox.(interface {
		RunOverlay(context.Context, ports.SandboxJob, []ports.File) (ports.SandboxResult, error)
	}); ok {
		return overlay.RunOverlay(ctx, selected, files)
	}
	return ports.SandboxResult{}, fmt.Errorf("sandbox does not support overlay jobs")
}

func (s profileSandbox) selectProfile(ctx context.Context, job ports.SandboxJob) (ports.SandboxJob, error) {
	if s.Sandbox == nil {
		return ports.SandboxJob{}, fmt.Errorf("sandbox profile selector is not configured")
	}
	if s.Profiles == nil {
		return job, nil
	}
	if job.Profile != "" {
		return job, nil
	}
	active, found, err := s.Profiles.ActiveProfile(ctx, string(job.Repo))
	if err != nil {
		return ports.SandboxJob{}, fmt.Errorf("resolve active profile for %s: %w", job.Repo, err)
	}
	if found {
		job.Profile = strings.ReplaceAll(strings.ToLower(active.Repo), "/", "-")
	}
	return job, nil
}

var _ ports.Sandbox = profileSandbox{}
