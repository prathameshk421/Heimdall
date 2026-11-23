package rootlessrunner

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/profileyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// gitRecorder records every git call and can fail on one.
type gitRecorder struct {
	mu     sync.Mutex
	calls  [][]string
	envs   [][]string
	failOn string
}

func (g *gitRecorder) Run(_ context.Context, _ string, env, args []string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, args)
	g.envs = append(g.envs, env)
	if slicesContainsStr(args, "init") {
		// the real git creates the bare repository at the last argument
		if err := os.MkdirAll(args[len(args)-1], 0o700); err != nil {
			return nil, err
		}
	}
	if g.failOn != "" && strings.Contains(strings.Join(args, " "), g.failOn) {
		return []byte("fatal: boom"), errors.New("exit 128")
	}
	return nil, nil
}

func historyRunner(t *testing.T, hosts []string, git GitRunner) (Runner, Policy) {
	t.Helper()
	p := jobPolicy()
	p.ModuleCache = filepath.Join(t.TempDir(), "module-cache")
	spec := p.Profiles["acme-pay"]
	spec.FetchHosts = hosts
	spec.Checks = append(append([]profileyaml.Check(nil), spec.Checks...),
		profileyaml.Check{ID: "mine", Lens: "contract", When: []string{"**/*"}, Image: "checks", Run: "heimdall-mine /history", History: true})
	active := map[string]RepoProfile{"acme-pay": spec}
	p.Profiles = nil
	return Runner{Policy: p, Executor: &checkExecutor{output: []byte(sarifOut)}, Root: t.TempDir(), Git: git, active: active}, p
}

func TestAMirrorLivesUnderTheHashOfItsRepositoryNeverItsName(t *testing.T) {
	t.Parallel()
	p := Policy{ModuleCache: "/var/lib/heimdall-runner/module-cache"}
	dir := p.mirrorDir("Acme/Pay")
	require.Equal(t, dir, p.mirrorDir("acme/pay"), "the case of a repository name does not make a second mirror")
	require.Equal(t, "/var/lib/heimdall-runner/history", filepath.Dir(dir))
	require.NotContains(t, dir, "acme")
	require.True(t, strings.HasSuffix(dir, ".git"))
	for _, hostile := range []string{"../../etc/passwd", "a/../../b", "x/y;z"} {
		require.Equal(t, "/var/lib/heimdall-runner/history", filepath.Dir(p.mirrorDir(change.Repo(hostile))), hostile)
	}
}

func TestOnlyAPlainRepositoryNameBecomesAFetchURL(t *testing.T) {
	t.Parallel()
	url, err := mirrorURL("acme/pay-demo.v2")
	require.NoError(t, err)
	require.Equal(t, "https://github.com/acme/pay-demo.v2.git", url)
	for _, bad := range []string{"", "acme", "acme/pay/extra", "../pay", "acme/..", "-x/pay", "acme/-pay", "acme/pay.git", "acme/pa y", "acme/pay?x=1", "a@b/pay", "acme/pay#f"} {
		_, err := mirrorURL(change.Repo(bad))
		require.Error(t, err, bad)
	}
}

func TestTheMirrorCommandsCarryEveryRestrictionAndOnlyFetchOnTheWarmPath(t *testing.T) {
	t.Parallel()
	fresh := historyCommands("http://127.0.0.1:9", "/h/x.git", "https://github.com/acme/pay.git", true)
	update := historyCommands("http://127.0.0.1:9", "/h/x.git", "https://github.com/acme/pay.git", false)
	require.Len(t, fresh, 5, "init, three configs, one fetch")
	require.Len(t, update, 1, "an existing mirror is only fetched")
	for _, cmd := range append(append([][]string{}, fresh...), update...) {
		joined := strings.Join(cmd, " ")
		for _, want := range []string{"core.hooksPath=/dev/null", "credential.helper=", "protocol.allow=never", "protocol.https.allow=always",
			"http.proxy=http://127.0.0.1:9", "http.followRedirects=false", "transfer.fsckObjects=true"} {
			require.Contains(t, joined, want)
		}
	}
	fetch := strings.Join(update[0], " ")
	require.Contains(t, fetch, "fetch")
	require.Contains(t, fetch, "--filter=blob:none")
	require.Contains(t, fetch, "--depth=5000")
	require.Contains(t, fetch, "--no-tags")
	require.Contains(t, fetch, "+refs/heads/*:refs/heads/*")
	require.Contains(t, strings.Join(fresh[0], " "), "init --bare")
	require.Contains(t, strings.Join(fresh[3], " "), "partialclonefilter blob:none")
}

func TestGitRunsWithNothingInheritedFromTheRunner(t *testing.T) {
	t.Parallel()
	env := gitEnv("/h")
	require.ElementsMatch(t, []string{"PATH=/usr/bin:/bin", "HOME=/h", "LANG=C", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}, env)
}

