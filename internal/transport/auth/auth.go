package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
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

// Person is one entry of the role map: a person of the people map, the stable identity that proves them, and their
// role. With GitHub sign-in the identity is GitHubID and Login must be empty. With OIDC it is Login, the value of the
// ID token claim, and GitHubID must be zero. A GitHub login is never matched: it can be renamed, and a freed login
// can be claimed by another account.
type Person struct {
	Name     string
	Login    string
	GitHubID int64
	Role     Role
}

// OIDC selects a generic OpenID Connect provider (Okta, Google, Keycloak, Entra ID). Claim names the ID token claim
// that is matched against Person.Login: "sub" (the default when empty) or "email". An email counts only when
// email_verified is true, and only when the config chooses it: an email can be reassigned to another person, while
// the subject is stable for one issuer.
type OIDC struct {
	Issuer string
	Claim  string
}

// GitHub selects GitHub OAuth on github.com. GitHub is not an OpenID Connect provider, so the identity comes from its
// user API: the numeric id. The endpoints are fixed, not configured: GitHub Enterprise Server is a later work
// package (D5).
type GitHub struct{}

// githubUserURL is the GitHub user API. The board reads the numeric id and the login of the signed-in user there.
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
	// Login is the name the provider showed at sign-in (a GitHub login, an email or a subject). It is for display only:
	// it can change, and the board never grants access by it.
	Login string
	Role  Role
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
	if cfg.OIDC != nil && cfg.OIDC.Claim == "" {
		oc := *cfg.OIDC
		oc.Claim = claimSub
		cfg.OIDC = &oc
	}
	if err := checkConfig(cfg); err != nil {
		return nil, fmt.Errorf("board auth: %w", err)
	}
	people, err := newRoleMap(cfg.People, matchModeOf(cfg))
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
	if cfg.OIDC.Claim != claimEmail && cfg.OIDC.Claim != claimSub {
		errs = append(errs, fmt.Errorf("the OIDC login claim %q must be email or sub", cfg.OIDC.Claim))
	}
	return errs
}

// matchMode says what a proven identity is matched against.
type matchMode int

const (
	matchGitHubID matchMode = iota + 1 // the numeric GitHub user id
	matchSubject                       // the OIDC subject, with its case
	matchEmail                         // the OIDC email, without regard to case
)

func matchModeOf(cfg Config) matchMode {
	switch {
	case cfg.GitHub != nil:
		return matchGitHubID
	case cfg.OIDC.Claim == claimEmail:
		return matchEmail
	}
	return matchSubject
}

// roleMap finds a person by the stable key of their identity, or by name.
type roleMap struct {
	mode   matchMode
	byKey  map[string]Person
	byName map[string]Person
}

func newRoleMap(people []Person, mode matchMode) (*roleMap, error) {
	if len(people) == 0 {
		return nil, errors.New("the role map must name at least one person")
	}
	m := &roleMap{mode: mode, byKey: map[string]Person{}, byName: map[string]Person{}}
	for _, p := range people {
		if p.Name == "" {
			return nil, errors.New("every person needs a name")
		}
		if p.Role < RoleViewer || p.Role > RoleAdmin {
			return nil, fmt.Errorf("person %s has no valid role", p.Name)
		}
		key, err := m.personKey(p)
		if err != nil {
			return nil, err
		}
		if _, dup := m.byName[p.Name]; dup {
			return nil, fmt.Errorf("person %s is listed twice", p.Name)
		}
		if other, dup := m.byKey[key]; dup {
			return nil, fmt.Errorf("person %s has the %s of person %s", p.Name, m.identityName(), other.Name)
		}
		m.byKey[key], m.byName[p.Name] = p, p
	}
	return m, nil
}

func (m *roleMap) identityName() string {
	if m.mode == matchGitHubID {
		return "GitHub id"
	}
	return "login"
}

// personKey is the stable key of a listed person. GitHub people are keyed by id alone: a login is refused, so it
// can never become a fallback.
func (m *roleMap) personKey(p Person) (string, error) {
	if m.mode == matchGitHubID {
		switch {
		case p.GitHubID <= 0:
			return "", fmt.Errorf("person %s needs a github_id (the number of their GitHub account; "+
				"a GitHub login can be renamed and then claimed by another account, so it is never matched)", p.Name)
		case p.Login != "":
			return "", fmt.Errorf("person %s: the login is not used with GitHub sign-in; name the person by github_id only", p.Name)
		}
		return githubKey(p.GitHubID), nil
	}
	switch {
	case p.GitHubID != 0:
		return "", fmt.Errorf("person %s: the GitHub id is used only with GitHub sign-in", p.Name)
	case p.Login == "":
		return "", fmt.Errorf("person %s needs a login", p.Name)
	}
	return m.key(p.Login), nil
}

// githubKey is the key of a GitHub id: its decimal text, the same text the gate puts in a session.
func githubKey(id int64) string {
	return strconv.FormatInt(id, 10)
}

// key folds an email to lower case and leaves a subject or a GitHub id as it is.
func (m *roleMap) key(identity string) string {
	if m.mode == matchEmail {
		return strings.ToLower(identity)
	}
	return identity
}

// byProof returns the person a proven identity belongs to.
func (m *roleMap) byProof(id identity) (Person, bool) {
	p, ok := m.byKey[m.key(id.key)]
	return p, ok
}

// current returns the person a session names, but only while the map still gives them the identity the session
// proved.
func (m *roleMap) current(name, key string) (Person, bool) {
	p, ok := m.byName[name]
	if !ok || key == "" {
		return Person{}, false
	}
	want, err := m.personKey(p)
	if err != nil || subtle.ConstantTimeCompare([]byte(want), []byte(m.key(key))) != 1 {
		return Person{}, false
	}
	return p, true
}
