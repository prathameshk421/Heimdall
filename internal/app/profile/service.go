// Package profile owns repository sandbox-profile review, trial and promotion.
package profile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Authorizer checks human policy-owner rights. Implementations derive roles from the merge-base policy,
// never from a caller-supplied role.
type Authorizer interface {
	AuthorizeProfile(context.Context, string, change.Repo, string) error
}

// Config contains the durable lifecycle and the isolated runner capabilities.
type Config struct {
	Store      ports.ProfileStore
	Parser     ports.ProfileParser
	Policy     ports.ProfilePolicySource
	Trials     ports.ProfileTrialRunner
	Syncer     ports.ProfileSyncer
	Warmer     ports.ProfileWarmer
	Authorizer Authorizer
	Clock      ports.Clock
}

// Service owns one complete sandbox-profile lifecycle.
type Service struct{ cfg Config }

// New validates all required lifecycle capabilities.
func New(cfg Config) (*Service, error) {
	var missing []string
	if cfg.Store == nil {
		missing = append(missing, "store")
	}
	if cfg.Parser == nil {
		missing = append(missing, "parser")
	}
	if cfg.Policy == nil {
		missing = append(missing, "policy")
	}
	if cfg.Trials == nil {
		missing = append(missing, "trials")
	}
	if cfg.Syncer == nil {
		missing = append(missing, "syncer")
	}
	if cfg.Warmer == nil {
		missing = append(missing, "warmer")
	}
	if cfg.Authorizer == nil {
		missing = append(missing, "authorizer")
	}
	if cfg.Clock == nil {
		missing = append(missing, "clock")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("profile service missing %s", strings.Join(missing, ", "))
	}
	return &Service{cfg: cfg}, nil
}

// Propose validates a profile against the runner's image allowlist and stores an immutable proposal.
func (s *Service) Propose(ctx context.Context, repo change.Repo, author string, artifact []byte) (ports.ProfileProposal, bool, error) {
	if author == "" {
		return ports.ProfileProposal{}, false, errors.New("profile proposal requires a mapped author")
	}
	if err := (ports.ProfileWarmRequest{Repo: repo, SHA: id.SHA(strings.Repeat("a", 40))}).Validate(); err != nil {
		return ports.ProfileProposal{}, false, fmt.Errorf("invalid profile repository: %w", err)
	}
	images, err := s.cfg.Policy.AllowedImages(ctx)
	if err != nil {
		return ports.ProfileProposal{}, false, fmt.Errorf("read runner image allowlist: %w", err)
	}
	spec, err := s.cfg.Parser.Parse(artifact, images)
	if err != nil {
		return ports.ProfileProposal{}, false, fmt.Errorf("parse proposed profile: %w", err)
	}
	h := sha256.New()
	_, _ = h.Write([]byte(strings.ToLower(string(repo))))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(artifact)
	p := ports.ProfileProposal{ID: hex.EncodeToString(h.Sum(nil)), Repo: string(repo), State: ports.ProfileProposed, Author: author, Artifact: append([]byte(nil), artifact...), Spec: spec, CreatedAt: s.cfg.Clock.Now()}
	if err := p.Validate(); err != nil {
		return ports.ProfileProposal{}, false, fmt.Errorf("validate profile proposal: %w", err)
	}
	created, err := s.cfg.Store.PutProfile(ctx, p)
	if err != nil {
		return ports.ProfileProposal{}, false, err
	}
	return p, created, nil
}

// Approve records a mapped human's policy authorization for a proposed profile.
func (s *Service) Approve(ctx context.Context, actor, proposalID string) error {
	p, found, err := s.cfg.Store.Profile(ctx, proposalID)
	if err != nil {
		return err
	}
	if !found {
		return ports.ErrNotFound
	}
	if err := s.cfg.Authorizer.AuthorizeProfile(ctx, actor, change.Repo(p.Repo), "approve"); err != nil {
		return fmt.Errorf("authorize profile approval: %w", err)
	}
	if (p.State == ports.ProfileApproved || p.State == ports.ProfilePromoted) && p.Approver == actor {
		return nil
	}
	ok, err := s.cfg.Store.ApproveProfile(ctx, proposalID, actor, s.cfg.Clock.Now())
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("profile is not awaiting approval")
	}
	return nil
}

