package rootlessrunner

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/profileyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Services (TECH_SPEC 12.2, D153): the throwaway containers a profile's test jobs may reach. A job with services runs
// on a private `--internal` network created for that job alone, shared with its services and nobody else. An internal
// network has no route out, so the job still has no egress, and its services are reachable only by their alias. The
// runner removes the services and the network when the job ends, whatever its outcome; every service also carries
// its own timeout, so a runner that dies mid-job leaves nothing running past it.
//
// A service container is third-party software that untrusted test code can talk to, so it gets the job's limits and
// no more: a non-root user, no capabilities, no new privileges, bounded processes, memory and CPU, no host mount.

// serviceSpec is one service resolved from a promoted profile: its image is a digest reference the runner holds.
type serviceSpec struct {
	Name  string
	Image string
	Env   map[string]string
	Port  int
	Data  string
}

const (
	// jobLabel marks every container and network the runner creates, so the boot sweep can find what a dead runner left.
	jobLabel       = "--label=heimdall.job=1"
	jobLabelFilter = "label=heimdall.job=1"
	// defaultServiceReady bounds the wait for every service of a job to accept on its port.
	defaultServiceReady = 30 * time.Second
	// probeEvery is the pause between two connection attempts of the probe.
	probeEvery = 500 * time.Millisecond
	// tcpProbe tries a TCP connection with bash's /dev/tcp up to $2 times, half a second apart, and exits 0 on the first
	// one accepted. The address, port and count are its positional arguments ($0, $1, $2), never script text. The loop
	// ends by itself, so a probe the runner's kill does not reach still stops.
	tcpProbe = `n=$2; while [ "$n" -gt 0 ]; do if (exec 3<>"/dev/tcp/$0/$1") 2>/dev/null; then exit 0; fi; n=$((n-1)); sleep 0.5; done; exit 1`
)

var serviceEnvKey = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)

// resolveServices resolves the services of a promoted profile for one job. A check job never gets services: a check
// is a static tool over the tree.
func (p Policy) resolveServices(profile string, in JobInput) ([]serviceSpec, error) {
	spec, ok := p.Profiles[profile]
	if !ok || in.CheckID != "" || len(spec.Services) == 0 {
		return nil, nil
	}
	out := make([]serviceSpec, 0, len(spec.Services))
	seen := map[string]bool{}
	for _, svc := range spec.Services {
		if !validProfileName(svc.Name) || seen[svc.Name] {
			return nil, fmt.Errorf("service name %q is not a unique profile name", svc.Name)
		}
		seen[svc.Name] = true
		image, err := p.resolveImage(svc.Image, spec)
		if err != nil {
			return nil, err
		}
		if err := checkService(svc); err != nil {
			return nil, err
		}
		out = append(out, serviceSpec{Name: svc.Name, Image: image, Env: svc.Env, Port: svc.Port, Data: svc.Data})
	}
	return out, nil
}

// checkService re-checks at run time what profile validation already refused: the environment, the port and the
// data directory, which reaches a --tmpfs argument.
func checkService(svc profileyaml.Service) error {
	if svc.Port < 1 || svc.Port > 65535 || (svc.Data != "" && !ports.ValidServiceDataDir(svc.Data)) {
		return fmt.Errorf("service %q needs a TCP port and, if any, an absolute data directory with no mount options", svc.Name)
	}
	return checkServiceEnv(svc)
}

func checkServiceEnv(svc profileyaml.Service) error {
	if len(svc.Env) > 16 {
		return errors.New("a service holds at most 16 environment variables")
	}
	for key, value := range svc.Env {
		if !serviceEnvKey.MatchString(key) || len(value) > 256 || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("service %q has an environment variable outside the policy", svc.Name)
		}
	}
	return nil
}

// serviceNames returns the network and container names of one job. jobID is the runner's own random job directory
// name, so no two jobs share a network and no request text reaches a name.
func serviceNames(jobID string) (network string, container func(service string) string) {
	return "heimdall-" + jobID + "-net", func(service string) string { return "heimdall-" + jobID + "-" + service }
}

// networkCreateArgs creates the job's private network. --internal gives it no route out.
func networkCreateArgs(network string) []string {
	return []string{"network", "create", "--internal", jobLabel, network}
}

// serviceStartArgs starts one service detached on the job's network, reachable by its name.
func serviceStartArgs(jobID string, svc serviceSpec, timeoutSeconds int) []string {
	network, container := serviceNames(jobID)
	args := []string{
		"run", "-d", "--rm", "--pull=never", jobLabel, "--name", container(svc.Name),
		"--network", network, "--network-alias", svc.Name,
		"--user=65532:65532", "--cap-drop=ALL", "--security-opt=no-new-privileges",
		"--pids-limit=256", "--memory=512m", "--cpus=1", "--timeout=" + strconv.Itoa(timeoutSeconds+30),
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=64m,mode=1777",
	}
	if svc.Data != "" {
		// A fresh tmpfs the fixed non-root user can write; an image that needs more than this is not allowlisted.
		args = append(args, "--tmpfs", svc.Data+":rw,nosuid,nodev,size=256m,mode=1777")
	}
	for _, key := range slices.Sorted(maps.Keys(svc.Env)) {
		args = append(args, "--env", key+"="+svc.Env[key])
	}
	return append(args, svc.Image)
}

