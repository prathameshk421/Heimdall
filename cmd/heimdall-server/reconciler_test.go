package main

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reconcile"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/liveness"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The in-memory store also serves the reconciler's outbox, open-changes and dead-job ports.
func (m *memStore) EnsureEffects(_ context.Context, _ id.ChangeID, fx []change.Effect) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range fx {
		known := false
		for _, old := range m.fx {
			known = known || old.Key == f.Key
		}
		if !known {
			m.fx = append(m.fx, f)
		}
	}
	return nil
}

func (m *memStore) OpenChanges(context.Context) ([]id.ChangeID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []id.ChangeID
	for c := range m.events {
		out = append(out, c)
	}
	return out, nil
}

func (*memStore) DeadEffects(context.Context, id.ChangeID) ([]change.DeadJob, error) { return nil, nil }

type testInputs struct{}

func (testInputs) Policy(context.Context, change.Repo, id.SHA) (policy.Policy, error) {
	return policy.Policy{}, nil
}
func (testInputs) Outside(context.Context, change.View) (change.Outside, error) {
	return change.Outside{}, nil
}
func (testInputs) Profile(context.Context, change.Repo) (change.Profile, error) {
	return change.Profile{}, nil
}
func (testInputs) Bundle(ctx context.Context, kind, subject string, c id.ChangeID, evs []change.Event) ([]byte, error) {
	return reconcileInputs{}.Bundle(ctx, kind, subject, c, evs) // the real bundle: tests read it back through get_task
}

type noCalls struct{}

func (noCalls) RecordCall(context.Context, ports.ToolCall) error { return nil }
func (noCalls) RecentCalls(context.Context, id.AgentName, int) ([]ports.ToolCall, error) {
	return nil, nil
}
func (noCalls) TicketCalls(context.Context, id.ChangeID, int) ([]ports.ToolCall, error) {
	return nil, nil
}

type healthyProvider struct{}

func (healthyProvider) Probe(context.Context) (liveness.Health, error) { return liveness.Healthy, nil }

// reconcileStore is what the reconciler needs from a store: the in-memory one and postgres both have it.
type reconcileStore interface {
	ports.EventStore
	ports.EffectEnsurer
	ports.OpenChanges
	ports.DeadEffects
}

// newTestReconciler is the reconciler of the server on the in-memory store: no pick wait (the pick row records its
// default at once), the real clock, and the two reviewers of the journey tests.
func newTestReconciler(t *testing.T, store reconcileStore, reviewers ...change.ActorID) *reconcile.Reconciler {
	t.Helper()
	rec, err := reconcile.New(reconcile.Config{
		Store: store, Effects: store, Open: store, Dead: store, Inputs: testInputs{}, Calls: noCalls{}, Health: healthyProvider{},
		Agents: func(kind, subject string) (id.AgentName, bool) {
			switch kind {
			case "tests":
				return "test-author", true
			case "verdict":
				return "resolver", true
			}
			return id.AgentName(subject), true
		},
		IDs: ids.New(), Clock: clock.System{}, Logger: slog.New(slog.DiscardHandler), Wait: clock.Wait,
		Settings: change.Settings{Reviewers: reviewers, TestAttempts: 3,
			Timeouts: map[string]time.Duration{"tests": time.Hour, "review": time.Hour, "verdict": time.Hour}},
		Windows: liveness.Windows{Ack: time.Hour, Stall: time.Hour},
	})
	require.NoError(t, err)
	return rec
}
