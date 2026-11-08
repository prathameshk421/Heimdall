package graphiti

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestSearchPassesServerGroupsAndMapsGraphitiFacts(t *testing.T) {
	caller := &callerSpy{replies: map[string]json.RawMessage{"search_memory_facts": json.RawMessage(`{"facts":[{"uuid":"f-1","fact":"refund ids are stable","source_url":"https://example.test/adr/4","valid_at":"2026-01-01T00:00:00Z"}]}`), "search_nodes": json.RawMessage(`{"nodes":[]}`)}}
	adapter, err := New(caller)
	require.NoError(t, err)
	items, err := adapter.Search(context.Background(), ports.MemoryQuery{Question: "refund ids", Groups: []string{"payments", "shared"}, Limit: 20, AsOf: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	require.Equal(t, []string{"search_memory_facts", "search_nodes"}, caller.names)
	require.Equal(t, []string{"payments", "shared"}, caller.args["group_ids"])
	require.Equal(t, "memory:f-1", items[0].Ref)
	require.Equal(t, ports.LabelObserved, items[0].Label)
	require.Equal(t, "https://example.test/adr/4", items[0].SourceURL)
}

func TestSearchPreservesHeimdallSourceKeyAndTrustMetadata(t *testing.T) {
	caller := &callerSpy{replies: map[string]json.RawMessage{
		"search_memory_facts": json.RawMessage(`{"facts":[{"uuid":"f-2","fact":"reviewed decision","heimdall_key":"workflow:evt:payments","heimdall_status":"confirmed"}]}`),
		"search_nodes":        json.RawMessage(`{"nodes":[]}`),
	}}
	adapter, err := New(caller)
	require.NoError(t, err)
	items, err := adapter.Search(context.Background(), ports.MemoryQuery{Question: "decision", Groups: []string{"payments"}, Limit: 20})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "workflow:evt:payments", items[0].SourceKey)
	require.Equal(t, ports.LabelConfirmed, items[0].Label)
}

func TestAddEpisodeCarriesStableSourceAndNeverExpandsGroup(t *testing.T) {
	caller := &callerSpy{replies: map[string]json.RawMessage{"add_memory": json.RawMessage(`{}`)}}
	adapter, err := New(caller)
	require.NoError(t, err)
	err = adapter.AddEpisode(context.Background(), ports.Episode{Key: "episode:adr:4:1", Source: "adr", Status: ports.EpisodeConfirmed, GroupID: "payments", Body: `{"claim":"refund ids are stable"}`})
	require.NoError(t, err)
	require.Equal(t, "add_memory", caller.names[0])
	require.Equal(t, "payments", caller.args["group_id"])
	require.Equal(t, "episode:adr:4:1", caller.args["name"], "the name carries the stable key")
	require.Contains(t, caller.args["episode_body"], "episode:adr:4:1")
	require.NotContains(t, caller.args, "uuid", "Graphiti reads a uuid as an update of an existing episode, which fails for a new one")
}

func TestForgetFindsTheEpisodeByNameThenDeletesItsGraphitiUUID(t *testing.T) {
	caller := &callerSpy{replies: map[string]json.RawMessage{
		"get_episodes":   json.RawMessage(`{"episodes":[{"uuid":"g-other","name":"episode:other"},{"uuid":"g-7","name":"episode:adr:4:1"}]}`),
		"delete_episode": json.RawMessage(`{}`),
	}}
	adapter, err := New(caller)
	require.NoError(t, err)
	require.NoError(t, adapter.ForgetEpisode(context.Background(), "episode:adr:4:1", "payments"))
	require.Equal(t, []string{"get_episodes", "delete_episode"}, caller.names)
	require.Equal(t, "g-7", caller.args["uuid"])
	require.Equal(t, "payments", caller.args["group_id"])
}

func TestForgetOfAnEpisodeThatIsNotThereDeletesNothing(t *testing.T) {
	caller := &callerSpy{replies: map[string]json.RawMessage{"get_episodes": json.RawMessage(`{"episodes":[]}`)}}
	adapter, err := New(caller)
	require.NoError(t, err)
	require.NoError(t, adapter.ForgetEpisode(context.Background(), "episode:gone", "payments"))
	require.Equal(t, []string{"get_episodes"}, caller.names)
}

func TestForgetFailsWhenTheScanIsFullAndTheEpisodeIsNotInIt(t *testing.T) {
	var episodes []string
	for i := range maxEpisodeScan {
		episodes = append(episodes, fmt.Sprintf(`{"uuid":"g-%d","name":"episode:%d"}`, i, i))
	}
	caller := &callerSpy{replies: map[string]json.RawMessage{"get_episodes": json.RawMessage(`{"episodes":[` + strings.Join(episodes, ",") + `]}`)}}
	adapter, err := New(caller)
	require.NoError(t, err)
	require.Error(t, adapter.ForgetEpisode(context.Background(), "episode:older", "payments"), "an older episode must not be reported as forgotten")
}

func TestSearchRefusesEmptyGroupScope(t *testing.T) {
	caller := &callerSpy{replies: map[string]json.RawMessage{}}
	adapter, err := New(caller)
	require.NoError(t, err)
	_, err = adapter.Search(context.Background(), ports.MemoryQuery{Question: "q", Limit: 20})
	require.ErrorContains(t, err, "groups")
	require.Empty(t, caller.names)
}

type callerSpy struct {
	names    []string
	args     map[string]any
	allArgs  []map[string]any
	replies  map[string]json.RawMessage
	failing  map[string]bool
	failWith error
}

func (c *callerSpy) Call(_ context.Context, name string, args map[string]any) (json.RawMessage, error) {
	c.names = append(c.names, name)
	c.args = args
	c.allArgs = append(c.allArgs, args)
	if c.failing[name] {
		if c.failWith != nil {
			return nil, c.failWith
		}
		return nil, errors.New("boom")
	}
	return c.replies[name], nil
}

func TestAsOfKeepsOnlyFactsValidAtThatTimeAndSendsNoInvertedServerFilter(t *testing.T) {
	caller := &callerSpy{replies: map[string]json.RawMessage{
		"search_memory_facts": json.RawMessage(`{"facts":[
			{"uuid":"old","fact":"limit is 50","valid_at":"2025-01-01T00:00:00Z","invalid_at":"2025-06-01T00:00:00Z"},
			{"uuid":"now","fact":"limit is 100","valid_at":"2025-06-01T00:00:00Z"},
			{"uuid":"future","fact":"limit is 200","valid_at":"2027-01-01T00:00:00Z"},
			{"uuid":"undated","fact":"refunds are idempotent"}]}`),
		"search_nodes": json.RawMessage(`{"nodes":[]}`),
	}}
	adapter, err := New(caller)
	require.NoError(t, err)
	items, err := adapter.Search(context.Background(), ports.MemoryQuery{Question: "limit", Groups: []string{"payments"}, Limit: 20, AsOf: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	var refs []string
	for _, item := range items {
		refs = append(refs, item.Ref)
	}
	require.Equal(t, []string{"memory:now", "memory:undated"}, refs)
	for _, call := range caller.allArgs {
		require.NotContains(t, call, "valid_at_after")
		require.NotContains(t, call, "invalid_at_before")
	}
}

func TestSearchOverFetchesWhenFilteringByTimeSoTheLimitStillFills(t *testing.T) {
	caller := &callerSpy{replies: map[string]json.RawMessage{"search_memory_facts": json.RawMessage(`{"facts":[]}`), "search_nodes": json.RawMessage(`{"nodes":[]}`)}}
	adapter, err := New(caller)
	require.NoError(t, err)
	_, err = adapter.Search(context.Background(), ports.MemoryQuery{Question: "q", Groups: []string{"g"}, Limit: 10, AsOf: time.Now()})
	require.NoError(t, err)
	require.Greater(t, caller.allArgs[0]["max_facts"], 10)
}

func TestSearchFoldsEntityNamesIntoTheQueryBecauseGraphitiHasNoSuchArgument(t *testing.T) {
	caller := &callerSpy{replies: map[string]json.RawMessage{"search_memory_facts": json.RawMessage(`{"facts":[]}`), "search_nodes": json.RawMessage(`{"nodes":[]}`)}}
	adapter, err := New(caller)
	require.NoError(t, err)
	_, err = adapter.Search(context.Background(), ports.MemoryQuery{Question: "who owns it", Entities: []string{"ledger"}, Groups: []string{"g"}, Limit: 5})
	require.NoError(t, err)
	require.Contains(t, caller.allArgs[0]["query"], "ledger")
	require.NotContains(t, caller.allArgs[0], "entity_names")
}

func TestSearchStillAnswersFromFactsWhenTheNodeSearchFails(t *testing.T) {
	caller := &callerSpy{replies: map[string]json.RawMessage{"search_memory_facts": json.RawMessage(`{"facts":[{"uuid":"f","fact":"a fact"}]}`)}, failing: map[string]bool{"search_nodes": true}}
	adapter, err := New(caller)
	require.NoError(t, err)
	items, err := adapter.Search(context.Background(), ports.MemoryQuery{Question: "q", Groups: []string{"g"}, Limit: 5})
	require.NoError(t, err)
	require.Len(t, items, 1)
}

func TestSearchFailsWhenTheFactSearchFailsAndKeepsTheCause(t *testing.T) {
	cause := errors.New("graphiti HTTP status 500")
	caller := &callerSpy{failing: map[string]bool{"search_memory_facts": true}, failWith: cause}
	adapter, err := New(caller)
	require.NoError(t, err)
	_, err = adapter.Search(context.Background(), ports.MemoryQuery{Question: "q", Groups: []string{"g"}, Limit: 5})
	require.ErrorIs(t, err, ports.ErrUnavailable)
	require.ErrorIs(t, err, cause)
}
