package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	// tokenTTL is how long a minted installation token is reused. GitHub tokens last an hour, so the margin is 10 min.
	tokenTTL = 50 * time.Minute
	// jwtTTL is the life of an App JWT. GitHub accepts at most 10 minutes.
	jwtTTL = 9 * time.Minute
	// jwtSkew back-dates iat so a small clock difference does not reject the JWT.
	jwtSkew = time.Minute
	// jwtHeader is the fixed RS256 header of every App JWT.
	jwtHeader = `{"alg":"RS256","typ":"JWT"}`
)

// Op names one write operation. Each one has its own token, narrowed to the permission it needs.
type Op string

// The write operations. A token minted for one cannot do the work of another.
const (
	OpCheckWrite   Op = "check_write"
	OpBranchWrite  Op = "branch_write"
	OpCommentWrite Op = "comment_write"
)

// cachedToken is an installation token and the time until which it is reused.
type cachedToken struct {
	token string
	until time.Time
}

// TokenSource mints installation tokens from the App JWT and caches them. The key stays inside it. It is safe for
// concurrent use.
type TokenSource struct {
	app    App
	key    *rsa.PrivateKey
	client *client
	clock  ports.Clock

	mu    sync.Mutex
	cache map[string]cachedToken
}

// Reader returns a token that can read contents, pull requests, issues and checks across the installation. It cannot
// write anything.
func (t *TokenSource) Reader(ctx context.Context) (string, error) {
	perms := map[string]string{"contents": "read", "pull_requests": "read", "issues": "read", "checks": "read"}
	return t.mint(ctx, "reader", perms, nil)
}

// Writer returns a token narrowed to one operation and to one repository. Writer(op, repo) can do nothing else.
func (t *TokenSource) Writer(ctx context.Context, op Op, repo change.Repo) (string, error) {
	perms, ok := writerPermissions(op)
	if !ok {
		return "", fmt.Errorf("github: unknown token operation %q", string(op))
	}
	name, err := repoName(repo)
	if err != nil {
		return "", err
	}
	return t.mint(ctx, "writer|"+string(op)+"|"+repo.String(), perms, []string{name})
}

// writerPermissions is the one permission each write operation needs.
func writerPermissions(op Op) (map[string]string, bool) {
	switch op {
	case OpCheckWrite:
		return map[string]string{"checks": "write"}, true
	case OpBranchWrite:
		return map[string]string{"contents": "write"}, true
	case OpCommentWrite:
		return map[string]string{"pull_requests": "write", "issues": "write"}, true
	}
	return nil, false
}

// repoName is the repository part of owner/name, which is what an installation token's repositories list names.
func repoName(repo change.Repo) (string, error) {
	if _, err := change.ParseRepo(repo.String()); err != nil {
		return "", fmt.Errorf("github: %w", err)
	}
	_, name, _ := strings.Cut(repo.String(), "/")
	return name, nil
}

// cached returns a token still inside its cache window.
func (t *TokenSource) cached(key string, now time.Time) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.cache[key]
	if !ok || !now.Before(c.until) {
		return "", false
	}
	return c.token, true
}

// mint returns a cached token or asks GitHub for a new one. The request carries the App JWT. A nil repos list means
// the token is not narrowed to repositories.
func (t *TokenSource) mint(ctx context.Context, key string, perms map[string]string, repos []string) (string, error) {
	now := t.clock.Now()
	if tok, ok := t.cached(key, now); ok {
		return tok, nil
	}
	jwt, err := t.appJWT(now)
	if err != nil {
		return "", err
	}
	body := map[string]any{"permissions": perms}
	if repos != nil {
		body["repositories"] = repos
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("github: encode token request: %w", err)
	}
	path := fmt.Sprintf("/app/installations/%d/access_tokens", t.app.InstallationID)
	resp, err := t.client.call(ctx, request{method: http.MethodPost, path: path, auth: jwt, body: raw})
	if err != nil {
		return "", fmt.Errorf("github installation token: %w", err)
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(resp.body, &out); err != nil || out.Token == "" {
		return "", errors.New("github installation token: reply has no token")
	}
	until := now.Add(tokenTTL)
	if out.ExpiresAt.Before(until) {
		until = out.ExpiresAt
	}
	t.mu.Lock()
	t.cache[key] = cachedToken{token: out.Token, until: until}
	t.mu.Unlock()
	return out.Token, nil
}

// appJWT signs an RS256 JWT for the App: header, claims, then a PKCS#1 v1.5 signature over SHA-256.
func (t *TokenSource) appJWT(now time.Time) (string, error) {
	claims, err := json.Marshal(map[string]any{
		"iat": now.Add(-jwtSkew).Unix(),
		"exp": now.Add(jwtTTL).Unix(),
		"iss": strconv.FormatInt(t.app.AppID, 10),
	})
	if err != nil {
		return "", fmt.Errorf("github: encode app JWT: %w", err)
	}
	signing := base64.RawURLEncoding.EncodeToString([]byte(jwtHeader)) + "." +
		base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, t.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", errors.New("github: sign app JWT failed")
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// parseKey reads the App's RSA key, PKCS#1 or PKCS#8. Its errors never include the key.
func parseKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("github: App private key is not PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("github: App private key is neither PKCS#1 nor PKCS#8")
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("github: App private key is not RSA")
	}
	return key, nil
}
