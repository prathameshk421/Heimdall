package gitlog

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// TestMonthlySpendOnAFreshRepoIsZeroAndKnown is the AM16 finding: a fresh install has no budget file, and that is $0
// spent, known, not "the budget file could not be read" (ACTION_MODE 7).
func TestMonthlySpendOnAFreshRepoIsZeroAndKnown(t *testing.T) {
	g := newFakeGit(t)
	s := g.store(t, testKey)
	ctx := t.Context()

	got, known, err := s.ReadMonthlySpend(ctx, "2026-11")
	require.NoError(t, err)
	require.True(t, known, "a fresh repo spent nothing, and that is a fact")
	require.Zero(t, got)

	// The ref exists but the month does not.
	seed(ctx, t, s, prChange(t, 7), "room-1")
	got, known, err = s.ReadMonthlySpend(ctx, "2026-11")
	require.NoError(t, err)
	require.True(t, known)
	require.Zero(t, got)
}

// TestAddMonthlySpendIsIdempotentByRunKey covers the double-count trap: the gate runs twice on one proof run, and the
// month must hold that spend once.
func TestAddMonthlySpendIsIdempotentByRunKey(t *testing.T) {
	g := newFakeGit(t)
	s := g.store(t, testKey)
	ctx := t.Context()

	require.NoError(t, s.AddMonthlySpend(ctx, "2026-11", 250_000, "run-1"))
	got, known, err := s.ReadMonthlySpend(ctx, "2026-11")
	require.NoError(t, err)
	require.True(t, known)
	require.Equal(t, int64(250_000), got)

	require.NoError(t, s.AddMonthlySpend(ctx, "2026-11", 250_000, "run-1"), "a repeat of one run is a no-op")
	got, _, err = s.ReadMonthlySpend(ctx, "2026-11")
	require.NoError(t, err)
	require.Equal(t, int64(250_000), got, "the run is counted once")

	require.NoError(t, s.AddMonthlySpend(ctx, "2026-11", 75_000, "run-2"))
	got, _, err = s.ReadMonthlySpend(ctx, "2026-11")
	require.NoError(t, err)
	require.Equal(t, int64(325_000), got)

	// Another month is its own file.
	got, known, err = s.ReadMonthlySpend(ctx, "2026-12")
	require.NoError(t, err)
	require.True(t, known)
	require.Zero(t, got, "a new month starts at zero")

	// A different amount for a recorded run is refused, never added.
	err = s.AddMonthlySpend(ctx, "2026-11", 900_000, "run-1")
	require.ErrorIs(t, err, ErrBudgetRunRecorded)
	got, _, err = s.ReadMonthlySpend(ctx, "2026-11")
	require.NoError(t, err)
	require.Equal(t, int64(325_000), got)

	// A zero amount writes nothing at all.
	tip := g.tip()
	require.NoError(t, s.AddMonthlySpend(ctx, "2026-11", 0, "run-3"))
	require.Equal(t, tip, g.tip(), "a zero spend makes no commit")

	// The file is on the ref, under the month's name, and it is one commit on top of the tip the first add read.
	raw := g.file("budget/2026-11.json")
	require.NotEmpty(t, raw)
	var b monthlyBudget
	require.NoError(t, json.Unmarshal(raw, &b))
	require.Equal(t, budgetVersion, b.Version)
	require.Equal(t, "2026-11", b.Month)
	require.Equal(t, map[string]int64{"run-1": 250_000, "run-2": 75_000}, b.Runs)
}

