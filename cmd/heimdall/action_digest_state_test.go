package main

// The digest over a real state ref. It writes a change log through gitlog.Store into the fake GitHub, then runs
// `heimdall action digest -repo` against that fake, so the read of the ref, the MAC check, the fold and the extraction
// are all exercised the way they run against GitHub.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/github"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/gitlog"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/digest"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// stateKey is the MAC key the test's state ref is signed with. It is a test value, never a real key.
func stateKey() []byte { return bytes.Repeat([]byte{9}, 32) }

// seedStateRef writes the fixture's log into the fake's refs/heimdall/state.
func seedStateRef(t *testing.T, gh *fakeGitHub, events []change.Event) {
	t.Helper()
	tokens, err := github.NewStaticToken(gh.token)
	require.NoError(t, err)
	api, err := github.NewAction(github.ActionConfig{BaseURL: gh.srv.URL, Tokens: tokens,
		HTTPClient: &http.Client{Timeout: 5 * time.Second}, Clock: testClock{}, Wait: noWait})
	require.NoError(t, err)
	store, err := gitlog.New(gitlog.Config{Repo: fakeRepo, Git: api, Key: stateKey(), Wait: noWait})
	require.NoError(t, err)
	_, err = store.Append(context.Background(), digestChangeID, 0, events, nil)
	require.NoError(t, err)
	require.NotEmpty(t, gh.refs["refs/heimdall/state"])
}

// testClock is the clock the seeded store needs. It never moves during one test.
type testClock struct{}

func (testClock) Now() time.Time { return digestNow }

// digestStateDeps wires the digest step to the fake GitHub and the test's key. env is the environment a case wants,
// over the three values a real run reads.
func digestStateDeps(env map[string]string) digestDeps {
	d := digestDeps{getenv: func(k string) string { return env[k] }, http: &http.Client{Timeout: 5 * time.Second},
		wait: noWait, now: func() time.Time { return digestNow }}
	d.state = func(ctx context.Context, src digestSource, stderr io.Writer) (digest.Input, error) {
		return readStateRef(ctx, src, stderr, d)
	}
	return d
}

// stateEnv is the environment of a run that may read the fake's state ref.
func stateEnv(gh *fakeGitHub) map[string]string {
	return map[string]string{"GITHUB_TOKEN": gh.token, "GITHUB_API_URL": gh.srv.URL,
		"HEIMDALL_LOG_KEY": hex.EncodeToString(stateKey())}
}

// TestDigestReadsTheStateRef is the -repo path end to end: one change log in the fake, one pull request in the report,
// with its fast check, its proof run, its cost and its wall time.
func TestDigestReadsTheStateRef(t *testing.T) {
	gh := newFakeGitHub(t)
	seedStateRef(t, gh, digestChangeEvents(t))
	code, stdout, stderr := runDigestWith(t, digestStateDeps(stateEnv(gh)), "-repo", fakeRepo, "-days", "0",
		"-format", "json")
	require.Equal(t, exitActionOK, code, stderr)
	var got digest.Report
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	require.Equal(t, fakeRepo, got.Repo)
	require.Equal(t, 1, got.PRs)
	require.Equal(t, 1, got.Shadow.Checked, "the change has a fast check and a proof run")
	require.Equal(t, 1, got.Shadow.WouldBlock, "the proof run blocked")
	require.Equal(t, 1, got.Shadow.RunBlocked)
	require.Equal(t, 1, got.Shadow.Proven)
	require.Equal(t, int64(250_000), got.Shadow.Cost.TotalMicros)
	require.Equal(t, int64(300_000), got.Misses.Proof.MaxMS, "the run took 5 minutes in the fixture")
	require.Equal(t, 1, got.Shadow.BreakGlassOpen)
}

