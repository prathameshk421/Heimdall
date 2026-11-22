// Package probe holds the escape probes of AM11. They run inside the sandbox as the heimdall-sbx user. Each probe
// tries one escape. A probe passes when the escape is blocked and fails with "ESCAPED" when it works. A probe that
// cannot make its attempt (a missing tool, a missing input) skips with "UNKNOWN": the harness counts that as not
// proven, never as blocked.
//
// Test names say where they run: TestExec* in the execution phase, TestFetch* in the fetch phase, TestBoth* in both.
// Install scripts of a package manager run PR code in the fetch phase, so every probe that does not need the
// network runs in both. The harness (probe_linux_test.go in the parent package) plants the canaries and reads the
// results.
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
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

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
	// HostSockets is every Unix socket the harness found on the host as root: paths from find, and abstract names
	// (with a leading "@") and bound paths from the host's /proc/net/unix.
	HostSockets []string `json:"host_sockets"`
	// WritableDirs is every world-writable directory of the host, found as root. Each phase writes one plant file
	// into each; the harness checks that none of them appears on the host.
	WritableDirs []string `json:"writable_dirs"`
	// HostPlants are canary files the harness wrote, mode 0644, into the host's shared temp directories.
	HostPlants []string `json:"host_plants"`
	// CmdlinePID is a runner-user process with the canary in its argv.
	CmdlinePID int `json:"cmdline_pid"`
	// KeySerials are keys with the canary as payload in the runner user's session and user keyrings.
	KeySerials []int  `json:"key_serials"`
	KeyDesc    string `json:"key_desc"`
	// HostQueue is a POSIX message queue the harness made in the host's /dev/mqueue.
	HostQueue string `json:"host_queue"`
	// LoopbackPort is a TCP listener of the runner user on 127.0.0.1, for example a local service of the loop.
	LoopbackPort int `json:"loopback_port"`
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

func phase() string { return os.Getenv("HEIMDALL_PROBE_PHASE") }

// blocked records one escape attempt: err == nil means the escape worked.
func blocked(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("PROBE %s %s: ESCAPED", phase(), what)
		return
	}
	t.Logf("PROBE %s %s: blocked: %v", phase(), what, err)
}

// unknown ends a probe that could not make its attempt. It is never a pass.
func unknown(t *testing.T, what, why string) {
	t.Helper()
	t.Skipf("PROBE %s %s: UNKNOWN: %s", phase(), what, why)
}

// tool returns p, or ends the probe as unknown when the binary is missing.
func tool(t *testing.T, what, p string) string {
	t.Helper()
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		unknown(t, what, "no "+p+" on this image")
	}
	return p
}

// run runs a command and folds its output into the error.
func run(stdin string, argv ...string) (string, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		return s, fmt.Errorf("%w: %s", err, s)
	}
	return s, nil
}

func isCanary(c config, value string) bool {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:]) == c.CanarySHA256
}

// holdsCanary reports whether any NUL- or space-separated word of b, or any part after "=" or "/", is the canary.
func holdsCanary(c config, b []byte) bool {
	for _, w := range strings.FieldsFunc(string(b), func(r rune) bool { return r == 0 || r == ' ' || r == '\n' }) {
		if strings.Contains(w, canaryKey) || isCanary(c, w) {
			return true
		}
		for _, sep := range []string{"=", "/", "-"} {
			for _, part := range strings.Split(w, sep) {
				if isCanary(c, part) {
					return true
				}
			}
		}
	}
	return false
}

func timeoutCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func dial(t *testing.T, network, addr string) error {
	var d net.Dialer
	conn, err := d.DialContext(timeoutCtx(t), network, addr)
	if conn != nil {
		_ = conn.Close()
	}
	return err
}

// ---- both phases: processes, /proc and the loop ----

func TestBothParentEnviron(t *testing.T) {
	c := load(t)
	_, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", c.ParentPID))
	blocked(t, "read the loop's /proc/<pid>/environ", err)
}