// Trial runs the approved artifact in an isolated one-off job and durably records its JUnit result.
func (s *Service) Trial(ctx context.Context, proposalID string, sha id.SHA) (ports.ProfileTrial, error) {
	p, found, err := s.cfg.Store.Profile(ctx, proposalID)
	if err != nil {
		return ports.ProfileTrial{}, err
	}
	if !found {
		return ports.ProfileTrial{}, ports.ErrNotFound
	}
	if p.State != ports.ProfileApproved {
		return ports.ProfileTrial{}, errors.New("only an approved profile can be trialed")
	}
	job := ports.ProfileTrialJob{Repo: change.Repo(p.Repo), SHA: sha, Name: profileName(change.Repo(p.Repo)), Artifact: p.Artifact, TestsDir: p.Spec.TestsDir}
	if err := job.Validate(); err != nil {
		return ports.ProfileTrial{}, fmt.Errorf("invalid profile trial: %w", err)
	}
	result, err := s.cfg.Trials.Trial(ctx, job)
	if err != nil {
		return ports.ProfileTrial{}, fmt.Errorf("run profile trial: %w", err)
	}
	if err := result.Validate(); err != nil {
		return ports.ProfileTrial{}, fmt.Errorf("profile trial result: %w", err)
	}
	outcome := ports.TrialPassed
	if result.Failed > 0 || result.Errored > 0 {
		outcome = ports.TrialFailed
	}
	trial := ports.ProfileTrial{Outcome: outcome, Passed: result.Passed, Failed: result.Failed, Errored: result.Errored, SHA: sha, LogRef: result.LogRef}
	updated, err := s.cfg.Store.RecordProfileTrial(ctx, proposalID, trial, s.cfg.Clock.Now())
	if err != nil {
		return ports.ProfileTrial{}, err
	}
	if !updated {
		return ports.ProfileTrial{}, errors.New("profile changed state while its trial was running")
	}
	return trial, nil
}

// Promote selects a passing, authorized profile and synchronizes it to the isolated runner.
func (s *Service) Promote(ctx context.Context, actor, proposalID string) error {
	p, found, err := s.cfg.Store.Profile(ctx, proposalID)
	if err != nil {
		return err
	}
	if !found {
		return ports.ErrNotFound
	}
	if err := s.cfg.Authorizer.AuthorizeProfile(ctx, actor, change.Repo(p.Repo), "promote"); err != nil {
		return fmt.Errorf("authorize profile promotion: %w", err)
	}
	if p.Trial.Outcome != ports.TrialPassed {
		return errors.New("profile requires a passing isolated trial before promotion")
	}
	if err := s.cfg.Syncer.SyncProfile(ctx, profileName(change.Repo(p.Repo)), p.Artifact); err != nil {
		return fmt.Errorf("sync promoted profile to runner: %w", err)
	}
	if p.State == ports.ProfilePromoted {
		return nil
	}
	ok, err := s.cfg.Store.PromoteProfile(ctx, proposalID, s.cfg.Clock.Now())
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("profile is not eligible for promotion")
	}
	return nil
}

// Warm fetches dependencies through the runner's host-restricted cache-warm job.
func (s *Service) Warm(ctx context.Context, repo change.Repo, sha id.SHA) (ports.ProfileWarmResult, error) {
	_, found, err := s.cfg.Store.ActiveProfile(ctx, string(repo))
	if err != nil {
		return ports.ProfileWarmResult{}, err
	}
	name := ""
	if found {
		name = profileName(repo)
	}
	req := ports.ProfileWarmRequest{Repo: repo, SHA: sha, Profile: name}
	if err := req.Validate(); err != nil {
		return ports.ProfileWarmResult{}, fmt.Errorf("invalid cache warm request: %w", err)
	}
	return s.cfg.Warmer.Warm(ctx, req)
}

func profileName(repo change.Repo) string {
	return strings.ReplaceAll(strings.ToLower(string(repo)), "/", "-")
}
