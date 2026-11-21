package auth_test

import (
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/auth"
)

func validConfig() auth.Config {
	return auth.Config{
		ClientID: clientID, ClientSecret: clientSecret, RedirectURL: "https://heimdall.example.com/auth/callback",
		OIDC: &auth.OIDC{Issuer: "https://id.example.com", Claim: "email"}, SessionKey: sessionKey,
		SessionTTL: time.Hour, People: defaultPeople(), Now: time.Now, Logger: slog.New(slog.DiscardHandler),
	}
}

func TestNewRefusesAnUnsafeConfig(t *testing.T) {
	cases := map[string]func(c *auth.Config){
		"no provider":        func(c *auth.Config) { c.OIDC = nil },
		"two providers":      func(c *auth.Config) { c.GitHub = &auth.GitHub{} },
		"short key":          func(c *auth.Config) { c.SessionKey = sessionKey[:16] },
		"no client id":       func(c *auth.Config) { c.ClientID = "" },
		"no client secret":   func(c *auth.Config) { c.ClientSecret = "" },
		"redirect elsewhere": func(c *auth.Config) { c.RedirectURL = "https://heimdall.example.com/callback" },
		"relative redirect":  func(c *auth.Config) { c.RedirectURL = "/auth/callback" },
		"ttl too long":       func(c *auth.Config) { c.SessionTTL = 25 * time.Hour },
		"ttl too short":      func(c *auth.Config) { c.SessionTTL = time.Minute },
		"no people":          func(c *auth.Config) { c.People = nil },
		"no clock":           func(c *auth.Config) { c.Now = nil },
		"no logger":          func(c *auth.Config) { c.Logger = nil },
		"unknown claim":      func(c *auth.Config) { c.OIDC.Claim = "preferred_username" },
		"no issuer":          func(c *auth.Config) { c.OIDC.Issuer = "" },
		"role zero":          func(c *auth.Config) { c.People = []auth.Person{{Name: "x", Login: "x@example.com"}} },
		"empty login":        func(c *auth.Config) { c.People = []auth.Person{{Name: "x", Role: auth.RoleViewer}} },
		"empty name":         func(c *auth.Config) { c.People = []auth.Person{{Login: "x@example.com", Role: auth.RoleViewer}} },
		"login shared by two": func(c *auth.Config) {
			c.People = append(c.People, auth.Person{Name: "v2", Login: "VERA@example.com", Role: auth.RoleAdmin})
		},
		"person listed twice": func(c *auth.Config) {
			c.People = append(c.People, auth.Person{Name: "vera", Login: "v@example.com", Role: auth.RoleAdmin})
		},
	}
	for name, breakIt := range cases {
		cfg := validConfig()
		cfg.OIDC = &auth.OIDC{Issuer: "https://id.example.com", Claim: "email"}
		breakIt(&cfg)
		_, err := auth.New(cfg)
		require.Error(t, err, name)
		require.NotContains(t, err.Error(), clientSecret, name)
	}
	_, err := auth.New(validConfig())
	require.NoError(t, err)
}

func githubConfig() auth.Config {
	cfg := validConfig()
	cfg.OIDC, cfg.GitHub, cfg.People = nil, &auth.GitHub{}, githubPeople()
	return cfg
}

// GitHub people are matched by the numeric id. A person without one is refused at startup, by name, and the login is
// never a fallback.
func TestNewRefusesAGitHubPersonWithoutAStableId(t *testing.T) {
	cases := map[string]struct {
		people []auth.Person
		want   string
	}{
		"no id":            {[]auth.Person{{Name: "ana", Role: auth.RoleViewer}}, "person ana needs a github_id"},
		"login but no id":  {[]auth.Person{{Name: "ana", Login: "ana-gh", Role: auth.RoleViewer}}, "person ana needs a github_id"},
		"negative id":      {[]auth.Person{{Name: "ana", GitHubID: -5, Role: auth.RoleViewer}}, "person ana needs a github_id"},
		"id and a login":   {[]auth.Person{{Name: "ana", GitHubID: 5, Login: "ana-gh", Role: auth.RoleViewer}}, "person ana: the login is not used"},
		"id shared":        {append(githubPeople(), auth.Person{Name: "ana2", GitHubID: 1002, Role: auth.RoleAdmin}), "person ana2 has the GitHub id of person ana"},
		"second one lacks": {[]auth.Person{{Name: "ana", GitHubID: 5, Role: auth.RoleViewer}, {Name: "bob", Role: auth.RoleViewer}}, "person bob needs a github_id"},
	}
	for name, c := range cases {
		cfg := githubConfig()
		cfg.People = c.people
		_, err := auth.New(cfg)
		require.ErrorContains(t, err, c.want, name)
		require.NotContains(t, err.Error(), clientSecret, name)
	}
	_, err := auth.New(githubConfig())
	require.NoError(t, err)
}

func TestNewRefusesAGitHubIdOutsideGitHubMode(t *testing.T) {
	cfg := validConfig()
	cfg.People = []auth.Person{{Name: "vera", Login: "vera@example.com", GitHubID: 7, Role: auth.RoleViewer}}
	_, err := auth.New(cfg)
	require.ErrorContains(t, err, "person vera: the GitHub id is used only with GitHub sign-in")
}

func TestParseRole(t *testing.T) {
	for in, want := range map[string]auth.Role{"viewer": auth.RoleViewer, "approver": auth.RoleApprover, "admin": auth.RoleAdmin} {
		got, err := auth.ParseRole(in)
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, in, got.String())
	}
	for _, in := range []string{"", "Admin", "owner"} {
		_, err := auth.ParseRole(in)
		require.Error(t, err, in)
	}
}

func TestParseSessionKey(t *testing.T) {
	key, err := auth.ParseSessionKey(base64.StdEncoding.EncodeToString(sessionKey))
	require.NoError(t, err)
	require.Equal(t, sessionKey, key)
	key, err = auth.ParseSessionKey(base64.RawURLEncoding.EncodeToString(sessionKey))
	require.NoError(t, err)
	require.Equal(t, sessionKey, key)
	for _, bad := range []string{"", "short", base64.StdEncoding.EncodeToString(sessionKey[:31]), strings.Repeat("A", 64)} {
		_, err := auth.ParseSessionKey(bad)
		require.Error(t, err)
		if len(bad) >= 8 {
			require.NotContains(t, err.Error(), bad, "the error never repeats the key")
		}
	}
}

func TestDevModeRefusesANonLoopbackListener(t *testing.T) {
	for _, addr := range []net.Addr{
		&net.TCPAddr{IP: net.IPv4zero, Port: 8080}, &net.TCPAddr{IP: net.IPv6unspecified, Port: 8080},
		&net.TCPAddr{IP: net.ParseIP("10.0.0.5"), Port: 8080}, &net.TCPAddr{IP: net.ParseIP("192.168.1.2"), Port: 8080},
		&net.UnixAddr{Name: "/tmp/heimdall.sock", Net: "unix"}, nil,
	} {
		require.Error(t, auth.CheckLoopback(addr), "%v", addr)
	}
	for _, addr := range []net.Addr{
		&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}, &net.TCPAddr{IP: net.IPv6loopback, Port: 8080},
	} {
		require.NoError(t, auth.CheckLoopback(addr), "%v", addr)
	}
}

func TestDevModeServesWithADeveloperPrincipalAndStillChecksOrigin(t *testing.T) {
	h := auth.Off()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := auth.FromContext(r.Context())
		if !ok {
			http.Error(w, "no principal", http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, p.Person+" "+p.Role.String())
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "developer admin", rec.Body.String())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}
