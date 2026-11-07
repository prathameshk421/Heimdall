package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/gateuse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/intake"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/prsync"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

type sourceVCS struct {
	vcsPort
	called bool
	err    error
}

func (v *sourceVCS) Tarball(context.Context, change.Repo, id.SHA) (io.ReadCloser, error) {
	v.called = true
	if v.err != nil {
		return nil, v.err
	}
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func TestVCSSourceFetchProbesTarballAndPropagatesErrors(t *testing.T) {
	repo := change.Repo("owner/repo")
	sha := id.SHA("abc")
	t.Run("opens tarball", func(t *testing.T) {
		v := &sourceVCS{}
		require.NoError(t, (vcsSource{vcs: v}).Fetch(context.Background(), repo, sha))
		require.True(t, v.called, "Fetch must call Tarball")
	})
	t.Run("does not swallow tarball error", func(t *testing.T) {
		want := errors.New("tarball unavailable")
		v := &sourceVCS{err: want}
		err := (vcsSource{vcs: v}).Fetch(context.Background(), repo, sha)
		require.True(t, v.called, "Fetch must call Tarball")
		require.ErrorIs(t, err, want)
	})
}

// fakeCursorTable is an in-memory cursor table. A source that was never set is ports.ErrNotFound, as in postgres.
type fakeCursorTable struct {
	values map[string]string
	err    error
	saved  []string
}

func (f *fakeCursorTable) GetCursor(_ context.Context, source string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	v, ok := f.values[source]
	if !ok {
		return "", fmt.Errorf("cursor %s: %w", source, ports.ErrNotFound)
	}
	return v, nil
}

func (f *fakeCursorTable) SetCursor(_ context.Context, source, value string) error {
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[source] = value
	f.saved = append(f.saved, source+"="+value)
	return nil
}

func TestCursorStoreLoadMapsNotFoundToTheEmptyCursor(t *testing.T) {
	c := cursorStore{table: &fakeCursorTable{}, source: "jira:KAN"}
	cur, err := c.Load(context.Background())
	require.NoError(t, err, "a source with no cursor yet is the first poll, not a failure")
	require.Equal(t, ports.Cursor(""), cur)
}

func TestCursorStoreLoadReturnsTheSavedCursorForItsSource(t *testing.T) {
	table := &fakeCursorTable{values: map[string]string{"jira:KAN": "2026-11-05T10:00:00Z", "jira:OTHER": "other"}}
	c := cursorStore{table: table, source: "jira:KAN"}
	cur, err := c.Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, ports.Cursor("2026-11-05T10:00:00Z"), cur)
}

func TestCursorStoreLoadKeepsOtherErrors(t *testing.T) {
	boom := errors.New("connection reset")
	c := cursorStore{table: &fakeCursorTable{err: boom}, source: "jira:KAN"}
	_, err := c.Load(context.Background())
	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, ports.ErrNotFound, "a database failure must not look like a first poll")
}

func TestCursorStoreSaveWritesUnderItsSource(t *testing.T) {
	table := &fakeCursorTable{}
	c := cursorStore{table: table, source: "jira:KAN"}
	require.NoError(t, c.Save(context.Background(), ports.Cursor("2026-11-05T10:05:00Z")))
	require.Equal(t, []string{"jira:KAN=2026-11-05T10:05:00Z"}, table.saved)
}

func TestHumanTokenStoreReturnsOnlyPolicyPersonIdentity(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "alice_token"), []byte("mapped-human-credential\n"), 0o600))
	store, err := newTokenStore(config.Config{SecretsDir: dir, HumanTokens: map[string]string{"alice": "alice_token"}})
	require.NoError(t, err)
	principal, err := store.Resolve(context.Background(), mcp.HashToken("mapped-human-credential"))
	require.NoError(t, err)
	require.Equal(t, mcp.Principal{Person: "alice", Human: true}, principal)
}

func TestHumanTokenStoreRefusesCredentialCollisionAcrossIdentities(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "alice_token"), []byte("shared-credential\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bob_token"), []byte("shared-credential\n"), 0o600))
	_, err := newTokenStore(config.Config{SecretsDir: dir, HumanTokens: map[string]string{"alice": "alice_token", "bob": "bob_token"}})
	require.ErrorContains(t, err, "already assigned to another principal")
}

// memStore is an in-memory ports.EventStore with the optimistic version check of the Postgres adapter.
type memStore struct {
	mu     sync.Mutex
	events map[id.ChangeID][]change.Event
	fx     []change.Effect
}

func newMemStore() *memStore {
	return &memStore{events: map[id.ChangeID][]change.Event{}}
}

func (m *memStore) Append(_ context.Context, cid id.ChangeID, expected int64, evs []change.Event, fx []change.Effect) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if int64(len(m.events[cid])) != expected {
		return 0, fmt.Errorf("append to %s: %w", cid, ports.ErrVersionConflict)
	}
	m.events[cid] = append(m.events[cid], evs...)
	m.fx = append(m.fx, fx...)
	return int64(len(m.events[cid])), nil
}

