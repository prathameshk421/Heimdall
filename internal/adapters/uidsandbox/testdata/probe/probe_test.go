// Package probe holds the escape probes of AM11. They run inside the sandbox as the heimdall-sbx user. Each probe
// tries one escape. A probe passes when the escape is blocked and fails with "ESCAPED" when it works. The harness
// (probe_linux_test.go in the parent package) plants the canaries and reads the results.
package probe

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"rsc.io/quote"
)

const canaryKey = "HEIMDALL_PROBE_CANARY"

type config struct {
	ParentPID    int      `json:"parent_pid"`
	HomeCanary   string   `json:"home_canary"`
	TempCanary   string   `json:"temp_canary"`
	CanarySHA256 string   `json:"canary_sha256"`
	ProxyPort    int      `json:"proxy_port"`
	CredFiles    []string `json:"cred_files"`
	RunnerHome   string   `json:"runner_home"`
}

func load(t *testing.T) config {
	t.Helper()
	b, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatalf("probe config: %v", err)
	}
	var c config
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("probe config: %v", err)
	}
	return c
}

// blocked records one escape attempt: err == nil means the escape worked.
func blocked(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("PROBE %s: ESCAPED", what)
		return
	}
	t.Logf("PROBE %s: blocked: %v", what, err)
}

func isCanary(c config, value string) bool {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:]) == c.CanarySHA256
}

func timeoutCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// ---- execution phase ----

func TestExecParentEnviron(t *testing.T) {
	c := load(t)
	_, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", c.ParentPID))
	blocked(t, "read the parent's /proc/<pid>/environ", err)
}

func TestExecParentMem(t *testing.T) {
	c := load(t)
	f, err := os.Open(fmt.Sprintf("/proc/%d/mem", c.ParentPID))
	if err == nil {
		buf := make([]byte, 16)
		_, err = f.ReadAt(buf, 0x400000)
		_ = f.Close()
		if err != nil && !errors.Is(err, syscall.EIO) {
			// Open worked; a read error other than EIO still means no access to memory.
			blocked(t, "read the parent's /proc/<pid>/mem", err)
			return
		}
	}
	blocked(t, "open the parent's /proc/<pid>/mem", err)
}

// TestExecEnvironScan reads every /proc/<pid>/environ it can and looks for the canary in any of them.
func TestExecEnvironScan(t *testing.T) {
	c := load(t)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatalf("read /proc: %v", err)
	}
	readable, denied := 0, 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "environ"))
		if err != nil {
			denied++
			continue
		}
		readable++
		for _, kv := range strings.Split(string(b), "\x00") {
			k, v, _ := strings.Cut(kv, "=")
			if k == canaryKey || isCanary(c, v) {
				t.Errorf("PROBE environ scan: ESCAPED: canary in /proc/%d/environ", pid)
			}
		}
	}
	t.Logf("PROBE environ scan of every process: blocked: canary in none; %d readable (own uid), %d denied", readable, denied)
}

func TestExecOwnEnvironment(t *testing.T) {
	c := load(t)
	allowed := map[string]bool{}
	for _, k := range []string{"PATH", "HOME", "TMPDIR", "USER", "LOGNAME", "GOFLAGS", "GOPROXY", "GOSUMDB",
		"GOTOOLCHAIN", "GOPATH", "GOCACHE", "GOMODCACHE", "GOTELEMETRY", "CGO_ENABLED", "PWD"} {
		allowed[k] = true
	}
	var extra []string
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if k == canaryKey || isCanary(c, v) {
			t.Errorf("PROBE own environment: ESCAPED: canary in the sandbox environment")
		}
		if !allowed[k] {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		t.Errorf("PROBE own environment: ESCAPED: unexpected variables %v", extra)
		return
	}
	t.Logf("PROBE own environment: blocked: only fixed keys, %d variables", len(os.Environ()))
}

func readCanary(c config, p string) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if isCanary(c, strings.TrimSpace(string(b))) {
		return nil
	}
	return fmt.Errorf("read %s but it held no canary", p)
}

func TestExecHomeCanary(t *testing.T) {
	c := load(t)
	blocked(t, "read the runner home canary "+c.HomeCanary, readCanary(c, c.HomeCanary))
	_, err := os.ReadDir(c.RunnerHome)
	blocked(t, "list the runner home", err)
}

func TestExecTempCanary(t *testing.T) {
	c := load(t)
	blocked(t, "read the _temp canary "+c.TempCanary, readCanary(c, c.TempCanary))
}

func TestExecRunnerCredentials(t *testing.T) {
	c := load(t)
	if len(c.CredFiles) == 0 {
		t.Log("PROBE runner credential files: none found by the harness")
	}
	for _, p := range c.CredFiles {
		_, err := os.ReadFile(p)
		blocked(t, "read runner file "+p, err)
	}
}

