package rootlessrunner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/profileyaml"
)

var postgresRef = "registry.example/postgres@sha256:" + strings.Repeat("d", 64)

func servicePolicy() Policy {
	p := jobPolicy()
	spec := p.Profiles["acme-pay"]
	spec.Services = []profileyaml.Service{
		{Name: "db", Image: postgresRef, Env: map[string]string{"POSTGRES_PASSWORD": "test-only", "A_FIRST": "1"}},
		{Name: "cache", Image: "checks"},
	}
	spec.Tests.Kinds["web"] = profileyaml.Kind{Serve: "uvicorn app:app --port 8000", Ready: "http://127.0.0.1:8000/healthz", Run: "pytest {dir}"}
	p.Images["postgres"] = postgresRef
	p.Profiles = map[string]RepoProfile{"acme-pay": spec}
	return p
}

func TestAServiceStartsDetachedOnThePrivateNetworkWithTheLeastPrivilege(t *testing.T) {
	svcs, err := servicePolicy().resolveServices("acme-pay", JobInput{TestKind: "api"})
	require.NoError(t, err)
	require.Len(t, svcs, 2)

	network, container := serviceNames("job-123")
	require.Equal(t, []string{"network", "create", "--internal", network}, networkCreateArgs(network), "an internal network has no route out")

	args := serviceStartArgs("job-123", svcs[0], 120)
	for _, want := range []string{"-d", "--rm", "--pull=never", container("db"), network, "db", "--user=65532:65532",
		"--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=256", "--memory=512m", "--cpus=1", "--timeout=150"} {
		require.Contains(t, args, want)
	}
	require.Equal(t, postgresRef, args[len(args)-1])
	joined := strings.Join(args, " ")
	for _, banned := range []string{"--privileged", "--cap-add", "--publish", "-p ", "--mount", "docker.sock", "--network=host", "--read-only=false"} {
		require.NotContains(t, joined, banned)
	}
	require.Less(t, strings.Index(joined, "A_FIRST=1"), strings.Index(joined, "POSTGRES_PASSWORD="), "the environment comes out in a fixed order")
}

func TestServicesAreRefusedForEveryForbiddenInput(t *testing.T) {
	for name, mutate := range map[string]func(*RepoProfile){
		"an image the runner does not hold":  func(s *RepoProfile) { s.Services[0].Image = "evil" },
		"an unlisted digest":                 func(s *RepoProfile) { s.Services[0].Image = "registry.example/x@sha256:" + strings.Repeat("9", 64) },
		"a service name with a shell escape": func(s *RepoProfile) { s.Services[0].Name = "db;rm" },
		"two services with one name":         func(s *RepoProfile) { s.Services[1].Name = "db" },
		"a lower-case variable name":         func(s *RepoProfile) { s.Services[0].Env = map[string]string{"x": "1"} },
		"a variable value with a line break": func(s *RepoProfile) { s.Services[0].Env = map[string]string{"A": "1\n2"} },
	} {
		t.Run(name, func(t *testing.T) {
			p := servicePolicy()
			spec := p.Profiles["acme-pay"]
			spec.Services = append([]profileyaml.Service(nil), spec.Services...)
			mutate(&spec)
			p.Profiles = map[string]RepoProfile{"acme-pay": spec}
			_, err := p.resolveServices("acme-pay", JobInput{TestKind: "api"})
			require.Error(t, err)
		})
	}
}

func TestACheckJobGetsNoServicesAndTheBuiltInProfileNone(t *testing.T) {
	svcs, err := servicePolicy().resolveServices("acme-pay", JobInput{CheckID: "patterns"})
	require.NoError(t, err)
	require.Empty(t, svcs)
	svcs, err = servicePolicy().resolveServices("go", JobInput{})
	require.NoError(t, err)
	require.Empty(t, svcs)
}

func TestOnlyTheNetworkFlagOfAJobIsReplaced(t *testing.T) {
	args, err := resolveArgs(t, jobPolicy(), "", "api", "tests/heimdall/PAY-1")
	require.NoError(t, err)
	joined, err := useServiceNetwork(args, "heimdall-job-1-net")
	require.NoError(t, err)
	require.Contains(t, joined, "--network=heimdall-job-1-net")
	require.NotContains(t, joined, "--network=none")
	require.Len(t, joined, len(args))
	require.Contains(t, args, "--network=none", "the original slice is untouched")

	_, err = useServiceNetwork([]string{"run", "--network=host"}, "n")
	require.Error(t, err, "arguments the policy did not build are refused")
}