func TestBothParentMem(t *testing.T) {
	c := load(t)
	f, err := os.Open(fmt.Sprintf("/proc/%d/mem", c.ParentPID))
	if err == nil {
		buf := make([]byte, 16)
		_, err = f.ReadAt(buf, 0x400000)
		_ = f.Close()
		if err != nil && !errors.Is(err, syscall.EIO) {
			blocked(t, "read the loop's /proc/<pid>/mem", err)
			return
		}
	}
	blocked(t, "open the loop's /proc/<pid>/mem", err)
}

func TestBothParentCmdline(t *testing.T) {
	c := load(t)
	if c.CmdlinePID <= 0 {
		unknown(t, "read a runner process's argv", "the harness started no argv canary process")
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", c.CmdlinePID))
	if err == nil && !holdsCanary(c, b) {
		err = fmt.Errorf("read /proc/%d/cmdline but it held no canary", c.CmdlinePID)
	}
	blocked(t, "read the canary from a runner process's /proc/<pid>/cmdline", err)
}

// TestBothProcScan reads every environ and cmdline it can see and looks for the canary, and counts the processes of
// other users it can see at all.
func TestBothProcScan(t *testing.T) {
	c := load(t)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		unknown(t, "scan /proc", err.Error())
	}
	me := os.Getuid()
	seen, others, readable := 0, 0, 0
	var leaks []string
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		seen++
		var st syscall.Stat_t
		if syscall.Stat(filepath.Join("/proc", e.Name()), &st) == nil && int(st.Uid) != me {
			others++
		}
		for _, f := range []string{"environ", "cmdline"} {
			b, err := os.ReadFile(filepath.Join("/proc", e.Name(), f))
			if err != nil {
				continue
			}
			readable++
			if holdsCanary(c, b) {
				leaks = append(leaks, "/proc/"+e.Name()+"/"+f)
			}
		}
	}
	if len(leaks) > 0 {
		t.Errorf("PROBE %s scan every /proc/<pid>/environ and cmdline: ESCAPED: canary in %v", phase(), leaks)
	}
	if others > 0 {
		t.Errorf("PROBE %s processes of other users visible: ESCAPED: %d of %d", phase(), others, seen)
	}
	if len(leaks) == 0 && others == 0 {
		t.Logf("PROBE %s scan /proc: blocked: %d processes visible, all own uid; canary in none of %d readable files", phase(), seen, readable)
	}
}

func TestBothPtrace(t *testing.T) {
	c := load(t)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, pid := range []int{c.ParentPID, c.CmdlinePID} {
		if pid <= 0 {
			continue
		}
		err := syscall.PtraceAttach(pid)
		if err == nil {
			_ = syscall.PtraceDetach(pid)
		}
		blocked(t, fmt.Sprintf("ptrace attach to runner process %d", pid), err)
	}
}

func TestBothOwnEnvironment(t *testing.T) {
	c := load(t)
	allowed := map[string]bool{}
	for _, k := range []string{"PATH", "HOME", "TMPDIR", "USER", "LOGNAME", "GOFLAGS", "GOPROXY", "GOSUMDB",
		"GOTOOLCHAIN", "GOPATH", "GOCACHE", "GOMODCACHE", "GOTELEMETRY", "CGO_ENABLED", "PWD", "HEIMDALL_PROBE_PHASE"} {
		allowed[k] = true
	}
	if phase() == "fetch" {
		for _, k := range []string{"HTTPS_PROXY", "HTTP_PROXY", "https_proxy", "http_proxy"} {
			allowed[k] = true
		}
	}
	var extra []string
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if k == canaryKey || isCanary(c, v) {
			t.Errorf("PROBE %s own environment: ESCAPED: canary in the sandbox environment", phase())
		}
		if !allowed[k] {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		t.Errorf("PROBE %s own environment: ESCAPED: unexpected variables %v", phase(), extra)
		return
	}
	t.Logf("PROBE %s own environment: blocked: only fixed keys, %d variables", phase(), len(os.Environ()))
}

// ---- both phases: files ----

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

func TestBothHomeCanary(t *testing.T) {
	c := load(t)
	blocked(t, "read the runner home canary "+c.HomeCanary, readCanary(c, c.HomeCanary))
	_, err := os.ReadDir(c.RunnerHome)
	blocked(t, "list the runner home", err)
}

func TestBothTempCanary(t *testing.T) {
	c := load(t)
	blocked(t, "read the _temp canary "+c.TempCanary, readCanary(c, c.TempCanary))
}

