package auth_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/changelog"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/auth"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/dashboard"
)

const (
	pay7          = id.ChangeID("PAY-7")
	sessionCookie = "__Host-heimdall-session"
	loginCookie   = "__Host-heimdall-login"
)

// sessionKey is a fixed 32-byte key; otherKey is a second one, for cookies a different server sealed.
var (
	sessionKey = []byte("0123456789abcdef0123456789abcdef")
	otherKey   = []byte("fedcba9876543210fedcba9876543210")
)

// boardRoutes is every route the dashboard serves. No one may read any of them without a session.
func boardRoutes() []string {
	return []string{
		"/", "/changes/PAY-7", "/changes/PAY-7/report", "/changes/PAY-7/report.json", "/sse/changes/PAY-7",
		"/assets/dashboard.css", "/assets/dashboard.js", "/assets/report.css", "/assets/report.js",
		"/changes/NOPE-1", "/assets/missing.css",
	}
}

// defaultPeople is the role map of the rig: one person per role. Logins are emails, the OIDC default claim.
func defaultPeople() []auth.Person {
	return []auth.Person{
		{Name: "vera", Login: "vera@example.com", Role: auth.RoleViewer},
		{Name: "ana", Login: "ana@example.com", Role: auth.RoleApprover},
		{Name: "alice", Login: "alice@example.com", Role: auth.RoleAdmin},
	}
}

// githubPeople is the role map of a GitHub rig. People are matched by the numeric id, never by the login.
func githubPeople() []auth.Person {
	return []auth.Person{
		{Name: "vera", GitHubID: 1001, Role: auth.RoleViewer},
		{Name: "ana", GitHubID: 1002, Role: auth.RoleApprover},
		{Name: "alice", GitHubID: 1003, Role: auth.RoleAdmin},
	}
}

// fakeClock is the rig's clock. Tests move it forward to expire sessions and login states.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// rig is the board behind the gate, the fake identity provider, and a client that never follows redirects.
type rig struct {
	t      *testing.T
	idp    *fakeIDP
	clock  *fakeClock
	board  *httptest.Server
	client *http.Client
	github bool
}

// rigOptions tunes a rig. The zero value is OIDC with the email claim and the default people.
type rigOptions struct {
	github      bool
	claim       string
	people      []auth.Person
	key         []byte
	idp         *fakeIDP
	clock       *fakeClock
	revocations ports.RevocationStore
}

func newRig(t *testing.T, o rigOptions) *rig {
	t.Helper()
	r := &rig{t: t, idp: o.idp, clock: o.clock, github: o.github}
	if r.idp == nil {
		r.idp = newFakeIDP(t)
	}
	if r.clock == nil {
		r.clock = &fakeClock{now: time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)}
	}
	r.board = httptest.NewUnstartedServer(nil)
	redirect := "http://" + r.board.Listener.Addr().String() + "/auth/callback"
	r.idp.redirectURL = redirect
	gate, err := auth.New(r.config(o, redirect))
	require.NoError(t, err)
	r.board.Config.Handler = r.handler(gate)
	r.board.Start()
	t.Cleanup(r.board.Close)
	// Each rig has its own transport. httptest.Server.Close closes the idle connections of http.DefaultTransport, so
	// with a shared transport a parallel test that closes its board broke this test's request.
	transport, ok := http.DefaultTransport.(*http.Transport)
	require.True(t, ok)
	own := transport.Clone()
	t.Cleanup(own.CloseIdleConnections)
	r.client = &http.Client{Transport: own,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return r
}

func (r *rig) config(o rigOptions, redirect string) auth.Config {
	cfg := auth.Config{
		ClientID: clientID, ClientSecret: clientSecret, RedirectURL: redirect,
		SessionKey: o.key, SessionTTL: time.Hour, People: o.people, Now: r.clock.Now,
		HTTPClient: r.idp.srv.Client(), Logger: slog.New(slog.DiscardHandler),
		Revocations: o.revocations, AllowInsecureLoopback: true,
	}
	if cfg.SessionKey == nil {
		cfg.SessionKey = sessionKey
	}
	if cfg.People == nil && o.github {
		cfg.People = githubPeople()
	} else if cfg.People == nil {
		cfg.People = defaultPeople()
	}
	if o.github {
		cfg.GitHub = &auth.GitHub{}
		cfg.HTTPClient = &http.Client{Transport: toFake{fake: r.idp}}
	} else {
		claim := o.claim
		switch claim {
		case "":
			claim = "email"
		case "default":
			claim = "" // the gate's own default
		}
		cfg.OIDC = &auth.OIDC{Issuer: r.idp.issuer(), Claim: claim}
	}
	return cfg
}

// toFake sends the calls the gate makes to github.com and api.github.com to the fake. Only the test's client has it.
type toFake struct{ fake *fakeIDP }

func (t toFake) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != "github.com" && req.URL.Host != "api.github.com" {
		return nil, fmt.Errorf("the test client refuses %s", req.URL.Host)
	}
	target, err := url.Parse(t.fake.srv.URL)
	if err != nil {
		return nil, err
	}
	out := req.Clone(req.Context())
	out.URL.Scheme, out.URL.Host, out.Host = target.Scheme, target.Host, ""
	return t.fake.srv.Client().Transport.RoundTrip(out)
}

