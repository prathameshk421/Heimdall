// Package uidsandbox runs PR code on a GitHub-hosted Linux runner as a separate user, heimdall-sbx, so the Action
// mode loop can keep the model key in a process that PR code cannot read (ACTION_MODE.md 4.4, WP AM11).
//
// # Purpose
//
// The proof run must run tests the model wrote and the PR's own code. The loop that holds the key runs as the runner
// user. Every command that touches PR data runs through this package as heimdall-sbx, in a transient systemd
// service with no sudo rights, no supplementary groups, no capabilities, NoNewPrivileges, /run, the home
// directories and /tmp hidden, a read-only file system outside /sbx, an environment built only from fixed keys, and
// network access cut by phase. A runner that cannot do this returns ErrCannotIsolate, and the caller reports
// "unknown: runner cannot isolate PR code". There is no fallback to the runner user (R4).
//
// # API
//
//   - New(ctx, Config, Exec) checks the runner (Linux, systemd as init, passwordless root, nftables, every tool at a
//     fixed path) and returns a *Sandbox or a *CannotIsolateError. Off Linux it always returns that error.
//   - Sandbox.Setup checks the limits, creates the user, sets the runner home and _temp to 0700, denies cron and at,
//     makes /sbx/home, /sbx/tmp and /sbx/work, starts the fetch proxy, loads the nftables table, then runs the self
//     check (selfcheck.go) in one service of each phase. A systemd that rejects a property, or a service that started
//     with less isolation than asked, returns a *CannotIsolateError there, so no job runs on a weak service.
//   - Sandbox.Stage extracts a tar stream (the tarball of the head SHA, which holds no .git) into /sbx/work, as the
//     sandbox user, in an execution-phase service.
//   - Sandbox.Fetch runs a job in the fetch phase: host network, but nftables drops every packet of the uid except
//     TCP to the proxy on 127.0.0.1, and RestrictAddressFamilies=AF_INET AF_INET6 refuses every other socket family,
//     so no Unix socket (path or abstract) can even be created. The proxy (StartProxy, Allowlist) accepts only
//     CONNECT to an allowlisted registry host on an allowed port, refuses IP literals, and refuses a name that
//     resolves to a non-public address.
//   - Sandbox.Exec runs a job in the execution phase: PrivateNetwork=yes, so a private namespace with only lo, which
//     also cuts abstract Unix sockets.
//   - Every job is systemd-run --wait --pipe --collect --expand-environment=no --service-type=exec with the fixed
//     properties of serviceProps(phase), then CPUQuota, MemoryMax, TasksMax and RuntimeMaxSec (the job's Timeout,
//     rounded up), then env -i with the fixed keys. Nothing from a Job becomes a property. After every job KillAll
//     kills every process of the uid and fails if one is left.
//   - Result holds the exit status, bounded stdout and stderr, and a verify.Report: ResultGoTestJSON parses stdout
//     with the shared testevents parser, ResultJUnit reads the file as the sandbox user and uses verify.ParseBytes.
//   - Sandbox.Cleanup kills the uid, removes its crontab and the nftables table, and stops the proxy.
//
// The agentloop tools (run_test, run_check) are not here: adapters may not import app, so the wiring in AM14 wraps
// Fetch and Exec in agentloop.Tool values.
//
// # Invariants
//
//   - Commands go through the Exec port as explicit argv. argv[0] is an absolute path found in /usr/sbin, /usr/bin,
//     /sbin or /bin. PR data is never joined into a shell string. A job argv[0] may not contain "=" or start with
//     "-", because env(1) would read it as a variable or an option.
//   - A job cannot set PATH, HOME, TMPDIR, USER, LOGNAME or any proxy variable. Dir and ResultPath stay inside
//     /sbx/work.
//   - Every root command runs through sudo -n, so a password prompt is an error, never a hang.
//   - Files of the work tree are read as the sandbox user, so a symlink planted by the PR cannot make root read for it.
//   - Output is capped at MaxOutputBytes per stream, and a JUnit file at verify.MaxReportBytes.
//
// # Failure modes and traps
//
//   - A network namespace alone does not cut Unix sockets on the file system. Run 44868368873 (the scope design) let
//     resolvectl reach the host's systemd-resolved through /run/systemd/resolve/io.systemd.Resolve from both phases,
//     and the fetch phase reached the system D-Bus, snapd, polkit and the local ssh socket. That is why both phases
//     hide /run with TemporaryFileSystem=/run (/var/run links to it), hide /tmp with PrivateTmp and the homes with
//     ProtectHome, and why the fetch phase may open no AF_UNIX socket. Run 44910812111 proved the fix (0 of 77 host
//     sockets reachable from the execution phase, 0 of 79 from the fetch phase). Do not remove one of these
//     properties to make a tool work; a tool that needs one is an owner decision.
//   - PrivateDevices= binds the host's /dev/shm into the service's new /dev. Run 44909950640 read a 0644 canary of
//     the runner user there from both phases. That is why /dev/shm gets its own tmpfs (TemporaryFileSystem=/run
//     /dev/shm:mode=1777), and why Setup leaves a root-owned marker in the host's /tmp, /var/tmp and /dev/shm for
//     the self check to look for.
//   - A scope takes only resource and kill properties. That is why jobs run as a service, not a scope, and why
//     setpriv is gone: User=, NoNewPrivileges= and an empty CapabilityBoundingSet= do its work.
//   - systemd-run expands "$X" and "${X}" in the command line unless --expand-environment=no. It is always passed,
//     so PR argv stays literal.
//   - The fetch phase shares the host network namespace, so its /proc/net/unix lists the host's socket names (118
//     entries in run 44910812111). None can be reached: socket(AF_UNIX) fails with EAFNOSUPPORT.
//   - A real package manager that needs AF_UNIX in the fetch phase fails there. Only Go's was tried (it works).
//   - The runner user cannot signal the root sudo process. A cancelled job is killed through sudo: systemctl kill on
//     its scope, then pkill on the uid, in rounds until pgrep finds nothing.
//   - The runner's own credential files live under the home (/home/runner/actions-runner/cached/<version>). Only
//     the home's mode protects them; Setup sets it to 0700 (the image ships 0750).
//   - The fetch phase can still send data to an allowed registry, for example a module path that makes the Go proxy
//     contact another host. The sandbox user reads no secret, so this carries only what the PR already has.
//   - ProtectProc=invisible hides every process of another uid, so the loop's /proc/<pid>/cmdline is out of reach
//     (probed with the canary in the loop's argv). The loop must still never put the key in argv: a runner whose
//     kernel ignores the property would show it.
//   - systemd skips PrivateNetwork= on a kernel without network namespaces, and RestrictAddressFamilies= and the
//     other seccomp properties on a kernel without seccomp, and the service still starts. Exit status 0 is no proof.
//     The self check reads what the service got (uid, NoNewPrivs, every capability set, Seccomp mode 2, only lo in
//     the execution phase, and no /run/systemd, /run/dbus, /run/user, /proc/1, runner home, _temp or host marker)
//     and refuses the runner on any doubt. This is the rule for an escape that cannot be closed: the sandbox refuses to run, and the
//     proof run reports unknown.
//
// # Tests
//
//   - proxy_test.go: the allowlist (case, trailing dot, IP literals in every form, ports, labels, suffix tricks),
//     the CONNECT line parser (bad lines, other methods, userinfo), the public-address guard, and the proxy end to
//     end on loopback (tunnel, each refusal never dials, a private resolution is refused, Close ends tunnels).
//   - selfcheck_test.go: the self check report is accepted per phase and refused for each weakness (host network in
//     the execution phase, no seccomp, no no-new-privs, each capability set, a root uid, a visible host path, a
//     missing, repeated or unknown line, a cut output), its exact argv, and Setup failing on a weak service.
//   - sandbox_test.go: with a fake Exec, the setup commands and the nftables rules, the self check per phase, a
//     refused self check is ErrCannotIsolate, a zero limit is refused, a user with extra groups is refused, the exact
//     property list of each phase, that no hostile argv, Dir or Env changes the command up to env's "--", the full
//     argv chain of an execution job, the deadline rounding, the fetch job's proxy variables, job validation, a
//     survivor after the kill, the JUnit file read in the hardened service, jobs before Setup.
//   - probe_linux_test.go (tags linux, sbxprobe) is the escape probe harness. It runs only on a throwaway hosted
//     runner, with the probe module in testdata/probe. A probe that cannot make its attempt is UNKNOWN, and UNKNOWN
//     fails the harness. Evidence: run 44910812111 (Nov 21 2026, ubuntu-latest = Ubuntu 26.04.1, systemd 259.9,
//     kernel 7.0.0-1016-aws): the self check passed in both phases, 34 fetch and 36 execution probe tests blocked
//     their escapes with 0 unknown, the 45 s hang job ended at 45 s with no survivor, no plant reached any of 39
//     host world-writable directories, the journal holds no sandbox line, and the allowed fetch worked. Run
//     44909950640 (Azure kernel) found the /dev/shm escape above; every other probe was blocked there too. Runs
//     44868233309 and 44868368873 were the scope design. Details are in ACTION_MODE_SPIKE.md section 9.
package uidsandbox