func TestBothRunnerCredentials(t *testing.T) {
	c := load(t)
	if len(c.CredFiles) == 0 {
		unknown(t, "read the runner's credential files", "the harness found none")
	}
	for _, p := range c.CredFiles {
		_, err := os.ReadFile(p)
		blocked(t, "read runner file "+p, err)
	}
}

// TestBothHostTempCanaries reads canaries the runner user left in the host's /tmp, /var/tmp and /dev/shm.
func TestBothHostTempCanaries(t *testing.T) {
	c := load(t)
	if len(c.HostPlants) == 0 {
		unknown(t, "read host temp canaries", "the harness planted none")
	}
	for _, p := range c.HostPlants {
		blocked(t, "read the host canary "+p, readCanary(c, p))
	}
}

// TestBothHostQueue looks for the host's message queue: a shared /dev/mqueue or IPC namespace would show it.
func TestBothHostQueue(t *testing.T) {
	c := load(t)
	if c.HostQueue == "" {
		unknown(t, "see the host's message queue", "the harness made none")
	}
	_, err := os.Lstat(c.HostQueue)
	blocked(t, "see the host message queue "+c.HostQueue, err)
}

func TestBothOtherUsersFiles(t *testing.T) {
	for _, p := range []string{"/etc/shadow", "/etc/gshadow", "/etc/sudoers"} {
		_, err := os.ReadFile(p)
		blocked(t, "read "+p, err)
	}
	for _, d := range []string{"/root", "/home", "/run/user"} {
		ents, err := os.ReadDir(d)
		if err == nil && len(ents) == 0 {
			err = fmt.Errorf("%s lists empty", d)
		}
		blocked(t, "list "+d, err)
	}
}

// TestBothPlant writes one file into every world-writable directory of the host. The harness checks the host.
func TestBothPlant(t *testing.T) {
	c := load(t)
	if len(c.WritableDirs) == 0 {
		unknown(t, "plant files in shared directories", "the harness found no world-writable directory")
	}
	written, reasons := 0, map[string]int{}
	for _, d := range c.WritableDirs {
		p := filepath.Join(d, "heimdall-sbx-plant-"+phase())
		if err := os.WriteFile(p, []byte("planted by heimdall-sbx\n"), 0o644); err != nil {
			reasons[errClass(err)]++
			continue
		}
		written++
		t.Logf("PROBE %s plant %s: written inside; the harness checks the host", phase(), p)
	}
	t.Logf("PROBE %s plant into %d host world-writable directories: %d written inside (the harness checks the host), refused %v",
		phase(), len(c.WritableDirs), written, reasons)
}

func TestBothMountinfo(t *testing.T) {
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		unknown(t, "log mountinfo", err.Error())
	}
	t.Logf("INFO %s mountinfo:\n%s", phase(), b)
}

// ---- both phases: privileges ----

func TestBothSetuid(t *testing.T) {
	_, err := run("", tool(t, "sudo", "/usr/bin/sudo"), "-n", "true")
	blocked(t, "escalate with sudo -n true", err)
	_, err = run("", tool(t, "su", "/usr/bin/su"), "-c", "true", "root")
	blocked(t, "escalate with su root", err)
	// chage reads /etc/shadow only through its setgid bit; the runner user's own chage -l is the control.
	_, err = run("", tool(t, "chage", "/usr/bin/chage"), "-l", "heimdall-sbx")
	blocked(t, "read the shadow entry through setgid chage -l", err)
	if _, serr := os.Stat("/usr/bin/pkexec"); serr == nil {
		_, err = run("", "/usr/bin/pkexec", "true")
		blocked(t, "escalate with pkexec", err)
	} else {
		t.Logf("PROBE %s escalate with pkexec: not applicable: no pkexec on this image", phase())
	}
	p := filepath.Join(os.Getenv("TMPDIR"), "suid-"+phase())
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o700); err != nil {
		unknown(t, "make a setuid file", err.Error())
	}
	err = os.Chmod(p, 0o4755)
	if err == nil {
		var st syscall.Stat_t
		if serr := syscall.Stat(p, &st); serr == nil && st.Mode&syscall.S_ISUID == 0 {
			err = errors.New("chmod returned but the setuid bit is not set")
		}
	}
	blocked(t, "set the setuid bit on an own file", err)
}