func TestTheMirrorIsFetchedOnlyUnderAProfileThatListsTheForgeHost(t *testing.T) {
	t.Parallel()
	git := &gitRecorder{}
	r, _ := historyRunner(t, nil, git)
	_, err := r.WarmHistory(context.Background(), "acme-pay", "acme/pay", "")
	require.ErrorIs(t, err, ErrHistoryNotAllowed)
	r, _ = historyRunner(t, []string{"proxy.golang.org"}, git)
	_, err = r.WarmHistory(context.Background(), "acme-pay", "acme/pay", "")
	require.ErrorIs(t, err, ErrHistoryNotAllowed, "another host does not allow github.com")
	_, err = r.WarmHistory(context.Background(), "nope", "acme/pay", "")
	require.ErrorIs(t, err, ErrProfileNotFound)
	_, err = r.WarmHistory(context.Background(), "go", "acme/pay", "")
	require.ErrorIs(t, err, ErrProfileNotFound, "the built-in profile has no fetch_hosts to approve")
	require.Empty(t, git.calls, "a refused update never runs git")

	r, _ = historyRunner(t, []string{"github.com"}, git)
	_, err = r.WarmHistory(context.Background(), "acme-pay", "../evil", "")
	require.Error(t, err)
	require.Empty(t, git.calls)
}

func TestWarmingTheMirrorRunsGitWithTheConfinedConfigurationAndCleansAFailedCreation(t *testing.T) {
	t.Parallel()
	git := &gitRecorder{}
	r, p := historyRunner(t, []string{"github.com"}, git)
	res, err := r.WarmHistory(context.Background(), "acme-pay", "acme/pay", "")
	require.NoError(t, err)
	require.True(t, res.Fresh)
	require.Len(t, git.calls, 5)
	for _, env := range git.envs {
		require.Equal(t, gitEnv(p.historyRoot()), env)
	}

	failing := &gitRecorder{failOn: "fetch"}
	r, p = historyRunner(t, []string{"github.com"}, failing)
	_, err = r.WarmHistory(context.Background(), "acme-pay", "acme/pay", "")
	require.ErrorContains(t, err, "update history mirror")
	require.NotContains(t, err.Error(), "127.0.0.1", "the proxy address is not echoed")
	_, statErr := os.Stat(p.mirrorDir("acme/pay"))
	require.ErrorIs(t, statErr, os.ErrNotExist, "a mirror that failed to be created leaves nothing behind")
}

func TestAnExistingMirrorIsFetchedNotRecreated(t *testing.T) {
	t.Parallel()
	git := &gitRecorder{}
	r, p := historyRunner(t, []string{"github.com"}, git)
	require.NoError(t, os.MkdirAll(p.mirrorDir("acme/pay"), 0o700))
	res, err := r.WarmHistory(context.Background(), "acme-pay", "acme/pay", "")
	require.NoError(t, err)
	require.False(t, res.Fresh)
	require.Len(t, git.calls, 1)
}

func TestTheProxyRefusesEverythingButAnAllowlistedHostOnTheHTTPSPort(t *testing.T) {
	t.Parallel()
	h := connectHandler(context.Background(), []string{"github.com"})
	for name, tt := range map[string]struct {
		method, target string
		want           int
	}{
		"a plain GET":                      {http.MethodGet, "github.com:443", http.StatusMethodNotAllowed},
		"another host":                     {http.MethodConnect, "evil.example:443", http.StatusForbidden},
		"the metadata address":             {http.MethodConnect, "169.254.169.254:443", http.StatusForbidden},
		"loopback":                         {http.MethodConnect, "127.0.0.1:443", http.StatusForbidden},
		"an allowlisted host on port 80":   {http.MethodConnect, "github.com:80", http.StatusForbidden},
		"an allowlisted host with no port": {http.MethodConnect, "github.com", http.StatusForbidden},
		"a lookalike suffix":               {http.MethodConnect, "github.com.evil.example:443", http.StatusForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(context.Background(), tt.method, "http://proxy/", nil)
			req.Host = tt.target
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			require.Equal(t, tt.want, rec.Code)
		})
	}
}

