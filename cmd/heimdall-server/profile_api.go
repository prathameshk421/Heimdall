package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	appprofile "github.com/prathameshk421/flip_the_switch_hackathon/internal/app/profile"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

type profileAPI struct{ service *appprofile.Service }

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

func (a profileAPI) Trial(ctx context.Context, p mcp.Principal, proposalID string, sha id.SHA) (ports.ProfileTrial, error) {
	if p.Role != workflow.RoleProfiler {
		return ports.ProfileTrial{}, errors.New("only the profiler may run profile trials")
	}
	return a.service.Trial(ctx, proposalID, sha)
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