func TestBothNoNewPrivs(t *testing.T) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		unknown(t, "privileges", err.Error())
	}
	defer f.Close()
	fields := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), ":")
		fields[k] = strings.TrimSpace(v)
	}
	zero := "0000000000000000"
	if fields["NoNewPrivs"] != "1" || fields["CapEff"] != zero || fields["CapBnd"] != zero || fields["CapPrm"] != zero ||
		fields["CapAmb"] != zero || fields["Seccomp"] != "2" {
		t.Errorf("PROBE %s privileges: ESCAPED: NoNewPrivs=%s CapEff=%s CapPrm=%s CapBnd=%s CapAmb=%s Seccomp=%s", phase(),
			fields["NoNewPrivs"], fields["CapEff"], fields["CapPrm"], fields["CapBnd"], fields["CapAmb"], fields["Seccomp"])
		return
	}
	t.Logf("PROBE %s privileges: blocked: NoNewPrivs=1 all caps 0 Seccomp=2 Uid=%s Gid=%s Groups=%q", phase(),
		fields["Uid"], fields["Gid"], fields["Groups"])
}

func TestBothNamespaces(t *testing.T) {
	_, err := run("", tool(t, "unshare", "/usr/bin/unshare"), "--user", "--map-root-user", "--net", "true")
	blocked(t, "make a user namespace (unshare -Urn)", err)
	_, err = run("", "/usr/bin/unshare", "--mount", "true")
	blocked(t, "make a mount namespace (unshare -m)", err)
}

func TestBothKeyring(t *testing.T) {
	c := load(t)
	if len(c.KeySerials) == 0 || c.KeyDesc == "" {
		unknown(t, "read the runner's keys", "the harness made no key")
	}
	buf := make([]byte, 256)
	for _, id := range c.KeySerials {
		n, _, e := syscall.Syscall6(syscall.SYS_KEYCTL, keyctlRead, uintptr(id), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0)
		var err error
		if e != 0 {
			err = e
		} else if int(n) > len(buf) || !isCanary(c, string(buf[:n])) {
			err = fmt.Errorf("read key %d but it held no canary", id)
		}
		blocked(t, fmt.Sprintf("read runner key %d by serial", id), err)
	}
	typ, _ := syscall.BytePtrFromString("user")
	desc, _ := syscall.BytePtrFromString(c.KeyDesc)
	for name, ring := range map[string]int{"session": keySpecSession, "user": keySpecUser, "user session": keySpecUserSession} {
		r, _, e := syscall.Syscall6(syscall.SYS_KEYCTL, keyctlSearch, uintptr(ring), uintptr(unsafe.Pointer(typ)),
			uintptr(unsafe.Pointer(desc)), 0, 0)
		var err error
		if e != 0 {
			err = e
		} else {
			t.Logf("search found key %d", int32(r))
		}
		blocked(t, "find the runner key "+c.KeyDesc+" in the "+name+" keyring", err)
	}
}

// keyctl operations and special keyring ids, from linux/keyctl.h.
const (
	keyctlSearch       = 10
	keyctlRead         = 11
	keySpecSession     = -3
	keySpecUser        = -4
	keySpecUserSession = -5
)

func TestBothCgroup(t *testing.T) {
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		unknown(t, "cgroup", err.Error())
	}
	cg := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), "0::"))
	if !strings.Contains(cg, "heimdall-sbx-") {
		t.Errorf("PROBE %s own cgroup: ESCAPED: %q is not a heimdall-sbx unit", phase(), cg)
	}
	self := []byte(strconv.Itoa(os.Getpid()))
	for _, p := range []string{"/sys/fs/cgroup/cgroup.procs", "/sys/fs/cgroup/user.slice/cgroup.procs",
		"/sys/fs/cgroup/system.slice/cgroup.procs", filepath.Join("/sys/fs/cgroup", filepath.Dir(cg), "cgroup.procs")} {
		blocked(t, "move out of the unit's cgroup via "+p, os.WriteFile(p, self, 0))
	}
	own := filepath.Join("/sys/fs/cgroup", cg)
	for _, f := range []string{"memory.max", "pids.max", "cpu.max"} {
		blocked(t, "raise the own limit "+f, os.WriteFile(filepath.Join(own, f), []byte("max"), 0))
	}
	blocked(t, "make a child cgroup", os.Mkdir(filepath.Join(own, "escape"), 0o755))
}

