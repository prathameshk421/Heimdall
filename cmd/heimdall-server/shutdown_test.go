package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// shutdownChildEnv marks the re-executed test binary as the server process.
const shutdownChildEnv = "HEIMDALL_SHUTDOWN_CHILD"

// TestShutdownChildProcess is the child half of TestShutdownOnSignal. Run normally it does nothing. When the parent
// sets shutdownChildEnv it runs the real main, so the real signal handling in main is under test.
func TestShutdownChildProcess(_ *testing.T) {
	if os.Getenv(shutdownChildEnv) != "1" {
		return
	}
	os.Args = []string{"heimdall-server"}
	main()
}

// TestShutdownOnSignal starts the server in a child process, waits until it serves /healthz, sends one signal and
// requires a clean exit. SIGINT and SIGTERM are the two signals main registers for.
func TestShutdownOnSignal(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			addr := freeLoopbackAddr(t)
			cfg := writeConfig(t, strings.NewReplacer(
				"127.0.0.1:0", addr,
				"%s", t.TempDir(),
			).Replace(goodConfig))

			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			// The child gets only the two variables it needs, so no ambient configuration leaks into it.
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestShutdownChildProcess$")
			cmd.Env = []string{shutdownChildEnv + "=1", "HEIMDALL_CONFIG=" + cfg}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			require.NoError(t, cmd.Start())
			exit := waitInBackground(cmd)

			waitHealthy(t, "http://"+addr+"/healthz", cancel, exit, &stderr)
			require.NoError(t, cmd.Process.Signal(sig))

			select {
			case <-exit.done:
				require.NoError(t, exit.err, "server must exit cleanly after %v; stderr:\n%s", sig, stderr.String())
			case <-time.After(15 * time.Second):
				t.Fatalf("server did not exit within 15s after %v", sig)
			}
			require.NotContains(t, stderr.String(), "did not run", "a clean shutdown must not log a failure")
		})
	}
}

// childExit holds the result of cmd.Wait. err is set before done is closed, so a reader after <-done sees it.
type childExit struct {
	done chan struct{}
	err  error
}

// waitInBackground waits for cmd in a goroutine and reports when it has exited.
func waitInBackground(cmd *exec.Cmd) *childExit {
	exit := &childExit{done: make(chan struct{})}
	go func() {
		exit.err = cmd.Wait()
		close(exit.done)
	}()
	return exit
}

// waitHealthy polls url until it answers 200. It fails early if the child exits first. On timeout it kills the child
// and waits for it, so stderr is complete and safe to read.
func waitHealthy(t *testing.T, url string, kill context.CancelFunc, exit *childExit, stderr *bytes.Buffer) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-exit.done:
			t.Fatalf("server exited before it served /healthz: %v; stderr:\n%s", exit.err, stderr.String())
		case <-deadline.C:
			kill()
			<-exit.done
			t.Fatalf("server did not serve /healthz within 10s; stderr:\n%s", stderr.String())
		case <-ticker.C:
			if healthy(url) {
				return
			}
		}
	}
}

func healthy(url string) bool {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// freeLoopbackAddr returns a loopback address that was free a moment ago. The child binds it next.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}
