//go:build linux && sbxprobe

package uidsandbox

// The AM11 escape probe harness. It runs only on a throwaway GitHub-hosted runner, built with -tags sbxprobe:
//
//	GOOS=linux CGO_ENABLED=0 go test -c -tags sbxprobe -o probe.test ./internal/adapters/uidsandbox
//	HEIMDALL_PROBE_CANARY=<random> PROBE_TREE=<dir of testdata/probe> PROBE_GO_BIN=<go bin dir> \
//	  ./probe.test -test.v -test.run TestEscapeProbes
//
// It plants the canary in its own environment (it is the "loop" process), in a file in the runner home and in
// _temp, checks that the runner user itself can reach each of them (the controls), then runs the probe module of
// testdata/probe in the sandbox and requires every probe to be blocked.

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
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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
}

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
		ParentPID: os.Getpid(), CanarySHA256: hex.EncodeToString(sum[:]), RunnerHome: home,
		HomeCanary: filepath.Join(home, "heimdall-canary.txt"), TempCanary: filepath.Join(temp, "heimdall-canary.txt"),
		CredFiles: runnerFiles(t),
	}
	// World-readable on purpose: only the 0700 directories may stop the sandbox user.
	require.NoError(t, os.WriteFile(cfg.HomeCanary, []byte(canary+"\n"), 0o644))
	require.NoError(t, os.WriteFile(cfg.TempCanary, []byte(canary+"\n"), 0o644))
	controls(t, cfg, canary)

	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s, err := New(ctx, Config{
		RunnerHome: home, RunnerTemp: temp, Registries: []string{"proxy.golang.org", "sum.golang.org"},
		ToolPath: goBin + ":/usr/bin:/bin", Limits: Limits{CPUPercent: 200, MemoryMax: 4 << 30, TasksMax: 1024}, Logger: log,
	}, OSExec{})
	require.NoError(t, err, "New")
	require.NoError(t, s.Setup(ctx), "Setup")
	t.Cleanup(func() { require.NoError(t, s.Cleanup(ctx), "Cleanup") })
	t.Logf("SETUP uid=%d proxy port=%d", s.UID(), s.ProxyPort())
	hostChecks(t, s, cfg)

	cfg.ProxyPort = s.ProxyPort()
	require.NoError(t, s.Stage(ctx, tarTree(t, tree, cfg), 0), "Stage")

	goEnv := []string{"GOPATH=" + HomeDir + "/go", "GOCACHE=" + HomeDir + "/.cache/go-build", "GOTOOLCHAIN=local",
		"GOTELEMETRY=off", "CGO_ENABLED=0"}
	fetchEnv := append([]string{"GOPROXY=https://proxy.golang.org", "GOSUMDB=sum.golang.org", "GOFLAGS=-mod=mod"}, goEnv...)
	execEnv := append([]string{"GOPROXY=off", "GOFLAGS=-mod=readonly"}, goEnv...)

	res, err := s.Fetch(ctx, Job{Argv: []string{"go", "mod", "download", "-x"}, Env: fetchEnv, Timeout: 3 * time.Minute})
	require.NoError(t, err, "fetch go mod download")
	t.Logf("FETCH go mod download: exit %d\n%s", res.Exit, tail(res.Stderr, 4000))
	require.Equal(t, 0, res.Exit, "the allowed fetch must work")

	res, err = s.Fetch(ctx, Job{Argv: []string{"go", "test", "-json", "-count=1", "-v", "-run", "^TestFetch", "."},
		Env: fetchEnv, Timeout: 5 * time.Minute, Result: ResultGoTestJSON})
	require.NoError(t, err, "fetch probes")
	judge(t, "fetch", res)

	res, err = s.Exec(ctx, Job{Argv: []string{"go", "test", "-json", "-count=1", "-v", "-run", "^TestExec", "."},
		Env: execEnv, Timeout: 5 * time.Minute, Result: ResultGoTestJSON})
	require.NoError(t, err, "exec probes (includes the kill of every sandbox process)")
	judge(t, "exec", res)
	survivor(t, s)
}

// controls proves each probe target is reachable by the runner user, so a blocked probe means the sandbox blocked it.
func controls(t *testing.T, cfg probeConfig, canary string) {
	env, err := os.ReadFile("/proc/self/environ")
	require.NoError(t, err)
	require.Contains(t, string(env), "HEIMDALL_PROBE_CANARY="+canary, "control: canary in the loop's environ")
	for _, p := range []string{cfg.HomeCanary, cfg.TempCanary} {
		b, err := os.ReadFile(p)
		require.NoError(t, err)
		require.Equal(t, canary+"\n", string(b), "control: runner reads %s", p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, "github.com")
	require.NoError(t, err, "control: runner DNS")
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", "1.1.1.1:443")
	require.NoError(t, err, "control: runner TCP to 1.1.1.1:443")
	_ = c.Close()
	u, err := d.DialContext(ctx, "unix", "/var/run/docker.sock")
	require.NoError(t, err, "control: runner reaches the docker socket")
	_ = u.Close()
	t.Logf("CONTROL runner user: environ has canary, home and _temp canaries readable, DNS github.com=%v, TCP 1.1.1.1:443 ok, docker.sock ok", addrs)
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

// judge logs every probe line and requires every probe test to pass, which means every escape was blocked.
func judge(t *testing.T, phase string, res Result) {
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		var ev struct{ Action, Test, Output string }
		if json.Unmarshal([]byte(line), &ev) == nil && ev.Action == "output" && strings.Contains(ev.Output, "PROBE") {
			t.Logf("%s %s", strings.ToUpper(phase), strings.TrimSpace(ev.Output))
		}
	}
	cases := res.Report.Cases()
	require.NotEmpty(t, cases, "%s: no probe ran; stderr:\n%s", phase, tail(res.Stderr, 4000))
	for _, c := range cases {
		if c.Status != verify.StatusPass {
			t.Errorf("RESULT %s %s: %s: %s %s", phase, c.Name, c.Status, c.Message, c.Text)
			continue
		}
		t.Logf("RESULT %s %s: passed (escape blocked)", phase, c.Name)
	}
	t.Logf("RESULT %s: exit %d, %d probe tests, timed out %v", phase, res.Exit, len(cases), res.TimedOut)
}

func survivor(t *testing.T, s *Sandbox) {
	b, err := s.readAsUser(context.Background(), "survivor.pid")
	require.NoError(t, err, "read survivor.pid")
	pid := strings.TrimSpace(string(b))
	_, err = os.Stat("/proc/" + pid)
	if err == nil {
		t.Errorf("PROBE survivor: ESCAPED: pid %s still alive after the kill", pid)
	} else {
		t.Logf("PROBE survivor: blocked: pid %s gone after the kill: %v", pid, err)
	}
	code, out, err := s.root(context.Background(), nil, s.tools.pgrep, "-a", "-u", strconv.Itoa(s.UID()))
	require.NoError(t, err)
	if code != 1 {
		t.Errorf("PROBE survivor: ESCAPED: processes of the sandbox uid remain: %s", out)
		return
	}
	t.Logf("PROBE survivor: blocked: pgrep -u %d finds no process after the job", s.UID())
}

func tail(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
}
