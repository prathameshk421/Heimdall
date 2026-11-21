package uidsandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/testevents"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
)

// ErrCannotIsolate means this runner cannot run PR code in the sandbox. The proof run reports
// "unknown: runner cannot isolate PR code" for every part that runs PR code. There is no fallback.
var ErrCannotIsolate = errors.New("runner cannot isolate PR code")

// CannotIsolateError names what is missing. errors.Is(err, ErrCannotIsolate) is true for it.
type CannotIsolateError struct{ Reason string }

func (e *CannotIsolateError) Error() string { return ErrCannotIsolate.Error() + ": " + e.Reason }

// Is makes errors.Is match ErrCannotIsolate.
func (e *CannotIsolateError) Is(target error) bool { return target == ErrCannotIsolate }

// Other errors of the sandbox.
var (
	ErrBadJob    = errors.New("invalid sandbox job")
	ErrSurvivor  = errors.New("sandbox user still has processes after the kill")
	ErrNotSetup  = errors.New("sandbox is not set up")
	ErrSetupStep = errors.New("sandbox setup step failed")
)

// Fixed names and paths. None of them comes from PR data.
const (
	User     = "heimdall-sbx"
	Root     = "/sbx"
	WorkDir  = "/sbx/work"
	HomeDir  = "/sbx/home"
	TmpDir   = "/sbx/tmp"
	nftTable = "heimdall_sbx"
	// MaxOutputBytes caps stdout and stderr of one job, each.
	MaxOutputBytes = 4 << 20
	// killRounds bounds the kill loop of one cleanup.
	killRounds = 20
	// stageTimeout and readTimeout are the deadlines of the sandbox's own jobs: the tar extraction and the read of
	// a result file.
	stageTimeout = 10 * time.Minute
	readTimeout  = time.Minute
)

// Phase is where a job runs.
type Phase int

// The two phases of section 4.4.
const (
	// PhaseFetch has egress only through the proxy to the allowlisted registries.
	PhaseFetch Phase = iota + 1
	// PhaseExec runs in a private network namespace with loopback only.
	PhaseExec
)

// ResultFormat says how the job reports its tests.
type ResultFormat int

// Result formats.
const (
	// ResultNone keeps only the exit status and the output.
	ResultNone ResultFormat = iota
	// ResultJUnit reads a JUnit file the job wrote under the work tree, as the sandbox user, and parses it with
	// verify.ParseBytes.
	ResultJUnit
	// ResultGoTestJSON parses stdout as a go test -json stream with the shared testevents parser.
	ResultGoTestJSON
)

// Job is one command on the staged tree. Argv comes from the profile at the merge base; Dir and ResultPath are
// relative to the work tree. Env adds variables to the fixed sandbox set; it cannot replace a fixed one.
type Job struct {
	Argv       []string
	Dir        string
	Env        []string
	Timeout    time.Duration
	Result     ResultFormat
	ResultPath string
}

// Result is what one job produced.
type Result struct {
	Exit     int
	TimedOut bool
	Stdout   []byte
	Stderr   []byte
	Report   verify.Report
}

// Limits of the transient service of each job. Each must be positive; Setup refuses a zero.
type Limits struct {
	CPUPercent int   // CPUQuota, 100 is one core
	MemoryMax  int64 // bytes
	TasksMax   int
}

// Config configures the sandbox. Tools holds absolute paths of the system binaries; New fills it on Linux.
type Config struct {
	RunnerHome string
	RunnerTemp string
	Registries []string
	Ports      []int
	// ToolPath is the PATH inside jobs, for example the toolchain bin directory followed by /usr/bin:/bin.
	ToolPath string
	Limits   Limits
	Logger   *slog.Logger
}

// tools are the absolute paths of every binary the sandbox runs.
type tools struct {
	sudo, useradd, id, nft, systemdRun, systemctl, env, tar, pkill, pgrep, chmod, chown, mkdir, rm, crontab, tee, head, sh string
}

// Sandbox runs PR code as the heimdall-sbx user (ACTION_MODE 4.4).
type Sandbox struct {
	cfg   Config
	exec  Exec
	tools tools
	log   *slog.Logger

	mu    sync.Mutex
	uid   int
	proxy *Proxy
	seq   int
}

