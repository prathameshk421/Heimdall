package e2e_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

func newWorld(t *testing.T, mutate func(*e2e.LocalConfig)) *e2e.LocalWorld {
	t.Helper()
	cfg := e2e.LocalConfig{
		Story:  &e2e.Story{Name: "s", Repo: "o/r", BaseBranch: "main", GoodPR: "good", BadPR: "bad", InjectionPR: "inj", People: map[string]e2e.Person{"author": {Chat: "a", GitHub: "alice", Role: "author"}}},
		Agents: "scripted",
		Poll:   time.Millisecond,
		Token:  func(login string) (string, error) { return "token-for-" + login, nil },
	}
	if mutate != nil {
		mutate(&cfg)
	}
	return e2e.NewLocalWorld(cfg)
}

func TestPrereqServerProbesTheHealthEndpoint(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	up := newWorld(t, func(c *e2e.LocalConfig) { c.ServerURL = srv.URL })
	require.True(t, up.Prereq(t.Context(), e2e.NeedServer).OK)
	require.EqualValues(t, 1, hits.Load())

	srv.Close()
	down := newWorld(t, func(c *e2e.LocalConfig) { c.ServerURL = srv.URL })
	av := down.Prereq(t.Context(), e2e.NeedServer)
	require.False(t, av.OK)
	require.Contains(t, av.Why, srv.URL)

	none := newWorld(t, nil)
	require.False(t, none.Prereq(t.Context(), e2e.NeedServer).OK, "no server address configured")
}

func TestPrereqForwardNeedsEveryAddressToAnswer(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	good := ln.Addr().String()

	up := newWorld(t, func(c *e2e.LocalConfig) { c.ForwardAddrs = []string{good} })
	require.True(t, up.Prereq(t.Context(), e2e.NeedForward).OK)

	partial := newWorld(t, func(c *e2e.LocalConfig) { c.ForwardAddrs = []string{good, "127.0.0.1:1"} })
	av := partial.Prereq(t.Context(), e2e.NeedForward)
	require.False(t, av.OK)
	require.Contains(t, av.Why, "127.0.0.1:1")
	require.Contains(t, av.Why, "RUNBOOK", "the reason says where the fix is")
}

func githubStub(t *testing.T, branches map[string]int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token-for-alice", r.Header.Get("Authorization"), "the simulator passes a token explicitly per call")
		const prefix = "/repos/o/r/branches/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		code, ok := branches[strings.TrimPrefix(r.URL.Path, prefix)]
		if !ok {
			code = http.StatusNotFound
		}
		w.WriteHeader(code)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPrereqFixtureRepoChecksTheBaseAndEveryStoryBranch(t *testing.T) {
	all := githubStub(t, map[string]int{"main": 200, "good": 200, "bad": 200, "inj": 200})
	w := newWorld(t, func(c *e2e.LocalConfig) { c.GitHubAPI = all.URL })
	require.True(t, w.Prereq(t.Context(), e2e.NeedFixtureRepo).OK)

	empty := githubStub(t, nil)
	w = newWorld(t, func(c *e2e.LocalConfig) { c.GitHubAPI = empty.URL })
	av := w.Prereq(t.Context(), e2e.NeedFixtureRepo)
	require.False(t, av.OK)
	require.Contains(t, av.Why, "o/r")
	require.Contains(t, av.Why, "main")

	missing := githubStub(t, map[string]int{"main": 200, "good": 200, "inj": 200})
	w = newWorld(t, func(c *e2e.LocalConfig) { c.GitHubAPI = missing.URL })
	av = w.Prereq(t.Context(), e2e.NeedFixtureRepo)
	require.False(t, av.OK)
	require.Contains(t, av.Why, "bad")
}

func TestPrereqFixtureRepoSaysWhenItHasNoToken(t *testing.T) {
	w := newWorld(t, func(c *e2e.LocalConfig) {
		c.GitHubAPI = "http://127.0.0.1:1"
		c.Token = func(string) (string, error) { return "", errors.New("gh is not logged in as alice") }
	})
	av := w.Prereq(t.Context(), e2e.NeedFixtureRepo)
	require.False(t, av.OK)
	require.Contains(t, av.Why, "alice")
}

func TestPrereqForDriversNamesTheTaskThatBuildsThem(t *testing.T) {
	w := newWorld(t, nil)
	for need, task := range map[e2e.Need]string{
		e2e.NeedTracker: "T36", e2e.NeedGitHubUser: "T31", e2e.NeedChatUser: "T26",
		e2e.NeedScripted: "T38", e2e.NeedMemory: "T49", e2e.NeedContainer: "T68",
	} {
		av := w.Prereq(t.Context(), need)
		require.False(t, av.OK, string(need))
		require.Contains(t, av.Why, task, string(need))
	}
	require.False(t, w.Prereq(t.Context(), e2e.NeedManual).OK)
	require.Contains(t, w.Prereq(t.Context(), e2e.NeedManual).Why, "person")
}

func TestPrereqLiveAgentsFollowsTheAgentsSetting(t *testing.T) {
	scripted := newWorld(t, nil)
	av := scripted.Prereq(t.Context(), e2e.NeedLive)
	require.False(t, av.OK)
	require.Contains(t, av.Why, "AGENTS=live")
	live := newWorld(t, func(c *e2e.LocalConfig) { c.Agents = "live" })
	av = live.Prereq(t.Context(), e2e.NeedLive)
	require.False(t, av.OK, "live agents also need the budget guard")
	require.Contains(t, av.Why, "T19")
}

func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "heimdall")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700)) // #nosec G306 -- a test script the test itself runs
	return path
}

