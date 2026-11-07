package knowledge

import (
	"context"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestAskKnowledgeReplacesCallerGroupsWithPolicyTeamsAndAuditsRefs(t *testing.T) {
	mem := &memorySpy{items: []ports.MemoryItem{{Ref: "memory:refunds", Text: "idempotency", Label: ports.LabelConfirmed}}}
	audit := &auditSpy{}
	service, err := NewSearchService(mem, scopeSpy{"payments", "payments"}, audit, knowledgeClock{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	items, err := service.Ask(context.Background(), id.AgentName("reviewer"), id.TaskID("task-1"), change.Repo("acme/pay"), id.SHA("a01b2c3"), ports.MemoryQuery{Question: "refund idem", Groups: []string{"finance"}, Limit: 1})
	require.NoError(t, err)
	require.Equal(t, []string{"payments", "shared"}, mem.query.Groups)
	require.Equal(t, 20, mem.query.Limit)
	require.Equal(t, []string{"memory:refunds"}, audit.refs)
	require.Len(t, items, 1)
}

func TestAskKnowledgeRefusesUnlabeledMemoryResults(t *testing.T) {
	service, err := NewSearchService(&memorySpy{items: []ports.MemoryItem{{Ref: "memory:x", Text: "x", Label: "unknown"}}}, scopeSpy{"payments"}, &auditSpy{}, knowledgeClock{time.Now()})
	require.NoError(t, err)
	_, err = service.Ask(context.Background(), id.AgentName("reviewer"), id.TaskID("task-1"), change.Repo("acme/pay"), id.SHA("a01b2c3"), ports.MemoryQuery{Question: "q"})
	require.ErrorContains(t, err, "memory result 0 is invalid")
}

func TestAskKnowledgeTrustLabelsComeFromDurableSourceLedger(t *testing.T) {
	mem := &memorySpy{items: []ports.MemoryItem{{Ref: "memory:source", SourceKey: "workflow:event:payments", Label: ports.LabelConfirmed}}}
	ledger := sourceTrustSpy{status: ports.EpisodeObserved}
	service, err := NewSearchService(mem, scopeSpy{"payments"}, &auditSpy{}, knowledgeClock{time.Now()}, &ledger)
	require.NoError(t, err)
	items, err := service.Ask(context.Background(), "reviewer", "task", "acme/pay", "a01b2c3", ports.MemoryQuery{Question: "question"})
	require.NoError(t, err)
	require.Equal(t, ports.LabelObserved, items[0].Label, "stored source trust overrides a Graphiti label")
	ledger.status, ledger.invalidated = ports.EpisodeConfirmed, true
	service.cache = map[string]cachedSearch{}
	items, err = service.Ask(context.Background(), "reviewer", "task", "acme/pay", "a01b2c3", ports.MemoryQuery{Question: "another question"})
	require.NoError(t, err)
	require.Equal(t, ports.LabelSuperseded, items[0].Label, "a tombstoned source cannot remain confirmed")
}

func TestAskKnowledgeCachesForSixtySecondsButAuditsEachCallerQuery(t *testing.T) {
	clock := knowledgeClock{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	mem := &memorySpy{items: []ports.MemoryItem{{Ref: "memory:x", Text: "fact", Label: ports.LabelObserved}}}
	audit := &auditSpy{}
	service, err := NewSearchService(mem, scopeSpy{"payments"}, audit, clock)
	require.NoError(t, err)
	query := ports.MemoryQuery{Question: "  REFUND   ID "}
	_, err = service.Ask(context.Background(), "agent", "task", "repo", "a01b2c3", query)
	require.NoError(t, err)
	_, err = service.Ask(context.Background(), "agent", "task", "repo", "a01b2c3", ports.MemoryQuery{Question: "refund id"})
	require.NoError(t, err)
	require.Equal(t, 1, mem.calls)
	require.Equal(t, 2, audit.calls)
	clock.now = clock.now.Add(61 * time.Second)
	service.clock = clock
	_, err = service.Ask(context.Background(), "agent", "task", "repo", "a01b2c3", query)
	require.NoError(t, err)
	require.Equal(t, 2, mem.calls)
}

type memorySpy struct {
	query ports.MemoryQuery
	items []ports.MemoryItem
	calls int
}

func (m *memorySpy) AddEpisode(context.Context, ports.Episode) error { return nil }
func (m *memorySpy) Search(_ context.Context, q ports.MemoryQuery) ([]ports.MemoryItem, error) {
	m.calls++
	m.query = q
	return m.items, nil
}

type scopeSpy []string

func (s scopeSpy) GroupsForAgent(context.Context, id.AgentName, change.Repo, id.SHA) ([]string, error) {
	return s, nil
}

type auditSpy struct {
	refs  []string
	calls int
}

func (a *auditSpy) RecordKnowledgeQuery(_ context.Context, _ id.TaskID, _ id.AgentName, _ string, refs []string) error {
	a.calls++
	a.refs = refs
	return nil
}

type knowledgeClock struct{ now time.Time }

func (c knowledgeClock) Now() time.Time { return c.now }

type sourceTrustSpy struct {
	status      ports.EpisodeStatus
	invalidated bool
}

func (s *sourceTrustSpy) EpisodeTrust(context.Context, change.EffectKey) (ports.EpisodeStatus, bool, error) {
	return s.status, s.invalidated, nil
}
