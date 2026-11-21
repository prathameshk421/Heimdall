//go:build linux && sbxprobe

package uidsandbox

// The AM11 escape probe harness. It runs only on a throwaway GitHub-hosted runner, built with -tags sbxprobe:
//
//	GOOS=linux CGO_ENABLED=0 go test -c -tags sbxprobe -o probe.test ./internal/adapters/uidsandbox
//	HEIMDALL_PROBE_CANARY=<random> PROBE_TREE=<dir of testdata/probe> PROBE_GO_BIN=<go bin dir> \
//	  ./probe.test -test.v -test.run TestEscapeProbes -test.timeout 25m <the same canary>
//
// It is the "loop" process: it holds the canary in its environment and its argv, and plants it in files (the runner
// home, _temp, /tmp, /var/tmp, /dev/shm), in keys of its session and user keyrings and in a host message queue. It
// proves the runner user reaches each target (the controls), then runs the probe module of testdata/probe in both
// phases. Every probe must be blocked. A probe that could not run is UNKNOWN, and UNKNOWN fails the harness: it is
// not a proof.

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
)

type probeConfig struct {
	ParentPID    int      `json:"parent_pid"`
	HomeCanary   string   `json:"home_canary"`
	TempCanary   string   `json:"temp_canary"`
	CanarySHA256 string   `json:"canary_sha256"`
	ProxyPort    int      `json:"proxy_port"`
	CredFiles    []string `json:"cred_files"`
	RunnerHome   string   `json:"runner_home"`
	HostSockets  []string `json:"host_sockets"`
	WritableDirs []string `json:"writable_dirs"`
	HostPlants   []string `json:"host_plants"`
	CmdlinePID   int      `json:"cmdline_pid"`
	KeySerials   []int    `json:"key_serials"`
	KeyDesc      string   `json:"key_desc"`
	HostQueue    string   `json:"host_queue"`
	LoopbackPort int      `json:"loopback_port"`
}

const (
	keyDesc      = "heimdall-canary"
	hostQueue    = "/dev/mqueue/heimdall-host-canary"
	journalTag   = "heimdall-probe-"
	sandboxMark  = "marker from the sandbox"
	controlMark  = "marker from the runner user"
	hangTimeout  = 45 * time.Second
	hangSlack    = 30 * time.Second
	keySpecSess  = -3
	keySpecUser  = -4
	keyctlRead   = 11
	keyctlRevoke = 3
	keyctlLink   = 8
	keyctlUnlink = 9
)

