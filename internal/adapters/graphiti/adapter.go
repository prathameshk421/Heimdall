package graphiti

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Caller issues one MCP tools/call and returns the decoded tool result.
type Caller interface {
	Call(context.Context, string, map[string]any) (json.RawMessage, error)
}

// Adapter is the Graphiti implementation of team memory.
type Adapter struct{ caller Caller }

// New refuses a nil transport.
func New(caller Caller) (*Adapter, error) {
	if caller == nil {
		return nil, errors.New("graphiti caller must not be nil")
	}
	return &Adapter{caller: caller}, nil
}

// AddEpisode validates and submits an episode with source, status and scope metadata.
func (a *Adapter) AddEpisode(ctx context.Context, episode ports.Episode) error {
	if err := episode.Validate(); err != nil {
		return fmt.Errorf("validate memory episode: %w", err)
	}
	body, err := json.Marshal(struct {
		Key    string              `json:"heimdall_key"`
		Source string              `json:"heimdall_source"`
		Status ports.EpisodeStatus `json:"heimdall_status"`
		Body   json.RawMessage     `json:"episode"`
	}{string(episode.Key), episode.Source, episode.Status, json.RawMessage(episode.Body)})
	if err != nil {
		return fmt.Errorf("encode memory episode: %w", err)
	}
	args := map[string]any{"name": string(episode.Key), "episode_body": string(body), "source_description": episode.Source, "group_id": episode.GroupID, "uuid": episodeUUIDKey(string(episode.Key), episode.GroupID)}
	if _, err := a.caller.Call(ctx, "add_memory", args); err != nil {
		return fmt.Errorf("add Graphiti episode: %w", mapUnavailable(err))
	}
	return nil
}

// ForgetEpisode removes the stable source episode and its uniquely sourced facts from one group.
func (a *Adapter) ForgetEpisode(ctx context.Context, key, group string) error {
	if key == "" || group == "" {
		return errors.New("graphiti episode key and group are required")
	}
	_, err := a.caller.Call(ctx, "delete_episode", map[string]any{"uuid": episodeUUIDKey(key, group), "group_id": group})
	if err != nil {
		return fmt.Errorf("delete Graphiti episode: %w", mapUnavailable(err))
	}
	return nil
}

func episodeUUIDKey(key, group string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("heimdall:episode:"+group+":"+key)).String()
}

// overFetch is how many facts are requested per wanted item when results are filtered by time afterwards.
const overFetch = 4

// Search calls Graphiti's fact and node searches with the same server-derived groups. Graphiti's search tools take
// no entity-name or "as of" argument (search_nodes takes no date at all, and valid_at_after with invalid_at_before
// asks for the opposite of "valid at T"), so entity names are folded into the query and an as-of time is applied
// here to each fact's own validity window. The fact search is required; the node search only adds context, so its
// failure leaves the facts.
func (a *Adapter) Search(ctx context.Context, query ports.MemoryQuery) ([]ports.MemoryItem, error) {
	if err := query.Validate(); err != nil {
		return nil, fmt.Errorf("validate Graphiti query: %w", err)
	}
	groups := slices.Clone(query.Groups)
	slices.Sort(groups)
	groups = slices.Compact(groups)
	text := query.Question
	if len(query.Entities) > 0 {
		text += " " + strings.Join(query.Entities, " ")
	}
	maxFacts := query.Limit
	if !query.AsOf.IsZero() {
		maxFacts = query.Limit * overFetch
	}
	content, err := a.caller.Call(ctx, "search_memory_facts", map[string]any{"query": text, "group_ids": groups, "max_facts": maxFacts})
	if err != nil {
		return nil, fmt.Errorf("search Graphiti facts: %w", mapUnavailable(err))
	}
	items, err := decodeItems(content)
	if err != nil {
		return nil, err
	}
	items = validAt(items, query.AsOf)
	if nodes, err := a.caller.Call(ctx, "search_nodes", map[string]any{"query": text, "group_ids": groups, "max_nodes": query.Limit}); err == nil {
		if nodeItems, err := decodeNodes(nodes); err == nil {
			items = append(items, nodeItems...)
		}
	}
	if len(items) > query.Limit {
		items = items[:query.Limit]
	}
	for i := range items {
		if err := items[i].Validate(); err != nil {
			return nil, fmt.Errorf("graphiti fact %d: %w", i, err)
		}
	}
	return items, nil
}

