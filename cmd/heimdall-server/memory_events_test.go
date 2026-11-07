package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/stretchr/testify/require"
)

func TestWorkflowScopeResolverUsesMappedAssigneeTeamsAtTrustedDefaultBase(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "repos", "pay-demo", "base", "heimdall.yaml"))
	require.NoError(t, err)
	gen := ids.New()
	cid := id.ChangeID("PAY-142")
	triggered, err := json.Marshal(change.Triggered{Ticket: cid, Repo: "acme/pay-demo", Assignee: "bob-reviewer", Source: change.TriggerJira})
	require.NoError(t, err)
	eid := gen.NewEvent()
	event := change.Event{ID: eid, ChangeID: cid, Version: 1, Type: change.EventChangeTriggered,
		Actor: change.Actor{Kind: change.ActorJira, ID: "jira"}, OccurredAt: clock.System{}.Now().UTC(),
		CorrelationID: change.CorrelationID(eid), Payload: triggered}
	require.NoError(t, event.Validate())
	store := newMemStore()
	store.events[cid] = []change.Event{event}
	vcs := &policyMemoryVCS{dummyVCS: dummyVCS{}, raw: raw, head: "b100000000000000000000000000000000000001"}
	resolver := workflowScopeResolver{store: store, vcs: vcs}
	groups, err := resolver.GroupsForEvent(context.Background(), event)
	require.NoError(t, err)
	require.Equal(t, []string{"payments"}, groups)
	require.Equal(t, id.SHA("b100000000000000000000000000000000000001"), vcs.readAt)
}

func TestWorkflowScopeResolverFailsClosedForUnmappedActor(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "repos", "pay-demo", "base", "heimdall.yaml"))
	require.NoError(t, err)
	gen := ids.New()
	cid := id.ChangeID("PAY-143")
	triggered, err := json.Marshal(change.Triggered{Ticket: cid, Repo: "acme/pay-demo", Assignee: "missing-person", Source: change.TriggerJira})
	require.NoError(t, err)
	eid := gen.NewEvent()
	event := change.Event{ID: eid, ChangeID: cid, Version: 1, Type: change.EventChangeTriggered,
		Actor: change.Actor{Kind: change.ActorJira, ID: "jira"}, OccurredAt: time.Now().UTC(),
		CorrelationID: change.CorrelationID(eid), Payload: triggered}
	store := newMemStore()
	store.events[cid] = []change.Event{event}
	_, err = (workflowScopeResolver{store: store, vcs: &policyMemoryVCS{dummyVCS: dummyVCS{}, raw: raw, head: "b100000000000000000000000000000000000001"}}).GroupsForEvent(context.Background(), event)
	require.ErrorContains(t, err, "no trusted person mapping")
}

type policyMemoryVCS struct {
	dummyVCS
	raw    []byte
	head   id.SHA
	readAt id.SHA
}

func (v *policyMemoryVCS) DefaultHead(context.Context, change.Repo) (id.SHA, error) {
	return v.head, nil
}
func (v *policyMemoryVCS) ReadFile(_ context.Context, _ change.Repo, sha id.SHA, _ string) ([]byte, error) {
	v.readAt = sha
	return v.raw, nil
}
