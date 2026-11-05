package graphiti

import (
	"context"
	"encoding/json"
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

func TestAddEpisodeCarriesStableSourceAndNeverExpandsGroup(t *testing.T) {
	caller := &callerSpy{replies: map[string]json.RawMessage{"add_memory": json.RawMessage(`{}`)}}
	adapter, err := New(caller)
	require.NoError(t, err)
	err = adapter.AddEpisode(context.Background(), ports.Episode{Key: "episode:adr:4:1", Source: "adr", Status: ports.EpisodeConfirmed, GroupID: "payments", Body: `{"claim":"refund ids are stable"}`})
	require.NoError(t, err)
	require.Equal(t, "add_memory", caller.names[0])
	require.Equal(t, "payments", caller.args["group_id"])
	require.Contains(t, caller.args["episode_body"], "episode:adr:4:1")
	firstUUID := caller.args["uuid"]
	require.NotEmpty(t, firstUUID)
	require.NoError(t, adapter.AddEpisode(context.Background(), ports.Episode{Key: "episode:adr:4:1", Source: "adr", Status: ports.EpisodeConfirmed, GroupID: "payments", Body: `{"claim":"refund ids are stable"}`}))
	require.Equal(t, firstUUID, caller.args["uuid"], "retries must use the same Graphiti episode UUID")
	require.NoError(t, adapter.ForgetEpisode(context.Background(), "episode:adr:4:1", "payments"))
	require.Equal(t, "delete_episode", caller.names[len(caller.names)-1])
	require.Equal(t, "payments", caller.args["group_id"])
	require.Equal(t, firstUUID, caller.args["uuid"])
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
	names   []string
	args    map[string]any
	replies map[string]json.RawMessage
}

func (c *callerSpy) Call(_ context.Context, name string, args map[string]any) (json.RawMessage, error) {
	c.names = append(c.names, name)
	c.args = args
	return c.replies[name], nil
}