// handler is the server's stack: the gate's own routes, then the dashboard behind Require(viewer), then test routes
// that need a higher role or take a POST.
func (r *rig) handler(gate *auth.Gate) http.Handler {
	h := changelog.NewHistory()
	h.Put(pay7, changelog.New(r.t, pay7).Add(change.EventChangeTriggered, changelog.Human("alice"),
		change.Triggered{Ticket: pay7, Repo: "owner/name", Assignee: "alice", Source: change.TriggerJira}).Events())
	router, err := transport.NewRouter(slog.New(slog.DiscardHandler))
	require.NoError(r.t, err)
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") })
	whoami := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		p, found := auth.FromContext(req.Context())
		if !found {
			http.Error(w, "no principal", http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, p.Person+" "+p.Role.String()+" "+p.CSRF)
	})
	mux := http.NewServeMux()
	mux.Handle("/", router)
	mux.Handle("GET /test/approver", gate.Require(auth.RoleApprover)(ok))
	mux.Handle("GET /test/admin", gate.Require(auth.RoleAdmin)(ok))
	mux.Handle("GET /test/whoami", gate.Require(auth.RoleViewer)(whoami))
	mux.Handle("POST /test/write", gate.Require(auth.RoleViewer)(ok))
	board, err := dashboard.Mount(mux, h, slog.New(slog.DiscardHandler), dashboard.Options{
		NewTicker: func(every time.Duration) (<-chan time.Time, func()) {
			tk := time.NewTicker(every)
			return tk.C, tk.Stop
		},
		Access:  gate.Require(auth.RoleViewer),
		SignOut: auth.SignOutPath,
	})
	require.NoError(r.t, err)
	return gate.Mount(board)
}

// reply is one answer with its body read.
type reply struct {
	Status  int
	Header  http.Header
	Body    string
	Cookies []*http.Cookie
}

// do sends one request with the given cookies and headers and reads the answer. An SSE answer is cut after the
// headers and the first flush.
func (r *rig) do(method, path string, body io.Reader, header http.Header, cookies ...*http.Cookie) reply {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(r.t.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, r.board.URL+path, body)
	require.NoError(r.t, err)
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := r.client.Do(req)
	require.NoError(r.t, err)
	defer func() { _ = resp.Body.Close() }()
	var raw []byte
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		buf := make([]byte, 4096)
		n, _ := resp.Body.Read(buf)
		raw = buf[:n]
	} else {
		raw, err = io.ReadAll(resp.Body)
		require.NoError(r.t, err)
	}
	return reply{Status: resp.StatusCode, Header: resp.Header, Body: string(raw), Cookies: resp.Cookies()}
}

func (r *rig) get(path string, cookies ...*http.Cookie) reply {
	r.t.Helper()
	return r.do(http.MethodGet, path, nil, nil, cookies...)
}

// cookieNamed returns the cookie the answer set under name, or nil.
func cookieNamed(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// started is the answer of /auth/start: where the browser goes and the login cookie it carries back.
type started struct {
	authURL *url.URL
	cookie  *http.Cookie
}

func (r *rig) start(next string) started {
	r.t.Helper()
	path := "/auth/start"
	if next != "" {
		path += "?next=" + url.QueryEscape(next)
	}
	resp := r.get(path)
	require.Equal(r.t, http.StatusFound, resp.Status, resp.Body)
	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(r.t, err)
	c := cookieNamed(resp.Cookies, loginCookie)
	require.NotNil(r.t, c, "start sets the login cookie")
	return started{authURL: loc, cookie: c}
}

// claimsFor is a valid ID token for the login state s, with email login.
func (r *rig) claimsFor(s started, email string) map[string]any {
	now := r.clock.Now()
	return map[string]any{
		"iss": r.idp.issuer(), "aud": clientID, "sub": "sub-" + email, "email": email, "email_verified": true,
		"nonce": s.authURL.Query().Get("nonce"), "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}
}

// callback brings the browser back with code and the state of s.
func (r *rig) callback(s started, code string) reply {
	r.t.Helper()
	q := url.Values{"code": {code}, "state": {s.authURL.Query().Get("state")}}
	return r.get("/auth/callback?"+q.Encode(), s.cookie)
}

// finish issues a code for g at the fake and brings the browser back with it.
func (r *rig) finish(s started, g grant) reply {
	r.t.Helper()
	g.challenge = s.authURL.Query().Get("code_challenge")
	r.idp.issue("code-1", g)
	return r.callback(s, "code-1")
}

// loginAs signs in with an OIDC email and returns the session cookie.
func (r *rig) loginAs(email string) *http.Cookie {
	r.t.Helper()
	s := r.start("/")
	return r.sessionOf(r.finish(s, grant{claims: r.claimsFor(s, email)}))
}

// loginGitHub signs in on a GitHub rig as the account with this login and numeric id.
func (r *rig) loginGitHub(login string, id int64) *http.Cookie {
	r.t.Helper()
	return r.sessionOf(r.finish(r.start("/"), grant{login: login, githubID: id}))
}

func (r *rig) sessionOf(resp reply) *http.Cookie {
	r.t.Helper()
	require.Equal(r.t, http.StatusSeeOther, resp.Status, resp.Body)
	c := cookieNamed(resp.Cookies, sessionCookie)
	require.NotNil(r.t, c)
	return c
}

// requireRefused checks a protected answer leaks nothing: 401 with a fixed body, no board data.
func requireRefused(t *testing.T, resp reply, path string) {
	t.Helper()
	require.Equal(t, http.StatusUnauthorized, resp.Status, path)
	require.NotContains(t, resp.Body, "PAY-7", path)
	require.NotContains(t, resp.Body, "owner/name", path)
	require.NotContains(t, resp.Header.Get("Content-Type"), "event-stream", path)
	require.NotContains(t, resp.Header.Get("Content-Type"), "css", path)
	require.NotContains(t, resp.Header.Get("Content-Type"), "javascript", path)
}
