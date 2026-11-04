package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// App identifies the GitHub App the adapter acts as. PrivateKeyPEM is the App's RSA key in PEM form, PKCS#1 or PKCS#8.
// The key is used only to sign App JWTs. It is never printed and never put in an error.
type App struct {
	AppID          int64
	InstallationID int64
	PrivateKeyPEM  []byte
}

// Config is what New needs. BaseURL is the API root, https://api.github.com in production; tests point it at a fake.
// Clock and Wait are the only sources of time and of waiting, so tests run without sleeping.
type Config struct {
	BaseURL    string
	App        App
	HTTPClient *http.Client
	Clock      ports.Clock
	Wait       func(ctx context.Context, d time.Duration) error
}

// Adapter implements the VCS ports against GitHub. It is safe for concurrent use.
type Adapter struct {
	c      *client
	tokens *TokenSource
}

// The compile-time proof that the adapter satisfies every VCS port.
var (
	_ ports.PRReader     = (*Adapter)(nil)
	_ ports.PRComments   = (*Adapter)(nil)
	_ ports.BranchWriter = (*Adapter)(nil)
	_ ports.CheckWriter  = (*Adapter)(nil)
	_ ports.CodeReader   = (*Adapter)(nil)
	_ ports.RepoReader   = (*Adapter)(nil)
)

// New builds the adapter. It fails on a missing dependency, a base URL that is not absolute, or an App key that does
// not parse. Nothing is sent to GitHub until a method is called.
func New(cfg Config) (*Adapter, error) {
	if cfg.HTTPClient == nil || cfg.Clock == nil || cfg.Wait == nil {
		return nil, errors.New("github: HTTPClient, Clock and Wait are required")
	}
	origin, err := parseOrigin(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	key, err := parseKey(cfg.App.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	c := &client{
		http:    cfg.HTTPClient,
		root:    strings.TrimRight(origin.String(), "/"),
		origin:  origin,
		clock:   cfg.Clock,
		wait:    cfg.Wait,
		prCache: map[string]cachedPRs{},
	}
	tokens := &TokenSource{app: cfg.App, key: key, client: c, clock: cfg.Clock, cache: map[string]cachedToken{}}
	return &Adapter{c: c, tokens: tokens}, nil
}

// Tokens returns the token source. doctor reads the installation's permissions through it.
func (a *Adapter) Tokens() *TokenSource { return a.tokens }

func parseOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("github: base URL %q must be an absolute http or https URL", raw)
	}
	return u, nil
}

// repoPath is the API prefix of one repository. It refuses a name that is not owner/name, so a malformed value can
// never reach a URL.
func repoPath(repo change.Repo) (string, error) {
	if _, err := change.ParseRepo(repo.String()); err != nil {
		return "", fmt.Errorf("github: %w", err)
	}
	return "/repos/" + repo.String(), nil
}

// checkSHA refuses a value that is not a git hash, so it cannot change the path it is put in.
func checkSHA(sha id.SHA) error {
	if _, err := id.ParseSHA(string(sha)); err != nil {
		return fmt.Errorf("github: %w", err)
	}
	return nil
}

// escapePath escapes each segment of a slash-separated path, so a branch or file name cannot add a segment.
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}