func newSandbox(cfg Config, ex Exec, t tools) *Sandbox {
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if len(cfg.Ports) == 0 {
		cfg.Ports = []int{443}
	}
	return &Sandbox{cfg: cfg, exec: ex, tools: t, log: log, uid: -1}
}

// UID is the numeric uid of the sandbox user, or -1 before Setup.
func (s *Sandbox) UID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.uid
}

// ProxyPort is the loopback port of the fetch proxy, or 0 before Setup.
func (s *Sandbox) ProxyPort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proxy == nil {
		return 0
	}
	return s.proxy.Port()
}

// root runs argv as root through sudo -n and returns the exit status and the bounded output.
func (s *Sandbox) root(ctx context.Context, stdin io.Reader, argv ...string) (int, string, error) {
	var out bytes.Buffer
	lw := &limitWriter{w: &out, limit: MaxOutputBytes}
	full := append([]string{s.tools.sudo, "-n", "--"}, argv...)
	code, err := s.exec.Run(ctx, Cmd{Argv: full, Env: fixedPathEnv(), Stdin: stdin, Stdout: lw, Stderr: lw})
	return code, strings.TrimSpace(out.String()), err
}

// mustRoot runs argv as root and fails unless it exits 0.
func (s *Sandbox) mustRoot(ctx context.Context, stdin io.Reader, argv ...string) error {
	code, out, err := s.root(ctx, stdin, argv...)
	if err != nil {
		return fmt.Errorf("%s: %w", path.Base(argv[0]), err)
	}
	if code != 0 {
		return fmt.Errorf("%w: %s exited %d: %s", ErrSetupStep, path.Base(argv[0]), code, out)
	}
	return nil
}

