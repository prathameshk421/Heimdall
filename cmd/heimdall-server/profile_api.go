package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	appprofile "github.com/prathameshk421/flip_the_switch_hackathon/internal/app/profile"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

// profileAPI is the profile lifecycle as the MCP tools see it. store and blobs serve the profiler's previous_trial input.
type profileAPI struct {
	service *appprofile.Service
	store   ports.ProfileStore
	blobs   ports.BlobStore
}

// maxTrialLog bounds the trial log a profiler sees; the end is kept, where a failure is reported.
const maxTrialLog = 16 << 10

func (a profileAPI) Propose(ctx context.Context, p mcp.Principal, repo string, artifact []byte) (ports.ProfileProposal, bool, error) {
	if p.Role != workflow.RoleProfiler {
		return ports.ProfileProposal{}, false, errors.New("only the profiler may propose profiles")
	}
	return a.service.Propose(ctx, change.Repo(repo), string(p.Agent), artifact)
}

func (a profileAPI) Approve(ctx context.Context, p mcp.Principal, proposalID string) error {
	if p.Person == "" {
		return errors.New("mapped human principal is required")
	}
	return a.service.Approve(ctx, p.Person, proposalID)
}

// PreviousTrial is the failed trial of the repository's newest proposal, for the profiler that proposes again (D175):
// the outcome, the counts and the end of the log. It is empty when the newest proposal did not fail a trial.
func (a profileAPI) PreviousTrial(ctx context.Context, repo string) (string, error) {
	p, ok, err := a.store.LatestProfile(ctx, repo)
	if err != nil {
		return "", fmt.Errorf("read the newest profile proposal of %s: %w", repo, err)
	}
	if !ok || p.State != ports.ProfileApproved || p.Trial.Outcome != ports.TrialFailed {
		return "", nil
	}
	head := fmt.Sprintf("proposal %s failed its trial: %d passed, %d failed, %d errored", p.ID[:7], p.Trial.Passed, p.Trial.Failed, p.Trial.Errored)
	if p.Trial.LogRef == "" {
		return head + "; no log was kept", nil
	}
	r, err := a.blobs.Get(ctx, p.Trial.LogRef)
	if err != nil {
		return "", fmt.Errorf("read the trial log of %s: %w", p.ID, err)
	}
	defer func() { _ = r.Close() }()
	log, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read the trial log of %s: %w", p.ID, err)
	}
	if len(log) > maxTrialLog {
		log = log[len(log)-maxTrialLog:]
	}
	return head + "\n\n" + string(log), nil
}

func (a profileAPI) Promote(ctx context.Context, p mcp.Principal, proposalID string) error {
	if p.Person == "" {
		return errors.New("mapped human principal is required")
	}
	return a.service.Promote(ctx, p.Person, proposalID)
}

type profileAuthorizer struct {
	code  ports.CodeReader
	repos ports.RepoReader
}

func (a profileAuthorizer) AuthorizeProfile(ctx context.Context, actor string, repo change.Repo, action string) error {
	if actor == "" {
		return errors.New("mapped human owner is required")
	}
	if action != "approve" && action != "promote" {
		return fmt.Errorf("unsupported profile action %q", action)
	}
	head, err := a.repos.DefaultHead(ctx, repo)
	if err != nil {
		return fmt.Errorf("read policy base head: %w", err)
	}
	raw, err := a.code.ReadFile(ctx, repo, head, "heimdall.yaml")
	if err != nil {
		return fmt.Errorf("read policy at default branch: %w", err)
	}
	policyAtBase, err := policyyaml.Decode(raw)
	if err != nil {
		return fmt.Errorf("decode policy at default branch: %w", err)
	}
	if !slices.Contains(policyAtBase.PolicyOwners, actor) {
		return fmt.Errorf("person %q is not a policy owner", actor)
	}
	return nil
}

var _ appprofile.Authorizer = profileAuthorizer{}
var _ mcp.Profiles = profileAPI{}