func TestEscapeProbes(t *testing.T) {
	canary := os.Getenv("HEIMDALL_PROBE_CANARY")
	require.GreaterOrEqual(t, len(canary), 16, "HEIMDALL_PROBE_CANARY must hold a random value")
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	temp := os.Getenv("RUNNER_TEMP")
	require.NotEmpty(t, temp)
	tree, goBin := os.Getenv("PROBE_TREE"), os.Getenv("PROBE_GO_BIN")
	require.NotEmpty(t, tree)
	require.NotEmpty(t, goBin)

	sum := sha256.Sum256([]byte(canary))
	cfg := probeConfig{
		ParentPID: os.Getpid(), CmdlinePID: os.Getpid(), CanarySHA256: hex.EncodeToString(sum[:]), RunnerHome: home,
		HomeCanary: filepath.Join(home, "heimdall-canary.txt"), TempCanary: filepath.Join(temp, "heimdall-canary.txt"),
		CredFiles: runnerFiles(t), KeyDesc: keyDesc, HostQueue: hostQueue,
		HostPlants: []string{"/tmp/heimdall-host-canary.txt", "/var/tmp/heimdall-host-canary.txt", "/dev/shm/heimdall-host-canary.txt"},
	}
	// World-readable on purpose: only the sandbox may stop the sandbox user.
	for _, p := range append([]string{cfg.HomeCanary, cfg.TempCanary}, cfg.HostPlants...) {
		require.NoError(t, os.WriteFile(p, []byte(canary+"\n"), 0o644))
		t.Cleanup(func() { _ = os.Remove(p) })
	}
	q, err := os.OpenFile(hostQueue, os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, err, "control: make a host message queue")
	_ = q.Close()
	t.Cleanup(func() { _ = os.Remove(hostQueue) })
	cfg.KeySerials = keyControls(t, canary)
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go acceptAll(ln)
	cfg.LoopbackPort = ln.Addr().(*net.TCPAddr).Port

	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s, err := New(ctx, Config{
		RunnerHome: home, RunnerTemp: temp, Registries: []string{"proxy.golang.org", "sum.golang.org"},
		ToolPath: goBin + ":/usr/bin:/bin", Limits: Limits{CPUPercent: 200, MemoryMax: 4 << 30, TasksMax: 1024}, Logger: log,
	}, OSExec{})
	require.NoError(t, err, "New")
	cfg.HostSockets = hostSocketList(t, s)
	cfg.WritableDirs = writableDirs(t, s)
	controls(t, cfg, canary)
	socketControls(t, cfg)
	require.NoError(t, s.Setup(ctx), "Setup, including the self check of each phase")
	t.Cleanup(func() { require.NoError(t, s.Cleanup(ctx), "Cleanup") })
	t.Logf("SETUP uid=%d proxy port=%d; the self check passed in both phases", s.UID(), s.ProxyPort())
	hostChecks(t, s, cfg)

	cfg.ProxyPort = s.ProxyPort()
	require.NoError(t, s.Stage(ctx, tarTree(t, tree, cfg), 0), "Stage")

	goEnv := []string{"GOPATH=" + HomeDir + "/go", "GOCACHE=" + HomeDir + "/.cache/go-build", "GOTOOLCHAIN=local",
		"GOTELEMETRY=off", "CGO_ENABLED=0"}
	fetchEnv := append([]string{"GOPROXY=https://proxy.golang.org", "GOSUMDB=sum.golang.org", "GOFLAGS=-mod=mod",
		"HEIMDALL_PROBE_PHASE=fetch"}, goEnv...)
	execEnv := append([]string{"GOPROXY=off", "GOFLAGS=-mod=readonly", "HEIMDALL_PROBE_PHASE=exec"}, goEnv...)

	res, err := s.Fetch(ctx, Job{Argv: []string{"go", "mod", "download", "-x"}, Env: fetchEnv, Timeout: 3 * time.Minute})
	require.NoError(t, err, "fetch go mod download")
	t.Logf("FETCH go mod download: exit %d\n%s", res.Exit, tail(res.Stderr, 4000))
	require.Equal(t, 0, res.Exit, "the allowed fetch must work")

	res, err = s.Fetch(ctx, Job{Argv: []string{"go", "test", "-json", "-count=1", "-v", "-run", "^(TestFetch|TestBoth)", "."},
		Env: fetchEnv, Timeout: 5 * time.Minute, Result: ResultGoTestJSON})
	require.NoError(t, err, "fetch probes (includes the kill of every sandbox process)")
	judge(t, "fetch", res)
	survivor(t, s, "survivor-fetch.pid")

	res, err = s.Exec(ctx, Job{Argv: []string{"go", "test", "-json", "-count=1", "-v", "-run", "^(TestExec|TestBoth)", "."},
		Env: execEnv, Timeout: 5 * time.Minute, Result: ResultGoTestJSON})
	require.NoError(t, err, "exec probes (includes the kill of every sandbox process)")
	judge(t, "exec", res)
	survivor(t, s, "survivor-exec.pid")

	hang(t, s, execEnv)
	plants(t, s, cfg)
	journal(t, s)
}

func acceptAll(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_ = c.Close()
	}
}

