package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/stretchr/testify/require"
)

const testConfigPath = "/etc/heimdall/heimdall.toml"

// fakeDeps returns deps where every check passes, plus a record of the config path that was loaded.
type fakeDeps struct {
	doctorDeps
	loadedPath  string
	loadCalled  bool
	sawDeadline bool
}

func passingDeps(env map[string]string) *fakeDeps {
	f := &fakeDeps{}
	f.doctorDeps = doctorDeps{
		getenv: func(name string) string { return env[name] },
		loadConfig: func(path string) (config.Config, error) {
			f.loadCalled = true
			f.loadedPath = path
			return config.Config{}, nil
		},
		dockerVersion: func(ctx context.Context) (string, error) {
			_, f.sawDeadline = ctx.Deadline()
			return "27.0.1", nil
		},
	}
	return f
}

func runWith(t *testing.T, args []string, f *fakeDeps) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runDoctor(args, &out, &errOut, f.doctorDeps)
	return code, out.String(), errOut.String()
}

// statusOf returns the status column of the table row for check, or "" when there is no such row.
func statusOf(out, check string) string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == check {
			return fields[1]
		}
	}
	return ""
}

func TestDoctorPrintsTableAndExitsZeroWhenAllChecksPass(t *testing.T) {
	f := passingDeps(nil)
	code, out, _ := runWith(t, []string{"--config", testConfigPath}, f)

	require.Equal(t, 0, code, out)
	require.True(t, strings.HasPrefix(out, "CHECK"), "the table starts with a header, got:\n%s", out)
	require.Contains(t, out, "STATUS")
	require.Contains(t, out, "DETAIL")
	require.Equal(t, "ok", statusOf(out, "config"), out)
	require.Equal(t, "ok", statusOf(out, "docker"), out)
	require.Equal(t, "skipped", statusOf(out, "postgres"), "the Postgres check is out of scope and must say so")
	require.Contains(t, out, "no database DSN", "a skipped row carries its reason")
	require.True(t, f.sawDeadline, "the docker call must carry a deadline (CODING_STANDARDS 4)")
}

func TestDoctorExitsOneWhenDockerIsUnreachable(t *testing.T) {
	f := passingDeps(nil)
	f.dockerVersion = func(context.Context) (string, error) {
		return "", errors.New("Cannot connect to the Docker daemon at unix:///var/run/docker.sock")
	}
	code, out, _ := runWith(t, []string{"--config", testConfigPath}, f)

	require.Equal(t, 1, code, out)
	require.Equal(t, "fail", statusOf(out, "docker"), out)
	require.Contains(t, out, "Cannot connect to the Docker daemon")
	require.Equal(t, "ok", statusOf(out, "config"), "a failing check must not stop the others from running")
}

func TestDoctorExitsOneWhenConfigDoesNotLoad(t *testing.T) {
	f := passingDeps(nil)
	f.loadConfig = func(string) (config.Config, error) {
		return config.Config{}, errors.New("projects[0]: repo is required")
	}
	code, out, _ := runWith(t, []string{"--config", testConfigPath}, f)

	require.Equal(t, 1, code, out)
	require.Equal(t, "fail", statusOf(out, "config"), out)
	require.Contains(t, out, "repo is required")
	require.Equal(t, "ok", statusOf(out, "docker"), out)
}

func TestDoctorReadsTheConfigPathFromHeimdallConfig(t *testing.T) {
	f := passingDeps(map[string]string{"HEIMDALL_CONFIG": "/srv/heimdall.toml"})
	code, out, _ := runWith(t, nil, f)

	require.Equal(t, 0, code, out)
	require.True(t, f.loadCalled)
	require.Equal(t, "/srv/heimdall.toml", f.loadedPath)
	require.Equal(t, "ok", statusOf(out, "config"), out)
}

func TestDoctorFailsTheConfigCheckWhenNoPathIsGiven(t *testing.T) {
	f := passingDeps(nil)
	code, out, _ := runWith(t, nil, f)

	require.Equal(t, 1, code, out)
	require.False(t, f.loadCalled, "with no path there is nothing to load")
	require.Equal(t, "fail", statusOf(out, "config"), out)
	require.Contains(t, out, "no config file")
}

func TestDoctorJSONOutputIsAMachineReadableArray(t *testing.T) {
	f := passingDeps(nil)
	f.dockerVersion = func(context.Context) (string, error) { return "", errors.New("daemon down") }
	code, out, _ := runWith(t, []string{"--json", "--config", testConfigPath}, f)

	require.Equal(t, 1, code, out)
	var rows []checkResult
	require.NoError(t, json.Unmarshal([]byte(out), &rows), "output must be only JSON, got:\n%s", out)
	require.Len(t, rows, 3)
	byName := map[string]checkResult{}
	for _, r := range rows {
		byName[r.Check] = r
	}
	require.Equal(t, "ok", byName["config"].Status)
	require.Equal(t, "fail", byName["docker"].Status)
	require.Contains(t, byName["docker"].Detail, "daemon down")
	require.Equal(t, "skipped", byName["postgres"].Status)
	require.NotEmpty(t, byName["postgres"].Detail, "a skipped check gives its reason")
}

func TestDoctorRejectsAnUnknownFlagWithUsageStatus(t *testing.T) {
	f := passingDeps(nil)
	code, out, errOut := runWith(t, []string{"--bogus"}, f)

	require.Equal(t, 2, code)
	require.Empty(t, out, "a usage error prints no table")
	require.NotEmpty(t, errOut)
	require.False(t, f.loadCalled)
}
