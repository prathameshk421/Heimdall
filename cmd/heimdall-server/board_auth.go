package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/auth"
	dashboardedge "github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/dashboard"
)

// boardProviderTimeout bounds each call to the board's identity provider.
const boardProviderTimeout = 10 * time.Second

// mountBoard puts the web board and its sign-in in front of handler, as [board_auth] says. With no [board_auth] the
// board is not served at all. Dev mode serves it with no sign-in; checkBoardListener then holds it to loopback.
func mountBoard(ctx context.Context, handler http.Handler, cfg config.Config, a *app, logger *slog.Logger) (http.Handler, error) {
	var access func(http.Handler) http.Handler
	var gate *auth.Gate
	switch cfg.BoardAuth.Mode {
	case "":
		logger.InfoContext(ctx, "the web board is off: heimdall.toml has no [board_auth] section")
		return handler, nil
	case config.BoardAuthOff:
		logger.WarnContext(ctx, "the web board has no sign-in (board_auth.mode = off); it serves on loopback only")
		access = auth.Off()
	case config.BoardAuthGitHub, config.BoardAuthOIDC:
		g, err := newBoardGate(cfg, logger)
		if err != nil {
			return nil, err
		}
		gate, access = g, g.Require(auth.RoleViewer)
	default:
		return nil, fmt.Errorf("board_auth.mode %q is not known", cfg.BoardAuth.Mode)
	}
	opts := dashboardedge.Options{NewTicker: newTicker, Access: access}
	if gate != nil {
		opts.SignOut = auth.SignOutPath
	}
	board, err := dashboardedge.Mount(handler, postgres.NewStore(a.pool), logger, opts)
	if err != nil {
		return nil, fmt.Errorf("mount dashboard: %w", err)
	}
	if gate != nil {
		board = gate.Mount(board)
	}
	return board, nil
}

// newBoardGate reads the board secrets and builds the sign-in gate.
func newBoardGate(cfg config.Config, logger *slog.Logger) (*auth.Gate, error) {
	b := cfg.BoardAuth
	secret, err := cfg.Secret(b.ClientSecretSecret)
	if err != nil {
		return nil, fmt.Errorf("board_auth.client_secret_secret: %w", err)
	}
	rawKey, err := cfg.Secret(b.SessionKeySecret)
	if err != nil {
		return nil, fmt.Errorf("board_auth.session_key_secret: %w", err)
	}
	key, err := auth.ParseSessionKey(rawKey)
	if err != nil {
		return nil, fmt.Errorf("board_auth.session_key_secret: %w", err)
	}
	people := make([]auth.Person, 0, len(b.People))
	for _, p := range b.People {
		role, err := auth.ParseRole(p.Role)
		if err != nil {
			return nil, fmt.Errorf("board_auth.people.%s: %w", p.Name, err)
		}
		people = append(people, auth.Person{Name: p.Name, Login: p.Login, GitHubID: p.GitHubID, Role: role})
	}
	ac := auth.Config{
		ClientID: b.ClientID, ClientSecret: secret, RedirectURL: b.RedirectURL, SessionKey: key,
		SessionTTL: b.SessionTTL, People: people, Now: time.Now, Logger: logger,
		HTTPClient: &http.Client{Timeout: boardProviderTimeout},
	}
	if b.Mode == config.BoardAuthGitHub {
		ac.GitHub = &auth.GitHub{}
	} else {
		ac.OIDC = &auth.OIDC{Issuer: b.Issuer, Claim: b.LoginClaim}
	}
	gate, err := auth.New(ac)
	if err != nil {
		return nil, fmt.Errorf("board_auth: %w", err)
	}
	return gate, nil
}

// checkBoardListener holds dev mode to the address the server really listens on. Load already refused a
// non-loopback listen text; this also catches a name that resolves to another interface.
func checkBoardListener(cfg config.Config, ln net.Listener) error {
	if cfg.BoardAuth.Mode != config.BoardAuthOff {
		return nil
	}
	if err := auth.CheckLoopback(ln.Addr()); err != nil {
		return fmt.Errorf("board_auth: %w", err)
	}
	return nil
}