func TestBothKernelTunables(t *testing.T) {
	blocked(t, "write /proc/sys/kernel/hostname", os.WriteFile("/proc/sys/kernel/hostname", []byte("x"), 0))
	blocked(t, "write /proc/sysrq-trigger", os.WriteFile("/proc/sysrq-trigger", []byte("h"), 0))
	_, err := os.ReadFile("/proc/kmsg")
	blocked(t, "read /proc/kmsg", err)
	_, err = os.ReadFile("/dev/kmsg")
	blocked(t, "read /dev/kmsg", err)
	_, err = os.Stat("/dev/sda")
	if err == nil {
		_, err = os.ReadFile("/dev/sda")
	}
	blocked(t, "reach a raw disk (/dev/sda)", err)
}

// TestBothBPF tries to make an eBPF map. Unprivileged eBPF is a kernel attack surface.
func TestBothBPF(t *testing.T) {
	var attr [20]uint32
	attr[0], attr[1], attr[2], attr[3] = bpfMapTypeHash, 4, 4, 1
	fd, _, e := syscall.Syscall(sysBPF, bpfMapCreate, uintptr(unsafe.Pointer(&attr[0])), unsafe.Sizeof(attr))
	var err error
	if e != 0 {
		err = e
	} else {
		_ = syscall.Close(int(fd))
	}
	blocked(t, "create an eBPF map (bpf BPF_MAP_CREATE)", err)
}

// The bpf syscall on linux/amd64 and its constants, from linux/bpf.h.
const (
	sysBPF         = 321
	bpfMapCreate   = 0
	bpfMapTypeHash = 1
)

func TestBothSchedulers(t *testing.T) {
	_, err := run("* * * * * /usr/bin/sleep 1\n", tool(t, "crontab", "/usr/bin/crontab"), "-")
	blocked(t, "install a crontab", err)
	if _, serr := os.Stat("/usr/bin/at"); serr == nil {
		_, err = run("/usr/bin/sleep 1\n", "/usr/bin/at", "now", "+", "1", "minute")
		blocked(t, "queue an at job", err)
	} else {
		t.Logf("PROBE %s queue an at job: not applicable: no at on this image", phase())
		blocked(t, "write into the at spool", os.WriteFile("/var/spool/cron/atjobs/heimdall-sbx", []byte("x"), 0o700))
	}
	sdrun := tool(t, "systemd-run", "/usr/bin/systemd-run")
	_, err = run("", sdrun, "--user", "--scope", "/usr/bin/sleep", "1")
	blocked(t, "start a user systemd unit", err)
	_, err = run("", sdrun, "--scope", "/usr/bin/sleep", "1")
	blocked(t, "start a system systemd unit", err)
}

// ---- both phases: Unix sockets, D-Bus, journald ----

// resolveThroughHost asks the host's resolver over its Unix sockets, which a network namespace does not cut.
func resolveThroughHost(t *testing.T) {
	out, err := run("", tool(t, "resolvectl", "/usr/bin/resolvectl"), "query", "--legend=no", "example.com")
	if err == nil {
		t.Logf("resolvectl said: %s", out)
	}
	blocked(t, "DNS through systemd-resolved (resolvectl query example.com)", err)
	out, err = run("", tool(t, "getent", "/usr/bin/getent"), "ahosts", "example.com")
	if err == nil {
		t.Logf("getent said: %s", out)
	}
	blocked(t, "DNS through NSS (getent ahosts example.com)", err)
}

func dialUnix(t *testing.T, name string) error {
	addr := name
	if strings.HasPrefix(name, "@") {
		addr = "\x00" + name[1:]
	}
	return dial(t, "unix", addr)
}

// errClass shortens an error to its errno text, so the log can count the reasons.
func errClass(err error) string {
	var se syscall.Errno
	if errors.As(err, &se) {
		return se.Error()
	}
	return err.Error()
}