// validAt keeps the items whose validity window contains asOf. A zero asOf keeps everything; an undated item is
// kept, because Graphiti gave no window to exclude it by.
func validAt(items []ports.MemoryItem, asOf time.Time) []ports.MemoryItem {
	if asOf.IsZero() {
		return items
	}
	kept := items[:0]
	for _, item := range items {
		if !item.ValidAt.IsZero() && item.ValidAt.After(asOf) {
			continue
		}
		if item.InvalidAt != nil && !item.InvalidAt.After(asOf) {
			continue
		}
		kept = append(kept, item)
	}
	return kept
}

func decodeNodes(raw json.RawMessage) ([]ports.MemoryItem, error) {
	var result struct {
		Nodes []struct {
			UUID      string     `json:"uuid"`
			Name      string     `json:"name"`
			Summary   string     `json:"summary"`
			SourceURL string     `json:"source_url"`
			ValidAt   time.Time  `json:"valid_at"`
			InvalidAt *time.Time `json:"invalid_at"`
		} `json:"nodes"`
		Items []struct {
			UUID      string `json:"uuid"`
			Name      string `json:"name"`
			Summary   string `json:"summary"`
			SourceURL string `json:"source_url"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode Graphiti nodes: %w", err)
	}
	out := make([]ports.MemoryItem, 0, len(result.Nodes)+len(result.Items))
	for _, node := range result.Nodes {
		ref := node.UUID
		if !strings.HasPrefix(ref, "memory:") {
			ref = "memory:node:" + url.PathEscape(ref)
		}
		text := node.Summary
		if text == "" {
			text = node.Name
		}
		out = append(out, ports.MemoryItem{Ref: ref, Text: text, Label: ports.LabelObserved, SourceURL: node.SourceURL, ValidAt: node.ValidAt, InvalidAt: node.InvalidAt})
	}
	for _, node := range result.Items {
		ref := node.UUID
		if !strings.HasPrefix(ref, "memory:") {
			ref = "memory:node:" + url.PathEscape(ref)
		}
		text := node.Summary
		if text == "" {
			text = node.Name
		}
		out = append(out, ports.MemoryItem{Ref: ref, Text: text, Label: ports.LabelObserved, SourceURL: node.SourceURL})
	}
	return out, nil
}

func decodeItems(raw json.RawMessage) ([]ports.MemoryItem, error) {
	var direct []ports.MemoryItem
	if err := json.Unmarshal(raw, &direct); err == nil {
		return normalize(direct), nil
	}
	var result struct {
		Facts []json.RawMessage `json:"facts"`
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode Graphiti facts: %w", err)
	}
	facts := result.Facts
	if len(facts) == 0 {
		facts = result.Items
	}
	items := make([]ports.MemoryItem, 0, len(facts))
	for _, rawFact := range facts {
		var fact struct {
			UUID      string              `json:"uuid"`
			Fact      string              `json:"fact"`
			Name      string              `json:"name"`
			SourceURL string              `json:"source_url"`
			Source    string              `json:"source"`
			SourceKey string              `json:"heimdall_key"`
			Trust     ports.EpisodeStatus `json:"heimdall_status"`
			AltKey    string              `json:"source_key"`
			Label     ports.MemoryLabel   `json:"label"`
			ValidAt   time.Time           `json:"valid_at"`
			InvalidAt *time.Time          `json:"invalid_at"`
		}
		if err := json.Unmarshal(rawFact, &fact); err != nil {
			return nil, fmt.Errorf("decode Graphiti fact: %w", err)
		}
		text := fact.Fact
		if text == "" {
			text = fact.Name
		}
		ref := fact.UUID
		if !strings.HasPrefix(ref, "memory:") {
			ref = "memory:" + url.PathEscape(ref)
		}
		label := fact.Label
		if fact.Trust == ports.EpisodeConfirmed {
			label = ports.LabelConfirmed
		}
		if fact.Trust == ports.EpisodeObserved {
			label = ports.LabelObserved
		}
		if !label.Valid() {
			label = ports.LabelObserved
		}
		if fact.SourceKey == "" {
			fact.SourceKey = fact.AltKey
		}
		items = append(items, ports.MemoryItem{Ref: ref, SourceKey: fact.SourceKey, Text: text, Label: label, SourceURL: fact.SourceURL, ValidAt: fact.ValidAt, InvalidAt: fact.InvalidAt})
	}
	return normalize(items), nil
}

func normalize(items []ports.MemoryItem) []ports.MemoryItem {
	for i := range items {
		if !items[i].Label.Valid() {
			items[i].Label = ports.LabelObserved
		}
	}
	return items
}

// mapUnavailable reports a Graphiti failure as ports.ErrUnavailable and keeps the cause for errors.Is and the log.
// The causes the HTTP caller returns carry a status or a step, never query text or a response body.
func mapUnavailable(cause error) error {
	return fmt.Errorf("graphiti request failed: %w: %w", ports.ErrUnavailable, cause)
}
