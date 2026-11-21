// Package uidsandbox runs PR code on a GitHub-hosted Linux runner as a separate user, heimdall-sbx, so the Action
// mode loop can keep the model key in a process that PR code cannot read (ACTION_MODE.md 4.4, WP AM11).
//
// # Purpose
//
// The proof run must run tests the model wrote and the PR's own code. The loop that holds the key runs as the runner
// user. Every command that touches PR data runs through this package as heimdall-sbx: with no sudo rights, no
// supplementary groups, no capabilities, NoNewPrivs set, an environment built only from fixed keys, and network
// access cut by phase. A runner that cannot do this returns ErrCannotIsolate, and the caller reports
// "unknown: runner cannot isolate PR code". There is no fallback to the runner user (R4).
//
// # API
//
//   - New(ctx, Config, Exec) checks the runner (Linux, systemd as init, passwordless root, nftables, every tool at a
//     fixed path) and returns a *Sandbox or a *CannotIsolateError. Off Linux it always returns that error.
//   - Sandbox.Setup creates the user, sets the runner home and _temp to 0700, denies cron and at, makes /sbx/home,
//     /sbx/tmp and /sbx/work, starts the fetch proxy, loads the nftables table and creates the network namespace.
//   - Sandbox.Stage extracts a tar stream (the tarball of the head SHA, which holds no .git) into /sbx/work, as the
//     sandbox user.
//   - Sandbox.Fetch runs a job in the fetch phase: host network, but nftables drops every packet of the uid except
//     TCP to the proxy on 127.0.0.1. The proxy (StartProxy, Allowlist) accepts only CONNECT to an allowlisted
//     registry host on an allowed port, refuses IP literals, and refuses a name that resolves to a non-public address.
//   - Sandbox.Exec runs a job in the execution phase: inside network namespace heimdall-sbx with only lo.
//   - Both run under systemd-run --scope with CPUQuota, MemoryMax and TasksMax, then setpriv (uid, gid, no groups,
//     no-new-privs, no inheritable or bounding capabilities), then env -i with the fixed keys. The deadline is the
//     job's Timeout. After every job KillAll kills every process of the uid and fails if one is left.
//   - Result holds the exit status, bounded stdout and stderr, and a verify.Report: ResultGoTestJSON parses stdout
//     with the shared testevents parser, ResultJUnit reads the file as the sandbox user and uses verify.ParseBytes.
//   - Sandbox.Cleanup kills the uid, removes its crontab, the nftables table and the namespace, and stops the proxy.
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
//   - KNOWN ESCAPE, not fixed (run 44868368873, Nov 21 2026): a network namespace does not cut Unix sockets on the
//     file system. From the execution phase, resolvectl query example.com was answered by the host's
//     systemd-resolved through /run/systemd/resolve/io.systemd.Resolve. That is a DNS channel out of the execution
//     phase, and a way around the proxy allowlist in the fetch phase. The fetch phase also reached the system D-Bus,
//     snapd, polkit and the local ssh socket. The fix needs a mount namespace that hides /run, or a transient
//     systemd service (not a scope) with InaccessiblePaths. That is an owner decision; see ACTION_MODE_SPIKE.md 9.
//   - systemd scope units take only resource and kill properties, so NoNewPrivileges comes from setpriv, not from
//     the scope.
//   - The runner user cannot signal the root sudo process. A cancelled job is killed through sudo: systemctl kill on
//     its scope, then pkill on the uid, in rounds until pgrep finds nothing.
//   - The runner's own credential files live under the home (/home/runner/actions-runner/cached/<version>). Only
//     the home's mode protects them; Setup sets it to 0700 (the image ships 0750).
//   - The fetch phase can still send data to an allowed registry, for example a module path that makes the Go proxy
//     contact another host. The sandbox user reads no secret, so this carries only what the PR already has.
//   - /tmp, /var/tmp and /dev/shm stay shared and world-writable. Jobs get TMPDIR=/sbx/tmp, but a hostile job can
//     still plant files there for later runner steps.
//   - /proc/<pid>/cmdline of every process is readable by the sandbox user. The loop must never put the key in argv.
//
// # Tests
//
//   - proxy_test.go: the allowlist (case, trailing dot, IP literals in every form, ports, labels, suffix tricks),
//     the CONNECT line parser (bad lines, other methods, userinfo), the public-address guard, and the proxy end to
//     end on loopback (tunnel, each refusal never dials, a private resolution is refused, Close ends tunnels).
//   - sandbox_test.go: with a fake Exec, the setup commands and the nftables rules, a user with extra groups is
//     refused, the full argv chain of an execution job, the fetch job's proxy variables, job validation, a survivor
//     after the kill, the JUnit file read as the sandbox user, jobs before Setup.
//   - probe_linux_test.go (tags linux, sbxprobe) is the escape probe harness. It runs only on a throwaway hosted
//     runner, with the probe module in testdata/probe. Evidence: run 44868233309 (Nov 21 2026, ubuntu-latest =
//     Ubuntu 26.04.1, systemd 259, nftables 1.1.6): all 40 escape attempts in 21 probe tests were blocked, and the
//     allowed fetch worked. Run
//     44868368873 added host-resolver and Unix socket probes and found the escape above. Details are in
//     ACTION_MODE_SPIKE.md section 9.
package uidsandbox
