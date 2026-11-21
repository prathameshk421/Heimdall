package uidsandbox

import (
	"context"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeExec records every command and answers by the base name of the command after "sudo -n --".
type fakeExec struct {
	calls  [][]string
	stdin  []string
	answer func(argv []string) (int, string)
}

func (f *fakeExec) Run(_ context.Context, c Cmd) (int, error) {
	f.calls = append(f.calls, c.Argv)
	if c.Stdin != nil {
		b, _ := io.ReadAll(c.Stdin)
		f.stdin = append(f.stdin, string(b))
	}
	code, out := 0, ""
	if f.answer != nil {
		code, out = f.answer(c.Argv)
	}
	if c.Stdout != nil && out != "" {
		_, _ = io.WriteString(c.Stdout, out)
	}
	return code, nil
}

func testTools() tools {
	return tools{
		sudo: "/usr/bin/sudo", useradd: "/usr/sbin/useradd", id: "/usr/bin/id", nft: "/usr/sbin/nft", ip: "/usr/sbin/ip",
		systemdRun: "/usr/bin/systemd-run", systemctl: "/usr/bin/systemctl", setpriv: "/usr/bin/setpriv",
		env: "/usr/bin/env", tar: "/usr/bin/tar", pkill: "/usr/bin/pkill", pgrep: "/usr/bin/pgrep",
		chmod: "/usr/bin/chmod", chown: "/usr/bin/chown", mkdir: "/usr/bin/mkdir", rm: "/usr/bin/rm",
		tee: "/usr/bin/tee", head: "/usr/bin/head",
	}
}

// readyAnswer is a host where the user exists with uid 1500 in its own group only and no process survives.
func readyAnswer(argv []string) (int, string) {
	cmd := strings.Join(argv[3:], " ")
	switch {
	case cmd == "/usr/bin/id -u heimdall-sbx":
		return 0, "1500\n"
	case cmd == "/usr/bin/id -nG heimdall-sbx":
		return 0, "heimdall-sbx\n"
	case strings.HasPrefix(cmd, "/usr/bin/pgrep"):
		return 1, ""
	case strings.HasPrefix(cmd, "/usr/sbin/nft list table"):
		return 1, ""
	case strings.HasPrefix(cmd, "/usr/sbin/ip netns exec heimdall-sbx /usr/bin/env true"):
		return 1, ""
	}
	return 0, ""
}

func setupSandbox(t *testing.T, f *fakeExec) *Sandbox {
	t.Helper()
	s := newSandbox(Config{
		RunnerHome: "/home/runner", RunnerTemp: "/home/runner/work/_temp",
		Registries: []string{"proxy.golang.org"}, ToolPath: "/opt/go/bin:/usr/bin:/bin",
		Limits: Limits{CPUPercent: 200, MemoryMax: 4 << 30, TasksMax: 512},
	}, f, testTools())
	require.NoError(t, s.Setup(context.Background()))
	t.Cleanup(func() { _ = s.Cleanup(context.Background()) })
	return s
}

func TestSetupCommands(t *testing.T) {
	created := false
	f := &fakeExec{answer: func(argv []string) (int, string) {
		switch strings.Join(argv[3:], " ") {
		case "/usr/bin/id -u heimdall-sbx":
			if !created {
				return 1, "no such user"
			}
		case "/usr/sbin/useradd --system --user-group --home-dir /sbx/home --no-create-home --shell /usr/sbin/nologin heimdall-sbx":
			created = true
		}
		return readyAnswer(argv)
	}}
	s := setupSandbox(t, f)
	assert.Equal(t, 1500, s.UID())
	joined := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		require.Equal(t, []string{"/usr/bin/sudo", "-n", "--"}, c[:3], "every host change goes through sudo -n")
		joined = append(joined, strings.Join(c[3:], " "))
	}
	for _, want := range []string{
		"/usr/sbin/useradd --system --user-group --home-dir /sbx/home --no-create-home --shell /usr/sbin/nologin heimdall-sbx",
		"/usr/bin/chmod 0700 -- /home/runner",
		"/usr/bin/chmod 0700 -- /home/runner/work/_temp",
		"/usr/bin/tee -a /etc/cron.deny",
		"/usr/bin/chown --no-dereference heimdall-sbx:heimdall-sbx -- /sbx/work",
		"/usr/sbin/nft -f -",
		"/usr/sbin/ip netns add heimdall-sbx",
		"/usr/sbin/ip -n heimdall-sbx link set lo up",
	} {
		assert.Contains(t, joined, want)
	}
	rules := strings.Join(f.stdin, "\n")
	assert.Contains(t, rules, `meta skuid 1500 oifname "lo" ip daddr 127.0.0.1 tcp dport `+strconv.Itoa(s.ProxyPort())+" accept")
	assert.Contains(t, rules, "meta skuid 1500 counter drop")
}