func TestACheckSeesTheMirrorOnlyReadOnlyAndOnlyWhenItsRowAsksForIt(t *testing.T) {
	t.Parallel()
	git := &gitRecorder{}
	exec := &checkExecutor{output: []byte(sarifOut)}
	r, p := historyRunner(t, []string{"github.com"}, git)
	r.Executor = exec

	in := JobInput{Profile: "acme-pay", CheckID: "mine", Repo: "acme/pay", Archive: strings.NewReader("a"), Timeout: time.Minute}
	_, err := r.Run(context.Background(), in)
	require.ErrorIs(t, err, ErrNoHistoryMirror, "no mirror means no job, never a silently empty history")
	require.Nil(t, exec.args)

	require.NoError(t, os.MkdirAll(p.mirrorDir("acme/pay"), 0o700))
	in.Archive = strings.NewReader("a")
	_, err = r.Run(context.Background(), in)
	require.NoError(t, err)
	require.Contains(t, exec.args, "type=bind,src="+p.mirrorDir("acme/pay")+",dst=/history,ro=true")
	require.Contains(t, exec.args, "--network=none")

	exec.args = nil
	_, err = r.Run(context.Background(), JobInput{Profile: "acme-pay", CheckID: "patterns", Repo: "acme/pay", Archive: strings.NewReader("a"), Timeout: time.Minute})
	require.NoError(t, err)
	require.NotContains(t, strings.Join(exec.args, " "), "dst=/history", "a check that did not ask for history does not see it")

	exec.args = nil
	_, err = r.Run(context.Background(), JobInput{Profile: "acme-pay", TestKind: "api", Dir: "tests/heimdall/PAY-1", Repo: "acme/pay", Archive: strings.NewReader("a"), Timeout: time.Minute})
	require.NoError(t, err)
	require.NotContains(t, strings.Join(exec.args, " "), "dst=/history", "a test job never sees the mirror")
}

func TestAJobSpecWithAMirrorMismatchIsRefused(t *testing.T) {
	t.Parallel()
	p := jobPolicy()
	good := jobSpec{Image: goRef, Run: "x", ResultFile: resultSARIF, Archive: "/var/jobs/a.tgz", ModuleCache: "/var/cache/mod", Timeout: 60}
	for name, mutate := range map[string]func(*jobSpec){
		"a mirror without the history flag": func(j *jobSpec) { j.HistoryDir = "/var/lib/h/x.git" },
		"the flag without a mirror":         func(j *jobSpec) { j.History = true },
		"a mirror path that adds an option": func(j *jobSpec) { j.History, j.HistoryDir = true, "/var/lib/h/x.git,rw" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			j := good
			mutate(&j)
			_, err := p.profileJobArgs(j)
			require.Error(t, err)
		})
	}
}

func TestTheHistoryEndpointTakesOnlyAWarmJob(t *testing.T) {
	t.Parallel()
	r, _ := historyRunner(t, []string{"github.com"}, &gitRecorder{})
	body := `{"profile":"acme-pay","job":{"kind":"red_first","repo":"acme/pay","sha":"` + strings.Repeat("a", 40) + `","dir":"t","timeout":60000000000}}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/history", strings.NewReader(body))
	_, status := r.handleHistory(req)
	require.Equal(t, http.StatusBadRequest, status)
}

func TestAPrivateMirrorFetchUsesTheRequestTokenOnlyInTheEnvironment(t *testing.T) {
	t.Parallel()
	git := &gitRecorder{}
	r, _ := historyRunner(t, []string{historyHost}, git)
	token := "ghs_" + strings.Repeat("Q", 36)
	_, err := r.WarmHistory(context.Background(), "acme-pay", change.Repo("acme/pay"), token)
	require.NoError(t, err)
	for i, args := range git.calls {
		require.NotContains(t, strings.Join(args, " "), "QQQQ", "the token never reaches git's arguments")
		env := strings.Join(git.envs[i], "\n")
		require.Contains(t, env, "GIT_CONFIG_COUNT=1")
		require.Contains(t, env, "GIT_CONFIG_KEY_0=http.https://github.com/.extraHeader")
		require.Contains(t, env, "GIT_CONFIG_VALUE_0=Authorization: Basic eC1hY2Nlc3MtdG9rZW46Z2hzX1FRUVFRUVFRUVFRUVFRUVFRUVFRUVFRUVFRUVFRUVFRUVFRUQ==")
	}

	public := &gitRecorder{}
	r, _ = historyRunner(t, []string{historyHost}, public)
	_, err = r.WarmHistory(context.Background(), "acme-pay", change.Repo("acme/pay"), "")
	require.NoError(t, err)
	for _, env := range public.envs {
		require.NotContains(t, strings.Join(env, "\n"), "GIT_CONFIG_COUNT", "a public repository gets no header")
	}
}

func TestAFailedPrivateFetchNeverRepeatsTheToken(t *testing.T) {
	t.Parallel()
	token := "ghs_" + strings.Repeat("Z", 36)
	git := &leakyGit{token: token}
	r, _ := historyRunner(t, []string{historyHost}, git)
	_, err := r.WarmHistory(context.Background(), "acme-pay", change.Repo("acme/pay"), token)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "ZZZZ")
}

// leakyGit fails with output that echoes the token, as a misbehaving remote or a verbose git might.
type leakyGit struct{ token string }

func (g *leakyGit) Run(_ context.Context, _ string, _, args []string) ([]byte, error) {
	if slicesContainsStr(args, "init") {
		if err := os.MkdirAll(args[len(args)-1], 0o700); err != nil {
			return nil, err
		}
	}
	if slicesContainsStr(args, "fetch") {
		return []byte("fatal: Authorization: Basic x-access-token:" + g.token), errors.New("exit 128")
	}
	return nil, nil
}
