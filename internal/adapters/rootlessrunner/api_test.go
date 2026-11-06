package rootlessrunner

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

type apiTestExecutor struct{ calls int }

func (e *apiTestExecutor) Execute(_ context.Context, _ []string, _ string) ([]byte, error) {
	e.calls++
	return []byte("{\"Action\":\"pass\",\"Package\":\"fixture\",\"Test\":\"TestSafe\"}\n"), nil
}

func TestHandlerRejectsTooManyOverlaysBeforeExecution(t *testing.T) {
	executor := &apiTestExecutor{}
	runner := Runner{Policy: Policy{Images: map[string]string{"go": "registry.example/go@sha256:" + strings.Repeat("a", 64)}}, Executor: executor, Root: t.TempDir()}
	in := request{Profile: "go", Job: ports.SandboxJob{Kind: ports.SandboxRedFirst, Repo: "acme/pay", SHA: id.SHA(strings.Repeat("a", 40)), Dir: "tests/heimdall/PAY-1", Timeout: time.Minute}, Archive: []byte("archive")}
	for i := range 257 {
		in.Overlays = append(in.Overlays, ports.File{Path: "test-" + strings.Repeat("a", i+1), Content: []byte("test")})
	}
	body, err := json.Marshal(in)
	require.NoError(t, err)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/v1/run", strings.NewReader(string(body)))
	out, status := runner.handleRun(req)
	require.Equal(t, 400, status)
	require.Equal(t, "too many test overlay files", out.Error)
	require.Zero(t, executor.calls)
}
