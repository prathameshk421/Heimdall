package uidsandbox

import (
	"context"
	"io"
	"path"
	"slices"
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
		sudo: "/usr/bin/sudo", useradd: "/usr/sbin/useradd", id: "/usr/bin/id", nft: "/usr/sbin/nft",
		systemdRun: "/usr/bin/systemd-run", systemctl: "/usr/bin/systemctl",
		env: "/usr/bin/env", tar: "/usr/bin/tar", pkill: "/usr/bin/pkill", pgrep: "/usr/bin/pgrep",
		chmod: "/usr/bin/chmod", chown: "/usr/bin/chown", mkdir: "/usr/bin/mkdir", rm: "/usr/bin/rm",
		tee: "/usr/bin/tee", grep: "/usr/bin/grep", head: "/usr/bin/head", sh: "/usr/bin/sh",
	}
}

// readyAnswer is a host where the user exists with uid 1500 in its own group only and no process survives.
func readyAnswer(argv []string) (int, string) {
	if isSelfCheck(argv) {
		return selfCheckAnswer(argv)
	}
	cmd := strings.Join(argv[3:], " ")
	switch {
	case cmd == "/usr/bin/id -u heimdall-sbx":
		return 0, "1500\n"
	case cmd == "/usr/bin/id -nG heimdall-sbx":
		return 0, "heimdall-sbx\n"
	case strings.HasPrefix(cmd, "/usr/bin/pgrep"):
		return 1, ""
	case strings.HasPrefix(cmd, "/usr/bin/grep -qxF"):
		return 1, "" // the user is not in the deny files yet, so Setup appends it
	case strings.HasPrefix(cmd, "/usr/sbin/nft list table"):
		return 1, ""
	}
	return 0, ""
}

func testConfig() Config {
	return Config{
		RunnerHome: "/home/runner", RunnerTemp: "/home/runner/work/_temp",
		Registries: []string{"proxy.golang.org"}, ToolPath: "/opt/go/bin:/usr/bin:/bin",
		Limits: Limits{CPUPercent: 200, MemoryMax: 4 << 30, TasksMax: 512},
	}
}

func setupSandbox(t *testing.T, f *fakeExec) *Sandbox {
	t.Helper()
	s := newSandbox(testConfig(), f, testTools())
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
		"/usr/bin/grep -qxF -- heimdall-sbx /etc/cron.deny",
		"/usr/bin/grep -qxF -- heimdall-sbx /etc/at.deny",
		"/usr/bin/tee -a -- /etc/cron.deny",
		"/usr/bin/tee -a -- /etc/at.deny",
		"/usr/bin/chown --no-dereference heimdall-sbx:heimdall-sbx -- /sbx/work",
		"/usr/sbin/nft -f -",
		"/usr/bin/tee -- /tmp/heimdall-sbx-host-marker",
		"/usr/bin/tee -- /var/tmp/heimdall-sbx-host-marker",
		"/usr/bin/tee -- /dev/shm/heimdall-sbx-host-marker",
	} {
		assert.Contains(t, joined, want)
	}
	// The self check starts one service per phase with the full hardening (TestSelfCheckArgv pins its argv).
	var checks []string
	for _, c := range f.calls {
		if c[3] == "/usr/bin/systemd-run" {
			require.True(t, isSelfCheck(c), "only the self check starts a service during Setup")
			checks = append(checks, strings.Join(c, " "))
		}
	}
	require.Len(t, checks, 2)
	assert.Contains(t, checks[0], "-p PrivateNetwork=yes")
	assert.Contains(t, checks[1], "-p RestrictAddressFamilies=AF_INET AF_INET6")
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
	cfg := testConfig()
	cfg.RunnerHome, cfg.RunnerTemp = "/h", "/t"
	s := newSandbox(cfg, f, testTools())
	err := s.Setup(context.Background())
	require.ErrorIs(t, err, ErrSetupStep)
	assert.Contains(t, err.Error(), "docker")
}

func TestSetupRefusesZeroLimit(t *testing.T) {
	for name, l := range map[string]Limits{
		"cpu":    {MemoryMax: 1, TasksMax: 1},
		"memory": {CPUPercent: 100, TasksMax: 1},
		"tasks":  {CPUPercent: 100, MemoryMax: 1},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Limits = l
			f := &fakeExec{answer: readyAnswer}
			require.ErrorIs(t, newSandbox(cfg, f, testTools()).Setup(context.Background()), ErrSetupStep)
			assert.Empty(t, f.calls, "nothing changes on the host before the limits are checked")
		})
	}
}

