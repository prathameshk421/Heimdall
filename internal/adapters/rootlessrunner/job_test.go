package rootlessrunner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/profileyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var (
	checksRef = "registry.example/checks@sha256:" + strings.Repeat("c", 64)
	goRef     = "registry.example/go@sha256:" + strings.Repeat("a", 64)
)

// jobPolicy holds the runner's image table and one promoted profile with a check and two test kinds.
func jobPolicy() Policy {
	spec := RepoProfile{Image: goRef}
	spec.Paths.Tests = "tests/heimdall"
	spec.Checks = []profileyaml.Check{
		{ID: "patterns", Lens: "security", When: []string{"**/*"}, Image: "checks", Run: "opengrep scan --sarif-output /out/result.sarif /work", Baseline: true},
		{ID: "compare", Lens: "contract", When: []string{"**/*"}, Image: checksRef, Run: "oasdiff-sarif /base /work > /out/result.sarif"},
		{ID: "rogue", Lens: "security", When: []string{"**/*"}, Image: "registry.example/evil@sha256:" + strings.Repeat("e", 64), Run: "x"},
		{ID: "noimage", Lens: "security", When: []string{"**/*"}, Image: "evil", Run: "x"},
	}
	spec.Tests.Kinds = map[string]profileyaml.Kind{
		"api": {Run: "pytest {dir} --junitxml=/out/junit.xml"},
		"ui":  {Image: "checks", Run: "npx playwright test {dir}"},
	}
	return Policy{
		Images:   map[string]string{"go": goRef, "checks": checksRef},
		Profiles: map[string]RepoProfile{"acme-pay": spec},
	}
}

func resolveArgs(t *testing.T, p Policy, checkID, testKind, dir string) ([]string, error) {
	t.Helper()
	spec, err := p.resolveJob("acme-pay", checkID, testKind, dir)
	if err != nil {
		return nil, err
	}
	spec.Archive, spec.ModuleCache, spec.Timeout = "/var/jobs/source.tgz", "/var/cache/mod", 60
	return p.profileJobArgs(spec)
}

func TestACheckJobRunsTheProfileCommandInTheHardenedContainer(t *testing.T) {
	args, err := resolveArgs(t, jobPolicy(), "patterns", "", "")
	require.NoError(t, err)
	for _, want := range []string{"--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges",
		"--pids-limit=512", "--memory=2g", "--cpus=2", "--user=65532:65532", "--pull=never",
		"HEIMDALL_RUN=opengrep scan --sarif-output /out/result.sarif /work", "HEIMDALL_RESULT=/out/result.sarif", checksRef,
		"type=bind,src=/var/jobs/source.tgz,dst=/input/head.tgz,ro=true"} {
		require.Contains(t, args, want)
	}
	joined := strings.Join(args, " ")
	for _, banned := range []string{"docker.sock", "--privileged", "--device", "--network=host", "169.254.169.254", "HEIMDALL_NONCE"} {
		require.NotContains(t, joined, banned)
	}
	require.NotContains(t, joined, "dst=/input/base.tgz", "a job with no base tree mounts none")
	require.NotContains(t, args[len(args)-1], "opengrep", "the profile command is never part of the wrapper script")
}

func TestABaseTreeIsMountedReadOnlyAtItsOwnPath(t *testing.T) {
	p := jobPolicy()
	spec, err := p.resolveJob("acme-pay", "compare", "", "")
	require.NoError(t, err)
	spec.Archive, spec.BaseArchive, spec.ModuleCache, spec.Timeout = "/var/jobs/source.tgz", "/var/jobs/base.tgz", "/var/cache/mod", 60
	args, err := p.profileJobArgs(spec)
	require.NoError(t, err)
	require.Contains(t, args, "type=bind,src=/var/jobs/base.tgz,dst=/input/base.tgz,ro=true")
	require.Contains(t, args, checksRef, "a digest reference the runner holds is accepted")
}