// TestMonthlySpendRefusesACorruptFile: a month total that cannot be believed is unknown, never $0.
func TestMonthlySpendRefusesACorruptFile(t *testing.T) {
	cases := []struct {
		name string
		body string
		want error
	}{
		{"not json", "{", ErrBudgetFile},
		{"an unknown field", `{"version":1,"month":"2026-11","runs":{},"extra":1}`, ErrBudgetFile},
		{"another version", `{"version":2,"month":"2026-11","runs":{}}`, ErrBudgetFile},
		{"another month", `{"version":1,"month":"2026-10","runs":{}}`, ErrBudgetFile},
		{"a negative amount", `{"version":1,"month":"2026-11","runs":{"r":-1}}`, ErrBudgetFile},
		{"data after the value", `{"version":1,"month":"2026-11","runs":{}} x`, ErrBudgetFile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newFakeGit(t)
			g.writeFiles(map[string][]byte{"budget/2026-11.json": []byte(tc.body)})
			s := g.store(t, testKey)
			got, known, err := s.ReadMonthlySpend(t.Context(), "2026-11")
			require.False(t, known, "a malformed file is not $0")
			require.Zero(t, got)
			require.ErrorIs(t, err, tc.want)
			requireNoSecrets(t, err.Error())
		})
	}
}

// TestMonthlySpendRefusesBadArguments: the month and the run key are checked before any call.
func TestMonthlySpendRefusesBadArguments(t *testing.T) {
	g := newFakeGit(t)
	s := g.store(t, testKey)
	ctx := t.Context()
	before := g.callCount("")

	for _, month := range []string{"", "2026-1", "2026-13", "26-11", "../x", "2026-11/../x"} {
		_, _, err := s.ReadMonthlySpend(ctx, month)
		require.Error(t, err, month)
		require.Error(t, s.AddMonthlySpend(ctx, month, 1, "run-1"), month)
	}
	for _, key := range []string{"", strings.Repeat("k", MaxRunKey+1), "a b", "a\tb"} {
		require.Error(t, s.AddMonthlySpend(ctx, "2026-11", 1, key), key)
	}
	require.Error(t, s.AddMonthlySpend(ctx, "2026-11", -1, "run-1"), "a negative amount is refused")
	require.Equal(t, before, g.callCount(""), "a refused argument makes no call")
}

// TestMonthlySpendIsReadWithoutTheKey: the proof job holds no MAC key, and the month's total needs none.
func TestMonthlySpendIsReadWithoutTheKey(t *testing.T) {
	g := newFakeGit(t)
	s := g.store(t, testKey)
	require.NoError(t, s.AddMonthlySpend(t.Context(), "2026-11", 125_000, "run-1"))

	reader, err := NewMonthReader(MonthConfig{Repo: change.Repo(testRepo), Git: g.api(t, testToken)})
	require.NoError(t, err)
	got, known, err := reader.ReadMonthlySpend(t.Context(), "2026-11")
	require.NoError(t, err)
	require.True(t, known)
	require.Equal(t, int64(125_000), got)
}

func TestNewMonthReaderRefusesBadConfig(t *testing.T) {
	_, err := NewMonthReader(MonthConfig{Repo: change.Repo(testRepo)})
	require.Error(t, err, "Git is required")
	_, err = NewMonthReader(MonthConfig{Repo: "not a repo", Git: newFakeGit(t).api(t, testToken)})
	require.Error(t, err)
	_, err = NewMonthReader(MonthConfig{Repo: change.Repo(testRepo), Git: newFakeGit(t).api(t, testToken), Ref: "refs/x"})
	require.Error(t, err, "a ref needs two segments")
}

// TestAddMonthlySpendRetriesOnAnotherWriter: the budget write is a compare-and-swap, and it rebuilds on the new tip.
func TestAddMonthlySpendRetriesOnAnotherWriter(t *testing.T) {
	g := newFakeGit(t)
	s := g.store(t, testKey)
	ctx := t.Context()
	seed(ctx, t, s, prChange(t, 7), "room-1")

	// Another writer moves the ref once, after the store read it.
	var moved bool
	g.hookBeforeRefWrite = func(int) {
		if !moved {
			moved = true
			g.writeFiles(map[string][]byte{"budget/2026-11.json": []byte(`{"version":1,"month":"2026-11","runs":{"other":10}}`)})
		}
	}
	require.NoError(t, s.AddMonthlySpend(ctx, "2026-11", 90, "run-1"))

	got, known, err := s.ReadMonthlySpend(ctx, "2026-11")
	require.NoError(t, err)
	require.True(t, known)
	require.Equal(t, int64(100), got, "the other writer's entry and ours are both kept")
}

