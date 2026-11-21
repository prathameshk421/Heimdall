package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

// Two kinds of failure, so the callback can answer each one right: the provider could not be reached (502), or it
// was reached and the proof did not hold (401).
var (
	errUnavailable = errors.New("identity provider unavailable")
	errRefused     = errors.New("identity proof refused")
)

// maxUserBody bounds the GitHub user answer.
const maxUserBody = 1 << 20

// provider is one identity provider. authURL is where the browser signs in; identify redeems the code with the
// PKCE verifier and returns the proven login.
type provider interface {
	authURL(ctx context.Context, state, nonce, verifier string) (string, error)
	identify(ctx context.Context, code, verifier, nonce string) (string, error)
}

// newProvider builds the selected provider and the label of its sign-in button.
func newProvider(cfg Config, client *http.Client) (provider, string) {
	base := oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.RedirectURL}
	if cfg.GitHub != nil {
		base.Endpoint = github.Endpoint
		base.Endpoint.AuthStyle = oauth2.AuthStyleInParams
		return &githubProvider{oauth: base, client: client}, "Sign in with GitHub"
	}
	base.Scopes = []string{oidc.ScopeOpenID}
	if cfg.OIDC.Claim == "email" {
		base.Scopes = append(base.Scopes, "email")
	}
	return &oidcProvider{issuer: cfg.OIDC.Issuer, claim: cfg.OIDC.Claim, base: base, client: client, now: cfg.Now},
		"Sign in with your company account"
}

// exchange redeems a code. A refusal by the token endpoint is errRefused; anything else is errUnavailable. Neither
// error repeats the endpoint's body, which may hold more than an error code.
func exchange(ctx context.Context, cfg oauth2.Config, client *http.Client, code, verifier string) (*oauth2.Token, error) {
	tok, err := cfg.Exchange(context.WithValue(ctx, oauth2.HTTPClient, client), code, oauth2.VerifierOption(verifier))
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) {
			return nil, fmt.Errorf("%w: token endpoint said %q", errRefused, re.ErrorCode)
		}
		return nil, fmt.Errorf("%w: token endpoint: %w", errUnavailable, err)
	}
	return tok, nil
}

// oidcProvider is a generic OpenID Connect provider. Discovery runs on first use and is kept once it succeeds.
type oidcProvider struct {
	issuer string
	claim  string
	base   oauth2.Config
	client *http.Client
	now    func() time.Time

	mu       sync.Mutex
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func (p *oidcProvider) discover(ctx context.Context) (*oauth2.Config, *oidc.IDTokenVerifier, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.oauth != nil {
		return p.oauth, p.verifier, nil
	}
	// go-oidc keeps this context, without its cancel, for later key set fetches; it carries the client.
	op, err := oidc.NewProvider(oidc.ClientContext(ctx, p.client), p.issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: discover %s: %w", errUnavailable, p.issuer, err)
	}
	cfg := p.base
	cfg.Endpoint = op.Endpoint()
	p.oauth = &cfg
	p.verifier = op.Verifier(&oidc.Config{ClientID: p.base.ClientID, Now: p.now})
	return p.oauth, p.verifier, nil
}

func (p *oidcProvider) authURL(ctx context.Context, state, nonce, verifier string) (string, error) {
	cfg, _, err := p.discover(ctx)
	if err != nil {
		return "", err
	}
	return cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce)), nil
}

// identify redeems the code and verifies the ID token: signature, issuer, audience, expiry and nonce. It returns the
// configured claim. An email counts only when the provider says it is verified.
func (p *oidcProvider) identify(ctx context.Context, code, verifier, nonce string) (string, error) {
	cfg, v, err := p.discover(ctx)
	if err != nil {
		return "", err
	}
	tok, err := exchange(ctx, *cfg, p.client, code, verifier)
	if err != nil {
		return "", err
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return "", fmt.Errorf("%w: the token answer has no ID token", errRefused)
	}
	idt, err := v.Verify(oidc.ClientContext(ctx, p.client), raw)
	if err != nil {
		return "", fmt.Errorf("%w: ID token: %w", errRefused, err)
	}
	if nonce == "" || subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(nonce)) != 1 {
		return "", fmt.Errorf("%w: ID token nonce does not match", errRefused)
	}
	if p.claim == "sub" {
		return idt.Subject, nil
	}
	var claims struct {
		Email    string `json:"email"`
		Verified any    `json:"email_verified"`
	}
	if err := idt.Claims(&claims); err != nil {
		return "", fmt.Errorf("%w: ID token claims: %w", errRefused, err)
	}
	if claims.Email == "" || !isTrue(claims.Verified) {
		return "", fmt.Errorf("%w: the ID token has no verified email", errRefused)
	}
	return claims.Email, nil
}

// isTrue accepts email_verified as a JSON true, or as the text "true" that some providers send.
func isTrue(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b == "true"
	}
	return false
}

// githubProvider is GitHub OAuth with PKCE. The login comes from GET /user with the user's token.
type githubProvider struct {
	oauth  oauth2.Config
	client *http.Client
}

func (p *githubProvider) authURL(_ context.Context, state, _, verifier string) (string, error) {
	return p.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("allow_signup", "false")), nil
}

func (p *githubProvider) identify(ctx context.Context, code, verifier, _ string) (string, error) {
	tok, err := exchange(ctx, p.oauth, p.client, code, verifier)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubUserURL, nil)
	if err != nil {
		return "", fmt.Errorf("%w: user request: %w", errUnavailable, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	tok.SetAuthHeader(req)
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: user API: %w", errUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }() // read-only body: a close error changes nothing
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: user API answered %d", errRefused, resp.StatusCode)
	}
	var user struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxUserBody)).Decode(&user); err != nil {
		return "", fmt.Errorf("%w: user API body: %w", errUnavailable, err)
	}
	if user.Login == "" {
		return "", fmt.Errorf("%w: user API gave no login", errRefused)
	}
	return user.Login, nil
}
