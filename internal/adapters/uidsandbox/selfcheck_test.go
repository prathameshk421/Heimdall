package uidsandbox

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goodReport is what the self check script prints in a correctly hardened service of the phase, for uid 1500.
func goodReport(phase Phase) string {
	b := strings.Builder{}
	b.WriteString("Uid:\t1500\t1500\t1500\t1500\n")
	b.WriteString("Gid:\t1400\t1400\t1400\t1400\n")
	b.WriteString("Groups:\t1400\n")
	b.WriteString("NoNewPrivs:\t1\n")
	b.WriteString("Seccomp:\t2\n")
	for _, c := range []string{"CapPrm", "CapEff", "CapBnd", "CapAmb"} {
		b.WriteString(c + ":\t0000000000000000\n")
	}
	b.WriteString("netdev Inter-|   Receive                                                |  Transmit\n")
	b.WriteString("netdev  face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets\n")
	b.WriteString("netdev     lo:       0       0    0    0    0     0          0         0        0       0\n")
	if phase == PhaseFetch {
		b.WriteString("netdev   eth0: 123456    789    0    0    0     0          0         0    98765     432\n")
	}
	b.WriteString("workdir ok\n")
	b.WriteString("end\n")
	return b.String()
}

func TestSelfReportAccepted(t *testing.T) {
	for _, phase := range []Phase{PhaseExec, PhaseFetch} {
		require.NoError(t, checkSelfReport(phase, 1500, goodReport(phase)), "phase %d", phase)
	}
}

// Each broken report is a hardening that systemd skipped or weakened without an error. Each must refuse the runner.
func TestSelfReportRefused(t *testing.T) {
	replace := func(phase Phase, old, with string) string {
		r := goodReport(phase)
		require.Contains(t, r, old)
		return strings.Replace(r, old, with, 1)
	}
	cases := map[string]struct {
		phase  Phase
		report string
		reason string
	}{
		"exec has the host network": {PhaseExec, replace(PhaseExec, "end\n",
			"netdev   eth0: 1 1 0 0 0 0 0 0 1 1\nend\n"), "eth0"},
		"exec has no interface list": {PhaseExec, strings.ReplaceAll(goodReport(PhaseExec),
			"netdev     lo:       0       0    0    0    0     0          0         0        0       0\n", ""), "loopback"},
		"no seccomp (address families and the other filters skipped)": {PhaseFetch,
			replace(PhaseFetch, "Seccomp:\t2", "Seccomp:\t0"), "Seccomp"},
		"no seccomp in exec":     {PhaseExec, replace(PhaseExec, "Seccomp:\t2", "Seccomp:\t0"), "Seccomp"},
		"no no-new-privs":        {PhaseExec, replace(PhaseExec, "NoNewPrivs:\t1", "NoNewPrivs:\t0"), "NoNewPrivs"},
		"a capability":           {PhaseFetch, replace(PhaseFetch, "CapEff:\t0000000000000000", "CapEff:\t0000000000003000"), "CapEff"},
		"a bounding capability":  {PhaseExec, replace(PhaseExec, "CapBnd:\t0000000000000000", "CapBnd:\t000001ffffffffff"), "CapBnd"},
		"an ambient capability":  {PhaseExec, replace(PhaseExec, "CapAmb:\t0000000000000000", "CapAmb:\t0000000000000400"), "CapAmb"},
		"a permitted capability": {PhaseExec, replace(PhaseExec, "CapPrm:\t0000000000000000", "CapPrm:\t0000000000000001"), "CapPrm"},
		"runs as root":           {PhaseExec, replace(PhaseExec, "Uid:\t1500\t1500\t1500\t1500", "Uid:\t0\t0\t0\t0"), "Uid"},
		"saved uid is root":      {PhaseExec, replace(PhaseExec, "Uid:\t1500\t1500\t1500\t1500", "Uid:\t1500\t1500\t0\t1500"), "Uid"},
		"a host path is visible": {PhaseExec, replace(PhaseExec, "end\n", "visible /run/systemd\nend\n"), "/run/systemd"},
		"the fetch phase sees the runner home": {PhaseFetch,
			replace(PhaseFetch, "end\n", "visible /home/runner\nend\n"), "/home/runner"},
		"a field is missing":   {PhaseExec, strings.Replace(goodReport(PhaseExec), "CapAmb:\t0000000000000000\n", "", 1), "CapAmb"},
		"a field is repeated":  {PhaseExec, replace(PhaseExec, "end\n", "NoNewPrivs:\t0\nend\n"), "NoNewPrivs"},
		"the script was cut":   {PhaseExec, strings.TrimSuffix(goodReport(PhaseExec), "end\n"), "end"},
		"an unknown line":      {PhaseExec, replace(PhaseExec, "end\n", "hello\nend\n"), "hello"},
		"workdir unwritable":   {PhaseExec, replace(PhaseExec, "workdir ok\n", "workdir unwritable\n"), "not writable"},
		"workdir line missing": {PhaseExec, strings.Replace(goodReport(PhaseExec), "workdir ok\n", "", 1), "not writable"},
		"empty output":         {PhaseFetch, "", "end"},
		"wrong phase argument": {Phase(9), goodReport(PhaseExec), "phase"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := checkSelfReport(c.phase, 1500, c.report)
			require.ErrorIs(t, err, ErrCannotIsolate)
			assert.Contains(t, err.Error(), c.reason)
		})
	}
}

