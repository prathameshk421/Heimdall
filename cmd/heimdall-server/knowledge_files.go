package main

import (
	"context"
	"fmt"
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/graphiti"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/openrouter"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/knowledge"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/pack"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

func assembleKnowledgeRetention(d deps, ad adapters, store knowledgeStore, events ports.EventStore, blobs ports.BlobStore, clk ports.Clock) (*knowledge.FileService, *knowledge.EpisodeWorker, *knowledge.WorkflowIngestor, error) {
	if err := checkKnowledgeDeps(ad, store, blobs, clk); err != nil {
		return nil, nil, nil, err
	}
	deleter, err := knowledgeBlobDeleter(blobs)
	if err != nil {
		return nil, nil, nil, err
	}
	caller, err := graphiti.NewHTTPCaller("http://graphiti:8000/mcp", d.httpClient)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build knowledge Graphiti caller: %w", err)
	}
	memory, err := graphiti.New(caller)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build knowledge Graphiti adapter: %w", err)
	}
	scanner, err := pack.NewSecretScanner()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build knowledge file scanner: %w", err)
	}
	worker, err := knowledge.NewEpisodeWorker(store, memory, clk)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build memory episode worker: %w", err)
	}
	lifecycle, err := knowledge.NewQueuedEpisodeLifecycle(memory, store, clk)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build memory episode lifecycle: %w", err)
	}
	service, err := knowledge.NewFileService(ad.vcs, ad.vcs, blobs, deleter, store,
		knowledge.PolicyFileApproval{Code: ad.vcs, Decode: policyyaml.Decode}, worker, lifecycle,
		knowledgeScanner{scanner: scanner}, clk)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build knowledge file service: %w", err)
	}
	workflowIngestor, err := newWorkflowIngestor(d, ad, events, store, scanner)
	if err != nil {
		return nil, nil, nil, err
	}
	return service, worker, workflowIngestor, nil
}

func checkKnowledgeDeps(ad adapters, store knowledgeStore, blobs ports.BlobStore, clk ports.Clock) error {
	if ad.vcs == nil || store == nil || blobs == nil || clk == nil {
		return fmt.Errorf("knowledge file service dependencies are incomplete")
	}
	return nil
}

func knowledgeBlobDeleter(blobs ports.BlobStore) (ports.BlobDeleter, error) {
	deleter, ok := blobs.(ports.BlobDeleter)
	if !ok {
		return nil, fmt.Errorf("knowledge blob store does not support deletion")
	}
	return deleter, nil
}

// newWorkflowIngestor builds the room digest and the workflow memory ingestor that feeds the episode queue.
func newWorkflowIngestor(d deps, ad adapters, events ports.EventStore, store knowledgeStore, scanner *pack.SecretScanner) (*knowledge.WorkflowIngestor, error) {
	scopes := workflowScopeResolver{store: events, vcs: ad.vcs}
	digest, err := knowledge.NewRoomDigest(events, ad.chat, openrouter.RoomDigestClient{
		Secrets: configSecretStore{config: d.configuration}, KeyName: "openrouter_api_key", HTTP: d.httpClient,
	}, store, scopes, scopes, knowledgeScanner{scanner: scanner})
	if err != nil {
		return nil, fmt.Errorf("build change room digest: %w", err)
	}
	workflowIngestor, err := knowledge.NewWorkflowIngestor(store, scopes, knowledgeScanner{scanner: scanner}, digest)
	if err != nil {
		return nil, fmt.Errorf("build workflow memory ingestor: %w", err)
	}
	return workflowIngestor, nil
}

type configSecretStore struct{ config config.Config }

func (s configSecretStore) Get(name string) (string, error) { return s.config.Secret(name) }

type knowledgeStore interface {
	ports.KnowledgeFiles
	ports.EpisodeQueue
}

type knowledgeScanner struct{ scanner *pack.SecretScanner }

func (s knowledgeScanner) Scan(raw string) (string, []knowledge.RedactionSummary, error) {
	clean, hits, err := s.scanner.Scan(raw)
	if err != nil {
		return "", nil, err
	}
	result := make([]knowledge.RedactionSummary, 0, len(hits))
	for _, hit := range hits {
		result = append(result, knowledge.RedactionSummary{Kind: hit.Kind, Count: hit.Count})
	}
	return clean, result, nil
}

var _ knowledgeRetention = (*knowledge.FileService)(nil)

type knowledgeFileAPI struct {
	files  *knowledge.FileService
	scopes knowledge.PolicyScopes
}

func newKnowledgeFileManager(files *knowledge.FileService, code knowledge.PolicyCode) mcp.KnowledgeFiles {
	if files == nil || code == nil {
		return nil
	}
	return knowledgeFileAPI{files: files, scopes: knowledge.PolicyScopes{Code: code, Decode: policyyaml.Decode}}
}

func (a knowledgeFileAPI) Stage(ctx context.Context, p mcp.Principal, assignment mcp.Assignment, path, group, kind, sourceURL string, retentionDays int) (mcp.KnowledgeFileInfo, error) {
	repo, base := change.Repo(assignment.Task.Change.Repo), assignment.Task.Change.BaseSHA
	groups, err := a.scopes.GroupsForAgent(ctx, p.Agent, repo, base)
	if err != nil {
		return mcp.KnowledgeFileInfo{}, fmt.Errorf("resolve reviewer knowledge groups: %w", err)
	}
	if !slices.Contains(groups, group) {
		return mcp.KnowledgeFileInfo{}, fmt.Errorf("group %q is outside the reviewer policy scope", group)
	}
	doc, removed, err := a.files.Stage(ctx, string(p.Agent), repo, path, group, kind, sourceURL, retentionDays)
	if err != nil {
		return mcp.KnowledgeFileInfo{}, err
	}
	return mcp.KnowledgeFileInfo{Ref: doc.Ref, Path: doc.Path, SHA: string(doc.SHA), Group: doc.Group, SourceURL: doc.SourceURL, State: doc.State, ExpiresAt: doc.ExpiresAt, Removed: removed}, nil
}

func (a knowledgeFileAPI) Approve(ctx context.Context, p mcp.Principal, ref string) error {
	if !p.Human || p.Person == "" || p.Agent != "" {
		return fmt.Errorf("mapped human principal is required")
	}
	return a.files.Approve(ctx, p.Person, ref)
}

func (a knowledgeFileAPI) Forget(ctx context.Context, p mcp.Principal, ref string) error {
	if !p.Human || p.Person == "" || p.Agent != "" {
		return fmt.Errorf("mapped human principal is required")
	}
	return a.files.Forget(ctx, p.Person, ref)
}

var _ mcp.KnowledgeFiles = knowledgeFileAPI{}