// keyControls adds a key with the canary as payload to the runner user's session and user keyrings and reads each
// back, so a blocked read inside is the sandbox. A key in the user keyring is readable only by a possessor, so the
// user keyring is linked into the session keyring first (run 44909825507 failed this control without the link). The
// keys are revoked and the link removed at the end.
func keyControls(t *testing.T, canary string) []int {
	user, sess := keySpecUser, keySpecSess
	_, _, e := syscall.Syscall(syscall.SYS_KEYCTL, keyctlLink, uintptr(user), uintptr(sess))
	require.Zero(t, e, "control: link the user keyring into the session keyring: %v", e)
	t.Cleanup(func() { _, _, _ = syscall.Syscall(syscall.SYS_KEYCTL, keyctlUnlink, uintptr(user), uintptr(sess)) })
	typ, _ := syscall.BytePtrFromString("user")
	desc, _ := syscall.BytePtrFromString(keyDesc)
	payload := []byte(canary)
	var serials []int
	for name, ring := range map[string]int{"session": keySpecSess, "user": keySpecUser} {
		r, _, e := syscall.Syscall6(syscall.SYS_ADD_KEY, uintptr(unsafe.Pointer(typ)), uintptr(unsafe.Pointer(desc)),
			uintptr(unsafe.Pointer(&payload[0])), uintptr(len(payload)), uintptr(ring), 0)
		require.Zero(t, e, "control: add_key to the %s keyring: %v", name, e)
		id := int(int32(r))
		buf := make([]byte, 256)
		n, _, e := syscall.Syscall6(syscall.SYS_KEYCTL, keyctlRead, uintptr(id), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0)
		require.Zero(t, e, "control: read key %d back: %v", id, e)
		require.Equal(t, canary, string(buf[:n]), "control: key %d holds the canary", id)
		t.Logf("CONTROL runner user keyring %s: key %d %q holds the canary and reads back", name, id, keyDesc)
		serials = append(serials, id)
		t.Cleanup(func() { _, _, _ = syscall.Syscall(syscall.SYS_KEYCTL, keyctlRevoke, uintptr(id), 0) })
	}
	return serials
}

// hostSocketList finds every Unix socket of the host as root: every socket file outside /proc and /sys, and every
// abstract name and bound path in the host's /proc/net/unix (the harness runs in the host network namespace).
func hostSocketList(t *testing.T, s *Sandbox) []string {
	code, out, err := s.root(context.Background(), nil, "/usr/bin/find", "/", "(", "-path", "/proc", "-o", "-path", "/sys",
		")", "-prune", "-o", "-type", "s", "-print")
	require.NoError(t, err)
	t.Logf("SETUP find / -type s as root: exit %d", code)
	seen := map[string]bool{}
	var list []string
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			list = append(list, n)
		}
	}
	files := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "/") {
			files++
			add(l)
		}
	}
	b, err := os.ReadFile("/proc/net/unix")
	require.NoError(t, err)
	abstract := 0
	for _, line := range strings.Split(string(b), "\n")[1:] {
		if f := strings.Fields(line); len(f) >= 8 {
			if strings.HasPrefix(f[7], "@") {
				abstract++
			}
			add(f[7])
		}
	}
	t.Logf("SETUP host Unix sockets: %d socket files from find, %d abstract names, %d distinct in all: %v", files, abstract, len(list), list)
	require.NotEmpty(t, list)
	return list
}

// writableDirs finds every world-writable directory of the host as root, outside /proc, /sys and /sbx. find prunes
// each one it prints, so the list holds the top of each world-writable tree.
func writableDirs(t *testing.T, s *Sandbox) []string {
	code, out, err := s.root(context.Background(), nil, "/usr/bin/find", "/", "(", "-path", "/proc", "-o", "-path", "/sys",
		"-o", "-path", Root, ")", "-prune", "-o", "-type", "d", "-perm", "-0002", "-print", "-prune")
	require.NoError(t, err)
	var dirs []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "/") {
			dirs = append(dirs, l)
		}
	}
	t.Logf("SETUP find / -type d -perm -0002 -prune as root: exit %d; %d world-writable directories: %v", code, len(dirs), dirs)
	require.Contains(t, dirs, "/tmp")
	return dirs
}

