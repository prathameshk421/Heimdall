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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// fakeClock is a ports.Clock that only moves when a test moves it.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// fakeWait records each backoff and advances the fake clock instead of sleeping.
type fakeWait struct {
	clock *fakeClock
	waits []time.Duration
}

func (w *fakeWait) wait(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.waits = append(w.waits, d)
	w.clock.advance(d)
	return nil
}

// recorded is one request the fake GitHub received.
type recorded struct {
	Method, Path, Query, Auth, IfNoneMatch string
	Body                                   []byte
}

// fakeGitHub is an httptest server that answers scripted routes ("METHOD /path") and records every request.
type fakeGitHub struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	routes map[string]http.HandlerFunc
	reqs   []recorded
	// gate, when set, sees every request before its route. A non-zero status refuses the request with it.
	gate func(r *http.Request) int
}

func newFake(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{t: t, routes: map[string]http.HandlerFunc{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) route(method, path string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[method+" "+path] = h
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.reqs = append(f.reqs, recorded{
		Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
		Auth: r.Header.Get("Authorization"), IfNoneMatch: r.Header.Get("If-None-Match"), Body: body,
	})
	h, ok := f.routes[r.Method+" "+r.URL.Path]
	f.mu.Unlock()
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	if f.gate != nil {
		if code := f.gate(r); code != 0 {
			http.Error(w, "refused by token scope", code)
			return
		}
	}
	if !ok {
		f.t.Errorf("unscripted request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		http.Error(w, "no route", http.StatusTeapot)
		return
	}
	h(w, r)
}

func (f *fakeGitHub) requests() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.reqs...)
}

// count returns how many recorded requests match method and path.
func (f *fakeGitHub) count(method, path string) int {
	n := 0
	for _, r := range f.requests() {
		if r.Method == method && r.Path == path {
			n++
		}
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// testKey returns a fresh 2048-bit RSA key and its PKCS#1 PEM encoding.
func testKey(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	return key, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
}

// installTokenRoute scripts POST /app/installations/42/access_tokens. It verifies the App JWT with the public key,
// issues tokens ghs_1, ghs_2, ... that expire an hour after the fake clock, and records nothing itself.
func installTokenRoute(t *testing.T, f *fakeGitHub, clock *fakeClock, key *rsa.PrivateKey) *int {
	t.Helper()
	issued := new(int)
	f.route("POST", "/app/installations/42/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		if err := verifyJWT(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), &key.PublicKey, clock.now); err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials: " + err.Error()})
			return
		}
		*issued++
		writeJSON(w, http.StatusCreated, map[string]any{
			"token":      fmt.Sprintf("ghs_%d", *issued),
			"expires_at": clock.now.Add(time.Hour).UTC().Format(time.RFC3339),
		})
	})
	return issued
}

// verifyJWT checks an RS256 token: header alg, signature, issuer 7 and the iat/exp window around now.
func verifyJWT(token string, pub *rsa.PublicKey, now time.Time) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("jwt has %d parts", len(parts))
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("signature encoding: %w", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
		return fmt.Errorf("signature: %w", err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("payload encoding: %w", err)
	}
	var claims struct {
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
		Iss string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return fmt.Errorf("payload: %w", err)
	}
	if claims.Iss != "7" {
		return fmt.Errorf("issuer %q", claims.Iss)
	}
	if now.Unix() < claims.Iat || now.Unix() >= claims.Exp {
		return fmt.Errorf("jwt not valid at now")
	}
	return nil
}

// newTestAdapter builds an adapter against the fake. Tokens are minted through the fake's token route.
type testEnv struct {
	f     *fakeGitHub
	clock *fakeClock
	wait  *fakeWait
	key   *rsa.PrivateKey
	a     *Adapter
	mints *int
}

// envOption changes the adapter configuration a test env builds.
type envOption func(*Config)

func newTestEnv(t *testing.T, opts ...envOption) *testEnv {
	t.Helper()
	f := newFake(t)
	clock := &fakeClock{now: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
	key, pemBytes := testKey(t)
	mints := installTokenRoute(t, f, clock, key)
	w := &fakeWait{clock: clock}
	cfg := Config{
		BaseURL:    f.srv.URL,
		App:        App{AppID: 7, InstallationID: 42, PrivateKeyPEM: pemBytes},
		HTTPClient: f.srv.Client(),
		Clock:      clock,
		Wait:       w.wait,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &testEnv{f: f, clock: clock, wait: w, key: key, a: a, mints: mints}
}

// testRepo is the fixture repository the unit tests name (no request goes to the real API).
const testRepo = change.Repo("aarush323/heimdall-fixture-pay")
