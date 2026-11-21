package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/graphiti"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/profileyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agenttools"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/intake"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/knowledge"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/outbox"
	appprofile "github.com/prathameshk421/flip_the_switch_hackathon/internal/app/profile"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/prsync"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reconcile"
	appreview "github.com/prathameshk421/flip_the_switch_hackathon/internal/app/review"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reviewlearn"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/trigger"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

// newTicketPollers builds one ticket poller per listed project, each with its own cursor source provider:project
// (A10 item 10).
func newTicketPollers(ad adapters, store *postgres.Store, eng reconcile.Recorder, blobs ports.BlobStore, gen ports.IDs, clk ports.Clock) ([]poller, error) {
	pollers := make([]poller, 0, len(ad.projectKeys))
	for _, key := range ad.projectKeys {
		tp, err := trigger.New(trigger.Config{
			Tickets:  ad.projectTickets[key],
			Starter:  starter{eng: eng},
			Cursors:  cursorStore{table: store, source: ad.trackerProvider + ":" + key},
			Projects: ad.projects,
			Blobs:    blobs,
			Clock:    clk,
			IDs:      gen,
		})
		if err != nil {
			return nil, fmt.Errorf("build ticket poller for %s: %w", key, err)
		}
		pollers = append(pollers, tp)
	}
	return pollers, nil
}

// cursorsOf returns the store as a cursor table when it is one.
func cursorsOf(store ports.EventStore) ports.CursorTable {
	if c, ok := store.(ports.CursorTable); ok {
		return c
	}
	return nil
}

// closerOf returns the blob store as a Closer when it is one.
func closerOf(blobs ports.BlobStore) io.Closer {
	if c, ok := blobs.(io.Closer); ok {
		return c
	}
	return nil
}

func newUploadHandlers(d deps, ad adapters) (http.Handler, error) {
	cardsUpload, err := newCardsHandler(d.configuration, ad.tokens)
	if err != nil {
		return nil, fmt.Errorf("build cards handler: %w", err)
	}
	return cardsUpload, nil
}

// mcpStore is the event store, the durable episode-trust ledger that knowledge search reads, and the history that the
// reviewer brief learns from.
type mcpStore interface {
	ports.EventStore
	ports.EpisodeSourceTrust
	reviewlearn.History
	ports.ToolCalls
}

// newTasksAdapter builds the MCP task adapter: test submissions advance the engine, and agent output is filtered.
func newTasksAdapter(d deps, toolsSvc *agenttools.Service, tracker *assignmentAgentTracker, store mcpStore, eng reconcile.Recorder) (*tasksAdapter, error) {
	outputFilter, err := appreview.NewOutputFilter(nil, outputAudit{logger: d.logger})
	if err != nil {
		return nil, fmt.Errorf("build agent output filter: %w", err)
	}
	return &tasksAdapter{svc: toolsSvc, tracker: tracker, focus: cursorsOf(store), filter: outputFilter, advanceTests: func(ctx context.Context, changeID id.ChangeID, event change.Event) error {
		err := eng.Append(ctx, changeID, []change.Event{event})
		return err
	}}, nil
}