// socketControls proves the runner user reaches the host's Unix sockets, so a blocked connect inside is the sandbox.
func socketControls(t *testing.T, cfg probeConfig) {
	var reached []string
	for _, n := range cfg.HostSockets {
		addr := n
		if strings.HasPrefix(n, "@") {
			addr = "\x00" + n[1:]
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var d net.Dialer
		c, err := d.DialContext(ctx, "unix", addr)
		cancel()
		if err == nil {
			_ = c.Close()
			reached = append(reached, n)
		}
	}
	t.Logf("CONTROL runner user reaches %d of %d host Unix sockets: %v", len(reached), len(cfg.HostSockets), reached)
	for _, must := range []string{"/run/systemd/resolve/io.systemd.Resolve", "/run/dbus/system_bus_socket"} {
		require.Contains(t, reached, must, "control: the runner user reaches %s", must)
	}
	for _, argv := range [][]string{
		{"/usr/bin/resolvectl", "query", "--legend=no", "example.com"},
		{"/usr/bin/busctl", "--system", "--no-pager", "list"},
		{"/usr/bin/chage", "-l", os.Getenv("USER")},
		{"/usr/bin/logger", "-t", journalTag + "control", controlMark},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
		cancel()
		require.NoError(t, err, "control: %v: %s", argv, out)
		t.Logf("CONTROL runner user %v: works, %d bytes, first line %q", argv, len(out), strings.SplitN(string(out), "\n", 2)[0])
	}
}

// controls proves each probe target is reachable by the runner user, so a blocked probe means the sandbox blocked it.
func controls(t *testing.T, cfg probeConfig, canary string) {
	env, err := os.ReadFile("/proc/self/environ")
	require.NoError(t, err)
	require.Contains(t, string(env), "HEIMDALL_PROBE_CANARY="+canary, "control: canary in the loop's environ")
	cmdline, err := os.ReadFile("/proc/self/cmdline")
	require.NoError(t, err)
	require.Contains(t, strings.Split(string(cmdline), "\x00"), canary, "control: canary in the loop's argv (pass it as the last argument)")
	for _, p := range append([]string{cfg.HomeCanary, cfg.TempCanary}, cfg.HostPlants...) {
		b, err := os.ReadFile(p)
		require.NoError(t, err)
		require.Equal(t, canary+"\n", string(b), "control: runner reads %s", p)
	}
	_, err = os.Lstat(cfg.HostQueue)
	require.NoError(t, err, "control: the host message queue exists")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, "github.com")
	require.NoError(t, err, "control: runner DNS")
	var d net.Dialer
	for _, addr := range []string{"1.1.1.1:443", "127.0.0.1:" + strconv.Itoa(cfg.LoopbackPort)} {
		c, err := d.DialContext(ctx, "tcp", addr)
		require.NoError(t, err, "control: runner TCP to %s", addr)
		_ = c.Close()
	}
	u, err := d.DialContext(ctx, "unix", "/var/run/docker.sock")
	require.NoError(t, err, "control: runner reaches the docker socket")
	_ = u.Close()
	t.Logf("CONTROL runner user: environ and argv have the canary; home, _temp, /tmp, /var/tmp, /dev/shm canaries readable; "+
		"host queue %s exists; DNS github.com=%v; TCP 1.1.1.1:443 and loopback :%d ok; docker.sock ok",
		cfg.HostQueue, addrs, cfg.LoopbackPort)
}