// TestResetReplacesABadLog is ACTION_MODE 2.7: a log that failed its MAC is replaced by a fresh one that starts with
// state.reset, and the new head verifies.
func TestResetReplacesABadLog(t *testing.T) {
	g := newFakeGit(t)
	s := g.store(t, testKey)
	ctx := t.Context()
	cid := prChange(t, 7)

	fx := roomEffect(t, cid, 3, "a", `{"text":"hi"}`)
	_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-1", "room-2", "room-3"), []change.Effect{fx})
	require.NoError(t, err)
	_, err = s.Load(ctx, cid)
	require.NoError(t, err)

	// A writer with push access edits the log under the MAC.
	g.writeFiles(map[string][]byte{logPath(cid): []byte(`{"broken":true}` + "\n")})
	_, err = s.Load(ctx, cid)
	requireIntegrity(t, err, cid, "malformed")

	reset := change.StateReset{Reason: "state failed verification: mac does not match"}
	ev := change.Event{
		ID: newEventID(), ChangeID: cid, Version: 1, Type: change.EventStateReset,
		Actor: change.Actor{Kind: change.ActorSystem, ID: "action-gate"}, OccurredAt: base,
		CorrelationID: "corr-reset", Payload: mustJSON(t, reset),
	}
	require.NoError(t, s.Reset(ctx, cid, []change.Event{ev}))

	got, err := s.Load(ctx, cid)
	require.NoError(t, err, "the new log verifies")
	require.Len(t, got, 1)
	require.Equal(t, change.EventStateReset, got[0].Type)
	require.Nil(t, got[0].PrevHash)
	require.NoError(t, change.VerifyChain(got))

	// A later append extends the new log, so the change goes on.
	n, err := s.Append(ctx, cid, 1, roomEvents(cid, 1, "room-9"), nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), n)

	// The effects file of the old log was replaced: its contents are gone with the log it belonged to.
	require.Empty(t, g.file(effPath(cid)), "the effects file is empty, matching the new head's digest")
}

// TestResetOnAFreshRefCreatesTheLog: a repo with no state ref yet can be reset into existence.
func TestResetOnAFreshRefCreatesTheLog(t *testing.T) {
	g := newFakeGit(t)
	s := g.store(t, testKey)
	cid := prChange(t, 11)
	ev := change.Event{
		ID: newEventID(), ChangeID: cid, Version: 1, Type: change.EventStateReset,
		Actor: change.Actor{Kind: change.ActorSystem, ID: "action-gate"}, OccurredAt: base,
		CorrelationID: "corr-reset", Payload: mustJSON(t, change.StateReset{Reason: "state failed verification: mac does not match"}),
	}
	require.NoError(t, s.Reset(t.Context(), cid, []change.Event{ev}))
	got, err := s.Load(t.Context(), cid)
	require.NoError(t, err)
	require.Len(t, got, 1)
}

// TestResetRefusesABatchItCouldNotRead: reset is a repair, not a way to write any batch, so a batch that does not
// start with state.reset is refused with no call and nothing written.
func TestResetRefusesABatchItCouldNotRead(t *testing.T) {
	g := newFakeGit(t)
	s := g.store(t, testKey)
	ctx := t.Context()
	cid := prChange(t, 7)
	seed(ctx, t, s, cid, "room-1")
	tip := g.tip()
	before := g.callCount("")

	require.Error(t, s.Reset(ctx, cid, nil), "an empty batch has no state.reset")
	require.Error(t, s.Reset(ctx, cid, roomEvents(cid, 0, "room-1")), "the first event must be state.reset")
	bad := change.Event{ID: newEventID(), ChangeID: cid, Version: 1, Type: change.EventStateReset,
		Actor: change.Actor{Kind: change.ActorSystem, ID: "action-gate"}, OccurredAt: base,
		CorrelationID: "corr-reset", Payload: mustJSON(t, change.StateReset{Reason: "x"})}
	bad.Version = 4
	require.Error(t, s.Reset(ctx, cid, []change.Event{bad}), "a version that does not start at 1 is refused")
	require.Equal(t, tip, g.tip())
	require.Equal(t, before, g.callCount(""), "a refused batch makes no call")
}

