package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Role is what a signed-in person may do on the board. A higher role includes every lower one.
type Role int

// The roles, lowest first. The zero Role is no role and is never granted.
const (
	RoleViewer Role = iota + 1
	RoleApprover
	RoleAdmin
)

// ParseRole reads a role name of heimdall.toml: viewer, approver or admin.
func ParseRole(s string) (Role, error) {
	switch s {
	case "viewer":
		return RoleViewer, nil
	case "approver":
		return RoleApprover, nil
	case "admin":
		return RoleAdmin, nil
	}
	return 0, fmt.Errorf("role %q must be viewer, approver or admin", s)
}

// String is the role name as heimdall.toml writes it.
func (r Role) String() string {
	switch r {
	case RoleViewer:
		return "viewer"
	case RoleApprover:
		return "approver"
	case RoleAdmin:
		return "admin"
	}
	return "none"
}

// Person is one entry of the role map: a person of the people map, the login that proves them, and their role.
type Person struct {
	Name  string
	Login string
	Role  Role
}

// OIDC selects a generic OpenID Connect provider (Okta, Google, Keycloak, Entra ID). Claim names the ID token claim
// that is matched against Person.Login: "email" (only when email_verified is true) or "sub".
type OIDC struct {
	Issuer string
	Claim  string
}

// GitHub selects GitHub OAuth on github.com. GitHub is not an OpenID Connect provider, so the login comes from its
// user API. The endpoints are fixed, not configured: GitHub Enterprise Server is a later work package (D5).
type GitHub struct{}

// githubUserURL is the GitHub user API. The board reads the login of the signed-in user there.
const githubUserURL = "https://api.github.com/user"

// Paths the gate serves. SignOutPath is the page a board links to.
const (
	SignOutPath  = "/auth/logout"
	loginPath    = "/auth/login"
	startPath    = "/auth/start"
	callbackPath = "/auth/callback"
	stylePath    = "/auth/assets/login.css"
)

const (
	minSessionTTL = 5 * time.Minute
	maxSessionTTL = 24 * time.Hour
	// loginTTL bounds the time between the start of a sign-in and the provider's answer.
	loginTTL = 10 * time.Minute
	// providerTimeout bounds every call to the identity provider when Config.HTTPClient is nil.
	providerTimeout = 10 * time.Second
	// pageCSP lets the gate's pages load their style from this origin and post their forms here only.
	pageCSP = "default-src 'none'; style-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
)

// Config is everything the gate needs. Exactly one of OIDC and GitHub is set.
type Config struct {
	ClientID     string
	ClientSecret string
	// RedirectURL is this server's absolute callback URL, ending in /auth/callback. It must equal the URL
	// registered at the provider.
	RedirectURL string
	OIDC        *OIDC
	GitHub      *GitHub
	// SessionKey is the 32-byte AES-256 key that seals the cookies. ParseSessionKey reads it from a secret file.
	SessionKey []byte
	// SessionTTL is the absolute life of a session, from 5 minutes to 24 hours. A session is never extended.
	SessionTTL time.Duration
	People     []Person
	// Now is the clock. The package never reads the system clock.
	Now func() time.Time
	// HTTPClient calls the provider. Nil means a client with a 10 second timeout.
	HTTPClient *http.Client
	Logger     *slog.Logger
}

// Principal is the signed-in person of a request. Read it with FromContext.
type Principal struct {
	Person string
	Login  string
	Role   Role
	// CSRF is the token an unsafe request of this session must carry, in the X-CSRF-Token header or the csrf form
	// field. It is empty in dev mode.
	CSRF string
}

type principalKey struct{}

// FromContext returns the principal that Require or Off put in the request context.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

func withPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// Gate signs people in and guards routes. Build it with New.
type Gate struct {
	provider provider
	sealer   *sealer
	people   *roleMap
	ttl      time.Duration
	now      func() time.Time
	logger   *slog.Logger
	revoked  *revocations
	origin   *http.CrossOriginProtection
	pages    *pages
}