func TestSelfCheckArgv(t *testing.T) {
	f := &fakeExec{answer: readyAnswer}
	s := setupSandbox(t, f)
	var checks [][]string
	for _, c := range f.calls {
		if c[3] == "/usr/bin/systemd-run" {
			checks = append(checks, c)
		}
	}
	require.Len(t, checks, 2, "one self check per phase")
	for i, phase := range []Phase{PhaseExec, PhaseFetch} {
		head, tail := unitHead(t, checks[i])
		assert.Equal(t, wantHead(phase, "heimdall-sbx-"+strconv.Itoa(i+1)+".service", "60", "/sbx/work"), head)
		assert.Equal(t, append([]string{"/usr/bin/sh", "-c", selfCheckScript, "sh"}, hiddenPaths(s.cfg)...), tail,
			"the self check runs the fixed script with the fixed paths as arguments, nothing else")
	}
	assert.Contains(t, hiddenPaths(s.cfg), "/home/runner")
	assert.Contains(t, hiddenPaths(s.cfg), "/home/runner/work/_temp")
	assert.Contains(t, hiddenPaths(s.cfg), "/run/systemd")
	assert.Contains(t, hiddenPaths(s.cfg), "/proc/1")
	for _, m := range []string{"/tmp/heimdall-sbx-host-marker", "/var/tmp/heimdall-sbx-host-marker", "/dev/shm/heimdall-sbx-host-marker"} {
		assert.Contains(t, hiddenPaths(s.cfg), m, "a marker Setup leaves in a shared host directory must be invisible inside")
	}
}

// Run 44909950640: the host's /dev/shm was visible in both phases. Its marker in the report refuses the runner.
func TestSelfReportRefusesSharedDevShm(t *testing.T) {
	r := strings.Replace(goodReport(PhaseFetch), "end\n", "visible /dev/shm/heimdall-sbx-host-marker\nend\n", 1)
	err := checkSelfReport(PhaseFetch, 1500, r)
	require.ErrorIs(t, err, ErrCannotIsolate)
	assert.Contains(t, err.Error(), "/dev/shm/heimdall-sbx-host-marker")
}

// A service that ran but reports weaker isolation than asked makes Setup fail with ErrCannotIsolate.
func TestSetupRefusesWeakService(t *testing.T) {
	f := &fakeExec{answer: func(argv []string) (int, string) {
		if isSelfCheck(argv) && hasProp(argv, "PrivateNetwork=yes") {
			return 0, strings.Replace(goodReport(PhaseExec), "end\n", "netdev   eth0: 1 1 0 0 0 0 0 0 1 1\nend\n", 1)
		}
		return readyAnswer(argv)
	}}
	s := newSandbox(testConfig(), f, testTools())
	err := s.Setup(t.Context())
	require.ErrorIs(t, err, ErrCannotIsolate)
	assert.Contains(t, err.Error(), "eth0")
	_ = s.Cleanup(t.Context())
}

// isSelfCheck reports whether a recorded command is a self check service.
func isSelfCheck(argv []string) bool {
	return len(argv) > 3 && argv[3] == "/usr/bin/systemd-run" && slices.Contains(argv, selfCheckScript)
}

// hasProp reports whether a recorded systemd-run command sets the property p.
func hasProp(argv []string, p string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "-p" && argv[i+1] == p {
			return true
		}
	}
	return false
}

// selfCheckAnswer answers a self check with the good report of its phase.
func selfCheckAnswer(argv []string) (int, string) {
	if hasProp(argv, "PrivateNetwork=yes") {
		return 0, goodReport(PhaseExec)
	}
	return 0, goodReport(PhaseFetch)
}