// A systemd that rejects a property makes the runner unable to isolate: no job may run with less hardening.
func TestSetupSelfCheckRefused(t *testing.T) {
	f := &fakeExec{answer: func(argv []string) (int, string) {
		if argv[3] == "/usr/bin/systemd-run" {
			return 1, "Unknown assignment: ProtectProc=invisible"
		}
		return readyAnswer(argv)
	}}
	s := newSandbox(testConfig(), f, testTools())
	err := s.Setup(context.Background())
	require.ErrorIs(t, err, ErrCannotIsolate)
	assert.Contains(t, err.Error(), "ProtectProc=invisible")
	_ = s.Cleanup(context.Background())
}

// commonProps is the hardening both phases share, in order.
var commonProps = []string{
	"User=heimdall-sbx",
	"Group=heimdall-sbx",
	"NoNewPrivileges=yes",
	"CapabilityBoundingSet=",
	"AmbientCapabilities=",
	"ProtectSystem=strict",
	"ProtectHome=yes",
	"TemporaryFileSystem=/run /dev/shm:mode=1777",
	"PrivateTmp=yes",
	"PrivateDevices=yes",
	"PrivateIPC=yes",
	"ProtectProc=invisible",
	"ProtectKernelTunables=yes",
	"ProtectKernelModules=yes",
	"ProtectKernelLogs=yes",
	"ProtectControlGroups=yes",
	"ProtectClock=yes",
	"ProtectHostname=yes",
	"RestrictNamespaces=yes",
	"RestrictSUIDSGID=yes",
	"RestrictRealtime=yes",
	"LockPersonality=yes",
	"SystemCallArchitectures=native",
	"RemoveIPC=yes",
	"KeyringMode=private",
	"UMask=0077",
}

func TestServiceProps(t *testing.T) {
	// SEC-06: the execution phase may write only the work tree, so no state survives between Prove runs; the fetch
	// phase fills the module cache and the shared tmp.
	exec := append(append([]string{}, commonProps...), "ReadWritePaths=/sbx/work", "PrivateNetwork=yes")
	fetch := append(append([]string{}, commonProps...), "ReadWritePaths=/sbx/work /sbx/home /sbx/tmp",
		"RestrictAddressFamilies=AF_INET AF_INET6")
	assert.Equal(t, exec, serviceProps(PhaseExec))
	assert.Equal(t, fetch, serviceProps(PhaseFetch))
	assert.NotContains(t, serviceProps(PhaseFetch), "PrivateNetwork=yes", "the fetch phase needs the host network for the proxy")
}

// SEC-16: a Setup on a runner that already denies the user appends nothing to the deny files.
func TestSetupDenyFilesAreIdempotent(t *testing.T) {
	for _, listed := range []bool{false, true} {
		name := "appends when not listed"
		if listed {
			name = "appends nothing when already listed"
		}
		t.Run(name, func(t *testing.T) {
			f := &fakeExec{answer: func(argv []string) (int, string) {
				if listed && strings.HasPrefix(strings.Join(argv[3:], " "), "/usr/bin/grep -qxF") {
					return 0, ""
				}
				return readyAnswer(argv)
			}}
			setupSandbox(t, f)
			var appends []string
			for _, c := range f.calls {
				cmd := strings.Join(c[3:], " ")
				if strings.HasPrefix(cmd, "/usr/bin/tee -a") {
					appends = append(appends, cmd)
				}
			}
			if listed {
				assert.Empty(t, appends, "the deny files must not grow on a repeated Setup")
				return
			}
			assert.Equal(t, []string{"/usr/bin/tee -a -- /etc/cron.deny", "/usr/bin/tee -a -- /etc/at.deny"}, appends)
		})
	}
}

// unitHead returns everything of a job command up to and including env's "--", and the rest.
func unitHead(t *testing.T, argv []string) ([]string, []string) {
	t.Helper()
	for i := 3; i+4 < len(argv); i++ {
		if argv[i] == "--" && argv[i+1] == "/usr/bin/env" {
			require.Equal(t, []string{"-i", "-C"}, argv[i+2:i+4])
			return argv[:i+6], argv[i+6:]
		}
	}
	t.Fatalf("no env after systemd-run in %q", argv)
	return nil, nil
}

func wantHead(phase Phase, unit, timeoutSec, dir string) []string {
	h := []string{"/usr/bin/sudo", "-n", "--", "/usr/bin/systemd-run", "--wait", "--pipe", "--collect", "--quiet",
		"--expand-environment=no", "--service-type=exec", "--unit=" + unit}
	for _, p := range serviceProps(phase) {
		h = append(h, "-p", p)
	}
	return append(h, "-p", "CPUQuota=200%", "-p", "MemoryMax=4294967296", "-p", "TasksMax=512",
		"-p", "RuntimeMaxSec="+timeoutSec, "--", "/usr/bin/env", "-i", "-C", dir, "--")
}