func TestPrereqCLIWantsAnExecutableBinary(t *testing.T) {
	require.False(t, newWorld(t, nil).Prereq(t.Context(), e2e.NeedCLI).OK)
	bin := script(t, "exit 0")
	require.True(t, newWorld(t, func(c *e2e.LocalConfig) { c.HeimdallBinary = bin }).Prereq(t.Context(), e2e.NeedCLI).OK)
	missing := filepath.Join(t.TempDir(), "nope")
	av := newWorld(t, func(c *e2e.LocalConfig) { c.HeimdallBinary = missing }).Prereq(t.Context(), e2e.NeedCLI)
	require.False(t, av.OK)
	require.Contains(t, av.Why, missing)
}

func TestHeimdallRunsTheBinaryAndReportsWhatAPersonSaw(t *testing.T) {
	bin := script(t, `echo "out: $1 $2"; echo "err: $HOME_TEST" >&2; exit 3`)
	w := newWorld(t, func(c *e2e.LocalConfig) { c.HeimdallBinary = bin })
	res, err := w.Heimdall().RunWithEnv(t.Context(), []string{"HOME_TEST=x"}, "connect", "--json")
	require.NoError(t, err, "a non-zero exit is a result, not an error")
	require.Equal(t, 3, res.ExitCode)
	require.Equal(t, "out: connect --json\n", res.Stdout)
	require.Equal(t, "err: x\n", res.Stderr)
}

func TestHeimdallErrorsWhenTheBinaryCannotStart(t *testing.T) {
	w := newWorld(t, func(c *e2e.LocalConfig) { c.HeimdallBinary = filepath.Join(t.TempDir(), "nope") })
	_, err := w.Heimdall().Run(t.Context(), "doctor")
	require.Error(t, err)
}

func TestUntilReturnsWhenTheConditionHolds(t *testing.T) {
	w := newWorld(t, nil)
	calls := 0
	err := w.Clock().Until(t.Context(), "the gate to turn red", time.Second, func(context.Context) (bool, string, error) {
		calls++
		return calls == 3, "no check yet", nil
	})
	require.NoError(t, err)
	require.Equal(t, 3, calls)
}

func TestUntilTimesOutWithWhatItSawAndTheNumber(t *testing.T) {
	w := newWorld(t, nil)
	err := w.Clock().Until(t.Context(), "the gate to turn red", 20*time.Millisecond, func(context.Context) (bool, string, error) {
		return false, "no check run", nil
	})
	var red *e2e.Red
	require.ErrorAs(t, err, &red)
	require.Contains(t, red.Expected, "the gate to turn red")
	require.Contains(t, red.Expected, "20ms")
	require.Equal(t, "no check run", red.Saw)
}

func TestUntilStopsOnAConditionErrorAndOnContextCancel(t *testing.T) {
	w := newWorld(t, nil)
	boom := errors.New("api down")
	err := w.Clock().Until(t.Context(), "x", time.Second, func(context.Context) (bool, string, error) { return false, "", boom })
	require.ErrorIs(t, err, boom)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = w.Clock().Until(ctx, "x", time.Hour, func(context.Context) (bool, string, error) { return false, "waiting", nil })
	require.ErrorIs(t, err, context.Canceled)
}

func TestUnbuiltDriversSayWhichTaskBuildsThem(t *testing.T) {
	w := newWorld(t, nil)
	_, err := w.Tracker().CreateTicket(t.Context(), e2e.Ticket{})
	task, ok := e2e.IsNotBuilt(err)
	require.True(t, ok)
	require.Equal(t, "T36", task)
	_, err = w.Chat().Transcript(t.Context(), "room")
	task, ok = e2e.IsNotBuilt(err)
	require.True(t, ok)
	require.Equal(t, "T26", task)
	_, err = w.GitHub().OpenPR(t.Context(), "o/r", "h", "b", "t", "b", "alice")
	task, ok = e2e.IsNotBuilt(err)
	require.True(t, ok)
	require.Equal(t, "T31", task)
	_, err = w.Server().Spend(t.Context())
	task, ok = e2e.IsNotBuilt(err)
	require.True(t, ok)
	require.Equal(t, "T19", task)
}

func TestPrereqServerIsDownWhenItAnswersButIsUnhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	t.Cleanup(srv.Close)
	av := newWorld(t, func(c *e2e.LocalConfig) { c.ServerURL = srv.URL }).Prereq(t.Context(), e2e.NeedServer)
	require.False(t, av.OK)
	require.Contains(t, av.Why, "503")
}
