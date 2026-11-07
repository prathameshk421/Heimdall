package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Scopes resolves an agent's mapped person and team memberships from policy.
type Scopes interface {
	GroupsForAgent(context.Context, id.AgentName, change.Repo, id.SHA) ([]string, error)
}

// SearchAudit records the question and result references against the active task.
type SearchAudit interface {
	RecordKnowledgeQuery(context.Context, id.TaskID, id.AgentName, string, []string) error
}

// SearchService combines policy scope, Graphiti and task audit.
type SearchService struct {
	memory ports.Memory
	scopes Scopes
	audit  SearchAudit
	clock  ports.Clock
	trust  ports.EpisodeSourceTrust
	mu     sync.Mutex
	cache  map[string]cachedSearch
}

type cachedSearch struct {
	items   []ports.MemoryItem
	expires time.Time
}

// NewSearchService requires all collaborators so a result cannot bypass scope or audit.
func NewSearchService(memory ports.Memory, scopes Scopes, audit SearchAudit, clock ports.Clock, ledgers ...ports.EpisodeSourceTrust) (*SearchService, error) {
	if memory == nil || scopes == nil || audit == nil || clock == nil {
		return nil, errors.New("knowledge search dependencies must not be nil")
	}
	service := &SearchService{memory: memory, scopes: scopes, audit: audit, clock: clock, cache: make(map[string]cachedSearch)}
	if len(ledgers) > 1 {
		return nil, errors.New("knowledge search accepts at most one source trust ledger")
	}
	if len(ledgers) == 1 {
		service.trust = ledgers[0]
	}
	return service, nil
}

// Ask queries memory for the assignment owner. Groups from query are discarded and rebuilt from policy.
func (s *SearchService) Ask(ctx context.Context, agent id.AgentName, task id.TaskID, repo change.Repo, base id.SHA, query ports.MemoryQuery) ([]ports.MemoryItem, error) {
	if agent == "" || task == "" {
		return nil, errors.New("knowledge search requires an active agent assignment")
	}
	query, err := s.scopedQuery(ctx, agent, repo, base, query)
	if err != nil {
		return nil, err
	}
	key, err := searchKey(query)
	if err != nil {
		return nil, fmt.Errorf("key knowledge query: %w", err)
	}
	items, err := s.search(ctx, key, query)
	if err != nil {
		return nil, err
	}
	if err := s.audit.RecordKnowledgeQuery(ctx, task, agent, query.Question, refsOf(items)); err != nil {
		return nil, fmt.Errorf("audit knowledge query: %w", err)
	}
	return items, nil
}

func (s *SearchService) scopedQuery(ctx context.Context, agent id.AgentName, repo change.Repo, base id.SHA, query ports.MemoryQuery) (ports.MemoryQuery, error) {
	if strings.TrimSpace(query.Question) == "" || len(query.Question) > 1024 || len(query.Entities) > 20 {
		return query, errors.New("knowledge query is invalid")
	}
	groups, err := s.scopes.GroupsForAgent(ctx, agent, repo, base)
	if err != nil {
		return query, fmt.Errorf("resolve knowledge groups: %w", err)
	}
	query.Groups = uniqueSorted(append(groups, "shared"))
	query.Limit = 20
	if err := query.Validate(); err != nil {
		return query, fmt.Errorf("validate scoped knowledge query: %w", err)
	}
	return query, nil
}

func (s *SearchService) search(ctx context.Context, key string, query ports.MemoryQuery) ([]ports.MemoryItem, error) {
	items, hit := s.cached(key)
	if hit {
		return items, nil
	}
	items, err := s.memory.Search(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("search team memory: %w", err)
	}
	for i, item := range items {
		if err := item.Validate(); err != nil {
			return nil, fmt.Errorf("memory result %d is invalid: %w", i, err)
		}
		if s.trust != nil && item.SourceKey != "" {
			status, invalidated, err := s.trust.EpisodeTrust(ctx, change.EffectKey(item.SourceKey))
			if err != nil && !errors.Is(err, ports.ErrNotFound) {
				return nil, fmt.Errorf("read durable memory source trust: %w", err)
			}
			switch {
			case err != nil || invalidated:
				item.Label = ports.LabelSuperseded
			case status == ports.EpisodeConfirmed:
				item.Label = ports.LabelConfirmed
			default:
				item.Label = ports.LabelObserved
			}
			items[i] = item
		}
	}
	s.remember(key, items)
	return items, nil
}

func refsOf(items []ports.MemoryItem) []string {
	refs := make([]string, 0, len(items))
	for _, item := range items {
		refs = append(refs, item.Ref)
	}
	return refs
}

func searchKey(query ports.MemoryQuery) (string, error) {
	query.Question = strings.Join(strings.Fields(strings.ToLower(query.Question)), " ")
	query.Groups = uniqueSorted(query.Groups)
	query.Entities = slices.Clone(query.Entities)
	slices.Sort(query.Entities)
	encoded, err := json.Marshal(query)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
func (s *SearchService) cached(key string) ([]ports.MemoryItem, bool) {
	now := s.clock.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[key]
	if !ok {
		return nil, false
	}
	if !now.Before(entry.expires) {
		delete(s.cache, key)
		return nil, false
	}
	return slices.Clone(entry.items), true
}
func (s *SearchService) remember(key string, items []ports.MemoryItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	for cacheKey, value := range s.cache {
		if !now.Before(value.expires) {
			delete(s.cache, cacheKey)
		}
	}
	s.cache[key] = cachedSearch{items: slices.Clone(items), expires: now.Add(60 * time.Second)}
}

func uniqueSorted(groups []string) []string {
	set := make(map[string]struct{}, len(groups))
	out := make([]string, 0, len(groups))
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if _, ok := set[group]; ok {
			continue
		}
		set[group] = struct{}{}
		out = append(out, group)
	}
	slices.Sort(out)
	return out
}