// No job input can add, remove or override a property: argv, Dir and Env only ever follow env's "--".
func TestJobCannotChangeProps(t *testing.T) {
	hostile := []Job{
		{Argv: []string{"go", "-p", "PrivateNetwork=no", "--", "-p", "User=root"}, Timeout: time.Minute},
		{Argv: []string{"go"}, Env: []string{"PrivateNetwork=no", "NoNewPrivileges=no", "RestrictAddressFamilies=AF_UNIX",
			"ProtectHome=no", "TemporaryFileSystem=", "Group=root"}, Timeout: time.Minute},
		{Argv: []string{"go"}, Dir: "a -p PrivateNetwork=no", Timeout: time.Minute},
		{Argv: []string{"go", "${HOME}", "$PATH", "%h"}, Timeout: time.Minute},
	}
	for _, phase := range []Phase{PhaseExec, PhaseFetch} {
		for i, j := range hostile {
			require.NoError(t, validateJob(j))
			f := &fakeExec{answer: readyAnswer}
			s := setupSandbox(t, f)
			f.calls = nil
			_, err := s.run(context.Background(), phase, j)
			require.NoError(t, err)
			dir, _ := relPath(j.Dir)
			head, tail := unitHead(t, f.calls[0])
			assert.Equal(t, wantHead(phase, "heimdall-sbx-3.service", "60", path.Join("/sbx/work", dir)), head, "phase %d job %d", phase, i)
			assert.Equal(t, j.Argv, tail[len(tail)-len(j.Argv):], "argv stays literal and last")
		}
	}
}

func TestRuntimeMaxSec(t *testing.T) {
	for d, want := range map[time.Duration]string{
		time.Minute: "60", 1500 * time.Millisecond: "2", time.Millisecond: "1", time.Second: "1",
	} {
		assert.Equal(t, want, runtimeMaxSec(d), d.String())
	}
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
		"/usr/bin/systemd-run", "--wait", "--pipe", "--collect", "--quiet", "--expand-environment=no",
		"--service-type=exec", "--unit=heimdall-sbx-3.service",
	}
	for _, p := range commonProps {
		want = append(want, "-p", p)
	}
	want = append(want, "-p", "ReadWritePaths=/sbx/work", "-p", "PrivateNetwork=yes",
		"-p", "CPUQuota=200%", "-p", "MemoryMax=4294967296", "-p", "TasksMax=512", "-p", "RuntimeMaxSec=60", "--",
		"/usr/bin/env", "-i", "-C", "/sbx/work/pkg", "--",
		"PATH=/opt/go/bin:/usr/bin:/bin", "HOME=/sbx/home", "TMPDIR=/tmp", "USER=heimdall-sbx", "LOGNAME=heimdall-sbx",
		"GOCACHE=/tmp/gocache",
		"GOFLAGS=-mod=mod",
		"go", "test", "./...; curl evil",
	)
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
	assert.NotContains(t, got, "PrivateNetwork")
	assert.Contains(t, got, "-p RestrictAddressFamilies=AF_INET AF_INET6")
	assert.Contains(t, got, "-p ReadWritePaths=/sbx/work /sbx/home /sbx/tmp", "the fetch phase fills the module cache")
	assert.Contains(t, got, "TMPDIR=/sbx/tmp")
	assert.NotContains(t, got, "GOCACHE=", "the fetch phase keeps the default build cache under the writable home")
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
		"reserved GOCACHE":  {Argv: []string{"go"}, Env: []string{"GOCACHE=/sbx/home/poison"}, Timeout: time.Second},
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
	head, _ := unitHead(t, read)
	assert.Equal(t, wantHead(PhaseExec, "heimdall-sbx-4.service", "60", "/sbx/work"), head,
		"the result file is read as the sandbox user in the hardened service, not by root")
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

func TestStageOmitsRestrictSUIDSGIDWhileExecKeepsIt(t *testing.T) {
	f := &fakeExec{answer: readyAnswer}
	s := setupSandbox(t, f)
	require.NoError(t, s.Stage(t.Context(), strings.NewReader(""), 0))
	var stageCall []string
	for _, c := range f.calls {
		if len(c) > 3 && c[3] == "/usr/bin/systemd-run" && slices.Contains(c, "/usr/bin/tar") {
			stageCall = c
		}
	}
	require.NotNil(t, stageCall)
	assert.False(t, hasProp(stageCall, "RestrictSUIDSGID=yes"), "Stage omits RestrictSUIDSGID=yes so GNU tar openat2 works on Ubuntu 26.04")
	assert.True(t, hasProp(stageCall, "NoNewPrivileges=yes"))
	assert.True(t, hasProp(stageCall, "PrivateNetwork=yes"))
	assert.True(t, hasProp(stageCall, "ProtectSystem=strict"))
	assert.Contains(t, serviceProps(PhaseExec), "RestrictSUIDSGID=yes", "PR code execution keeps RestrictSUIDSGID=yes")
	assert.Contains(t, serviceProps(PhaseFetch), "RestrictSUIDSGID=yes", "PR fetch keeps RestrictSUIDSGID=yes")
}
