package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/gateuse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reconcile"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/liveness"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Settings of the reconciler: how many test submissions a rejection may use, how long the PR author has to pick
// reviewers, and how long an agent may hold each kind of task. The liveness windows are the two waits of 6.3.
// probeModel is the model the provider probe asks for one token; openRouterKey is the secret file holding the key.
const (
	probeModel    = "deepseek/deepseek-v4-flash"
	openRouterKey = "openrouter_api_key"
)

const (
	testAttempts   = 3
	pickWait       = time.Minute
	testsTimeout   = 2 * time.Hour
	reviewTimeout  = 30 * time.Minute
	verdictTimeout = 30 * time.Minute
	ackWindow      = 3 * time.Minute
	stallWindow    = 10 * time.Minute
)

// reconcileInputs reads what the rows need from outside the log (reconcile.InputSource).
type reconcileInputs struct {
	code     ports.CodeReader
	gate     gateuse.Source
	profiles ports.ProfileStore
}

// Policy reads heimdall.yaml at sha. A repository with no policy file, or a change with no commit yet, has the zero
// policy: that is a fact about the repository, and the rows that read people or approvers then name nobody. A file
// that cannot be read or decoded is an error.
func (r reconcileInputs) Policy(ctx context.Context, repo change.Repo, sha id.SHA) (policy.Policy, error) {
	if sha == "" {
		return policy.Policy{}, nil
	}
	raw, err := r.code.ReadFile(ctx, repo, sha, "heimdall.yaml")
	if errors.Is(err, ports.ErrNotFound) {
		return policy.Policy{}, nil
	}
	if err != nil {
		return policy.Policy{}, err
	}
	return policyyaml.Decode(raw)
}

// Outside reads the facts the merge rule needs from the repository and the policy.
func (r reconcileInputs) Outside(ctx context.Context, v change.View) (change.Outside, error) {
	var approvers []string
	for _, a := range v.Approvals {
		if a.CommitSHA == v.PR.HeadSHA {
			approvers = append(approvers, string(a.Login))
		}
	}
	return r.gate.Outside(ctx, gateuse.Request{ChangeID: v.PR.Ticket, Repo: v.PR.Repo, Number: v.PR.Number,
		HeadSHA: v.PR.HeadSHA, Author: v.PR.Author, Approvers: approvers})
}

// Profile returns the repository's promoted profile, or the zero Profile when it has none.
func (r reconcileInputs) Profile(ctx context.Context, repo change.Repo) (change.Profile, error) {
	p, ok, err := r.profiles.ActiveProfile(ctx, string(repo))
	if err != nil {
		return change.Profile{}, err
	}
	if !ok {
		return change.Profile{}, nil
	}
	out := change.Profile{TestsDir: p.Spec.TestsDir}
	for _, c := range p.Spec.Checks {
		out.Checks = append(out.Checks, change.ProfileCheck{ID: c.ID, When: c.When})
	}
	return out, nil
}

// keyReader lets the OpenRouter prober read its key through the configuration.
type keyReader struct{ cfg config.Config }

// Get reads one named key.
func (k keyReader) Get(name string) (string, error) { return k.cfg.Secret(name) }

// agentFor says which agent holds a task: the test author and the resolver are configured, a review seat is its own
// agent name.
func agentFor(agents config.Agents) func(kind, subject string) (id.AgentName, bool) {
	return func(kind, subject string) (id.AgentName, bool) {
		switch kind {
		case "tests":
			return id.AgentName(agents.TestAuthor), agents.TestAuthor != ""
		case "verdict":
			return id.AgentName(agents.Resolver), agents.Resolver != ""
		case "review":
			name, err := id.ParseAgentName(subject)
			return name, err == nil
		default:
			return "", false
		}
	}
}

// newReconciler builds the reconciler on the store (which should be the Kicking one for writers).
func newReconciler(ad adapters, store *postgres.Store, openrouter ports.ModelHealth, gen ports.IDs, clk ports.Clock, logger *slog.Logger) (*reconcile.Reconciler, error) {
	gate, err := gateuse.NewSource(gateuse.SourceConfig{PRs: ad.vcs, Code: ad.vcs, Decode: policyyaml.Decode})
	if err != nil {
		return nil, fmt.Errorf("build gate source: %w", err)
	}
	reviewers := make([]change.ActorID, 0, len(ad.agents.Reviewers))
	for _, r := range ad.agents.Reviewers {
		reviewers = append(reviewers, change.ActorID(r))
	}
	return reconcile.New(reconcile.Config{
		Store: store, Effects: store, Open: store, Dead: store, Calls: store, Health: openrouter,
		Inputs: reconcileInputs{code: ad.vcs, gate: gate, profiles: store},
		Agents: agentFor(ad.agents), IDs: gen, Clock: clk, Logger: logger, Wait: clock.Wait,
		Settings: change.Settings{Reviewers: reviewers, TestAttempts: testAttempts, PickWait: pickWait,
			Timeouts: map[string]time.Duration{"tests": testsTimeout, "review": reviewTimeout, "verdict": verdictTimeout}},
		Windows: liveness.Windows{Ack: ackWindow, Stall: stallWindow},
	})
}
