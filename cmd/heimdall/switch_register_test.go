package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRegistrar struct {
	id, key string
	err     error
	gotType string
	gotName string
}

func (f *fakeRegistrar) Register(_ context.Context, agentType, name, _ string) (string, string, error) {
	f.gotType, f.gotName = agentType, name
	return f.id, f.key, f.err
}

func TestRegisterCoordinatorStoresTheKeyAndReturnsOnlyTheAgentID(t *testing.T) {
	reg := &fakeRegistrar{id: "agent-1", key: "sw-key-value"}
	stored := map[string]string{}
	store := func(name string, value []byte) error { stored[name] = string(value); return nil }

	id, err := registerCoordinator(context.Background(), reg, store, "switch_api_key", "opencode", "heimdall-coord-live")
	if err != nil {
		t.Fatalf("registerCoordinator() error = %v", err)
	}
	if id != "agent-1" {
		t.Fatalf("agent id = %q, want agent-1", id)
	}
	if stored["switch_api_key"] != "sw-key-value" {
		t.Fatalf("stored = %v, want the key under switch_api_key", stored)
	}
	if reg.gotType != "opencode" || reg.gotName != "heimdall-coord-live" {
		t.Fatalf("registered type=%q name=%q", reg.gotType, reg.gotName)
	}
}

func TestRegisterCoordinatorNeverLeaksTheKeyInAnError(t *testing.T) {
	reg := &fakeRegistrar{id: "agent-1", key: "sw-key-value"}
	store := func(string, []byte) error { return errors.New("disk full") }

	id, err := registerCoordinator(context.Background(), reg, store, "switch_api_key", "opencode", "heimdall-coord-live")
	if err == nil {
		t.Fatal("registerCoordinator() error = nil, want the store failure")
	}
	if strings.Contains(err.Error(), "sw-key-value") || strings.Contains(id, "sw-key-value") {
		t.Fatalf("key leaked: id=%q err=%v", id, err)
	}
}

func TestRegisterCoordinatorStoresNothingWhenRegistrationFails(t *testing.T) {
	reg := &fakeRegistrar{err: errors.New("409 conflict")}
	called := false
	store := func(string, []byte) error { called = true; return nil }

	if _, err := registerCoordinator(context.Background(), reg, store, "switch_api_key", "opencode", "x"); err == nil {
		t.Fatal("registerCoordinator() error = nil, want the registration failure")
	}
	if called {
		t.Fatal("store was called after a failed registration")
	}
}