// TestResetRefusesACorruptBudget: the reset never touches another change's files.
func TestResetRefusesACorruptBudget(t *testing.T) {
	g := newFakeGit(t)
	s := g.store(t, testKey)
	ctx := t.Context()
	cid := prChange(t, 7)
	seed(ctx, t, s, cid, "room-1")
	other := prChange(t, 8)
	seed(ctx, t, s, other, "other-1")
	before, err := s.Load(ctx, other)
	require.NoError(t, err)

	ev := change.Event{
		ID: newEventID(), ChangeID: cid, Version: 1, Type: change.EventStateReset,
		Actor: change.Actor{Kind: change.ActorSystem, ID: "action-gate"}, OccurredAt: base,
		CorrelationID: "corr-reset", Payload: mustJSON(t, change.StateReset{Reason: "state failed verification: chain broken"}),
	}
	require.NoError(t, s.Reset(ctx, cid, []change.Event{ev}))
	after, err := s.Load(ctx, other)
	require.NoError(t, err)
	require.Equal(t, before, after, "another change's log is untouched")
}

// TestResetKeepsTheKeyOutOfErrors: a refused reset names no secret.
func TestResetKeepsTheKeyOutOfErrors(t *testing.T) {
	g := newFakeGit(t)
	s := g.store(t, testKey)
	cid := prChange(t, 7)
	err := s.Reset(t.Context(), cid, nil)
	require.Error(t, err)
	requireNoSecrets(t, err.Error())
	require.NotContains(t, err.Error(), string(testKey))
}

// TestBudgetRefusesAnUnreadableTree: a budget entry that is not a directory or a plain file is a malformed store.
func TestBudgetRefusesAnUnreadableTree(t *testing.T) {
	g := newFakeGit(t)
	g.writeFiles(map[string][]byte{"budget": []byte("not a directory")})
	s := g.store(t, testKey)
	_, known, err := s.ReadMonthlySpend(t.Context(), "2026-11")
	require.False(t, known)
	require.ErrorIs(t, err, ErrBudgetFile)
	require.Error(t, s.AddMonthlySpend(t.Context(), "2026-11", 1, "run-1"))
}

// TestBudgetRefusesAnOversizedBlob: a budget file over the limit is a size failure, not a month with no spend.
func TestBudgetRefusesAnOversizedBlob(t *testing.T) {
	g := newFakeGit(t)
	size := int64(maxBudgetBytes + 1)
	g.blobSizeLie = &size
	g.writeFiles(map[string][]byte{"budget/2026-11.json": []byte(`{"version":1,"month":"2026-11","runs":{}}`)})
	s := g.store(t, testKey)
	_, known, err := s.ReadMonthlySpend(t.Context(), "2026-11")
	require.False(t, known)
	require.ErrorIs(t, err, ErrTooLarge)
}

// TestBudgetErrorsKeepTheirKind keeps the failure mapping of the store's other readers.
func TestBudgetErrorsKeepTheirKind(t *testing.T) {
	g := newFakeGit(t)
	size := int64(maxBudgetBytes + 1)
	g.blobSizeLie = &size
	g.writeFiles(map[string][]byte{"budget/2026-11.json": []byte(`{}`)})
	s := g.store(t, testKey)
	_, _, err := s.ReadMonthlySpend(t.Context(), "2026-11")
	var limit *LimitError
	require.ErrorAs(t, err, &limit)
	requireNoSecrets(t, err.Error())
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}

// TestMonthlySpendSurvivesAnHTTPFailure: a read that failed is unknown with an error, never $0.
func TestMonthlySpendSurvivesAnHTTPFailure(t *testing.T) {
	g := newFakeGit(t)
	s := g.store(t, testKey)
	g.intercept = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasPrefix(r.URL.Path, "/repos/"+testRepo+"/git/ref/") {
			w.WriteHeader(http.StatusBadGateway)
			return true
		}
		return false
	}
	_, known, err := s.ReadMonthlySpend(context.Background(), "2026-11")
	require.False(t, known)
	require.ErrorIs(t, err, ports.ErrUnavailable, "a read that failed is unknown, and the caller sees why")
}