// TestDigestIgnoresATamperedLog: a log whose head does not match its events fails the store's check, so it is reported
// and left out. It never adds to a total.
func TestDigestIgnoresATamperedLog(t *testing.T) {
	gh := newFakeGitHub(t)
	events := digestChangeEvents(t)
	events[len(events)-1].Payload = []byte(`{"head":"` + strings.Repeat("f", 40) + `","login":"ana",` +
		`"reason":"a different reason entirely","source_ref":"comment-2"}`)
	seedStateRefUnchecked(t, gh, events)
	code, stdout, stderr := runDigestWith(t, digestStateDeps(stateEnv(gh)), "-repo", fakeRepo, "-format", "json")
	require.Equal(t, exitActionOK, code, "one untrusted log does not fail the whole report")
	require.Contains(t, stderr, "was not read")
	var got digest.Report
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	require.Equal(t, 0, got.PRs, "a log that fails verification adds nothing")
}

// seedStateRefUnchecked writes a log the store accepts as bytes but whose head does not match, so a later read fails the
// MAC or the chain. It builds the file by hand and places it in the fake's tree.
func seedStateRefUnchecked(t *testing.T, gh *fakeGitHub, events []change.Event) {
	t.Helper()
	var buf bytes.Buffer
	for _, e := range events {
		raw, err := json.Marshal(e)
		require.NoError(t, err)
		buf.Write(raw)
		buf.WriteByte('\n')
	}
	gh.mu.Lock()
	defer gh.mu.Unlock()
	blob := gitSHA(buf.String())
	gh.blobs[blob] = buf.Bytes()
	head := gitSHA("not the head of this log")
	gh.blobs[head] = []byte(`{"version": 99, "hash": "` + strings.Repeat("0", 64) + `", "mac": "` +
		strings.Repeat("0", 64) + `"}`)
	tree := gitSHA("unchecked-tree")
	gh.trees[tree] = map[string]fakeEntry{
		"changes/" + string(digestChangeID) + ".jsonl": {mode: "100644", sha: blob},
		"changes/" + string(digestChangeID) + ".head":  {mode: "100644", sha: head},
	}
	commit := gitSHA("unchecked-commit")
	gh.commits[commit] = fakeCommit{tree: tree}
	gh.refs["refs/heimdall/state"] = commit
}

// TestDigestReportsAnUnreadableStateRef: a missing ref is an error, so a repository with no log is never reported as a
// clean week.
func TestDigestReportsAnUnreadableStateRef(t *testing.T) {
	gh := newFakeGitHub(t)
	code, _, stderr := runDigestWith(t, digestStateDeps(stateEnv(gh)), "-repo", fakeRepo)
	require.Equal(t, exitActionFail, code)
	require.Contains(t, stderr, "refs/heimdall/state")
}

// TestDigestNeedsTheLogKey: without the key the state ref cannot be verified, so the step refuses to read it.
func TestDigestNeedsTheLogKey(t *testing.T) {
	gh := newFakeGitHub(t)
	env := stateEnv(gh)
	delete(env, "HEIMDALL_LOG_KEY")
	code, _, stderr := runDigestWith(t, digestStateDeps(env), "-repo", fakeRepo)
	require.Equal(t, exitActionFail, code)
	require.Contains(t, stderr, "HEIMDALL_LOG_KEY")
	require.Zero(t, gh.count(""), "no GitHub call without a key to verify the log")
}

// TestDigestNeedsTheToken: a missing token fails before the ref is read.
func TestDigestNeedsTheToken(t *testing.T) {
	gh := newFakeGitHub(t)
	env := stateEnv(gh)
	delete(env, "GITHUB_TOKEN")
	code, _, stderr := runDigestWith(t, digestStateDeps(env), "-repo", fakeRepo)
	require.Equal(t, exitActionFail, code)
	require.Contains(t, stderr, "GITHUB_TOKEN")
}

// TestProofOfAnUnfinishedRun: a run that started and never reported is not a result, so the report counts no run and
// the check counts the pull request as checked by its fast check alone.
func TestProofOfAnUnfinishedRun(t *testing.T) {
	a := &change.ActionInfo{Proof: &change.ProofInfo{WorkflowRun: 1, Head: testHead, Done: false}}
	require.Equal(t, digest.ProofRecord{}, proofOf(a))
	require.Equal(t, int64(0), proofWallMS(nil, a))
	require.Equal(t, proof.KindBlocked, proofOf(&change.ActionInfo{Proof: &change.ProofInfo{Done: true,
		Kind: proof.KindBlocked}}).Kind)
}