// New checks cfg and builds the gate. It does not call the provider: an OIDC issuer is discovered on the first
// sign-in, so a provider outage closes the board but never stops the server. Errors never hold the client secret.
func New(cfg Config) (*Gate, error) {
	if err := checkConfig(cfg); err != nil {
		return nil, fmt.Errorf("board auth: %w", err)
	}
	people, err := newRoleMap(cfg.People, foldLogins(cfg))
	if err != nil {
		return nil, fmt.Errorf("board auth: %w", err)
	}
	s, err := newSealer(cfg.SessionKey)
	if err != nil {
		return nil, fmt.Errorf("board auth: %w", err)
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: providerTimeout}
	}
	p, label := newProvider(cfg, client)
	pg, err := newPages(label)
	if err != nil {
		return nil, fmt.Errorf("board auth: %w", err)
	}
	return &Gate{
		provider: p, sealer: s, people: people, ttl: cfg.SessionTTL, now: cfg.Now, logger: cfg.Logger,
		revoked: newRevocations(), origin: http.NewCrossOriginProtection(), pages: pg,
	}, nil
}

func checkConfig(cfg Config) error {
	errs := checkProviderChoice(cfg)
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		errs = append(errs, errors.New("the client id and the client secret are required"))
	}
	if u, err := url.Parse(cfg.RedirectURL); err != nil || !u.IsAbs() || u.Host == "" || u.Path != callbackPath {
		errs = append(errs, fmt.Errorf("the redirect URL must be absolute and end in %s", callbackPath))
	}
	if cfg.SessionTTL < minSessionTTL || cfg.SessionTTL > maxSessionTTL {
		errs = append(errs, errors.New("the session TTL must be between 5m and 24h"))
	}
	if cfg.Now == nil || cfg.Logger == nil {
		errs = append(errs, errors.New("the clock and the logger are required"))
	}
	return errors.Join(errs...)
}

// checkProviderChoice requires exactly one provider, and a complete OIDC one.
func checkProviderChoice(cfg Config) []error {
	if (cfg.OIDC == nil) == (cfg.GitHub == nil) {
		return []error{errors.New("exactly one of OIDC and GitHub must be set")}
	}
	if cfg.OIDC == nil {
		return nil
	}
	var errs []error
	if cfg.OIDC.Issuer == "" {
		errs = append(errs, errors.New("the OIDC issuer is required"))
	}
	if cfg.OIDC.Claim != "email" && cfg.OIDC.Claim != "sub" {
		errs = append(errs, fmt.Errorf("the OIDC login claim %q must be email or sub", cfg.OIDC.Claim))
	}
	return errs
}

// foldLogins reports whether logins match without regard to case: GitHub logins and emails do, OIDC subjects do not.
func foldLogins(cfg Config) bool {
	return cfg.GitHub != nil || (cfg.OIDC != nil && cfg.OIDC.Claim == "email")
}

// roleMap finds a person by login or by name.
type roleMap struct {
	fold    bool
	byLogin map[string]Person
	byName  map[string]Person
}

func newRoleMap(people []Person, fold bool) (*roleMap, error) {
	if len(people) == 0 {
		return nil, errors.New("the role map must name at least one person")
	}
	m := &roleMap{fold: fold, byLogin: map[string]Person{}, byName: map[string]Person{}}
	for _, p := range people {
		switch {
		case p.Name == "" || p.Login == "":
			return nil, errors.New("every person needs a name and a login")
		case p.Role < RoleViewer || p.Role > RoleAdmin:
			return nil, fmt.Errorf("person %s has no valid role", p.Name)
		}
		if _, dup := m.byName[p.Name]; dup {
			return nil, fmt.Errorf("person %s is listed twice", p.Name)
		}
		key := m.key(p.Login)
		if other, dup := m.byLogin[key]; dup {
			return nil, fmt.Errorf("person %s has the login of person %s", p.Name, other.Name)
		}
		m.byLogin[key], m.byName[p.Name] = p, p
	}
	return m, nil
}

func (m *roleMap) key(login string) string {
	if m.fold {
		return strings.ToLower(login)
	}
	return login
}

// byProof returns the person a proven login belongs to.
func (m *roleMap) byProof(login string) (Person, bool) {
	p, ok := m.byLogin[m.key(login)]
	return p, ok
}

// current returns the person a session names, but only while the map still gives them the login the session proved.
func (m *roleMap) current(name, login string) (Person, bool) {
	p, ok := m.byName[name]
	if !ok || subtle.ConstantTimeCompare([]byte(m.key(p.Login)), []byte(m.key(login))) != 1 {
		return Person{}, false
	}
	return p, true
}