func TestExecDNS(t *testing.T) {
	_, err := net.DefaultResolver.LookupHost(timeoutCtx(t), "github.com")
	blocked(t, "DNS lookup of github.com (system resolver)", err)
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, "8.8.8.8:53")
	}}
	_, err = r.LookupHost(timeoutCtx(t), "github.com")
	blocked(t, "DNS lookup of github.com through 8.8.8.8", err)
}

func TestExecTCP(t *testing.T) {
	for _, addr := range []string{"1.1.1.1:443", "140.82.112.3:443", "[2606:4700:4700::1111]:443", "169.254.169.254:80"} {
		var d net.Dialer
		conn, err := d.DialContext(timeoutCtx(t), "tcp", addr)
		if conn != nil {
			_ = conn.Close()
		}
		blocked(t, "outbound TCP connect to "+addr, err)
	}
}

func TestExecInterfaces(t *testing.T) {
	ifs, err := net.Interfaces()
	if err != nil {
		t.Fatalf("interfaces: %v", err)
	}
	var names []string
	for _, i := range ifs {
		names = append(names, i.Name)
	}
	if len(names) != 1 || names[0] != "lo" {
		t.Errorf("PROBE network interfaces: ESCAPED: %v", names)
		return
	}
	t.Logf("PROBE network interfaces: blocked: only %v", names)
}

// TestExecProxy tries the fetch proxy, and a disallowed host through it, from the execution phase.
func TestExecProxy(t *testing.T) {
	c := load(t)
	err := connectThrough(t, "127.0.0.1:"+strconv.Itoa(c.ProxyPort), "example.com:443")
	blocked(t, "CONNECT example.com:443 through the fetch proxy from the execution phase", err)
	err = connectThrough(t, "127.0.0.1:"+strconv.Itoa(c.ProxyPort), "proxy.golang.org:443")
	blocked(t, "CONNECT proxy.golang.org:443 through the fetch proxy from the execution phase", err)
}

func TestExecDockerSocket(t *testing.T) {
	for _, p := range []string{"/var/run/docker.sock", "/run/containerd/containerd.sock"} {
		var d net.Dialer
		conn, err := d.DialContext(timeoutCtx(t), "unix", p)
		if conn != nil {
			_ = conn.Close()
		}
		blocked(t, "connect to "+p, err)
	}
}

func TestExecSudo(t *testing.T) {
	out, err := exec.Command("/usr/bin/sudo", "-n", "true").CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	blocked(t, "escalate with sudo -n true", err)
	out, err = exec.Command("/usr/bin/su", "-c", "true", "root").CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	blocked(t, "escalate with su root", err)
}

func TestExecNoNewPrivs(t *testing.T) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	defer f.Close()
	fields := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), ":")
		fields[k] = strings.TrimSpace(v)
	}
	if fields["NoNewPrivs"] != "1" || fields["CapEff"] != "0000000000000000" || fields["CapBnd"] != "0000000000000000" {
		t.Errorf("PROBE privileges: ESCAPED: NoNewPrivs=%s CapEff=%s CapBnd=%s", fields["NoNewPrivs"], fields["CapEff"], fields["CapBnd"])
		return
	}
	t.Logf("PROBE privileges: blocked: NoNewPrivs=1 CapEff=0 CapBnd=0 Uid=%s Groups=%q", fields["Uid"], fields["Groups"])
}

func TestExecSchedulers(t *testing.T) {
	if _, err := os.Stat("/usr/bin/crontab"); err == nil {
		cmd := exec.Command("/usr/bin/crontab", "-")
		cmd.Stdin = strings.NewReader("* * * * * /usr/bin/sleep 1\n")
		out, err := cmd.CombinedOutput()
		if err != nil {
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		blocked(t, "install a crontab", err)
	} else {
		t.Log("PROBE install a crontab: not applicable, no crontab binary")
	}
	if _, err := os.Stat("/usr/bin/at"); err == nil {
		cmd := exec.Command("/usr/bin/at", "now", "+", "1", "minute")
		cmd.Stdin = strings.NewReader("/usr/bin/sleep 1\n")
		out, err := cmd.CombinedOutput()
		if err != nil {
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		blocked(t, "queue an at job", err)
	} else {
		t.Log("PROBE queue an at job: not applicable, no at binary")
	}
	out, err := exec.Command("/usr/bin/systemd-run", "--user", "--scope", "/usr/bin/sleep", "1").CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	blocked(t, "start a user systemd unit", err)
}

// resolveThroughHost asks the host's resolver over its Unix sockets, which a network namespace does not cut.
func resolveThroughHost(t *testing.T, phase string) {
	if _, err := os.Stat("/usr/bin/resolvectl"); err == nil {
		out, err := exec.Command("/usr/bin/resolvectl", "query", "--legend=no", "example.com").CombinedOutput()
		if err != nil {
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		} else {
			t.Logf("resolvectl said: %s", strings.TrimSpace(string(out)))
		}
		blocked(t, phase+" DNS through systemd-resolved (resolvectl query example.com)", err)
	}
	out, err := exec.Command("/usr/bin/getent", "ahosts", "example.com").CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	} else {
		t.Logf("getent said: %s", strings.TrimSpace(string(out)))
	}
	blocked(t, phase+" DNS through NSS (getent ahosts example.com)", err)
}

// unixSockets reports which host Unix sockets the user can connect to. Reaching one is not an escape by itself;
// the list shows what a namespace does not cut.
func unixSockets(t *testing.T, phase string) {
	b, err := os.ReadFile("/proc/net/unix")
	if err != nil {
		t.Fatalf("read /proc/net/unix: %v", err)
	}
	seen := map[string]bool{}
	var reached []string
	for _, line := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 8 || seen[f[7]] {
			continue
		}
		name := f[7]
		seen[name] = true
		addr := name
		if strings.HasPrefix(name, "@") {
			addr = "\x00" + name[1:]
		}
		var d net.Dialer
		conn, err := d.DialContext(timeoutCtx(t), "unix", addr)
		if err == nil {
			_ = conn.Close()
			reached = append(reached, name)
		}
	}
	t.Logf("PROBE %s host Unix sockets: %d listed, reachable: %v", phase, len(seen), reached)
}

