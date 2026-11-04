package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/github"
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

// fakeInstallation is the GitHub installation reader a test injects. It records each call and its deadline.
type fakeInstallation struct {
	inst        github.Installation
	err         error
	calls       int
	sawDeadline bool
}

func (f *fakeInstallation) Installation(ctx context.Context) (github.Installation, error) {
	f.calls++
	_, f.sawDeadline = ctx.Deadline()
	return f.inst, f.err
}

// goodInstallation holds exactly the permissions the product needs, across all repositories.
func goodInstallation() github.Installation {
	return github.Installation{
		Permissions: map[string]string{
			"checks": "write", "contents": "write", "pull_requests": "write", "issues": "write", "metadata": "read",
		},
		Repositories: github.Repositories{All: true},
	}
}

// gitHubConfig loads a heimdall.toml that selects GitHub as the VCS provider.
func gitHubConfig(t *testing.T) config.Config {
	t.Helper()
	text := "secrets_dir = \"" + t.TempDir() + "\"\n" +
		"[vcs]\nprovider = \"github\"\n\n" +
		"[[projects]]\ntracker_project = \"KAN\"\nrepo = \"aarush323/PBL_TOS_RISK_ANALYZER\"\nbase_branch = \"heimdall-demo\"\n"
	path := filepath.Join(t.TempDir(), "heimdall.toml")
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	return cfg
}

// gitHubDeps returns deps whose config selects GitHub and whose installation reader is reader. A nil reader is
// passed on as a nil reader.
func gitHubDeps(t *testing.T, reader *fakeInstallation) *fakeDeps {
	t.Helper()
	cfg := gitHubConfig(t)
	f := passingDeps(nil)
	f.loadConfig = func(string) (config.Config, error) { return cfg, nil }
	if reader != nil {
		f.githubReader = func(config.Config) (installationReader, error) { return reader, nil }
	}
	return f
}

// rowsByName runs the JSON output and returns the rows by check name.
func rowsByName(t *testing.T, out string) map[string]checkResult {
	t.Helper()
	var rows []checkResult
	require.NoError(t, json.Unmarshal([]byte(out), &rows), "output must be only JSON, got:\n%s", out)
	byName := map[string]checkResult{}
	for _, r := range rows {
		byName[r.Check] = r
	}
	return byName
}

func TestDoctorGitHubPermissionsAreOkWhenTheyMatchTheProduct(t *testing.T) {
	reader := &fakeInstallation{inst: goodInstallation()}
	f := gitHubDeps(t, reader)
	code, out, _ := runWith(t, []string{"--json", "--config", testConfigPath}, f)

	require.Equal(t, 0, code, out)
	rows := rowsByName(t, out)
	for _, name := range []string{"github.checks", "github.contents", "github.issues", "github.pull_requests", "github.metadata"} {
		require.Equal(t, "ok", rows[name].Status, "row %s: %+v", name, rows[name])
	}
	require.Equal(t, "ok", rows["github.repositories"].Status)
	require.Contains(t, rows["github.repositories"].Detail, "all repositories")
	require.Equal(t, 1, reader.calls, "the installation is read once")
	require.True(t, reader.sawDeadline, "the GitHub call must carry a deadline")
}

func TestDoctorExitsOneWhenAPermissionTheProductNeedsIsMissing(t *testing.T) {
	inst := goodInstallation()
	delete(inst.Permissions, "checks")
	f := gitHubDeps(t, &fakeInstallation{inst: inst})
	code, out, _ := runWith(t, []string{"--json", "--config", testConfigPath}, f)

	require.Equal(t, 1, code, out)
	row := rowsByName(t, out)["github.checks"]
	require.Equal(t, "missing", row.Status, out)
	require.Contains(t, row.Detail, "not granted")
}

func TestDoctorMissingWhenOnlyReadIsGrantedAndWriteIsNeeded(t *testing.T) {
	inst := goodInstallation()
	inst.Permissions["contents"] = "read"
	f := gitHubDeps(t, &fakeInstallation{inst: inst})
	code, out, _ := runWith(t, []string{"--json", "--config", testConfigPath}, f)

	require.Equal(t, 1, code, out)
	row := rowsByName(t, out)["github.contents"]
	require.Equal(t, "missing", row.Status, out)
	require.Contains(t, row.Detail, "has read, needs write")
}

func TestDoctorWarnsAboutAPermissionTheProductDoesNotNeedAndExitsZero(t *testing.T) {
	inst := goodInstallation()
	inst.Permissions["workflows"] = "write"
	f := gitHubDeps(t, &fakeInstallation{inst: inst})
	code, out, _ := runWith(t, []string{"--json", "--config", testConfigPath}, f)

	require.Equal(t, 0, code, out)
	row := rowsByName(t, out)["github.workflows"]
	require.Equal(t, "too broad", row.Status, out)
	require.Contains(t, row.Detail, "does not need")
}

func TestDoctorStrictFailsOnATooBroadPermission(t *testing.T) {
	inst := goodInstallation()
	inst.Permissions["workflows"] = "write"
	f := gitHubDeps(t, &fakeInstallation{inst: inst})
	code, out, _ := runWith(t, []string{"--strict", "--json", "--config", testConfigPath}, f)

	require.Equal(t, 1, code, out)
	require.Equal(t, "too broad", rowsByName(t, out)["github.workflows"].Status)
}

