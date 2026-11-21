package config

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const oidcBoard = `
[board_auth]
mode = "oidc"
issuer = "https://id.example.com/realms/eng"
client_id = "heimdall-board"
client_secret_secret = "board_client_secret"
session_key_secret = "board_session_key"
redirect_url = "https://heimdall.example.com/auth/callback"
session_ttl = "2h"
login_claim = "sub"

[board_auth.people.alice]
login = "00u1abcd"
role = "admin"

[board_auth.people.bob]
login = "00u2efgh"
role = "viewer"
`

const githubBoard = `
[board_auth]
mode = "github"
client_id = "Iv1.abc"
client_secret_secret = "board_client_secret"
session_key_secret = "board_session_key"
redirect_url = "https://heimdall.example.com/auth/callback"

[board_auth.people.ana]
login = "Ana-GH"
role = "approver"
`

func loadBoard(t *testing.T, listen, section string) (Config, error) {
	t.Helper()
	return Load(writeTOML(t, "secrets_dir = \""+t.TempDir()+"\"\nlisten = \""+listen+"\"\n"+section))
}

func TestBoardAuthAbsentLeavesTheBoardOff(t *testing.T) {
	cfg, err := loadBoard(t, "0.0.0.0:8080", "")
	require.NoError(t, err)
	require.Equal(t, BoardAuthMode(""), cfg.BoardAuth.Mode, "no section means no board, never an open one")
}

func TestBoardAuthReadsOIDC(t *testing.T) {
	cfg, err := loadBoard(t, "0.0.0.0:8080", oidcBoard)
	require.NoError(t, err)
	require.Equal(t, BoardAuth{
		Mode:               BoardAuthOIDC,
		Issuer:             "https://id.example.com/realms/eng",
		ClientID:           "heimdall-board",
		ClientSecretSecret: "board_client_secret",
		SessionKeySecret:   "board_session_key",
		RedirectURL:        "https://heimdall.example.com/auth/callback",
		SessionTTL:         2 * time.Hour,
		LoginClaim:         "sub",
		People: []BoardPerson{
			{Name: "alice", Login: "00u1abcd", Role: "admin"},
			{Name: "bob", Login: "00u2efgh", Role: "viewer"},
		},
	}, cfg.BoardAuth)
}

func TestBoardAuthReadsGitHubWithDefaults(t *testing.T) {
	cfg, err := loadBoard(t, "0.0.0.0:8080", githubBoard)
	require.NoError(t, err)
	require.Equal(t, BoardAuthGitHub, cfg.BoardAuth.Mode)
	require.Equal(t, 8*time.Hour, cfg.BoardAuth.SessionTTL, "the default session lasts one work day")
	require.Empty(t, cfg.BoardAuth.Issuer)
	require.Empty(t, cfg.BoardAuth.LoginClaim, "github logins come from the user API, not a claim")
	require.Equal(t, []BoardPerson{{Name: "ana", Login: "Ana-GH", Role: "approver"}}, cfg.BoardAuth.People)
}

func TestBoardAuthOIDCDefaultsToTheVerifiedEmailClaim(t *testing.T) {
	section := strings.Replace(oidcBoard, "login_claim = \"sub\"\n", "", 1)
	cfg, err := loadBoard(t, "0.0.0.0:8080", section)
	require.NoError(t, err)
	require.Equal(t, "email", cfg.BoardAuth.LoginClaim)
}

func TestBoardAuthDevModeNeedsALoopbackListener(t *testing.T) {
	const off = "[board_auth]\nmode = \"off\"\n"
	for _, listen := range []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080"} {
		cfg, err := loadBoard(t, listen, off)
		require.NoError(t, err, listen)
		require.Equal(t, BoardAuthOff, cfg.BoardAuth.Mode)
	}
	for _, listen := range []string{"0.0.0.0:8080", ":8080", "[::]:8080", "10.0.0.5:8080", "heimdall.example.com:8080"} {
		_, err := loadBoard(t, listen, off)
		require.ErrorContains(t, err, "board_auth.mode off needs a loopback listen address", listen)
	}
	// The default listen address is every interface, so dev mode without listen is refused too.
	_, err := Load(writeTOML(t, "secrets_dir = \""+t.TempDir()+"\"\n"+off))
	require.ErrorContains(t, err, "board_auth.mode off needs a loopback listen address")
}

