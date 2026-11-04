package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const goodConfig = `
listen = "127.0.0.1:0"
secrets_dir = "%s"

[[projects]]
tracker_project = "KAN"
repo = "aarush323/PBL_TOS_RISK_ANALYZER"
base_branch = "heimdall-demo"
`

func noEnv(string) string { return "" }

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

// realListen is the production listener, with the context as the test would have it.
func realListen(ctx context.Context, network, address string) (net.Listener, error) {
	var lc net.ListenConfig
	return lc.Listen(ctx, network, address)
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "heimdall.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestRunRefusesToStartOnBadConfig(t *testing.T) {
	bad := writeConfig(t, "bogus = true\n")
	listened := false
	listen := func(ctx context.Context, network, address string) (net.Listener, error) {
		listened = true
		return realListen(ctx, network, address)
	}
	err := run(context.Background(), []string{"--config", bad}, noEnv, discard(), listen)
	require.ErrorContains(t, err, "bogus")
	require.False(t, listened, "a server with bad config must not open a listener")
}

func TestRunRefusesWithoutAConfigPath(t *testing.T) {
	err := run(context.Background(), nil, noEnv, discard(), realListen)
	require.ErrorContains(t, err, "HEIMDALL_CONFIG")
}

func TestRunServesHealthzOnLoopbackUntilCancelled(t *testing.T) {
	cfg := writeConfig(t, strings.Replace(goodConfig, "%s", t.TempDir(), 1))
	addrCh := make(chan net.Addr, 1)
	listen := func(ctx context.Context, network, address string) (net.Listener, error) {
		ln, err := realListen(ctx, network, address)
		if err == nil {
			addrCh <- ln.Addr()
		}
		return ln, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- run(ctx, []string{"--config", cfg}, noEnv, discard(), listen) }()

	var addr net.Addr
	select {
	case addr = <-addrCh:
	case err := <-runErr:
		t.Fatalf("run returned before listening: %v", err)
	}
	require.True(t, strings.HasPrefix(addr.String(), "127.0.0.1:"), "loopback only, got %s", addr)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr.String()+"/healthz", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }() // test client: the body is fully read above
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.JSONEq(t, `{"status":"ok"}`, string(body))

	cancel()
	require.NoError(t, <-runErr, "a cancelled context shuts the server down cleanly")
}