// runnerFiles finds the runner's own files (.credentials, .runner) by walking up to the Runner.Worker process.
func runnerFiles(t *testing.T) []string {
	var found []string
	pid := os.Getppid()
	for range 10 {
		exe, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
		if err == nil && strings.HasPrefix(filepath.Base(exe), "Runner.") {
			root := filepath.Dir(filepath.Dir(exe))
			for _, n := range []string{".credentials", ".credentials_rsaparams", ".runner", ".env", ".path"} {
				p := filepath.Join(root, n)
				if _, err := os.Stat(p); err == nil {
					found = append(found, p)
				}
			}
			found = append(found, filepath.Join(root, "_diag"))
			t.Logf("SETUP runner root %s (from %s), files %v", root, exe, found)
			return found
		}
		b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			break
		}
		f := strings.Fields(string(b[bytes.LastIndexByte(b, ')')+1:]))
		if len(f) < 2 {
			break
		}
		if pid, err = strconv.Atoi(f[1]); err != nil || pid <= 1 {
			break
		}
	}
	t.Log("SETUP runner root not found")
	return found
}

func hostChecks(t *testing.T, s *Sandbox, cfg probeConfig) {
	ctx := context.Background()
	for _, d := range []string{cfg.RunnerHome, filepath.Dir(cfg.TempCanary)} {
		fi, err := os.Stat(d)
		require.NoError(t, err)
		require.Equal(t, fs.FileMode(0o700), fi.Mode().Perm(), "%s mode", d)
	}
	_, out, err := s.root(ctx, nil, s.tools.sudo, "-l", "-U", User)
	require.NoError(t, err)
	require.Contains(t, out, "not allowed", "the sandbox user has no sudo rights")
	_, groups, err := s.root(ctx, nil, s.tools.id, User)
	require.NoError(t, err)
	t.Logf("HOST runner home and _temp are 0700; sudo -l -U %s: %q; id: %s", User, out, groups)
}

func tarTree(t *testing.T, dir string, cfg probeConfig) *bytes.Buffer {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return writeTarFile(tw, rel, b)
	})
	require.NoError(t, err)
	c, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, writeTarFile(tw, "config.json", c))
	require.NoError(t, tw.Close())
	return &buf
}

func writeTarFile(tw *tar.Writer, name string, b []byte) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := tw.Write(b)
	return err
}

// judge logs every probe line and requires every probe test to pass. A skipped probe is UNKNOWN: it could not make
// its attempt, so it proves nothing, and it fails the harness like an escape does.
func judge(t *testing.T, phase string, res Result) {
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		var ev struct{ Action, Test, Output string }
		if json.Unmarshal([]byte(line), &ev) == nil && ev.Action == "output" &&
			(strings.Contains(ev.Output, "PROBE") || strings.Contains(ev.Output, "INFO") || strings.HasPrefix(ev.Output, "        ")) {
			t.Logf("%s %s", strings.ToUpper(phase), strings.TrimRight(ev.Output, "\n"))
		}
	}
	cases := res.Report.Cases()
	require.NotEmpty(t, cases, "%s: no probe ran; stderr:\n%s", phase, tail(res.Stderr, 4000))
	unknown := 0
	for _, c := range cases {
		switch c.Status {
		case verify.StatusPass:
			t.Logf("RESULT %s %s: passed (escape blocked)", phase, c.Name)
		case verify.StatusSkipped:
			unknown++
			t.Errorf("RESULT %s %s: UNKNOWN (the probe could not run, so this is not proven): %s %s", phase, c.Name, c.Message, c.Text)
		case verify.StatusFail, verify.StatusError:
			t.Errorf("RESULT %s %s: %s: %s %s", phase, c.Name, c.Status, c.Message, c.Text)
		}
	}
	t.Logf("RESULT %s: exit %d, %d probe tests, %d unknown, timed out %v", phase, res.Exit, len(cases), unknown, res.TimedOut)
}

// survivor checks that the daemons a probe job started are gone after the job.
func survivor(t *testing.T, s *Sandbox, pidFile string) {
	b, err := s.readAsUser(context.Background(), pidFile)
	if err != nil {
		t.Errorf("PROBE survivor %s: UNKNOWN: no pid file: %v", pidFile, err)
		return
	}
	pid := strings.TrimSpace(string(b))
	if _, err := os.Stat("/proc/" + pid); err == nil {
		t.Errorf("PROBE survivor %s: ESCAPED: pid %s still alive after the job", pidFile, pid)
	} else {
		t.Logf("PROBE survivor %s: blocked: pid %s gone after the job: %v", pidFile, pid, err)
	}
	noProcess(t, s, "survivor "+pidFile)
}