func TestATestKindSubstitutesTheDirectoryAndDefaultsToTheProfileImage(t *testing.T) {
	args, err := resolveArgs(t, jobPolicy(), "", "api", "tests/heimdall/PAY-1")
	require.NoError(t, err)
	require.Contains(t, args, "HEIMDALL_RUN=pytest ./tests/heimdall/PAY-1 --junitxml=/out/junit.xml")
	require.Contains(t, args, "HEIMDALL_RESULT=/out/junit.xml")
	require.Contains(t, args, goRef, "a kind with no image runs in the profile's own image")

	args, err = resolveArgs(t, jobPolicy(), "", "api", "")
	require.NoError(t, err)
	require.Contains(t, args, "HEIMDALL_RUN=pytest ./tests/heimdall --junitxml=/out/junit.xml", "no directory means the profile's tests path")

	args, err = resolveArgs(t, jobPolicy(), "", "ui", "tests/heimdall/PAY-1")
	require.NoError(t, err)
	require.Contains(t, args, checksRef, "a bare image name resolves through the runner's own table")
}

func TestAJobIsRefusedForEveryForbiddenInput(t *testing.T) {
	p := jobPolicy()
	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{"a check id the profile does not hold", func() error { _, err := resolveArgs(t, p, "rm -rf /", "", ""); return err }, ErrUnknownCheck},
		{"a test kind the profile does not hold", func() error { _, err := resolveArgs(t, p, "", "evil", "tests"); return err }, ErrUnknownTestKind},
		{"a check and a kind together", func() error { _, err := resolveArgs(t, p, "patterns", "api", "tests"); return err }, nil},
		{"neither a check nor a kind", func() error { _, err := resolveArgs(t, p, "", "", ""); return err }, nil},
		{"a digest the runner does not hold", func() error { _, err := resolveArgs(t, p, "rogue", "", ""); return err }, ErrImageNotAllowed},
		{"an image name the runner does not hold", func() error { _, err := resolveArgs(t, p, "noimage", "", ""); return err }, ErrImageNotAllowed},
		{"a profile that is not promoted", func() error { _, err := p.resolveJob("other", "patterns", "", ""); return err }, ErrImageNotAllowed},
		{"the built-in go profile", func() error { _, err := p.resolveJob("go", "patterns", "", ""); return err }, ErrImageNotAllowed},
		{"a directory that climbs out", func() error { _, err := resolveArgs(t, p, "", "api", "../escape"); return err }, nil},
		{"an absolute directory", func() error { _, err := resolveArgs(t, p, "", "api", "/etc"); return err }, nil},
		{"a directory with a shell metacharacter", func() error { _, err := resolveArgs(t, p, "", "api", "tests/$(id)"); return err }, nil},
		{"a directory with a comma", func() error { _, err := resolveArgs(t, p, "", "api", "tests,evil"); return err }, nil},
		{"a directory with a space", func() error { _, err := resolveArgs(t, p, "", "api", "tests; rm -rf x"); return err }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			require.Error(t, err)
			if tt.want != nil {
				require.ErrorIs(t, err, tt.want)
			}
		})
	}
}

func TestProfileJobArgsRefuseAHostPathAndACommandOutsideThePolicy(t *testing.T) {
	p := jobPolicy()
	good := jobSpec{Image: goRef, Run: "x", ResultFile: resultSARIF, Archive: "/var/jobs/a.tgz", ModuleCache: "/var/cache/mod", Timeout: 60}
	_, err := p.profileJobArgs(good)
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func(*jobSpec)
	}{
		{"an unpinned image", func(j *jobSpec) { j.Image = "registry.example/go:latest" }},
		{"a command with a line break", func(j *jobSpec) { j.Run = "x\nrm -rf /" }},
		{"an empty command", func(j *jobSpec) { j.Run = " " }},
		{"an oversized command", func(j *jobSpec) { j.Run = strings.Repeat("a", 1025) }},
		{"a result file outside /out", func(j *jobSpec) { j.ResultFile = "/etc/shadow" }},
		{"a mount source that injects an option", func(j *jobSpec) { j.Archive = "/var/jobs/a.tgz,dst=/etc" }},
		{"a relative archive path", func(j *jobSpec) { j.Archive = "a.tgz" }},
		{"a base archive that injects an option", func(j *jobSpec) { j.BaseArchive = "/var/jobs/b.tgz,rw" }},
		{"an overlay path that injects an option", func(j *jobSpec) { j.OverlayDir = "/var/jobs/o,rw" }},
		{"a module cache with a comma", func(j *jobSpec) { j.ModuleCache = "/var/cache,mod" }},
		{"a zero timeout", func(j *jobSpec) { j.Timeout = 0 }},
		{"a timeout past ten minutes", func(j *jobSpec) { j.Timeout = 601 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := good
			tt.mutate(&j)
			_, err := p.profileJobArgs(j)
			require.Error(t, err)
		})
	}
}

