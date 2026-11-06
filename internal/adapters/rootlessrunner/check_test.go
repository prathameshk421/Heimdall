package rootlessrunner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestParseCheckOutputSplitsTheToolTextFromItsExitStatus(t *testing.T) {
	got, ok := parseCheckOutput([]byte("# pay\n./a.go:3:1: bad\nheimdall-check-exit=1\n"))
	require.True(t, ok)
	require.Equal(t, 1, got.Exit)
	require.Equal(t, "# pay\n./a.go:3:1: bad\n", got.Text)

	got, ok = parseCheckOutput([]byte("heimdall-check-exit=0\n"))
	require.True(t, ok)
	require.Zero(t, got.Exit)
}

func TestParseCheckOutputRefusesOutputWithoutAnExitStatus(t *testing.T) {
	_, ok := parseCheckOutput([]byte("./a.go:3:1: bad\n"))
	require.False(t, ok, "a killed job leaves no marker, so it is not a result")
	_, ok = parseCheckOutput([]byte("x heimdall-check-exit=oops"))
	require.False(t, ok)
}

func TestCheckArgsRunTheFixedVetCommandWithNoNetwork(t *testing.T) {
	p := Policy{Images: map[string]string{"go": "example.test/go@sha256:" + strings.Repeat("a", 64)}}
	args, err := p.ContainerArgsCheck(ports.CheckGoVet, "/tmp/src.tgz", "/var/cache/mod", 60)
	require.NoError(t, err)
	require.Contains(t, args, "--network=none")
	last := args[len(args)-1]
	require.Contains(t, last, "go vet ./...")
	require.Contains(t, last, "heimdall-check-exit=")
	require.NotContains(t, last, "go test")
}

func TestCheckArgsRefuseAnUnknownCheck(t *testing.T) {
	p := Policy{Images: map[string]string{"go": "example.test/go@sha256:" + strings.Repeat("a", 64)}}
	_, err := p.ContainerArgsCheck("rm -rf /", "/tmp/src.tgz", "/var/cache/mod", 60)
	require.ErrorIs(t, err, ErrUnknownCheck)
}

type checkExecutor struct {
	output []byte
	args   []string
}

func (e *checkExecutor) Execute(_ context.Context, args []string, _ string) ([]byte, error) {
	e.args = args
	return e.output, nil
}

func checkRequest(t *testing.T) *http.Request {
	t.Helper()
	in := request{Profile: "go", Job: ports.SandboxJob{Kind: ports.SandboxStatic, Check: ports.CheckGoVet, Repo: "acme/pay", SHA: id.SHA(strings.Repeat("a", 40)), Dir: "tests", Timeout: time.Minute}, Archive: []byte("archive")}
	body, err := json.Marshal(in)
	require.NoError(t, err)
	return httptest.NewRequestWithContext(context.Background(), "POST", "/v1/run", strings.NewReader(string(body)))
}

// A static check goes through the same job API: the handler returns the tool text and exit status, and a run that
// printed no exit status is refused instead of passing.
func TestHandlerReturnsACheckResultAndRefusesOutputWithoutAnExitStatus(t *testing.T) {
	policy := Policy{Images: map[string]string{"go": "registry.example/go@sha256:" + strings.Repeat("a", 64)}}

	ok := &checkExecutor{output: []byte("./a.go:3:1: bad\nheimdall-check-exit=1\n")}
	runner := Runner{Policy: policy, Executor: ok, Root: t.TempDir()}
	out, status := runner.handleRun(checkRequest(t))
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, string(out.Output), "./a.go:3:1: bad")
	require.Contains(t, ok.args[len(ok.args)-1], "go vet ./...")

	killed := &checkExecutor{output: []byte("./a.go:3:1: bad\n")}
	runner = Runner{Policy: policy, Executor: killed, Root: t.TempDir()}
	_, status = runner.handleRun(checkRequest(t))
	require.Equal(t, http.StatusUnprocessableEntity, status)
}