// procNetUnix lists the socket names of this network namespace's /proc/net/unix.
func procNetUnix(t *testing.T) []string {
	b, err := os.ReadFile("/proc/net/unix")
	if err != nil {
		t.Logf("read /proc/net/unix: %v", err)
		return nil
	}
	var names []string
	for _, line := range strings.Split(string(b), "\n")[1:] {
		if f := strings.Fields(line); len(f) >= 8 {
			names = append(names, f[7])
		}
	}
	return names
}

// TestBothHostSockets tries to connect to every Unix socket the harness found on the host, plus every one this
// namespace's /proc/net/unix lists. Every connect must fail.
func TestBothHostSockets(t *testing.T) {
	c := load(t)
	if len(c.HostSockets) == 0 {
		unknown(t, "connect to every host Unix socket", "the harness gave no host socket list")
	}
	all := map[string]bool{}
	for _, n := range c.HostSockets {
		all[n] = true
	}
	for _, n := range procNetUnix(t) {
		all[n] = true
	}
	reasons := map[string]int{}
	var reached []string
	for n := range all {
		if err := dialUnix(t, n); err != nil {
			reasons[errClass(err)]++
			continue
		}
		reached = append(reached, n)
	}
	if len(reached) > 0 {
		t.Errorf("PROBE %s connect to every host Unix socket: ESCAPED: %d of %d reachable: %v", phase(), len(reached), len(all), reached)
		return
	}
	t.Logf("PROBE %s connect to every host Unix socket: blocked: 0 of %d reachable; errors %v", phase(), len(all), reasons)
}

// TestBothNamedSockets tries the sockets of the escape write-up by name.
func TestBothNamedSockets(t *testing.T) {
	for _, p := range []string{
		"/run/systemd/resolve/io.systemd.Resolve", "/run/systemd/resolve/io.systemd.Resolve.Monitor",
		"/run/dbus/system_bus_socket", "/var/run/dbus/system_bus_socket", "/run/systemd/io.systemd.Manager",
		"/run/systemd/private", "/run/systemd/notify", "/run/systemd/journal/stdout", "/run/systemd/journal/socket",
		"/run/systemd/journal/dev-log", "/run/systemd/userdb/io.systemd.DynamicUser", "/run/snapd.socket",
		"/run/snapd-snap.socket", "/run/polkit/agent-helper.socket", "/run/ssh-unix-local/socket",
		"/var/run/docker.sock", "/run/docker.sock", "/run/containerd/containerd.sock", "/dev/log",
	} {
		blocked(t, "connect to "+p, dialUnix(t, p))
	}
}

func TestBothBusctl(t *testing.T) {
	_, err := run("", tool(t, "busctl", "/usr/bin/busctl"), "--system", "--no-pager", "list")
	blocked(t, "D-Bus system bus (busctl --system list)", err)
}

// TestBothJournal tries to write a marker into the host journal. The exit status proves nothing (logger exits 0 on
// ubuntu-latest when no socket is reachable), so the harness judges: it reads the host journal for the tag.
func TestBothJournal(t *testing.T) {
	tag := "heimdall-probe-" + phase()
	out, err := run("", tool(t, "logger", "/usr/bin/logger"), "-t", tag, "marker from the sandbox")
	t.Logf("PROBE %s write to syslog/journald with logger: attempted (err %v, output %q); the harness checks the host journal", phase(), err, out)
	out, err = run("", tool(t, "systemd-cat", "/usr/bin/systemd-cat"), "-t", tag, "/usr/bin/echo", "marker from the sandbox")
	t.Logf("PROBE %s write to journald with systemd-cat: attempted (err %v, output %q); the harness checks the host journal", phase(), err, out)
}

// ---- execution phase ----

func TestExecHostResolver(t *testing.T) { resolveThroughHost(t) }

func TestExecProcNetUnix(t *testing.T) {
	c := load(t)
	host := map[string]bool{}
	for _, n := range c.HostSockets {
		host[n] = true
	}
	inside := procNetUnix(t)
	var shared []string
	for _, n := range inside {
		if host[n] {
			shared = append(shared, n)
		}
	}
	if len(shared) > 0 {
		t.Errorf("PROBE exec /proc/net/unix shows host sockets: ESCAPED: %d of the host's visible: %v", len(shared), shared)
		return
	}
	t.Logf("PROBE exec /proc/net/unix shows host sockets: blocked: %d entries, none of the host's", len(inside))
}