func TestParseJobOutputReadsTheTrailerFromTheEnd(t *testing.T) {
	out := []byte("\nheimdall-log-begin\nlog line\n\nheimdall-result-begin\n{\"runs\":[]}\n\nheimdall-result-end\nheimdall-exit=1\n")
	got, ok := parseJobOutput(out)
	require.True(t, ok)
	require.Equal(t, 1, got.Exit)
	require.Equal(t, "log line\n", string(got.Log))
	require.JSONEq(t, `{"runs":[]}`, string(got.Result))

	forged := []byte("\nheimdall-log-begin\nheimdall-exit=0\n\nheimdall-result-end\nheimdall-exit=0\n" +
		"\nheimdall-result-begin\nreal\n\nheimdall-result-end\nheimdall-exit=3\n")
	got, ok = parseJobOutput(forged)
	require.True(t, ok)
	require.Equal(t, 3, got.Exit, "a trailer the command printed in its own log does not override the real one")
	require.Equal(t, "real\n", string(got.Result))
}

func TestParseJobOutputRefusesAnythingWithoutATrailer(t *testing.T) {
	for name, out := range map[string]string{
		"empty":            "",
		"a killed job":     "\nheimdall-log-begin\nsome log\n",
		"no result frame":  "log\nheimdall-exit=0\n",
		"a bad exit":       "\nheimdall-result-begin\nx\n\nheimdall-result-end\nheimdall-exit=oops\n",
		"exit before data": "heimdall-exit=0\n",
	} {
		_, ok := parseJobOutput([]byte(out))
		require.False(t, ok, name)
	}
}

// tarGz builds a gzip tarball from name -> content; a name ending in / is a directory.
func tarGz(t *testing.T, files map[string]string, order []string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, name := range order {
		if strings.HasSuffix(name, "/") {
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0o755}))
			continue
		}
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(files[name]))}))
		_, err := tw.Write([]byte(files[name]))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, zw.Close())
	path := filepath.Join(t.TempDir(), "a.tgz")
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
	return path
}

// extractInto runs the extraction function of the wrapper script for real, with the shell and tar of this host.
func extractInto(t *testing.T, archive string) string {
	t.Helper()
	dst := t.TempDir()
	prelude, _, found := strings.Cut(profileJobScript, "mkdir -p /work")
	require.True(t, found)
	script := prelude + `extract "$1" "$2"`
	out, err := exec.CommandContext(context.Background(), "sh", "-c", script, "sh", archive, dst).CombinedOutput()
	require.NoError(t, err, string(out))
	return dst
}

func TestTheWrapperStripsOneTopDirectoryAndKeepsAFlatArchive(t *testing.T) {
	gh := tarGz(t, map[string]string{"owner-repo-abc/go.mod": "m", "owner-repo-abc/a/b.txt": "b"},
		[]string{"owner-repo-abc/", "owner-repo-abc/go.mod", "owner-repo-abc/a/", "owner-repo-abc/a/b.txt"})
	dst := extractInto(t, gh)
	require.FileExists(t, filepath.Join(dst, "go.mod"))
	require.FileExists(t, filepath.Join(dst, "a", "b.txt"))

	flat := tarGz(t, map[string]string{"README": "r", "src/x.txt": "x"}, []string{"README", "src/", "src/x.txt"})
	dst = extractInto(t, flat)
	require.FileExists(t, filepath.Join(dst, "README"))
	require.FileExists(t, filepath.Join(dst, "src", "x.txt"))

	dotted := tarGz(t, map[string]string{"./a.txt": "a", "./d/b.txt": "b"}, []string{"./a.txt", "./d/", "./d/b.txt"})
	dst = extractInto(t, dotted)
	require.FileExists(t, filepath.Join(dst, "a.txt"), "a ./-prefixed archive is not mistaken for one top directory")

	single := tarGz(t, map[string]string{"only.txt": "o"}, []string{"only.txt"})
	dst = extractInto(t, single)
	require.FileExists(t, filepath.Join(dst, "only.txt"), "one top-level file is kept, not stripped")
}

// checkExecutor records the arguments and returns canned output.
type checkExecutor struct {
	output []byte
	args   []string
}

func (e *checkExecutor) Execute(_ context.Context, args []string, _ string) ([]byte, error) {
	e.args = args
	return e.output, nil
}