func TestExecHostResolver(t *testing.T) { resolveThroughHost(t, "exec phase") }

func TestExecUnixSockets(t *testing.T) { unixSockets(t, "exec phase") }

func TestFetchHostResolver(t *testing.T) { resolveThroughHost(t, "fetch phase") }

func TestFetchUnixSockets(t *testing.T) { unixSockets(t, "fetch phase") }

// TestExecSurvivor starts daemons that try to outlive the job. The harness checks that none is alive after cleanup.
func TestExecSurvivor(t *testing.T) {
	cmd := exec.Command("/usr/bin/sleep", "3600")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	if err := exec.Command("/usr/bin/setsid", "-f", "/usr/bin/nohup", "/usr/bin/sleep", "3600").Run(); err != nil {
		t.Fatalf("start double-forked daemon: %v", err)
	}
	if err := os.WriteFile("survivor.pid", []byte(strconv.Itoa(pid)), 0o600); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	t.Logf("PROBE survivor: started setsid daemon pid %d and a setsid -f nohup daemon; the harness checks after cleanup", pid)
}

// TestExecDependency is the positive check: a module fetched in the fetch phase builds offline.
func TestExecDependency(t *testing.T) {
	if quote.Hello() == "" {
		t.Fatal("rsc.io/quote returned nothing")
	}
	t.Logf("PROBE offline build with a fetched dependency: works: %q", quote.Hello())
}

// ---- fetch phase ----

func proxyAddr(t *testing.T) string {
	u, err := url.Parse(os.Getenv("HTTPS_PROXY"))
	if err != nil || u.Host == "" {
		t.Fatalf("HTTPS_PROXY not set in the fetch phase")
	}
	return u.Host
}

// connectThrough sends one CONNECT and returns nil only for a 200.
func connectThrough(t *testing.T, proxy, target string) error {
	t.Helper()
	var d net.Dialer
	conn, err := d.DialContext(timeoutCtx(t), "tcp", proxy)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target); err != nil {
		return err
	}
	status, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(status, "HTTP/1.1 200 ") {
		return errors.New("proxy answered " + strings.TrimSpace(status))
	}
	return nil
}

func TestFetchProxyAllowed(t *testing.T) {
	if err := connectThrough(t, proxyAddr(t), "proxy.golang.org:443"); err != nil {
		t.Fatalf("PROBE fetch allowed registry: the proxy refused proxy.golang.org:443: %v", err)
	}
	t.Log("PROBE fetch allowed registry: works: CONNECT proxy.golang.org:443 got 200")
}

func TestFetchProxyRefusals(t *testing.T) {
	for _, target := range []string{"example.com:443", "1.1.1.1:443", "169.254.169.254:80", "proxy.golang.org:80",
		"github.com:443", "proxy.golang.org.evil.example:443"} {
		blocked(t, "CONNECT "+target+" through the proxy", connectThrough(t, proxyAddr(t), target))
	}
}

func TestFetchDirect(t *testing.T) {
	for _, addr := range []string{"1.1.1.1:443", "[2606:4700:4700::1111]:443", "127.0.0.53:53", "169.254.169.254:80"} {
		var d net.Dialer
		conn, err := d.DialContext(timeoutCtx(t), "tcp", addr)
		if conn != nil {
			_ = conn.Close()
		}
		blocked(t, "fetch phase direct TCP to "+addr, err)
	}
	_, err := net.DefaultResolver.LookupHost(timeoutCtx(t), "github.com")
	blocked(t, "fetch phase DNS lookup of github.com", err)
}

func TestFetchDockerSocket(t *testing.T) {
	var d net.Dialer
	conn, err := d.DialContext(timeoutCtx(t), "unix", "/var/run/docker.sock")
	if conn != nil {
		_ = conn.Close()
	}
	blocked(t, "fetch phase connect to /var/run/docker.sock", err)
}