// TestExecUnixSocketOwn is the positive check: a test suite may still use its own Unix socket in the execution
// phase, inside the private namespaces.
func TestExecUnixSocketOwn(t *testing.T) {
	p := filepath.Join(os.Getenv("TMPDIR"), "own.sock")
	l, err := net.Listen("unix", p)
	if err != nil {
		t.Fatalf("PROBE exec own Unix socket: does not work: %v", err)
	}
	defer l.Close()
	if err := dialUnix(t, p); err != nil {
		t.Fatalf("PROBE exec own Unix socket: does not work: %v", err)
	}
	t.Log("PROBE exec own Unix socket: works")
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
	for _, addr := range []string{"1.1.1.1:443", "140.82.112.3:443", "[2606:4700:4700::1111]:443", "169.254.169.254:80",
		"168.63.129.16:80", "168.63.129.16:32526"} {
		blocked(t, "outbound TCP connect to "+addr, dial(t, "tcp", addr))
	}
	loopbackService(t)
}

// loopbackService tries the runner user's own listener on 127.0.0.1. The harness proves it answers the runner.
func loopbackService(t *testing.T) {
	c := load(t)
	if c.LoopbackPort <= 0 {
		unknown(t, "connect to the runner's loopback service", "the harness opened no listener")
	}
	blocked(t, "connect to the runner's loopback service 127.0.0.1:"+strconv.Itoa(c.LoopbackPort),
		dial(t, "tcp", "127.0.0.1:"+strconv.Itoa(c.LoopbackPort)))
}

func TestExecICMP(t *testing.T) { icmp(t) }

func TestExecInterfaces(t *testing.T) {
	ifs, err := net.Interfaces()
	if err != nil {
		unknown(t, "network interfaces", err.Error())
	}
	var names []string
	for _, i := range ifs {
		names = append(names, i.Name)
	}
	if len(names) != 1 || names[0] != "lo" {
		t.Errorf("PROBE exec network interfaces: ESCAPED: %v", names)
		return
	}
	t.Logf("PROBE exec network interfaces: blocked: only %v", names)
}

// TestExecProxy tries the fetch proxy from the execution phase.
func TestExecProxy(t *testing.T) {
	c := load(t)
	err := connectThrough(t, "127.0.0.1:"+strconv.Itoa(c.ProxyPort), "example.com:443")
	blocked(t, "CONNECT example.com:443 through the fetch proxy", err)
	err = connectThrough(t, "127.0.0.1:"+strconv.Itoa(c.ProxyPort), "proxy.golang.org:443")
	blocked(t, "CONNECT proxy.golang.org:443 through the fetch proxy", err)
}

// TestExecDependency is the positive check: a module fetched in the fetch phase builds offline.
func TestExecDependency(t *testing.T) {
	if quote.Hello() == "" {
		t.Fatal("rsc.io/quote returned nothing")
	}
	t.Logf("PROBE exec offline build with a fetched dependency: works: %q", quote.Hello())
}

// ---- both phases: survivors ----

// TestBothSurvivor starts daemons that try to outlive the job. The harness checks that none is alive afterwards.
func TestBothSurvivor(t *testing.T) {
	cmd := exec.Command("/usr/bin/sleep", "3600")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		unknown(t, "survivor", "start daemon: "+err.Error())
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	if err := exec.Command("/usr/bin/setsid", "-f", "/usr/bin/nohup", "/usr/bin/sleep", "3600").Run(); err != nil {
		unknown(t, "survivor", "start double-forked daemon: "+err.Error())
	}
	if err := os.WriteFile("survivor-"+phase()+".pid", []byte(strconv.Itoa(pid)), 0o600); err != nil {
		unknown(t, "survivor", "write pid: "+err.Error())
	}
	t.Logf("PROBE %s survivor: started setsid daemon pid %d and a setsid -f nohup daemon; the harness checks after the job", phase(), pid)
}