func TestSetupRefusesUserWithExtraGroups(t *testing.T) {
	f := &fakeExec{answer: func(argv []string) (int, string) {
		if strings.Join(argv[3:], " ") == "/usr/bin/id -nG heimdall-sbx" {
			return 0, "heimdall-sbx docker"
		}
		return readyAnswer(argv)
	}}
	s := newSandbox(Config{RunnerHome: "/h", RunnerTemp: "/t", Registries: []string{"proxy.golang.org"}}, f, testTools())
	err := s.Setup(context.Background())
	require.ErrorIs(t, err, ErrSetupStep)
	assert.Contains(t, err.Error(), "docker")
}

func TestExecArgvChain(t *testing.T) {
	f := &fakeExec{answer: readyAnswer}
	s := setupSandbox(t, f)
	f.calls = nil
	_, err := s.Exec(context.Background(), Job{
		Argv: []string{"go", "test", "./...; curl evil"}, Dir: "pkg", Env: []string{"GOFLAGS=-mod=mod"}, Timeout: time.Minute,
	})
	require.NoError(t, err)
	got := f.calls[0]
	want := []string{
		"/usr/bin/sudo", "-n", "--",
		"/usr/sbin/ip", "netns", "exec", "heimdall-sbx",
		"/usr/bin/systemd-run", "--scope", "--quiet", "--collect", "--unit=heimdall-sbx-1.scope",
		"-p", "CPUQuota=200%", "-p", "MemoryMax=4294967296", "-p", "TasksMax=512", "--",
		"/usr/bin/setpriv", "--reuid=1500", "--regid=1500", "--clear-groups", "--no-new-privs",
		"--inh-caps=-all", "--bounding-set=-all", "--",
		"/usr/bin/env", "-i", "-C", "/sbx/work/pkg", "--",
		"PATH=/opt/go/bin:/usr/bin:/bin", "HOME=/sbx/home", "TMPDIR=/sbx/tmp", "USER=heimdall-sbx", "LOGNAME=heimdall-sbx",
		"GOFLAGS=-mod=mod",
		"go", "test", "./...; curl evil",
	}
	assert.Equal(t, want, got, "PR data stays one argv element each; no shell")
	// Every job ends with the kill of the uid.
	last := strings.Join(f.calls[len(f.calls)-1][3:], " ")
	assert.True(t, strings.HasPrefix(last, "/usr/bin/pgrep"), last)
}

func TestFetchUsesHostNetworkAndProxy(t *testing.T) {
	f := &fakeExec{answer: readyAnswer}
	s := setupSandbox(t, f)
	f.calls = nil
	_, err := s.Fetch(context.Background(), Job{Argv: []string{"go", "mod", "download"}, Timeout: time.Minute})
	require.NoError(t, err)
	got := strings.Join(f.calls[0], " ")
	assert.NotContains(t, got, "netns exec")
	assert.Contains(t, got, "HTTPS_PROXY=http://127.0.0.1:"+strconv.Itoa(s.ProxyPort()))
	assert.Contains(t, got, "-C /sbx/work --")
}