func noProcess(t *testing.T, s *Sandbox, what string) {
	code, out, err := s.root(context.Background(), nil, s.tools.pgrep, "-a", "-u", strconv.Itoa(s.UID()))
	require.NoError(t, err)
	if code != 1 {
		t.Errorf("PROBE %s: ESCAPED: processes of the sandbox uid remain: %s", what, out)
		return
	}
	t.Logf("PROBE %s: blocked: pgrep -u %d finds no process", what, s.UID())
}

// hang runs a job that never ends, with a short timeout. The job must end near its timeout, report TimedOut, and
// leave no process, including the daemon it started in a new session.
func hang(t *testing.T, s *Sandbox, execEnv []string) {
	start := time.Now()
	res, err := s.Exec(context.Background(), Job{Argv: []string{"go", "test", "-count=1", "-v", "-run", "^TestHang$", "."},
		Env: execEnv, Timeout: hangTimeout})
	took := time.Since(start)
	t.Logf("TIMEOUT hang job: exit %d, timed out %v, took %s, err %v, stdout tail %q", res.Exit, res.TimedOut, took.Round(time.Second),
		err, tail(res.Stdout, 300))
	if err != nil {
		t.Errorf("PROBE timeout: the hang job returned an error: %v", err)
	}
	if !res.TimedOut || took > hangTimeout+hangSlack {
		t.Errorf("PROBE timeout: ESCAPED: the job ran %s past a %s timeout (timed out %v)", took, hangTimeout, res.TimedOut)
	} else {
		t.Logf("PROBE timeout: blocked: the job ended after %s for a %s timeout", took.Round(time.Second), hangTimeout)
	}
	if !strings.Contains(string(res.Stdout), "PROBE hang: daemon started") {
		t.Errorf("PROBE timeout: UNKNOWN: the hang test did not start its daemon before the timeout")
	}
	survivor(t, s, "hang.pid")
}

// plants checks that no file a phase wrote into a world-writable directory is on the host.
func plants(t *testing.T, s *Sandbox, cfg probeConfig) {
	argv := append([]string{"/usr/bin/find"}, cfg.WritableDirs...)
	argv = append(argv, "-maxdepth", "1", "-name", "heimdall-sbx-plant-*", "-print")
	_, out, err := s.root(context.Background(), nil, argv...)
	require.NoError(t, err)
	var found []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "heimdall-sbx-plant-") {
			found = append(found, l)
		}
	}
	if len(found) > 0 {
		t.Errorf("PROBE plants: ESCAPED: files written inside are on the host: %v", found)
		return
	}
	t.Logf("PROBE plants: blocked: none of the files written inside is in any of the host's %d world-writable directories", len(cfg.WritableDirs))
}

// journal checks the host journal: the runner's control marker must be there and no marker from the sandbox.
func journal(t *testing.T, s *Sandbox) {
	_, out, err := s.root(context.Background(), nil, "/usr/bin/journalctl", "--no-pager", "-o", "cat",
		"-t", journalTag+"control", "-t", journalTag+"fetch", "-t", journalTag+"exec")
	require.NoError(t, err)
	if !strings.Contains(out, controlMark) {
		t.Errorf("PROBE journal: UNKNOWN: the control marker is not in the host journal: %q", tail([]byte(out), 300))
		return
	}
	if strings.Contains(out, sandboxMark) {
		t.Errorf("PROBE journal: ESCAPED: the sandbox wrote into the host journal: %q", tail([]byte(out), 300))
		return
	}
	lines := slices.DeleteFunc(strings.Split(out, "\n"), func(l string) bool { return l == "" || l == controlMark })
	t.Logf("PROBE journal: blocked: the host journal has the control marker and no sandbox marker (%q)", strings.Join(lines, " | "))
}

func tail(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
}