// newAgentTools builds the service behind the agents' tools.
func newAgentTools(ad adapters, store mcpStore, pool *pgxpool.Pool, blobs ports.BlobStore, eng reconcile.Recorder, gen ports.IDs, clk ports.Clock, refs reviewRefs) (*agenttools.Service, error) {
	toolsSvc, err := agenttools.New(agenttools.Config{
		Events:          store,
		Index:           postgres.NewTaskIndex(pool),
		Blobs:           blobs,
		Clock:           clk,
		IDs:             gen,
		Asker:           &chatAsker{chat: ad.chat, store: store, ids: gen, public: ad.testsRoom},
		ReviewRefs:      refs,
		ReviewValidator: semanticReviewValidator{refs: refs},
		PRFiles:         ad.vcs,
		// Review events go through the engine, so the verify step sees each finding and requests its verification.
		Advance: func(ctx context.Context, cid id.ChangeID, evs []change.Event) error {
			err := eng.Append(ctx, cid, evs)
			return err
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build agenttools: %w", err)
	}
	return toolsSvc, nil
}

// assembleMCP wires the agent tools service, adapters, the reviewer brief and the MCP server. It also returns the card
// proposal worker, which is nil when no cards directory is configured.
func assembleMCP(
	d deps, ad adapters, store mcpStore, pool *pgxpool.Pool, blobs ports.BlobStore, eng reconcile.Recorder, gen ports.IDs, clk ports.Clock, files *knowledge.FileService, profiles mcp.Profiles,
	commands mcp.Proposer, needs func(context.Context, id.ChangeID) ([]string, error), profileStore ports.ProfileStore,
	withheld map[string]string,
) (*mcp.Server, *cardProposer, error) {
	knowledgeSvc, err := newKnowledgeSearch(d, ad, clk, store)
	if err != nil {
		return nil, nil, err
	}
	evidence, err := newCardEvidence(d, ad, store, clk, knowledgeSvc)
	if err != nil {
		return nil, nil, err
	}
	toolsSvc, err := newAgentTools(ad, store, pool, blobs, eng, gen, clk, reviewRefs{code: ad.vcs, memory: evidence, profiles: profileStore})
	if err != nil {
		return nil, nil, err
	}
	tokStore := ad.tokens
	if tokStore == nil {
		tokStore = &tokenStore{principals: make(map[string]mcp.Principal)}
	}
	tracker := &assignmentAgentTracker{agents: make(map[id.TaskID]id.AgentName)}
	tasksAdp, err := newTasksAdapter(d, toolsSvc, tracker, store, eng)
	if err != nil {
		return nil, nil, err
	}
	reviewsAdp := &mcpReviewsAdapter{svc: toolsSvc, tracker: tracker}
	cards, err := newCardReader(d.configuration.CardsDir, knowledge.PolicyScopes{Code: ad.vcs, Decode: policyyaml.Decode})
	if err != nil {
		return nil, nil, err
	}
	moat, err := newReviewMoat(d, ad, store, blobs, knowledgeSvc, clk)
	if err != nil {
		return nil, nil, err
	}
	mcpSrv, err := mcp.NewServer(mcp.Deps{
		Logger: d.logger, Clock: clk, Tokens: tokStore, Tasks: tasksAdp,
		Reviews: reviewsAdp, Code: ad.vcs, Blobs: blobs, Knowledge: knowledgeSvc,
		Cards:    cards,
		Files:    newKnowledgeFileManager(files, ad.vcs),
		Profiles: profiles,
		// run_check runs a profile check through the runner, never in this process. The job names the repository's
		// promoted profile, as every executor's job does; without it the runner refuses the check (found in H10).
		Sandbox:  profileSandbox{Sandbox: rootlessRunnerSandbox{client: ad.runner(blobs)}, Profiles: profileStore},
		Briefs:   moat.briefs,
		Graph:    moat.graph,
		Proposer: commands,
		Calls:    store,
		Paths:    ad.vcs,
		Needs:    needs,
		Checks:   profileChecks{profiles: profileStore},
		Withheld: withheld,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build mcp server: %w", err)
	}
	return mcpSrv, moat.proposer, nil
}

// newKnowledgeSearch builds the team-memory search service. The same service answers agent questions, checks the
// memory references a review cites, and re-queries them when an E3 finding is verified.
func newKnowledgeSearch(d deps, ad adapters, clk ports.Clock, trust ports.EpisodeSourceTrust) (*knowledge.SearchService, error) {
	graphitiCaller, err := graphiti.NewHTTPCaller(d.endpoints.graphitiURL, d.httpClient)
	if err != nil {
		return nil, fmt.Errorf("build Graphiti caller: %w", err)
	}
	memory, err := graphiti.New(graphitiCaller)
	if err != nil {
		return nil, fmt.Errorf("build Graphiti adapter: %w", err)
	}
	svc, err := knowledge.NewSearchService(memory, knowledge.PolicyScopes{Code: ad.vcs, Decode: policyyaml.Decode}, knowledgeQueryAudit{logger: d.logger}, clk, trust)
	if err != nil {
		return nil, fmt.Errorf("build knowledge search service: %w", err)
	}
	return svc, nil
}

func assembleProfileLifecycle(ad adapters, store ports.ProfileStore, blobs ports.BlobStore, clk ports.Clock) (mcp.Profiles, error) {
	service, err := newProfileService(ad, store, blobs, clk)
	if err != nil {
		return nil, err
	}
	return profileAPI{service: service, store: store, blobs: blobs}, nil
}

// newProfileService builds the sandbox profile lifecycle on the runner. The MCP tools and the profile_trial executor
// share its rules.
func newProfileService(ad adapters, store ports.ProfileStore, blobs ports.BlobStore, clk ports.Clock) (*appprofile.Service, error) {
	client := ad.runner(blobs)
	service, err := appprofile.New(appprofile.Config{
		Store: store, Parser: profileyaml.Parser{}, Policy: client, Trials: client, Syncer: client,
		Warmer: client, Authorizer: profileAuthorizer{code: ad.vcs, repos: ad.vcs}, Clock: clk,
	})
	if err != nil {
		return nil, fmt.Errorf("build sandbox profile lifecycle: %w", err)
	}
	return service, nil
}

// assembleSyncer wires the policy decoder, intake and the PR syncer. Their events go through the kicking store, so the
// reconciler wakes after each one. They record facts; the reconciler decides the gate.
func assembleSyncer(ad adapters, store *postgres.Store, events ports.EventStore, gen ports.IDs, clk ports.Clock) (*prsync.Syncer, *intake.Intake, error) {
	intakeCase, err := intake.New(intake.Config{
		Events: events,
		PRs:    ad.vcs,
		Code:   ad.vcs,
		Clock:  clk,
		IDs:    gen,
		Policy: policyyaml.Decode,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build intake: %w", err)
	}
	sync, err := prsync.New(prsync.Config{
		Repos:   ad.repos,
		PRs:     ad.vcs,
		Streams: store,
		Intake:  syncIntake{in: intakeCase},
		Reviews: reviewsAdapter{reader: ad.vcs},
		Checks:  checksAdapter{reader: ad.vcs},
		Events:  events,
		Clock:   clk,
		IDs:     gen,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build pr syncer: %w", err)
	}
	return sync, intakeCase, nil
}

// workflowAgentNames lists the configured test author, reviewers and resolver, so a change room is made with them in it.
func workflowAgentNames(a config.Agents) []string {
	names := make([]string, 0, len(a.Reviewers)+2)
	if a.TestAuthor != "" {
		names = append(names, a.TestAuthor)
	}
	names = append(names, a.Reviewers...)
	if a.Resolver != "" {
		names = append(names, a.Resolver)
	}
	return names
}

// baseExecutors builds the room_open, github_branch, and github_check executors.
func baseExecutors(
	ad adapters, in effects.Ingest, gen ports.IDs, clk ports.Clock,
) (room effects.RoomOpen, branch effects.GitHubBranch, check effects.GitHubCheck, err error) {
	room, err = effects.NewRoomOpen(effects.RoomOpenDeps{
		Tickets: ad.tickets, Chat: ad.chat, Ingest: in, IDs: gen, Clock: clk,
		Agents: workflowAgentNames(ad.agents),
	})
	if err != nil {
		return room, branch, check, fmt.Errorf("build room_open executor: %w", err)
	}
	branch, err = effects.NewGitHubBranch(effects.GitHubBranchDeps{
		Tickets: ad.tickets, Base: ad.vcs, Branches: ad.vcs, Ingest: in, IDs: gen, Clock: clk,
	})
	if err != nil {
		return room, branch, check, fmt.Errorf("build github_branch executor: %w", err)
	}
	check, err = effects.NewGitHubCheck(effects.GitHubCheckDeps{
		Checks: ad.vcs,
	})
	if err != nil {
		return room, branch, check, fmt.Errorf("build github_check executor: %w", err)
	}
	return room, branch, check, nil
}

// coreExecutors builds the executors of the core area: the room, the branch, the check run and the task assignment.
func coreExecutors(ad adapters, store ports.EventStore, in effects.Ingest, gen ports.IDs, clk ports.Clock) (map[change.EffectKind]outbox.Executor, error) {
	room, branch, check, err := baseExecutors(ad, in, gen, clk)
	if err != nil {
		return nil, err
	}
	inviter, ok := ad.chat.(ports.RoomInviter)
	if !ok {
		return nil, errors.New("build task_assign executor: the selected chat cannot invite agents into a room")
	}
	taskAssign, err := effects.NewTaskAssign(effects.TaskAssignDeps{
		Chat: ad.chat, Inviter: inviter, Focus: cursorsOf(store),
	})
	if err != nil {
		return nil, fmt.Errorf("build task_assign executor: %w", err)
	}
	return map[change.EffectKind]outbox.Executor{
		change.EffectRoomOpen:     room,
		change.EffectGitHubBranch: branch,
		change.EffectGitHubCheck:  check,
		change.EffectTaskAssign:   taskAssign,
	}, nil
}