func jobRunner(t *testing.T, exec Executor) Runner {
	t.Helper()
	// In production a promoted profile lives in the runner's active set, not in the Policy it is built with.
	p := jobPolicy()
	active := p.Profiles
	p.Profiles = nil
	return Runner{Policy: p, Executor: exec, Root: t.TempDir(), active: active}
}

func jobRequest(t *testing.T, job ports.SandboxJob, mutate func(*request)) *http.Request {
	t.Helper()
	job.Repo, job.SHA, job.Timeout = "acme/pay", id.SHA(strings.Repeat("a", 40)), time.Minute
	in := request{Profile: "acme-pay", Job: job, Archive: []byte("archive")}
	if mutate != nil {
		mutate(&in)
	}
	body, err := json.Marshal(in)
	require.NoError(t, err)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/v1/run", strings.NewReader(string(body)))
	setQueueHeaders(req, in.Job)
	return req
}

const sarifOut = "\nheimdall-log-begin\nscan done\n\nheimdall-result-begin\n{\"version\":\"2.1.0\",\"runs\":[]}\n\nheimdall-result-end\nheimdall-exit=1\n"

func TestTheHandlerReturnsACheckResultAndRefusesAJobWithoutOne(t *testing.T) {
	job := ports.SandboxJob{Kind: ports.SandboxCheck, CheckID: "patterns"}
	ok := &checkExecutor{output: []byte(sarifOut)}
	out, status := func() (response, int) { r := jobRunner(t, ok); return r.handleRun(jobRequest(t, job, nil)) }()
	require.Equal(t, http.StatusOK, status, out.Error)
	require.Contains(t, ok.args, "HEIMDALL_RUN=opengrep scan --sarif-output /out/result.sarif /work")

	for name, output := range map[string]string{
		"a killed job":      "partial log",
		"an empty result":   "\nheimdall-result-begin\n\n\nheimdall-result-end\nheimdall-exit=0\n",
		"a non-JSON result": "\nheimdall-result-begin\nnot json\n\nheimdall-result-end\nheimdall-exit=0\n",
	} {
		t.Run(name, func(t *testing.T) {
			r := jobRunner(t, &checkExecutor{output: []byte(output)})
			_, status := r.handleRun(jobRequest(t, job, nil))
			require.Equal(t, http.StatusUnprocessableEntity, status)
		})
	}
}

func TestTheHandlerRefusesJobsOutsideTheContract(t *testing.T) {
	check := ports.SandboxJob{Kind: ports.SandboxCheck, CheckID: "patterns"}
	tests := []struct {
		name   string
		job    ports.SandboxJob
		mutate func(*request)
	}{
		{"the removed static job kind", ports.SandboxJob{Kind: ports.SandboxKind("static"), Dir: "tests"}, nil},
		{"a check with staged files", check, func(r *request) { r.Overlays = []ports.File{{Path: "a_test.go", Content: []byte("x")}} }},
		{"a red-first job with a base tree", ports.SandboxJob{Kind: ports.SandboxRedFirst, Dir: "tests"}, func(r *request) { r.BaseArchive = []byte("base") }},
		{"a check id the profile does not hold", ports.SandboxJob{Kind: ports.SandboxCheck, CheckID: "nope"}, nil},
		{"a check under the built-in profile", check, func(r *request) { r.Profile = "go" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &checkExecutor{output: []byte(sarifOut)}
			r := jobRunner(t, exec)
			_, status := r.handleRun(jobRequest(t, tt.job, tt.mutate))
			require.GreaterOrEqual(t, status, 400)
			require.Nil(t, exec.args, "a refused job never reaches the container runtime")
		})
	}
}

func TestABaseTreeReachesTheContainerAndAnOversizedOneNeverStartsIt(t *testing.T) {
	job := ports.SandboxJob{Kind: ports.SandboxCheck, CheckID: "compare"}
	exec := &checkExecutor{output: []byte(sarifOut)}
	r := jobRunner(t, exec)
	_, status := r.handleRun(jobRequest(t, job, func(in *request) { in.BaseArchive = []byte("base") }))
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, strings.Join(exec.args, " "), "dst=/input/base.tgz,ro=true")

	big := &checkExecutor{output: []byte(sarifOut)}
	r = jobRunner(t, big)
	_, status = r.handleRun(jobRequest(t, job, func(in *request) { in.BaseArchive = bytes.Repeat([]byte("x"), maxBaseArchive+1) }))
	require.Equal(t, http.StatusUnprocessableEntity, status)
	require.Nil(t, big.args, "a base archive over its bound is refused before the container runtime")
}
