package uidsandbox

import (
	"fmt"
	"strconv"
	"strings"
)

// selfCheckScript runs in one hardened service of each phase during Setup. It uses only shell builtins, so it needs
// no tool inside the service. It prints the privilege lines of /proc/self/status, the interface table of the
// service's network namespace, one "visible" line for each argument path that exists inside, and "end".
//
// systemd skips some properties without an error when the kernel lacks the feature: PrivateNetwork= without network
// namespaces, RestrictAddressFamilies= and the other filters without seccomp. An exit status of 0 therefore proves
// little. checkSelfReport reads what the service really got.
const selfCheckScript = `while IFS= read -r l; do
  case "$l" in Uid:*|Gid:*|Groups:*|NoNewPrivs:*|Seccomp:*|CapPrm:*|CapEff:*|CapBnd:*|CapAmb:*) printf '%s\n' "$l";; esac
done < /proc/self/status
while IFS= read -r l; do printf 'netdev %s\n' "$l"; done < /proc/net/dev
for p in "$@"; do if [ -e "$p" ] || [ -L "$p" ]; then printf 'visible %s\n' "$p"; fi; done
printf 'end\n'`

// hiddenPaths are host paths that no sandbox service may see: the host's systemd and D-Bus sockets under /run, the
// user runtime directories, init's /proc entry (ProtectProc=invisible), the runner home and _temp (ProtectHome=yes),
// and the markers Setup leaves in the host's /tmp, /var/tmp (PrivateTmp=yes) and /dev/shm (its own tmpfs).
func hiddenPaths(cfg Config) []string {
	return append([]string{"/run/systemd", "/run/dbus", "/run/user", "/proc/1", cfg.RunnerHome, cfg.RunnerTemp},
		hostMarkers()...)
}

func phaseName(p Phase) string {
	switch p {
	case PhaseExec:
		return "exec"
	case PhaseFetch:
		return "fetch"
	}
	return "phase " + strconv.Itoa(int(p))
}

// statusField reports the /proc/self/status lines the script prints. Gid and Groups are printed for the log only.
func statusField(k string) bool {
	switch k {
	case "Uid", "Gid", "Groups", "NoNewPrivs", "Seccomp", "CapPrm", "CapEff", "CapBnd", "CapAmb":
		return true
	}
	return false
}

// selfReport is the parsed output of selfCheckScript.
type selfReport struct {
	fields map[string]string
	ifaces []string
}

// checkSelfReport checks the output of selfCheckScript in a service of phase that runs as uid. Any doubt is a
// *CannotIsolateError: a missing, repeated or unknown line, a cut output, a capability, no seccomp, a host path that
// is visible, or (execution phase) any interface but loopback.
func checkSelfReport(phase Phase, uid int, out string) error {
	if phase != PhaseExec && phase != PhaseFetch {
		return refuse(phase, "unknown phase")
	}
	rep, err := parseSelfReport(phase, out)
	if err != nil {
		return err
	}
	if err := checkStatus(phase, uid, rep.fields); err != nil {
		return err
	}
	if phase != PhaseExec {
		return nil
	}
	if !contains(rep.ifaces, "lo") {
		return refuse(phase, "the interface list has no loopback")
	}
	for _, n := range rep.ifaces {
		if n != "lo" {
			return refuse(phase, "the private network namespace has interface "+n+" (PrivateNetwork= not in effect)")
		}
	}
	return nil
}

func parseSelfReport(phase Phase, out string) (selfReport, error) {
	rep := selfReport{fields: map[string]string{}}
	ended := false
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if ended {
			return rep, refuse(phase, fmt.Sprintf("output after end: %q", line))
		}
		switch {
		case line == "end":
			ended = true
		case strings.HasPrefix(line, "visible "):
			return rep, refuse(phase, "host path visible inside: "+strings.TrimPrefix(line, "visible "))
		case strings.HasPrefix(line, "netdev "):
			if name, ok := ifaceName(strings.TrimPrefix(line, "netdev ")); ok {
				rep.ifaces = append(rep.ifaces, name)
			}
		default:
			k, v, ok := strings.Cut(line, ":")
			if !ok || !statusField(k) {
				return rep, refuse(phase, fmt.Sprintf("unexpected line %q", line))
			}
			if _, dup := rep.fields[k]; dup {
				return rep, refuse(phase, "repeated field "+k)
			}
			rep.fields[k] = strings.TrimSpace(v)
		}
	}
	if !ended {
		return rep, refuse(phase, "the self check output has no end line")
	}
	return rep, nil
}

func checkStatus(phase Phase, uid int, fields map[string]string) error {
	for _, k := range []string{"Uid", "NoNewPrivs", "Seccomp", "CapPrm", "CapEff", "CapBnd", "CapAmb"} {
		if _, ok := fields[k]; !ok {
			return refuse(phase, "missing field "+k)
		}
	}
	ids := strings.Fields(fields["Uid"])
	if len(ids) != 4 {
		return refuse(phase, "Uid line "+fields["Uid"])
	}
	for _, id := range ids {
		if id != strconv.Itoa(uid) {
			return refuse(phase, fmt.Sprintf("Uid %q, want %d in all four", fields["Uid"], uid))
		}
	}
	if fields["NoNewPrivs"] != "1" {
		return refuse(phase, "NoNewPrivs is "+fields["NoNewPrivs"])
	}
	// 2 is filter mode: the seccomp filters of RestrictAddressFamilies= and the other Restrict properties loaded.
	if fields["Seccomp"] != "2" {
		return refuse(phase, "Seccomp is "+fields["Seccomp"]+" (the seccomp filters did not load)")
	}
	for _, k := range []string{"CapPrm", "CapEff", "CapBnd", "CapAmb"} {
		v, err := strconv.ParseUint(fields[k], 16, 64)
		if err != nil || v != 0 {
			return refuse(phase, k+" is "+fields[k])
		}
	}
	return nil
}

// ifaceName reads the interface name of one /proc/net/dev line. The two header lines hold "|" and give no name.
func ifaceName(line string) (string, bool) {
	if strings.Contains(line, "|") {
		return "", false
	}
	name, _, ok := strings.Cut(line, ":")
	name = strings.TrimSpace(name)
	return name, ok && name != ""
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func refuse(phase Phase, reason string) error {
	return &CannotIsolateError{Reason: "self check, " + phaseName(phase) + " phase: " + reason}
}