// useServiceNetwork replaces the job's no-network flag with its private network. A job whose arguments do not carry the
// flag is refused: the arguments were not built by the policy.
func useServiceNetwork(args []string, network string) ([]string, error) {
	i := slices.Index(args, noNetwork)
	if i < 0 {
		return nil, errors.New("job arguments carry no network flag to replace")
	}
	out := slices.Clone(args)
	out[i] = "--network=" + network
	return out, nil
}

// startServices brings up the services of a job and returns the job's arguments joined to their network and the
// function that removes everything again. With no services it returns the arguments unchanged.
func (r *Runner) startServices(ctx context.Context, services []serviceSpec, jobID string, args []string, dir string, timeout time.Duration) ([]string, func(), error) {
	if len(services) == 0 {
		return args, func() {}, nil
	}
	network, container := serviceNames(jobID)
	joined, err := useServiceNetwork(args, network)
	if err != nil {
		return nil, nil, err
	}
	started := []string{}
	cleanup := func() {
		// The job's own context may be over: cleanup gets a fresh, short one.
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		for _, name := range started {
			_, _ = r.Executor.Execute(cctx, []string{"rm", "-f", "-t", "0", name}, dir)
		}
		_, _ = r.Executor.Execute(cctx, []string{"network", "rm", "-f", network}, dir)
	}
	if _, err := r.Executor.Execute(ctx, networkCreateArgs(network), dir); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("create job network: %w", err)
	}
	for _, svc := range services {
		if _, err := r.Executor.Execute(ctx, serviceStartArgs(jobID, svc, int(timeout.Seconds())), dir); err != nil {
			started = append(started, container(svc.Name))
			cleanup()
			return nil, nil, fmt.Errorf("start service %q: %w", svc.Name, err)
		}
		started = append(started, container(svc.Name))
	}
	if err := r.waitReady(ctx, network, container, services); err != nil {
		cleanup()
		return nil, nil, err
	}
	return joined, cleanup, nil
}

// waitReady waits until every service accepts a TCP connection on its port, within the runner's readiness bound. The
// job network is --internal, so the host cannot reach it; the probe runs in the rootless network namespace, where
// the job networks live.
func (r *Runner) waitReady(ctx context.Context, network string, container func(string) string, services []serviceSpec) error {
	bound := r.ServiceReady
	if bound <= 0 {
		bound = defaultServiceReady
	}
	attempts := strconv.Itoa(max(1, int(bound/probeEvery)))
	ctx, cancel := context.WithTimeout(ctx, bound+5*time.Second)
	defer cancel()
	for _, svc := range services {
		out, err := r.Executor.Execute(ctx, []string{"inspect", "--format",
			`{{(index .NetworkSettings.Networks "` + network + `").IPAddress}}`, container(svc.Name)}, "")
		if err != nil {
			return fmt.Errorf("inspect service %q: %w", svc.Name, err)
		}
		ip, err := serviceAddress(out)
		if err != nil {
			return fmt.Errorf("service %q: %w", svc.Name, err)
		}
		probe := []string{"unshare", "--rootless-netns", "/bin/bash", "-c", tcpProbe, ip, strconv.Itoa(svc.Port), attempts}
		if _, err := r.Executor.Execute(ctx, probe, ""); err != nil {
			return fmt.Errorf("service %q did not accept on port %d within %s: %w", svc.Name, svc.Port, bound, err)
		}
	}
	return nil
}

// serviceAddress reads the one IP address podman inspect printed.
func serviceAddress(out []byte) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(string(out)))
	if ip == nil {
		return "", errors.New("podman reported no address on the job network")
	}
	return ip.String(), nil
}

// containerID and jobNetwork are the only names the boot sweep passes on to podman rm.
var (
	containerID = regexp.MustCompile(`^[0-9a-f]{12,64}$`)
	jobNetwork  = regexp.MustCompile(`^heimdall-[A-Za-z0-9_.-]{1,128}-net$`)
)

// Sweep removes every container and network a previous runner process labelled and left behind (it died mid-job, or
// the host restarted). It runs once at boot, before the runner serves; at that point nothing labelled is in use.
func (r *Runner) Sweep(ctx context.Context) error {
	ids, err := r.labelled(ctx, []string{"ps", "-a", "-q", "--filter", jobLabelFilter}, containerID)
	if err != nil {
		return fmt.Errorf("sweep containers: %w", err)
	}
	if len(ids) > 0 {
		if _, err := r.Executor.Execute(ctx, append([]string{"rm", "-f", "-t", "0"}, ids...), ""); err != nil {
			return fmt.Errorf("sweep containers: %w", err)
		}
	}
	nets, err := r.labelled(ctx, []string{"network", "ls", "-q", "--filter", jobLabelFilter}, jobNetwork)
	if err != nil {
		return fmt.Errorf("sweep networks: %w", err)
	}
	if len(nets) > 0 {
		if _, err := r.Executor.Execute(ctx, append([]string{"network", "rm", "-f"}, nets...), ""); err != nil {
			return fmt.Errorf("sweep networks: %w", err)
		}
	}
	return nil
}

// labelled lists what one podman listing printed, refusing any line that is not the expected shape.
func (r *Runner) labelled(ctx context.Context, args []string, shape *regexp.Regexp) ([]string, error) {
	out, err := r.Executor.Execute(ctx, args, "")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Fields(string(out)) {
		if !shape.MatchString(line) {
			return nil, fmt.Errorf("unexpected name %q in the listing", line)
		}
		names = append(names, line)
	}
	return names, nil
}