func fixedPathEnv() []string { return []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"} }

// Setup prepares the user, the directories, the proxy and the nftables rule, then starts one empty job in each
// phase to prove that this systemd accepts every property. It is idempotent for the user and safe to call once per
// runner job.
func (s *Sandbox) Setup(ctx context.Context) error {
	allow, err := NewAllowlist(s.cfg.Registries, s.cfg.Ports)
	if err != nil {
		return fmt.Errorf("fetch allowlist: %w", err)
	}
	if l := s.cfg.Limits; l.CPUPercent <= 0 || l.MemoryMax <= 0 || l.TasksMax <= 0 {
		return fmt.Errorf("%w: every limit must be positive: %+v", ErrSetupStep, l)
	}
	if err := s.ensureUser(ctx); err != nil {
		return err
	}
	if err := s.lockRunnerDirs(ctx); err != nil {
		return err
	}
	if err := s.makeDirs(ctx); err != nil {
		return err
	}
	p, err := StartProxy(ctx, allow, s.log)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.proxy = p
	s.mu.Unlock()
	if err := s.installNft(ctx, p.Port()); err != nil {
		return err
	}
	if err := s.plantMarkers(ctx); err != nil {
		return err
	}
	return s.selfCheck(ctx)
}

// hostMarkers are root-owned empty files Setup leaves in the host's shared temp directories. The self check lists
// them among the paths no service may see, so a service whose /tmp, /var/tmp or /dev/shm is the host's is refused.
func hostMarkers() []string {
	return []string{"/tmp/heimdall-sbx-host-marker", "/var/tmp/heimdall-sbx-host-marker", "/dev/shm/heimdall-sbx-host-marker"}
}

func (s *Sandbox) plantMarkers(ctx context.Context) error {
	for _, m := range hostMarkers() {
		if err := s.mustRoot(ctx, strings.NewReader(""), s.tools.tee, "--", m); err != nil {
			return err
		}
	}
	return nil
}

// selfCheck runs selfCheckScript in a service of each phase. A systemd that rejects a property fails here, and so
// does a service that started with less isolation than asked (checkSelfReport), before any PR code runs. Both
// return a *CannotIsolateError, so the caller reports unknown and no job ever runs on a weak service.
func (s *Sandbox) selfCheck(ctx context.Context) error {
	for _, phase := range []Phase{PhaseExec, PhaseFetch} {
		var out bytes.Buffer
		lw := &limitWriter{w: &out, limit: MaxOutputBytes}
		cctx, cancel := context.WithTimeout(ctx, readTimeout)
		unit := s.nextUnit()
		argv := append([]string{s.tools.sh, "-c", selfCheckScript, "sh"}, hiddenPaths(s.cfg)...)
		code, err := s.exec.Run(cctx, Cmd{
			Argv: append([]string{s.tools.sudo, "-n", "--"}, s.unitArgv(phase, unit, readTimeout, WorkDir, nil, argv...)...),
			Env:  fixedPathEnv(), Stdout: lw, Stderr: lw,
			Cancel: func() error { return s.killUnit(context.WithoutCancel(ctx), unit) },
		})
		cancel()
		if kerr := s.KillAll(context.WithoutCancel(ctx)); kerr != nil {
			return errors.Join(err, kerr)
		}
		if err != nil {
			return fmt.Errorf("sandbox self check: %w", err)
		}
		if code != 0 {
			return &CannotIsolateError{Reason: fmt.Sprintf("systemd refused the hardened service (%s phase, exit %d): %s",
				phaseName(phase), code, strings.TrimSpace(out.String()))}
		}
		if err := checkSelfReport(phase, s.UID(), out.String()); err != nil {
			return err
		}
		s.log.InfoContext(ctx, "sandbox self check passed", "phase", phaseName(phase))
	}
	return nil
}

func (s *Sandbox) ensureUser(ctx context.Context) error {
	code, _, err := s.root(ctx, nil, s.tools.id, "-u", User)
	if err != nil {
		return fmt.Errorf("id: %w", err)
	}
	if code != 0 {
		if err := s.mustRoot(ctx, nil, s.tools.useradd, "--system", "--user-group", "--home-dir", HomeDir,
			"--no-create-home", "--shell", "/usr/sbin/nologin", User); err != nil {
			return err
		}
	}
	_, uidText, err := s.root(ctx, nil, s.tools.id, "-u", User)
	if err != nil {
		return fmt.Errorf("id: %w", err)
	}
	uid, err := strconv.Atoi(uidText)
	if err != nil || uid <= 0 {
		return fmt.Errorf("%w: uid of %s is %q", ErrSetupStep, User, uidText)
	}
	// The user must be in its own group only: no sudo, no docker, no adm.
	_, groups, err := s.root(ctx, nil, s.tools.id, "-nG", User)
	if err != nil {
		return fmt.Errorf("id: %w", err)
	}
	if groups != User {
		return fmt.Errorf("%w: %s has groups %q", ErrSetupStep, User, groups)
	}
	s.mu.Lock()
	s.uid = uid
	s.mu.Unlock()
	// cron and at would start processes after the kill. Deny the user both.
	for _, f := range []string{"/etc/cron.deny", "/etc/at.deny"} {
		if err := s.mustRoot(ctx, strings.NewReader(User+"\n"), s.tools.tee, "-a", f); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sandbox) lockRunnerDirs(ctx context.Context) error {
	for _, d := range []string{s.cfg.RunnerHome, s.cfg.RunnerTemp} {
		if d == "" || !path.IsAbs(d) || path.Clean(d) != d {
			return fmt.Errorf("%w: runner directory %q must be a clean absolute path", ErrSetupStep, d)
		}
		if err := s.mustRoot(ctx, nil, s.tools.chmod, "0700", "--", d); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sandbox) makeDirs(ctx context.Context) error {
	if err := s.mustRoot(ctx, nil, s.tools.mkdir, "-p", "-m", "0755", "--", Root); err != nil {
		return err
	}
	for _, d := range []string{HomeDir, TmpDir} {
		if err := s.mustRoot(ctx, nil, s.tools.mkdir, "-p", "-m", "0700", "--", d); err != nil {
			return err
		}
		if err := s.mustRoot(ctx, nil, s.tools.chown, "--no-dereference", User+":"+User, "--", d); err != nil {
			return err
		}
	}
	return s.resetWork(ctx)
}

func (s *Sandbox) resetWork(ctx context.Context) error {
	if err := s.mustRoot(ctx, nil, s.tools.rm, "-rf", "--one-file-system", "--", WorkDir); err != nil {
		return err
	}
	if err := s.mustRoot(ctx, nil, s.tools.mkdir, "-m", "0700", "--", WorkDir); err != nil {
		return err
	}
	return s.mustRoot(ctx, nil, s.tools.chown, "--no-dereference", User+":"+User, "--", WorkDir)
}

// nftRuleset drops every packet the uid sends, except TCP to the proxy on loopback. It covers IPv4 and IPv6.
func nftRuleset(uid, port int) string {
	return fmt.Sprintf(`table inet %[1]s {
	chain out {
		type filter hook output priority 0; policy accept;
		meta skuid %[2]d oifname "lo" ip daddr 127.0.0.1 tcp dport %[3]d accept
		meta skuid %[2]d counter drop
	}
}
`, nftTable, uid, port)
}

func (s *Sandbox) installNft(ctx context.Context, port int) error {
	if code, _, err := s.root(ctx, nil, s.tools.nft, "list", "table", "inet", nftTable); err == nil && code == 0 {
		if err := s.mustRoot(ctx, nil, s.tools.nft, "delete", "table", "inet", nftTable); err != nil {
			return err
		}
	}
	return s.mustRoot(ctx, strings.NewReader(nftRuleset(s.UID(), port)), s.tools.nft, "-f", "-")
}

// Stage replaces the work tree with the tar stream tree (for example the GitHub tarball of the head SHA, which holds
// no .git). It extracts as the sandbox user, so a hostile archive can write only where that user can.
func (s *Sandbox) Stage(ctx context.Context, tree io.Reader, stripComponents int) error {
	if s.UID() < 0 {
		return ErrNotSetup
	}
	if stripComponents < 0 || stripComponents > 4 {
		return fmt.Errorf("%w: strip components %d", ErrBadJob, stripComponents)
	}
	if err := s.resetWork(ctx); err != nil {
		return err
	}
	sctx, cancel := context.WithTimeout(ctx, stageTimeout)
	defer cancel()
	unit := s.nextUnit()
	var out bytes.Buffer
	lw := &limitWriter{w: &out, limit: MaxOutputBytes}
	code, err := s.exec.Run(sctx, Cmd{
		Argv: append([]string{s.tools.sudo, "-n", "--"}, s.unitArgv(PhaseExec, unit, stageTimeout, WorkDir, fixedPathEnv(),
			s.tools.tar, "-x", "-f", "-", "-C", WorkDir, "--no-same-owner", "--no-same-permissions",
			"--exclude=.git", "--strip-components="+strconv.Itoa(stripComponents))...),
		Env: fixedPathEnv(), Stdin: tree, Stdout: lw, Stderr: lw,
		Cancel: func() error { return s.killUnit(context.WithoutCancel(ctx), unit) },
	})
	if err == nil && code != 0 {
		err = fmt.Errorf("%w: tar exited %d: %s", ErrSetupStep, code, strings.TrimSpace(out.String()))
	}
	if kerr := s.KillAll(context.WithoutCancel(ctx)); kerr != nil {
		return errors.Join(err, kerr)
	}
	return err
}

// serviceProps is the fixed hardening of the transient service of a phase. Nothing from a Job enters it: the job's
// argv, directory and variables go after the "--" of systemd-run, to env(1). Both phases hide /run (every host
// socket of systemd, D-Bus, snapd, polkit and ssh lives there, and /var/run links to it), the home directories and
// /tmp, and make the whole file system read-only except the three sandbox directories. The execution phase has a
// private network namespace with loopback only, which also cuts abstract Unix sockets. The fetch phase keeps the
// host network for the proxy and may open only IPv4 and IPv6 sockets, so no Unix socket of any kind.
func serviceProps(phase Phase) []string {
	props := []string{
		"User=" + User,
		"Group=" + User,
		"NoNewPrivileges=yes",
		"CapabilityBoundingSet=",
		"AmbientCapabilities=",
		"ProtectSystem=strict",
		"ReadWritePaths=" + WorkDir + " " + HomeDir + " " + TmpDir,
		"ProtectHome=yes",
		// A fresh tmpfs on /dev/shm: PrivateDevices= binds the host's /dev/shm into the new /dev (run 44909950640
		// read a 0644 canary there from both phases).
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
	switch phase {
	case PhaseExec:
		props = append(props, "PrivateNetwork=yes")
	case PhaseFetch:
		props = append(props, "RestrictAddressFamilies=AF_INET AF_INET6")
	}
	return props
}

// runtimeMaxSec is the job deadline in whole seconds, rounded up, at least 1.
func runtimeMaxSec(d time.Duration) string {
	sec := int64((d + time.Second - 1) / time.Second)
	if sec < 1 {
		sec = 1
	}
	return strconv.FormatInt(sec, 10)
}

// unitArgv is the root command of one sandbox job: a transient service with the phase's hardening, the limits and
// the deadline, running env -i with only env and then argv. --wait and --pipe keep the caller attached to the
// job's stdio and exit status. --expand-environment=no keeps every argv element literal: systemd does not expand
// "$X" or "${X}" in it.
func (s *Sandbox) unitArgv(phase Phase, unit string, timeout time.Duration, dir string, env []string, argv ...string) []string {
	l := s.cfg.Limits
	out := []string{s.tools.systemdRun, "--wait", "--pipe", "--collect", "--quiet", "--expand-environment=no",
		"--service-type=exec", "--unit=" + unit}
	for _, p := range serviceProps(phase) {
		out = append(out, "-p", p)
	}
	out = append(out,
		"-p", "CPUQuota="+strconv.Itoa(l.CPUPercent)+"%",
		"-p", "MemoryMax="+strconv.FormatInt(l.MemoryMax, 10),
		"-p", "TasksMax="+strconv.Itoa(l.TasksMax),
		"-p", "RuntimeMaxSec="+runtimeMaxSec(timeout),
		"--", s.tools.env, "-i", "-C", dir, "--")
	out = append(out, env...)
	return append(out, argv...)
}

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// reservedEnv reports the keys the sandbox sets itself. A job cannot replace them.
func reservedEnv(k string) bool {
	switch strings.ToUpper(k) {
	case "PATH", "HOME", "TMPDIR", "USER", "LOGNAME", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "ALL_PROXY":
		return true
	}
	return false
}

// relPath checks a path that must stay inside the work tree.
func relPath(p string) (string, error) {
	if p == "" {
		return ".", nil
	}
	c := path.Clean(p)
	if path.IsAbs(p) || c == ".." || strings.HasPrefix(c, "../") || strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("%w: path %q leaves the work tree", ErrBadJob, p)
	}
	return c, nil
}

func validateJob(j Job) error {
	if err := validateArgv(j.Argv); err != nil {
		return err
	}
	if err := validateEnv(j.Env); err != nil {
		return err
	}
	if j.Timeout <= 0 {
		return fmt.Errorf("%w: timeout must be positive", ErrBadJob)
	}
	if _, err := relPath(j.Dir); err != nil {
		return err
	}
	if j.Result == ResultJUnit {
		if _, err := relPath(j.ResultPath); err != nil || j.ResultPath == "" {
			return fmt.Errorf("%w: JUnit result path %q", ErrBadJob, j.ResultPath)
		}
	}
	return nil
}

func validateArgv(argv []string) error {
	if len(argv) == 0 || argv[0] == "" {
		return fmt.Errorf("%w: empty argv", ErrBadJob)
	}
	// env(1) would read a first word with "=" as a variable and a leading "-" as an option.
	if strings.Contains(argv[0], "=") || strings.HasPrefix(argv[0], "-") {
		return fmt.Errorf("%w: argv[0] %q", ErrBadJob, argv[0])
	}
	for _, a := range argv {
		if strings.ContainsRune(a, 0) {
			return fmt.Errorf("%w: NUL in argv", ErrBadJob)
		}
	}
	return nil
}

func validateEnv(env []string) error {
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !envKey.MatchString(k) || reservedEnv(k) || strings.ContainsRune(v, 0) {
			return fmt.Errorf("%w: env entry %q", ErrBadJob, k)
		}
	}
	return nil
}

// jobEnv is the whole environment of a job: the fixed set, the phase's proxy variables, then the job's own.
func (s *Sandbox) jobEnv(phase Phase, j Job) []string {
	env := []string{"PATH=" + s.cfg.ToolPath, "HOME=" + HomeDir, "TMPDIR=" + TmpDir, "USER=" + User, "LOGNAME=" + User}
	if phase == PhaseFetch {
		proxy := "http://127.0.0.1:" + strconv.Itoa(s.ProxyPort())
		env = append(env, "HTTPS_PROXY="+proxy, "HTTP_PROXY="+proxy, "https_proxy="+proxy, "http_proxy="+proxy)
	}
	return append(env, j.Env...)
}

// jobArgv is the full root command of a job.
func (s *Sandbox) jobArgv(phase Phase, unit string, j Job) []string {
	dir, _ := relPath(j.Dir)
	return s.unitArgv(phase, unit, j.Timeout, path.Join(WorkDir, dir), s.jobEnv(phase, j), j.Argv...)
}

func (s *Sandbox) nextUnit() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	return "heimdall-sbx-" + strconv.Itoa(s.seq) + ".service"
}

// Fetch runs a dependency install with egress only through the proxy.
func (s *Sandbox) Fetch(ctx context.Context, j Job) (Result, error) { return s.run(ctx, PhaseFetch, j) }

// Exec runs tests with loopback only.
func (s *Sandbox) Exec(ctx context.Context, j Job) (Result, error) { return s.run(ctx, PhaseExec, j) }

func (s *Sandbox) run(ctx context.Context, phase Phase, j Job) (Result, error) {
	if s.UID() < 0 || s.ProxyPort() == 0 {
		return Result{}, ErrNotSetup
	}
	if err := validateJob(j); err != nil {
		return Result{}, err
	}
	unit := s.nextUnit()
	jctx, cancel := context.WithTimeout(ctx, j.Timeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := Cmd{
		Argv:   append([]string{s.tools.sudo, "-n", "--"}, s.jobArgv(phase, unit, j)...),
		Env:    fixedPathEnv(),
		Stdout: &limitWriter{w: &stdout, limit: MaxOutputBytes},
		Stderr: &limitWriter{w: &stderr, limit: MaxOutputBytes},
		Cancel: func() error { return s.killUnit(context.WithoutCancel(ctx), unit) },
	}
	code, runErr := s.exec.Run(jctx, cmd)
	res := Result{Exit: code, TimedOut: jctx.Err() != nil && ctx.Err() == nil, Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	// After each job: no process of the uid may survive.
	killErr := s.KillAll(context.WithoutCancel(ctx))
	if runErr != nil || killErr != nil {
		return res, errors.Join(runErr, killErr)
	}
	rep, err := s.report(ctx, j, res.Stdout)
	if err != nil {
		return res, err
	}
	res.Report = rep
	return res, nil
}

func (s *Sandbox) report(ctx context.Context, j Job, stdout []byte) (verify.Report, error) {
	switch j.Result {
	case ResultNone:
		return verify.Report{}, nil
	case ResultGoTestJSON:
		rep, _, err := testevents.ParseEvents(bytes.NewReader(stdout))
		if err != nil {
			return verify.Report{}, fmt.Errorf("parse go test events: %w", err)
		}
		return rep, nil
	case ResultJUnit:
		data, err := s.readAsUser(ctx, j.ResultPath)
		if err != nil {
			return verify.Report{}, err
		}
		rep, err := verify.ParseBytes(data)
		if err != nil {
			return verify.Report{}, fmt.Errorf("parse junit: %w", err)
		}
		return rep, nil
	}
	return verify.Report{}, fmt.Errorf("%w: result format %d", ErrBadJob, j.Result)
}

// readAsUser reads a file of the work tree as the sandbox user, so a symlink planted by the PR cannot make root read
// a file the user could not. At most verify.MaxReportBytes+1 bytes are kept, so ParseBytes refuses an oversize file.
func (s *Sandbox) readAsUser(ctx context.Context, rel string) ([]byte, error) {
	p, err := relPath(rel)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	rctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	unit := s.nextUnit()
	argv := append([]string{s.tools.sudo, "-n", "--"}, s.unitArgv(PhaseExec, unit, readTimeout, WorkDir, fixedPathEnv(),
		s.tools.head, "-c", strconv.Itoa(verify.MaxReportBytes+1), "--", path.Join(WorkDir, p))...)
	code, err := s.exec.Run(rctx, Cmd{
		Argv: argv, Env: fixedPathEnv(), Stdout: &limitWriter{w: &out, limit: verify.MaxReportBytes + 1},
		Cancel: func() error { return s.killUnit(context.WithoutCancel(ctx), unit) },
	})
	if err != nil {
		return nil, fmt.Errorf("read result: %w", err)
	}
	if kerr := s.KillAll(context.WithoutCancel(ctx)); kerr != nil {
		return nil, kerr
	}
	if code != 0 {
		return nil, fmt.Errorf("%w: result file %q not readable (exit %d)", ErrBadJob, rel, code)
	}
	return out.Bytes(), nil
}

// killUnit kills every process of a job's service, then every process of the uid.
func (s *Sandbox) killUnit(ctx context.Context, unit string) error {
	_, _, err := s.root(ctx, nil, s.tools.systemctl, "kill", "--signal=SIGKILL", "--", unit)
	if kerr := s.KillAll(ctx); kerr != nil {
		return errors.Join(err, kerr)
	}
	return err
}

// KillAll kills every process of the sandbox uid and checks that none is left. A process that forks while the kill
// runs is caught by the next round; after killRounds rounds a survivor is an error.
func (s *Sandbox) KillAll(ctx context.Context) error {
	if s.UID() <= 0 {
		return ErrNotSetup
	}
	uid := strconv.Itoa(s.UID())
	for range killRounds {
		if _, _, err := s.root(ctx, nil, s.tools.pkill, "-KILL", "-U", uid); err != nil {
			return fmt.Errorf("pkill: %w", err)
		}
		if _, _, err := s.root(ctx, nil, s.tools.pkill, "-KILL", "-u", uid); err != nil {
			return fmt.Errorf("pkill: %w", err)
		}
		code, _, err := s.root(ctx, nil, s.tools.pgrep, "-U", uid)
		if err != nil {
			return fmt.Errorf("pgrep: %w", err)
		}
		if code == 1 {
			code, _, err = s.root(ctx, nil, s.tools.pgrep, "-u", uid)
			if err != nil {
				return fmt.Errorf("pgrep: %w", err)
			}
		}
		if code == 1 {
			return nil
		}
	}
	return fmt.Errorf("%w: uid %s", ErrSurvivor, uid)
}

// Cleanup kills the uid, removes its crontab and the nftables table, and stops the proxy.
func (s *Sandbox) Cleanup(ctx context.Context) error {
	var errs []error
	if s.UID() > 0 {
		errs = append(errs, s.KillAll(ctx))
		if s.tools.crontab != "" {
			// Exit 1 means "no crontab", which is the expected state.
			_, _, err := s.root(ctx, nil, s.tools.crontab, "-r", "-u", User)
			errs = append(errs, err)
		}
	}
	_, _, err := s.root(ctx, nil, s.tools.nft, "delete", "table", "inet", nftTable)
	errs = append(errs, err)
	_, _, err = s.root(ctx, nil, append([]string{s.tools.rm, "-f", "--"}, hostMarkers()...)...)
	errs = append(errs, err)
	s.mu.Lock()
	p := s.proxy
	s.proxy = nil
	s.mu.Unlock()
	if p != nil {
		errs = append(errs, p.Close())
	}
	return errors.Join(errs...)
}

type limitWriter struct {
	w     io.Writer
	limit int64
	n     int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n >= l.limit {
		return len(p), nil
	}
	chunk := p
	if int64(len(chunk)) > l.limit-l.n {
		chunk = chunk[:l.limit-l.n]
	}
	n, err := l.w.Write(chunk)
	l.n += int64(n)
	if err != nil {
		return n, fmt.Errorf("bounded write: %w", err)
	}
	return len(p), nil
}