func TestBoardAuthDevModeTakesNoOtherKey(t *testing.T) {
	_, err := loadBoard(t, "127.0.0.1:8080", "[board_auth]\nmode = \"off\"\nclient_id = \"x\"\n")
	require.ErrorContains(t, err, "board_auth.client_id is not used when mode is off")
}

func TestBoardAuthRefusals(t *testing.T) {
	cases := []struct {
		name, from, to, want string
	}{
		{"unknown mode", `mode = "oidc"`, `mode = "saml"`, "board_auth.mode must be github, oidc or off"},
		{"no client id", `client_id = "heimdall-board"`, ``, "board_auth.client_id is required"},
		{"client secret is a path", `client_secret_secret = "board_client_secret"`, `client_secret_secret = "../x"`, "board_auth.client_secret_secret"},
		{"no session key", `session_key_secret = "board_session_key"`, ``, "board_auth.session_key_secret is required"},
		{"plain http issuer", `issuer = "https://id.example.com/realms/eng"`, `issuer = "http://id.example.com"`, "board_auth.issuer"},
		{"no issuer", `issuer = "https://id.example.com/realms/eng"`, ``, "board_auth.issuer is required"},
		{"plain http redirect", `redirect_url = "https://heimdall.example.com/auth/callback"`, `redirect_url = "http://heimdall.example.com/auth/callback"`, "board_auth.redirect_url"},
		{"redirect elsewhere", `redirect_url = "https://heimdall.example.com/auth/callback"`, `redirect_url = "https://heimdall.example.com/cb"`, "board_auth.redirect_url must end in /auth/callback"},
		{"redirect with query", `redirect_url = "https://heimdall.example.com/auth/callback"`, `redirect_url = "https://heimdall.example.com/auth/callback?x=1"`, "board_auth.redirect_url must have no query"},
		{"ttl too long", `session_ttl = "2h"`, `session_ttl = "48h"`, "board_auth.session_ttl must be between 5m and 24h"},
		{"ttl too short", `session_ttl = "2h"`, `session_ttl = "1m"`, "board_auth.session_ttl must be between 5m and 24h"},
		{"ttl not a duration", `session_ttl = "2h"`, `session_ttl = "two hours"`, "board_auth.session_ttl"},
		{"unknown claim", `login_claim = "sub"`, `login_claim = "preferred_username"`, "board_auth.login_claim must be email or sub"},
		{"unknown role", `role = "viewer"`, `role = "owner"`, "board_auth.people.bob.role must be viewer, approver or admin"},
		{"empty login", `login = "00u2efgh"`, `login = ""`, "board_auth.people.bob.login is required"},
		{"login with space", `login = "00u2efgh"`, `login = "a b"`, "board_auth.people.bob.login"},
		{"shared login", `login = "00u2efgh"`, `login = "00u1abcd"`, "board_auth.people.bob.login is also the login of board_auth.people.alice"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Contains(t, oidcBoard, c.from)
			_, err := loadBoard(t, "0.0.0.0:8080", strings.Replace(oidcBoard, c.from, c.to, 1))
			require.ErrorContains(t, err, c.want)
		})
	}
}

func TestBoardAuthNeedsAtLeastOnePerson(t *testing.T) {
	cut := strings.Index(oidcBoard, "[board_auth.people.alice]")
	require.Positive(t, cut)
	section := oidcBoard[:cut]
	_, err := loadBoard(t, "0.0.0.0:8080", section)
	require.ErrorContains(t, err, "board_auth.people must name at least one person")
}

func TestBoardAuthGitHubRefusesOIDCKeys(t *testing.T) {
	_, err := loadBoard(t, "0.0.0.0:8080", strings.Replace(githubBoard, "mode = \"github\"\n",
		"mode = \"github\"\nissuer = \"https://id.example.com\"\nlogin_claim = \"sub\"\n", 1))
	require.ErrorContains(t, err, "board_auth.issuer is used only when mode is oidc")
	require.ErrorContains(t, err, "board_auth.login_claim is used only when mode is oidc")
}

// A GitHub login and an email are matched without regard to case, so two people may not differ only in case.
func TestBoardAuthLoginsDifferingOnlyInCaseClashForGitHub(t *testing.T) {
	section := githubBoard + "\n[board_auth.people.anna]\nlogin = \"ana-gh\"\nrole = \"viewer\"\n"
	_, err := loadBoard(t, "0.0.0.0:8080", section)
	require.ErrorContains(t, err, "is also the login of")
}