// TestHang is not a probe: the harness runs it alone with a short job timeout. It starts a daemon in a new session,
// writes its pid, and then never ends. The harness checks that the job ends at its timeout and that nothing survives.
func TestHang(t *testing.T) {
	cmd := exec.Command("/usr/bin/sleep", "3600")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	if err := os.WriteFile("hang.pid", []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	_ = cmd.Process.Release()
	t.Log("PROBE hang: daemon started, now blocking")
	<-time.After(time.Hour)
}

// ---- fetch phase ----

func TestFetchHostResolver(t *testing.T) { resolveThroughHost(t) }

func TestFetchProcNetUnix(t *testing.T) {
	// The fetch phase shares the host network namespace by design, so the names are visible. TestBothHostSockets
	// proves none of them can be connected to.
	t.Logf("PROBE fetch /proc/net/unix: visible (names only, host network namespace): %d entries", len(procNetUnix(t)))
}

// TestFetchSocketFamilies: the fetch phase may open only IPv4 and IPv6 sockets.
func TestFetchSocketFamilies(t *testing.T) {
	l, err := net.Listen("unix", filepath.Join(os.Getenv("TMPDIR"), "fetch.sock"))
	if l != nil {
		_ = l.Close()
	}
	blocked(t, "create a Unix socket", err)
	_, err = net.ListenUnixgram("unixgram", &net.UnixAddr{Name: "@heimdall-sbx-fetch", Net: "unixgram"})
	blocked(t, "create an abstract Unix datagram socket", err)
	for name, fam := range map[string][3]int{
		"netlink": {syscall.AF_NETLINK, syscall.SOCK_RAW, syscall.NETLINK_ROUTE},
		"packet":  {syscall.AF_PACKET, syscall.SOCK_RAW, 0},
		"raw ip":  {syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_ICMP},
		"vsock":   {40, syscall.SOCK_STREAM, 0},
	} {
		fd, err := syscall.Socket(fam[0], fam[1], fam[2])
		if err == nil {
			_ = syscall.Close(fd)
		}
		blocked(t, "create a "+name+" socket", err)
	}
}

func proxyAddr(t *testing.T) string {
	u, err := url.Parse(os.Getenv("HTTPS_PROXY"))
	if err != nil || u.Host == "" {
		unknown(t, "fetch proxy", "HTTPS_PROXY not set in the fetch phase")
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
		"github.com:443", "proxy.golang.org.evil.example:443", "[::1]:443", "localhost:443"} {
		blocked(t, "CONNECT "+target+" through the proxy", connectThrough(t, proxyAddr(t), target))
	}
}

func TestFetchDirect(t *testing.T) {
	for _, addr := range []string{"1.1.1.1:443", "[2606:4700:4700::1111]:443", "127.0.0.53:53", "169.254.169.254:80",
		"168.63.129.16:80", "168.63.129.16:32526", "127.0.0.1:22", "[::1]:" + strconv.Itoa(load(t).ProxyPort)} {
		blocked(t, "direct TCP to "+addr, dial(t, "tcp", addr))
	}
	loopbackService(t)
	_, err := net.DefaultResolver.LookupHost(timeoutCtx(t), "github.com")
	blocked(t, "DNS lookup of github.com (system resolver)", err)
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "udp", "8.8.8.8:53")
	}}
	_, err = r.LookupHost(timeoutCtx(t), "github.com")
	blocked(t, "direct UDP DNS to 8.8.8.8", err)
}

func TestFetchICMP(t *testing.T) { icmp(t) }

// icmp sends an echo request to 1.1.1.1 through an unprivileged ICMP socket and waits for the reply.
func icmp(t *testing.T) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_ICMP)
	if err != nil {
		blocked(t, "ICMP echo to 1.1.1.1 (open ping socket)", err)
		return
	}
	defer syscall.Close(fd)
	tv := syscall.Timeval{Sec: 3}
	_ = syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
	echo := []byte{8, 0, 0, 0, 0, 1, 0, 1, 'h', 'e', 'i', 'm'}
	err = syscall.Sendto(fd, echo, 0, &syscall.SockaddrInet4{Addr: [4]byte{1, 1, 1, 1}})
	if err == nil {
		buf := make([]byte, 64)
		_, _, err = syscall.Recvfrom(fd, buf, 0)
	}
	blocked(t, "ICMP echo to 1.1.1.1", err)
}