// recordingExecutor records every call in order and can fail on one.
type orderExecutor struct {
	mu     sync.Mutex
	calls  [][]string
	failOn string
	output []byte
}

func (e *orderExecutor) Execute(_ context.Context, args []string, _ string) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, args)
	if e.failOn != "" && strings.Contains(strings.Join(args, " "), e.failOn) {
		return nil, errors.New("boom")
	}
	if len(args) > 0 && args[0] == "run" && !slicesContainsStr(args, "-d") {
		return e.output, nil
	}
	return nil, nil
}

func slicesContainsStr(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func (e *orderExecutor) verbs() []string {
	var out []string
	for _, c := range e.calls {
		v := c[0]
		if v == "run" && slicesContainsStr(c, "-d") {
			v = "service"
		}
		if v == "network" || v == "rm" {
			v += " " + c[1]
		}
		out = append(out, v)
	}
	return out
}

func serviceRunner(t *testing.T, exec Executor) Runner {
	t.Helper()
	p := servicePolicy()
	active := p.Profiles
	p.Profiles = nil
	return Runner{Policy: p, Executor: exec, Root: t.TempDir(), active: active}
}

func TestAJobWithServicesRunsOnItsPrivateNetworkAndLeavesNothingBehind(t *testing.T) {
	exec := &orderExecutor{output: []byte("\nheimdall-result-begin\n<testsuites/>\n\nheimdall-result-end\nheimdall-exit=0\n")}
	r := serviceRunner(t, exec)
	_, err := r.Run(context.Background(), JobInput{Profile: "acme-pay", TestKind: "api", Dir: "tests/heimdall/PAY-1", Archive: strings.NewReader("a"), Timeout: time.Minute})
	require.NoError(t, err)
	require.Equal(t, []string{"network create", "service", "service", "run"}, exec.verbs()[0:4])

	job := exec.calls[3]
	require.Equal(t, "run", job[0])
	netFlag := ""
	for _, a := range job {
		if strings.HasPrefix(a, "--network=") {
			netFlag = a
		}
	}
	require.True(t, strings.HasPrefix(netFlag, "--network=heimdall-job-") && strings.HasSuffix(netFlag, "-net"), netFlag)
	require.NotContains(t, job, "--network=none")
	require.Contains(t, exec.calls[1], "--network-alias")

	tail := exec.calls[4:]
	require.Len(t, tail, 3, "both services and the network are removed")
	require.Equal(t, "rm", tail[0][0])
	require.Equal(t, "rm", tail[1][0])
	require.Equal(t, []string{"network", "rm", "-f", exec.calls[0][3]}, tail[2])
}

func TestServicesAreRemovedWhenAServiceFailsToStartOrTheJobFails(t *testing.T) {
	exec := &orderExecutor{failOn: "cache"}
	r := serviceRunner(t, exec)
	_, err := r.Run(context.Background(), JobInput{Profile: "acme-pay", TestKind: "api", Dir: "tests/heimdall/PAY-1", Archive: strings.NewReader("a"), Timeout: time.Minute})
	require.ErrorContains(t, err, "start service")
	verbs := exec.verbs()
	require.Equal(t, "network create", verbs[0])
	require.Contains(t, verbs[len(verbs)-3:], "network rm", "a failed start still removes the network")
	for _, c := range exec.calls {
		require.False(t, c[0] == "run" && !slicesContainsStr(c, "-d"), "the job itself never started")
	}

	exec = &orderExecutor{failOn: "--env HEIMDALL_RUN"}
	r = serviceRunner(t, exec)
	_, err = r.Run(context.Background(), JobInput{Profile: "acme-pay", TestKind: "api", Dir: "tests/heimdall/PAY-1", Archive: strings.NewReader("a"), Timeout: time.Minute})
	require.Error(t, err)
	last := exec.calls[len(exec.calls)-1]
	require.Equal(t, []string{"network", "rm", "-f", exec.calls[0][3]}, last, "a failed job still removes its services and network")
}

func TestAJobWithoutServicesRunsOnNoNetworkAndTouchesNoNetworkCommands(t *testing.T) {
	exec := &orderExecutor{output: []byte(sarifOut)}
	p := jobPolicy()
	active := p.Profiles
	p.Profiles = nil
	r := Runner{Policy: p, Executor: exec, Root: t.TempDir(), active: active}
	_, err := r.Run(context.Background(), JobInput{Profile: "acme-pay", CheckID: "patterns", Archive: strings.NewReader("a"), Timeout: time.Minute})
	require.NoError(t, err)
	require.Len(t, exec.calls, 1)
	require.Contains(t, exec.calls[0], "--network=none")
}

func TestAServedAppReachesTheJobAsTwoEnvironmentVariables(t *testing.T) {
	p := servicePolicy()
	spec, err := p.resolveJob("acme-pay", "", "web", "tests/heimdall/PAY-1")
	require.NoError(t, err)
	spec.Archive, spec.ModuleCache, spec.Timeout = "/var/jobs/a.tgz", "/var/cache/mod", 60
	args, err := p.profileJobArgs(spec)
	require.NoError(t, err)
	require.Contains(t, args, "HEIMDALL_SERVE=uvicorn app:app --port 8000")
	require.Contains(t, args, "HEIMDALL_READY=http://127.0.0.1:8000/healthz")
	require.Contains(t, args, "--network=none", "serving an app needs no network: the ready URL is the job's own loopback")

	plain, err := resolveArgs(t, p, "", "api", "tests/heimdall/PAY-1")
	require.NoError(t, err)
	require.NotContains(t, plain, "HEIMDALL_READY=http://127.0.0.1:8000/healthz")
	for _, a := range plain {
		require.False(t, strings.HasPrefix(a, "HEIMDALL_SERVE="), "a kind with no app sets no serve variable")
	}
}

func TestAServedAppIsRefusedWithoutALoopbackReadyURL(t *testing.T) {
	p := servicePolicy()
	good := jobSpec{Image: goRef, Run: "x", ResultFile: resultJUnit, Archive: "/var/jobs/a.tgz", ModuleCache: "/var/cache/mod", Timeout: 60,
		Serve: "app", Ready: "http://127.0.0.1:8000/"}
	_, err := p.profileJobArgs(good)
	require.NoError(t, err)
	for name, mutate := range map[string]func(*jobSpec){
		"serve with no ready":        func(j *jobSpec) { j.Ready = "" },
		"ready with no serve":        func(j *jobSpec) { j.Serve = "" },
		"a ready URL off the loop":   func(j *jobSpec) { j.Ready = "http://example.com:80/" },
		"the metadata address":       func(j *jobSpec) { j.Ready = "http://169.254.169.254:80/" },
		"a serve line with a break":  func(j *jobSpec) { j.Serve = "app\nrm -rf /" },
		"a ready URL with userinfo":  func(j *jobSpec) { j.Ready = "http://127.0.0.1@evil.example:80/" },
		"a ready URL with no port":   func(j *jobSpec) { j.Ready = "http://127.0.0.1/" },
		"a ready URL that is a file": func(j *jobSpec) { j.Ready = "file:///etc/passwd" },
	} {
		t.Run(name, func(t *testing.T) {
			j := good
			mutate(&j)
			_, err := p.profileJobArgs(j)
			require.Error(t, err)
		})
	}
}

// The wrapper's wait loop, run for real with the host's shell and a stand-in curl: the job's run command starts only
// after the ready URL answers.
func TestTheWrapperWaitsForTheServedAppBeforeItRunsTheCommand(t *testing.T) {
	prelude, rest, found := strings.Cut(profileJobScript, "probe() {")
	require.True(t, found)
	_ = prelude
	wait, _, found := strings.Cut("probe() {"+rest, `if [ "$ready" = 1 ]`)
	require.True(t, found)

	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	require.NoError(t, os.Mkdir(bin, 0o750))
	curl := "#!/bin/sh\nn=$(cat " + dir + "/n 2>/dev/null || echo 0)\nn=$((n+1))\necho $n > " + dir + "/n\n[ -f " + dir + "/up ] && [ \"$n\" -ge 3 ]\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "curl"), []byte(curl), 0o700)) // #nosec G306 -- a stand-in tool for the test
	script := wait + `echo ready=$ready`
	cmd := exec.CommandContext(context.Background(), "sh", "-c", script)
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "HEIMDALL_SERVE=touch " + dir + "/up", "HEIMDALL_READY=http://127.0.0.1:8000/"}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "ready=1")
	require.FileExists(t, filepath.Join(dir, "up"), "the serve command ran in the background")
}