func TestDoctorCallsAWriteOnMetadataTooBroadWhereReadIsEnough(t *testing.T) {
	inst := goodInstallation()
	inst.Permissions["metadata"] = "write"
	f := gitHubDeps(t, &fakeInstallation{inst: inst})
	code, out, _ := runWith(t, []string{"--json", "--config", testConfigPath}, f)

	require.Equal(t, 0, code, out)
	row := rowsByName(t, out)["github.metadata"]
	require.Equal(t, "too broad", row.Status, out)
	require.Contains(t, row.Detail, "read is enough")
}

func TestDoctorCallsAdminTooBroadWhereWriteIsEnough(t *testing.T) {
	inst := goodInstallation()
	inst.Permissions["contents"] = "admin"
	f := gitHubDeps(t, &fakeInstallation{inst: inst})
	code, out, _ := runWith(t, []string{"--json", "--config", testConfigPath}, f)

	require.Equal(t, 0, code, out)
	require.Equal(t, "too broad", rowsByName(t, out)["github.contents"].Status)
}

func TestDoctorNotVerifiedWhenNoGitHubReaderIsWired(t *testing.T) {
	f := gitHubDeps(t, nil)
	code, out, _ := runWith(t, []string{"--json", "--config", testConfigPath}, f)

	require.Equal(t, 1, code, out)
	row := rowsByName(t, out)["github"]
	require.Equal(t, "not verified", row.Status, out)
	require.Contains(t, row.Detail, "no GitHub reader")
}

func TestDoctorNotVerifiedWhenTheGitHubCallFails(t *testing.T) {
	reader := &fakeInstallation{err: errors.New("github installation: status 401")}
	f := gitHubDeps(t, reader)
	code, out, _ := runWith(t, []string{"--json", "--config", testConfigPath}, f)

	require.Equal(t, 1, code, out)
	row := rowsByName(t, out)["github"]
	require.Equal(t, "not verified", row.Status, out)
	require.Contains(t, row.Detail, "status 401", "the reason is printed")
}

func TestDoctorNotVerifiedWhenTheReaderCannotBeBuilt(t *testing.T) {
	f := gitHubDeps(t, nil)
	f.githubReader = func(config.Config) (installationReader, error) {
		return nil, errors.New("secret \"github_app_id\": missing")
	}
	code, out, _ := runWith(t, []string{"--json", "--config", testConfigPath}, f)

	require.Equal(t, 1, code, out)
	require.Equal(t, "not verified", rowsByName(t, out)["github"].Status)
}

func TestDoctorSkipsGitHubWhenItIsNotSelected(t *testing.T) {
	reader := &fakeInstallation{inst: goodInstallation()}
	f := passingDeps(nil)
	f.githubReader = func(config.Config) (installationReader, error) { return reader, nil }
	code, out, _ := runWith(t, []string{"--json", "--config", testConfigPath}, f)

	require.Equal(t, 0, code, out)
	require.Len(t, rowsByName(t, out), 3, "no GitHub rows when GitHub is not the VCS provider")
	require.Equal(t, 0, reader.calls, "the GitHub call is not made")
}

func TestDoctorNamesTheSelectedRepositories(t *testing.T) {
	inst := goodInstallation()
	inst.Repositories = github.Repositories{Names: []string{"aarush323/heimdall-fixture-pay"}}
	f := gitHubDeps(t, &fakeInstallation{inst: inst})
	code, out, _ := runWith(t, []string{"--json", "--config", testConfigPath}, f)

	require.Equal(t, 0, code, out)
	row := rowsByName(t, out)["github.repositories"]
	require.Equal(t, "ok", row.Status)
	require.Contains(t, row.Detail, "aarush323/heimdall-fixture-pay")
}

func TestDoctorFailsWhenTheInstallationReachesNoRepository(t *testing.T) {
	inst := goodInstallation()
	inst.Repositories = github.Repositories{}
	f := gitHubDeps(t, &fakeInstallation{inst: inst})
	code, out, _ := runWith(t, []string{"--json", "--config", testConfigPath}, f)

	require.Equal(t, 1, code, out)
	require.Equal(t, "missing", rowsByName(t, out)["github.repositories"].Status)
}

func TestDoctorSkipsGitHubWhenAnotherVCSProviderIsSelected(t *testing.T) {
	text := "secrets_dir = \"" + t.TempDir() + "\"\n[vcs]\nprovider = \"gitlab\"\n\n" +
		"[[projects]]\ntracker_project = \"KAN\"\nrepo = \"aarush323/PBL_TOS_RISK_ANALYZER\"\nbase_branch = \"heimdall-demo\"\n"
	path := filepath.Join(t.TempDir(), "heimdall.toml")
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	cfg, err := config.Load(path)
	require.NoError(t, err)

	reader := &fakeInstallation{inst: goodInstallation()}
	f := passingDeps(nil)
	f.loadConfig = func(string) (config.Config, error) { return cfg, nil }
	f.githubReader = func(config.Config) (installationReader, error) { return reader, nil }
	code, out, _ := runWith(t, []string{"--json", "--config", testConfigPath}, f)

	require.Equal(t, 0, code, out)
	require.Len(t, rowsByName(t, out), 3, "a non-GitHub provider gets no GitHub rows")
	require.Equal(t, 0, reader.calls, "the GitHub call is made only for GitHub")
}