func (m *memStore) Load(_ context.Context, cid id.ChangeID) ([]change.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.events[cid]), nil
}

// newSliceEngine builds the engine of the slice on store, as assemble does.
func newSliceEngine(t *testing.T, store ports.EventStore) *engine.Engine {
	t.Helper()
	agents := config.Agents{
		TestAuthor: "test-author",
		Reviewers:  []string{"reviewer-1", "reviewer-2"},
		Resolver:   "resolver",
	}
	gen := ids.New()
	clk := clock.System{}
	wf, steps, err := sliceSteps(agents, gen, clk)
	require.NoError(t, err)
	eng, err := engine.New(engine.Config{
		Workflow: wf, Steps: steps, Store: store, Clock: clk, IDs: gen, MaxAttempts: maxAttempts,
		Roles: sliceRoles(), Schemas: sliceSchemas(),
	})
	require.NoError(t, err)
	return eng
}

// triggeredEvent is the change.triggered event the poller builds for a ticket.
func triggeredEvent(t *testing.T, key string) change.Event {
	t.Helper()
	gen := ids.New()
	raw, err := json.Marshal(change.Triggered{
		Ticket:   id.ChangeID(key),
		Repo:     change.Repo("aarush323/PBL_TOS_RISK_ANALYZER"),
		Assignee: change.ActorID("557058:ana"),
		Source:   change.TriggerJira,
	})
	require.NoError(t, err)
	eid := gen.NewEvent()
	ev := change.Event{
		ID:            eid,
		ChangeID:      id.ChangeID(key),
		Version:       1,
		Type:          change.EventChangeTriggered,
		Actor:         change.Actor{Kind: change.ActorJira, ID: "jira"},
		OccurredAt:    clock.System{}.Now().UTC(),
		CorrelationID: change.CorrelationID(eid.String()),
		Payload:       raw,
	}
	require.NoError(t, ev.Validate())
	return ev
}

func TestStarterStartsTheChangeAndCommitsItsRoomEffect(t *testing.T) {
	store := newMemStore()
	s := starter{eng: newSliceEngine(t, store)}
	require.NoError(t, s.Start(context.Background(), triggeredEvent(t, "KAN-1")))

	evs, err := store.Load(context.Background(), "KAN-1")
	require.NoError(t, err)
	require.Len(t, evs, 1, "the first event only: room.opened comes from the executor")
	require.Equal(t, change.EventChangeTriggered, evs[0].Type)
	require.Len(t, store.fx, 1)
	require.Equal(t, change.EffectRoomOpen, store.fx[0].Kind, "the switch.open_room step commits its effect with the trigger")
}

func TestStarterReturnsVersionConflictForAnExistingChange(t *testing.T) {
	store := newMemStore()
	s := starter{eng: newSliceEngine(t, store)}
	require.NoError(t, s.Start(context.Background(), triggeredEvent(t, "KAN-1")))

	err := s.Start(context.Background(), triggeredEvent(t, "KAN-1"))
	require.ErrorIs(t, err, ports.ErrVersionConflict, "the poller counts this as already triggered")
}

type fakeIntakeHandler struct {
	res intake.Result
	err error
}

func (f *fakeIntakeHandler) HandlePR(context.Context, change.Repo, int) (intake.Result, error) {
	return f.res, f.err
}

func TestPRIntakeReturnsChangeIDFromIntakeResult(t *testing.T) {
	h := &fakeIntakeHandler{res: intake.Result{Stream: id.ChangeID("PAY-1")}}
	pi := prIntake{in: h}
	cid, err := pi.HandlePR(context.Background(), change.Repo("acme/pay"), 1)
	require.NoError(t, err)
	require.Equal(t, id.ChangeID("PAY-1"), cid)
}

// The sync adapter keeps whether intake appended events, so a restarted sync can re-run the gate (prsync.Handled).
func TestSyncIntakeReturnsChangeIDAndAppendedFromIntakeResult(t *testing.T) {
	h := &fakeIntakeHandler{res: intake.Result{Stream: id.ChangeID("PAY-1"), Appended: true}}
	si := syncIntake{in: h}
	got, err := si.HandlePR(context.Background(), change.Repo("acme/pay"), 1)
	require.NoError(t, err)
	require.Equal(t, prsync.Handled{Change: id.ChangeID("PAY-1"), Appended: true}, got)
}

func TestPRIntakePropagatesError(t *testing.T) {
	boom := errors.New("intake failed")
	h := &fakeIntakeHandler{err: boom}
	pi := prIntake{in: h}
	_, err := pi.HandlePR(context.Background(), change.Repo("acme/pay"), 1)
	require.ErrorIs(t, err, boom)
}

type fakeGateEvaluator struct {
	called   atomic.Bool
	passedID id.ChangeID
	err      error
}

func (f *fakeGateEvaluator) Evaluate(_ context.Context, cid id.ChangeID) (gateuse.Result, error) {
	f.called.Store(true)
	f.passedID = cid
	return gateuse.Result{}, f.err
}

