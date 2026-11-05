package bundle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestBuildReplayAndInputSensitivity(t *testing.T) {
	req := testRequest()
	first, err := Build(req)
	require.NoError(t, err)
	second, err := Build(req)
	require.NoError(t, err)
	require.Equal(t, first.Bytes, second.Bytes)
	require.Equal(t, Hash(first), Hash(second))

	changed := req
	changed.Criteria = []string{"AC2 changed"}
	criteria, err := Build(changed)
	require.NoError(t, err)
	require.NotEqual(t, Hash(first), Hash(criteria))
	changed = req
	changed.Diff = "new diff"
	diff, err := Build(changed)
	require.NoError(t, err)
	require.NotEqual(t, Hash(first), Hash(diff))
	changed = req
	changed.Pack = "other pack"
	pack, err := Build(changed)
	require.NoError(t, err)
	require.NotEqual(t, Hash(first), Hash(pack))
}

func TestBuildCanonicalizesUnorderedListsWithoutHTMLEscaping(t *testing.T) {
	req := testRequest()
	req.Diff = "<change & review>"
	req.FilesChanged = []string{"z.go", "a.go"}
	req.Criteria = []string{"AC2", "AC1"}
	req.CI = []string{"z:success", "a:failure"}
	first, err := Build(req)
	require.NoError(t, err)
	req.FilesChanged = []string{"a.go", "z.go"}
	req.Criteria = []string{"AC1", "AC2"}
	req.CI = []string{"a:failure", "z:success"}
	second, err := Build(req)
	require.NoError(t, err)
	require.Equal(t, first.Bytes, second.Bytes)
	require.Contains(t, string(first.Bytes), "<change & review>")
	require.NotContains(t, string(first.Bytes), `\\u003c`)
}

func TestBuildRefusesRoleCapOverflowWithNumbers(t *testing.T) {
	req := testRequest()
	req.Diff = strings.Repeat("x", 160004)
	req.BudgetTokens = 50000
	_, err := Build(req)
	require.ErrorContains(t, err, "test-author")
	require.ErrorContains(t, err, "40000")
	require.ErrorContains(t, err, "exceeds budget")
}

func TestBuildRejectsRoleBudgetAndKeepsUntrustedAsReference(t *testing.T) {
	req := testRequest()
	req.Role = workflow.RoleReviewer
	req.BudgetTokens = 1
	_, err := Build(req)
	require.ErrorContains(t, err, "reviewer")
	require.ErrorContains(t, err, "1")

	req = testRequest()
	built, err := Build(req)
	require.NoError(t, err)
	require.Contains(t, string(built.Bytes), string(req.Untrusted[0].Ref))
	require.NotContains(t, string(built.Bytes), "text")
}

func TestStoreRoundTripAndMissingBlob(t *testing.T) {
	built, err := Build(testRequest())
	require.NoError(t, err)
	store := newMemoryBlobs()
	ref, err := Store(context.Background(), store, ports.BlobBundle, built)
	require.NoError(t, err)
	require.Equal(t, Hash(built), ref)
	raw, err := store.Get(context.Background(), ref)
	require.NoError(t, err)
	contents, err := io.ReadAll(raw)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.Equal(t, built.Bytes, contents)

	_, err = store.Get(context.Background(), workflow.BlobRef("blob:sha256:"+strings.Repeat("0", 64)))
	require.ErrorIs(t, err, ports.ErrNotFound)
}

func TestBuildFromEventsProjectsContextDeterministically(t *testing.T) {
	events := []change.Event{
		bundleEvent(t, change.EventChangeTriggered, change.Triggered{Ticket: "PAY-999", Repo: "acme/pay-demo", Assignee: "agent", Source: change.TriggerJira}),
		bundleEvent(t, change.EventBranchEnsured, change.BranchEnsured{Repo: "acme/pay-demo", Branch: "PAY-999-tests", BaseSHA: "abcdef1"}),
		bundleEvent(t, change.EventPROpened, change.PROpened{Repo: "acme/pay-demo", Number: 1, HeadSHA: "abcdef2", Author: "author"}),
		bundleEvent(t, change.EventPRPushed, change.PRPushed{Repo: "acme/pay-demo", Number: 1, HeadSHA: "abcdef3"}),
		bundleEvent(t, change.EventPRClosed, change.PRClosed{Repo: "acme/pay-demo", Number: 1, HeadSHA: "abcdef4"}),
		bundleEvent(t, change.EventTestsSubmitted, change.TestsSubmitted{
			Refs: []string{"tests/heimdall/PAY-999/a_test.go"}, ACMap: map[string][]string{"AC2": {"TestTwo"}, "AC1": {"TestOne"}}, SHA: "abcdef4", TreeHash: "abcdef5",
		}),
	}

	built, err := BuildFromEvents(workflow.RoleReviewer, "PAY-142", events)
	require.NoError(t, err)
	require.Contains(t, string(built.Bytes), `"id":"PAY-999"`)
	require.Contains(t, string(built.Bytes), `"base_sha":"abcdef1"`)
	require.Contains(t, string(built.Bytes), `"head_sha":"abcdef4"`)
	require.Contains(t, string(built.Bytes), `"criteria":["AC1","AC2"]`)
}

func bundleEvent(t *testing.T, typ change.EventType, payload change.Payload) change.Event {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return change.Event{
		ID: "3f1c9a52-8d2b-4e7a-9c11-0b6f5d4e3a21", ChangeID: id.ChangeID("PAY-142"), Version: 1,
		Type: typ, Actor: change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt: time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC), CorrelationID: "trace-1", Payload: raw,
	}
}

func testRequest() Request {
	return Request{
		Role: workflow.RoleTestAuthor, Change: "PAY-142", BaseSHA: "base", HeadSHA: "head",
		Diff: "diff", FilesChanged: []string{"b.go", "a.go"}, Criteria: []string{"AC1"},
		CI: []string{"acceptance:success"}, Untrusted: []UntrustedRef{{Source: "pr_body", Ref: workflow.BlobRef("blob:sha256:" + strings.Repeat("a", 64))}},
		Pack: "pack", Memory: "memory", BudgetTokens: 40000,
	}
}

type memoryBlobs struct{ data map[workflow.BlobRef][]byte }

func newMemoryBlobs() *memoryBlobs { return &memoryBlobs{data: make(map[workflow.BlobRef][]byte)} }

func (m *memoryBlobs) Put(_ context.Context, _ ports.BlobClass, r io.Reader) (workflow.BlobRef, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	ref, err := workflow.ParseBlobRef("blob:sha256:" + hex.EncodeToString(sum[:]))
	if err != nil {
		return "", err
	}
	m.data[ref] = b
	return ref, nil
}

func (m *memoryBlobs) Get(_ context.Context, ref workflow.BlobRef) (io.ReadCloser, error) {
	b, ok := m.data[ref]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

var _ ports.BlobStore = (*memoryBlobs)(nil)
