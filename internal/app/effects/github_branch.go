package effects

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// reBranchName is the shape of a branch name this executor writes. The VCS adapter applies git's full rules.
var reBranchName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// GitHubBranchDeps are the dependencies of GitHubBranch.
type GitHubBranchDeps struct {
	Tickets  ports.TicketReader
	Base     BaseReader
	Branches ports.BranchWriter
	Store    ports.EventStore
	Ingest   Ingest
	IDs      ports.IDs
	Clock    ports.Clock
}

// GitHubBranch performs github_branch effects: it makes the test branch of a change and records branch.ensured.
type GitHubBranch struct {
	deps GitHubBranchDeps
}

// NewGitHubBranch builds the executor, or refuses when a dependency is missing.
func NewGitHubBranch(d GitHubBranchDeps) (GitHubBranch, error) {
	var missing []string
	if d.Tickets == nil {
		missing = append(missing, "tickets")
	}
	if d.Base == nil {
		missing = append(missing, "base")
	}
	if d.Branches == nil {
		missing = append(missing, "branches")
	}
	if d.Store == nil {
		missing = append(missing, "store")
	}
	if d.Ingest == nil {
		missing = append(missing, "ingest")
	}
	if d.IDs == nil {
		missing = append(missing, "ids")
	}
	if d.Clock == nil {
		missing = append(missing, "clock")
	}
	if len(missing) > 0 {
		return GitHubBranch{}, fmt.Errorf("github_branch executor: missing %v", missing)
	}
	return GitHubBranch{deps: d}, nil
}

// Execute writes the branch unless branch.ensured is already stored. The name is the template of the payload with the
// ticket key and the slug of the ticket title. It starts with the ticket key and a hyphen.
func (g GitHubBranch) Execute(ctx context.Context, fx change.Effect) error {
	p, err := branchPayload(fx)
	if err != nil {
		return err
	}
	l, err := load(ctx, g.deps.Store, p.Ticket)
	if err != nil {
		return err
	}
	if l.has(change.EventBranchEnsured) {
		return nil
	}
	cause := l.first(change.EventRoomOpened)
	if cause == nil {
		return failf(nil, "github_branch %s: no room.opened in the change", p.Ticket)
	}
	name, err := g.name(ctx, p)
	if err != nil {
		return err
	}
	base, err := g.deps.Base.DefaultHead(ctx, p.Repo)
	if err != nil {
		return failf(err, "github_branch %s: read base commit", p.Ticket)
	}
	if err := g.deps.Branches.EnsureBranch(ctx, p.Repo, base, name); err != nil {
		return failf(err, "github_branch %s: ensure branch", p.Ticket)
	}
	payload := change.BranchEnsured{Repo: p.Repo, Branch: name, BaseSHA: base}
	if err := payload.Validate(); err != nil {
		return failf(err, "github_branch %s: branch.ensured refused", p.Ticket)
	}
	ev, err := newEvent(g.deps.IDs, g.deps.Clock, *cause, change.EventBranchEnsured,
		change.Actor{Kind: change.ActorGitHub, ID: githubActorID}, payload)
	if err != nil {
		return failf(err, "github_branch %s: build event", p.Ticket)
	}
	if err := g.deps.Ingest.Ingest(ctx, p.Ticket, []change.Event{ev}); err != nil {
		return failf(err, "github_branch %s: ingest branch.ensured", p.Ticket)
	}
	return nil
}

// name expands the template of the payload with the slug of the ticket title, and checks the result.
func (g GitHubBranch) name(ctx context.Context, p change.BranchPayload) (string, error) {
	t, err := g.deps.Tickets.Ticket(ctx, p.Ticket)
	if err != nil {
		return "", failf(err, "github_branch %s: read ticket", p.Ticket)
	}
	slug, err := Slug(t.Title)
	if err != nil {
		return "", failf(err, "github_branch %s: slug of ticket title", p.Ticket)
	}
	name := p.Name(slug)
	if !strings.HasPrefix(name, string(p.Ticket)+"-") || !reBranchName.MatchString(name) || strings.Contains(name, "..") {
		return "", failf(nil, "github_branch %s: name is not a safe branch name", p.Ticket)
	}
	return name, nil
}

// branchPayload checks the kind and the target of a github_branch effect and decodes its payload.
func branchPayload(fx change.Effect) (change.BranchPayload, error) {
	if fx.Kind != change.EffectGitHubBranch {
		return change.BranchPayload{}, failf(nil, "github_branch executor: effect %s has kind %s", fx.Key, fx.Kind)
	}
	p, err := change.DecodeBranchPayload(fx.Payload)
	if err != nil {
		return change.BranchPayload{}, failf(err, "github_branch effect %s: payload refused", fx.Key)
	}
	if err := checkTarget(fx, p.Ticket); err != nil {
		return change.BranchPayload{}, err
	}
	return p, nil
}
