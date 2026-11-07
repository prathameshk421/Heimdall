// Command heimdall-server.
//
// The server. Wiring only: it builds adapters, hands them to app and starts transports. Configuration
// is validated at boot and the server refuses to start on any error (T17).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport"
)

const (
	// readHeaderTimeout bounds how long a client may take to send headers (slowloris protection).
	readHeaderTimeout = 5 * time.Second
	// shutdownTimeout bounds the graceful shutdown after SIGINT or SIGTERM.
	shutdownTimeout = 10 * time.Second
)

// run loads the config named by args or HEIMDALL_CONFIG, listens with listen on cfg.Listen, and serves
// until ctx is cancelled. Any config problem returns before a listener is opened. listen is a parameter
// because the listen address comes from the config that run itself loads. When the config selects a
// provider, the ticket poller and the outbox drainer run beside the HTTP server (see openApp).
func run(
	ctx context.Context,
	args []string,
	getenv func(string) string,
	logger *slog.Logger,
	listen func(ctx context.Context, network, address string) (net.Listener, error),
) error {
	path, err := config.FilePath(args, getenv)
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	handler, err := transport.NewRouter(logger)
	if err != nil {
		return fmt.Errorf("build router: %w", err)
	}
	a, err := openApp(ctx, cfg, logger)
	if err != nil {
		return err
	}
	if a != nil {
		defer a.close()
	}
	handler = mountHTTP(handler, a)
	ln, err := listen(ctx, "tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Listen, err)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: readHeaderTimeout}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	stopLoops := startLoops(ctx, a)
	err = serveUntilDone(ctx, srv, served)
	stopLoops()
	return err
}

// serveUntilDone waits until the server fails or ctx is cancelled. On cancellation it shuts the server down within
// shutdownTimeout and returns once Serve has returned.
func serveUntilDone(ctx context.Context, srv *http.Server, served <-chan error) error {
	select {
	case err := <-served:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	// WithoutCancel: ctx is already done, but the shutdown needs its own deadline, not the cancelled one.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	var lc net.ListenConfig
	err := run(ctx, os.Args[1:], config.Getenv, logger, lc.Listen)
	stop()
	if err != nil {
		logger.ErrorContext(context.Background(), "heimdall-server did not run", "error", err)
		os.Exit(1)
	}
}

// mountHTTP returns the root HTTP handler. Exact paths are routed: /mcp only when the app built an MCP handler,
// /admin/agents/ only when agent setup is wired, and everything else goes to the router (/healthz among them).
// An exact-path mux keeps /mcp a 404 (not a subtree match) when no slice is selected.
func mountHTTP(router http.Handler, a *app) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", router)
	if a != nil && a.mcpHandler != nil {
		mux.Handle("/mcp", a.mcpHandler)
	}
	if a != nil && a.dashboard != nil {
		mux.Handle("/dashboard", http.StripPrefix("/dashboard", a.dashboard))
		mux.Handle("/dashboard/", http.StripPrefix("/dashboard", a.dashboard))
	}
	if a != nil && a.agentSetupHandler != nil {
		mux.Handle("/admin/agents/", a.agentSetupHandler)
	}
	if a != nil && a.operatorAnswer != nil {
		mux.Handle("/admin/answer", a.operatorAnswer)
	}
	return mux
}