func TestValidateJob(t *testing.T) {
	ok := Job{Argv: []string{"go", "test"}, Timeout: time.Second}
	require.NoError(t, validateJob(ok))
	bad := map[string]Job{
		"empty argv":        {Timeout: time.Second},
		"argv0 assignment":  {Argv: []string{"PATH=/tmp", "go"}, Timeout: time.Second},
		"argv0 option":      {Argv: []string{"-S", "sh"}, Timeout: time.Second},
		"NUL in argv":       {Argv: []string{"go", "a\x00b"}, Timeout: time.Second},
		"reserved PATH":     {Argv: []string{"go"}, Env: []string{"PATH=/tmp"}, Timeout: time.Second},
		"reserved proxy":    {Argv: []string{"go"}, Env: []string{"https_proxy=http://evil"}, Timeout: time.Second},
		"reserved no_proxy": {Argv: []string{"go"}, Env: []string{"No_Proxy=*"}, Timeout: time.Second},
		"bad env key":       {Argv: []string{"go"}, Env: []string{"A B=c"}, Timeout: time.Second},
		"env without =":     {Argv: []string{"go"}, Env: []string{"GOFLAGS"}, Timeout: time.Second},
		"no timeout":        {Argv: []string{"go"}},
		"dir escapes":       {Argv: []string{"go"}, Dir: "../home/runner", Timeout: time.Second},
		"absolute dir":      {Argv: []string{"go"}, Dir: "/etc", Timeout: time.Second},
		"junit no path":     {Argv: []string{"go"}, Result: ResultJUnit, Timeout: time.Second},
		"junit escapes":     {Argv: []string{"go"}, Result: ResultJUnit, ResultPath: "a/../../x", Timeout: time.Second},
	}
	for name, j := range bad {
		t.Run(name, func(t *testing.T) { require.ErrorIs(t, validateJob(j), ErrBadJob) })
	}
}

func TestKillAllReportsSurvivor(t *testing.T) {
	f := &fakeExec{answer: readyAnswer}
	s := setupSandbox(t, f)
	f.answer = func(argv []string) (int, string) {
		if strings.HasPrefix(argv[3], "/usr/bin/pgrep") {
			return 0, "4242"
		}
		return readyAnswer(argv)
	}
	require.ErrorIs(t, s.KillAll(context.Background()), ErrSurvivor)
	f.answer = readyAnswer
}

func TestReadResultAsSandboxUser(t *testing.T) {
	f := &fakeExec{answer: readyAnswer}
	s := setupSandbox(t, f)
	f.answer = func(argv []string) (int, string) {
		if argv[len(argv)-1] == "/sbx/work/out/junit.xml" {
			return 0, `<testsuite name="s"><testcase classname="c" name="t"/></testsuite>`
		}
		return readyAnswer(argv)
	}
	res, err := s.Exec(context.Background(), Job{
		Argv: []string{"make", "test"}, Timeout: time.Minute, Result: ResultJUnit, ResultPath: "out/junit.xml",
	})
	require.NoError(t, err)
	require.Len(t, res.Report.Cases(), 1)
	var read []string
	for _, c := range f.calls {
		if c[len(c)-1] == "/sbx/work/out/junit.xml" {
			read = c
		}
	}
	require.NotNil(t, read)
	assert.Contains(t, read, "--reuid=1500", "the result file is read as the sandbox user, not root")
}

func TestJobBeforeSetup(t *testing.T) {
	s := newSandbox(Config{}, &fakeExec{}, testTools())
	_, err := s.Exec(context.Background(), Job{Argv: []string{"go"}, Timeout: time.Second})
	require.ErrorIs(t, err, ErrNotSetup)
	require.ErrorIs(t, s.Stage(context.Background(), strings.NewReader(""), 0), ErrNotSetup)
}

func TestCannotIsolateIs(t *testing.T) {
	var err error = &CannotIsolateError{Reason: "missing tool nft"}
	require.ErrorIs(t, err, ErrCannotIsolate)
	assert.Equal(t, "runner cannot isolate PR code: missing tool nft", err.Error())
	require.NotErrorIs(t, ErrSurvivor, ErrCannotIsolate)
}
