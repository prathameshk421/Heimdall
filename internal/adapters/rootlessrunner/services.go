package rootlessrunner

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/profileyaml"
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
}

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
		if err := checkServiceEnv(svc); err != nil {
			return nil, err
		}
		out = append(out, serviceSpec{Name: svc.Name, Image: image, Env: svc.Env})
	}
	return out, nil
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
	return []string{"network", "create", "--internal", network}
}

// serviceStartArgs starts one service detached on the job's network, reachable by its name.
func serviceStartArgs(jobID string, svc serviceSpec, timeoutSeconds int) []string {
	network, container := serviceNames(jobID)
	args := []string{
		"run", "-d", "--rm", "--pull=never", "--name", container(svc.Name),
		"--network", network, "--network-alias", svc.Name,
		"--user=65532:65532", "--cap-drop=ALL", "--security-opt=no-new-privileges",
		"--pids-limit=256", "--memory=512m", "--cpus=1", "--timeout=" + strconv.Itoa(timeoutSeconds+30),
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=64m,mode=1777",
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
	return joined, cleanup, nil
}