func TestPRGateCallsEvaluator(t *testing.T) {
	eval := &fakeGateEvaluator{}
	pg := prGate{eval: eval}
	err := pg.Evaluate(context.Background(), id.ChangeID("PAY-42"))
	require.NoError(t, err)
	require.True(t, eval.called.Load())
	require.Equal(t, id.ChangeID("PAY-42"), eval.passedID)
}

func TestPRGatePropagatesError(t *testing.T) {
	boom := errors.New("gate failed")
	eval := &fakeGateEvaluator{err: boom}
	pg := prGate{eval: eval}
	err := pg.Evaluate(context.Background(), id.ChangeID("PAY-42"))
	require.ErrorIs(t, err, boom)
}

type fakeReviewReader struct {
	reviews []ports.PRReview
	err     error
}

func (f *fakeReviewReader) PRReviews(context.Context, change.Repo, int) ([]ports.PRReview, error) {
	return f.reviews, f.err
}

func TestReviewsAdapterMapsStateAndFields(t *testing.T) {
	sha := id.SHA("0123456789abcdef0123456789abcdef01234567")
	now := time.Now().UTC()
	reader := &fakeReviewReader{
		reviews: []ports.PRReview{
			{
				ID:          "rev-1",
				Login:       change.ActorID("bob"),
				State:       ports.ReviewStateApproved,
				CommitSHA:   sha,
				SubmittedAt: now,
			},
			{
				ID:          "rev-2",
				Login:       change.ActorID("alice"),
				State:       ports.ReviewStateDismissed,
				CommitSHA:   sha,
				SubmittedAt: now,
			},
		},
	}
	ra := reviewsAdapter{reader: reader}
	got, err := ra.PRReviews(context.Background(), change.Repo("acme/pay"), 1)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "rev-1", got[0].ID)
	require.Equal(t, change.ActorID("bob"), got[0].Login)
	require.Equal(t, prsync.ReviewApproved, got[0].State)
	require.Equal(t, sha, got[0].CommitSHA)
	require.Equal(t, now, got[0].SubmittedAt)
	require.Equal(t, prsync.ReviewDismissed, got[1].State)
}

func TestReviewsAdapterPropagatesError(t *testing.T) {
	boom := errors.New("review read failed")
	reader := &fakeReviewReader{err: boom}
	ra := reviewsAdapter{reader: reader}
	_, err := ra.PRReviews(context.Background(), change.Repo("acme/pay"), 1)
	require.ErrorIs(t, err, boom)
}

type fakeCheckReader struct {
	runs []ports.CheckRun
	err  error
}

func (f *fakeCheckReader) CheckRuns(context.Context, change.Repo, id.SHA) ([]ports.CheckRun, error) {
	return f.runs, f.err
}

func TestChecksAdapterMapsSHAAndFields(t *testing.T) {
	sha := id.SHA("0123456789abcdef0123456789abcdef01234567")
	reader := &fakeCheckReader{
		runs: []ports.CheckRun{
			{
				ID:         "run-1",
				Name:       "build",
				Status:     ports.CheckCompleted,
				Conclusion: change.CISuccess,
				SHA:        sha,
			},
		},
	}
	ca := checksAdapter{reader: reader}
	got, err := ca.CheckRuns(context.Background(), change.Repo("acme/pay"), sha)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "run-1", got[0].ID)
	require.Equal(t, "build", got[0].Name)
	require.Equal(t, prsync.CheckCompleted, got[0].Status)
	require.Equal(t, change.CISuccess, got[0].Conclusion)
	require.Equal(t, sha, got[0].SHA)
}

func TestChecksAdapterPropagatesError(t *testing.T) {
	boom := errors.New("check runs read failed")
	reader := &fakeCheckReader{err: boom}
	ca := checksAdapter{reader: reader}
	_, err := ca.CheckRuns(context.Background(), change.Repo("acme/pay"), id.SHA("0123456789abcdef0123456789abcdef01234567"))
	require.ErrorIs(t, err, boom)
}

func TestTokenStoreRejectsAgentConfiguredForMultipleRoles(t *testing.T) {
	secrets := t.TempDir()
	require.NoError(t, os.WriteFile(secrets+"/agent_token_shared", []byte("role-token"), 0o400))
	cfg := config.Config{SecretsDir: secrets, Agents: config.Agents{
		TestAuthor: "shared",
		Reviewers:  []string{"shared"},
	}}
	_, err := newTokenStore(cfg)
	require.Error(t, err, "one Switch identity cannot receive two role-scoped MCP principals")
}

func TestTokenStoreRejectsTokenSharedByDifferentAgents(t *testing.T) {
	secrets := t.TempDir()
	require.NoError(t, os.WriteFile(secrets+"/agent_token_test-author", []byte("same-token"), 0o400))
	require.NoError(t, os.WriteFile(secrets+"/agent_token_reviewer", []byte("same-token"), 0o400))
	cfg := config.Config{SecretsDir: secrets, Agents: config.Agents{
		TestAuthor: "test-author",
		Reviewers:  []string{"reviewer"},
	}}
	_, err := newTokenStore(cfg)
	require.Error(t, err, "two Switch identities cannot resolve to the same role-scoped MCP principal token")
}
