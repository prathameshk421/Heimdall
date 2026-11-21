package rootlessrunner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The contract tests run the server-side Client against the runner's own Handler over a real Unix socket
// (HARNESS_DESIGN B8). Only the container runtime is fake: the request the client encodes, the queue headers, the
// handler's checks, the wrapper trailer and the client's parsing are the production code on both sides.

type contractSource struct{}

func (contractSource) Tarball(context.Context, change.Repo, id.SHA) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("archive")), nil
}

type contractBlobs struct {
	mu   sync.Mutex
	puts map[workflow.BlobRef][]byte
}

func (b *contractBlobs) Put(_ context.Context, _ ports.BlobClass, r io.Reader) (workflow.BlobRef, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.puts == nil {
		b.puts = map[workflow.BlobRef][]byte{}
	}
	ref := workflow.BlobRef("blob:sha256:" + strings.Repeat("d", 64))
	b.puts[ref] = data
	return ref, nil
}

func (b *contractBlobs) Get(context.Context, workflow.BlobRef) (io.ReadCloser, error) {
	return nil, errors.New("not used")
}

// serveRunner starts the runner's Handler on a fresh Unix socket and returns a Client for it. The socket lives in a
// short temporary directory: a Unix socket path is limited to about 100 bytes.
func serveRunner(t *testing.T, exec Executor) (Client, *contractBlobs) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rr")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "runner.sock")
	ctx, cancel := context.WithCancel(context.Background())
	r := jobRunner(t, exec)
	served := make(chan error, 1)
	go func() { served <- Listen(ctx, socket, r.Handler()) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-served)
	})
	blobs := &contractBlobs{}
	c := Client{Socket: socket, Source: contractSource{}, Blobs: blobs}
	require.Eventually(t, func() bool {
		_, err := c.Health(context.Background())
		return err == nil
	}, 5*time.Second, 5*time.Millisecond, "the runner socket answers")
	return c, blobs
}

const junitOut = "\nheimdall-log-begin\npytest ran\n\nheimdall-result-begin\n" +
	`<testsuites><testsuite name="api" tests="2" failures="1"><testcase classname="pay" name="test_refund_ok"></testcase>` +
	`<testcase classname="pay" name="test_refund_twice"><failure message="charged twice">assert 2 == 1</failure></testcase></testsuite></testsuites>` +
	"\n\nheimdall-result-end\nheimdall-exit=1\n"

func TestTheClientAndTheRunnerAgreeOnHealth(t *testing.T) {
	c, _ := serveRunner(t, &checkExecutor{})
	kinds, err := c.Health(context.Background())
	require.NoError(t, err)
	require.Contains(t, kinds, ports.SandboxCheck)
	require.Contains(t, kinds, ports.SandboxTest)
	require.NotContains(t, kinds, ports.SandboxWarm, "a warm job goes to /v1/warm, not /v1/run")
}

func TestTheClientAndTheRunnerAgreeOnACheckJob(t *testing.T) {
	exec := &checkExecutor{output: []byte(sarifOut)}
	c, blobs := serveRunner(t, exec)
	for _, job := range []ports.SandboxJob{
		{Kind: ports.SandboxCheck, CheckID: "patterns", Profile: "acme-pay", Repo: "acme/pay", SHA: sha("a"), Change: "pr-acme-pay-7", Timeout: time.Minute},
		{Kind: ports.SandboxCheck, CheckID: "patterns", Profile: "acme-pay", Repo: "acme/pay", SHA: sha("a"), OnDemand: true, Timeout: time.Minute},
		{Kind: ports.SandboxCheck, CheckID: "patterns", Profile: "acme-pay", Repo: "acme/pay", SHA: sha("a"), Verify: true, Change: "KAN-5", Timeout: time.Minute},
		{Kind: ports.SandboxCheck, CheckID: "compare", Profile: "acme-pay", Repo: "acme/pay", SHA: sha("b"), Head: sha("a"), BaseSHA: sha("b"), Timeout: time.Minute},
	} {
		res, err := c.Run(context.Background(), job)
		require.NoError(t, err, "priority %d", job.Priority())
		require.JSONEq(t, `{"version":"2.1.0","runs":[]}`, string(bytes.TrimSpace(res.Sarif)))
		require.Equal(t, 1, res.Exit)
		require.Equal(t, "scan done\n", string(blobs.puts[res.LogRef]), "the log is stored apart from the result")
	}
	require.Contains(t, strings.Join(exec.args, " "), "dst=/input/base.tgz,ro=true", "the base tree reached the container")
}

func TestTheClientAndTheRunnerAgreeOnATestJob(t *testing.T) {
	exec := &checkExecutor{output: []byte(junitOut)}
	c, _ := serveRunner(t, exec)
	job := ports.SandboxJob{Kind: ports.SandboxTest, TestKind: "api", Profile: "acme-pay", Dir: "tests/heimdall/PAY-1",
		Repo: "acme/pay", SHA: sha("a"), Change: "KAN-5", Timeout: time.Minute}
	res, err := c.RunOverlay(context.Background(), job, []ports.File{{Path: "tests/heimdall/PAY-1/test_refund.py", Content: []byte("def test_x(): pass\n")}})
	require.NoError(t, err)
	require.Equal(t, 1, res.Passed)
	require.Equal(t, 1, res.Failed)
	require.Equal(t, 1, res.Exit)
	require.Contains(t, exec.args, "HEIMDALL_RUN=pytest ./tests/heimdall/PAY-1 --junitxml=/out/junit.xml")
}

func TestTheRunnerRefusesWhatTheClientWouldNeverSend(t *testing.T) {
	c, _ := serveRunner(t, &checkExecutor{output: []byte(sarifOut)})
	_, err := c.Run(context.Background(), ports.SandboxJob{Kind: ports.SandboxCheck, CheckID: "nope", Profile: "acme-pay", Repo: "acme/pay", SHA: sha("a"), Timeout: time.Minute})
	require.ErrorContains(t, err, "runner rejected job", "a check id the profile does not hold is refused by the runner")
}

// A trial whose command ran but printed no go test -json events is still an answer: the client gets a result with no
// tests and the output kept as its log, so the profile service records a failed trial the profiler can read (live,
// Nov 21: `go test {dir}/...` without -json passed its tests and the job died eight times as "no test results").
func TestATrialWithoutTestEventsComesBackWithItsLog(t *testing.T) {
	plain := "ok  \tgithub.com/acme/pay/tests/heimdall/KAN-22\t0.005s\n"
	c, blobs := serveRunner(t, &checkExecutor{output: []byte(plain)})
	artifact := "languages: [go]\nimage: " + goRef + "\nacceptance: go test {dir}/...\nseam: example.com/pay/internal/app\n" +
		"ac_tag: \"// heimdall:{ac}\"\npaths: { tests: tests/heimdall }\n"
	res, err := c.Trial(context.Background(), ports.ProfileTrialJob{Repo: "acme/pay", SHA: sha("a"), Name: "acme-pay",
		Artifact: []byte(artifact), TestsDir: "tests/heimdall"})
	require.NoError(t, err)
	require.Zero(t, res.Passed+res.Failed+res.Errored, "no test was reported")
	require.NotEmpty(t, res.LogRef)
	require.Contains(t, string(blobs.puts[res.LogRef]), "tests/heimdall/KAN-22", "the plain output is the log")
}
