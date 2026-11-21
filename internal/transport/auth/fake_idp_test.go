package auth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
)

const (
	clientID     = "heimdall-board"
	clientSecret = "board-client-secret"
)

// grant is one authorization code the fake will exchange: the PKCE challenge it was issued for, and what it proves.
type grant struct {
	challenge string
	claims    map[string]any // OIDC: the ID token claims
	login     string         // GitHub: the user login
}

// fakeIDP is an OpenID Connect provider and a GitHub OAuth server in one httptest server. It checks what a real one
// checks: the client credentials, the redirect URL, the PKCE verifier, and single use of each code.
type fakeIDP struct {
	t           *testing.T
	srv         *httptest.Server
	key         *rsa.PrivateKey
	redirectURL string
	mu          sync.Mutex
	codes       map[string]grant
	tokens      map[string]string
	exchanged   int
	// forgeKey, when set, signs the ID tokens instead of the key the JWKS publishes.
	forgeKey *rsa.PrivateKey
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &fakeIDP{t: t, key: key, codes: map[string]grant{}, tokens: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", f.discovery)
	mux.HandleFunc("GET /jwks", f.jwks)
	mux.HandleFunc("POST /token", f.oidcToken)
	mux.HandleFunc("POST /login/oauth/access_token", f.githubToken)
	mux.HandleFunc("GET /user", f.githubUser)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIDP) issuer() string { return f.srv.URL }

// issue registers a code for the challenge the board sent.
func (f *fakeIDP) issue(code string, g grant) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes[code] = g
}

func (f *fakeIDP) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                f.srv.URL,
		"authorization_endpoint":                f.srv.URL + "/authorize",
		"token_endpoint":                        f.srv.URL + "/token",
		"jwks_uri":                              f.srv.URL + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (f *fakeIDP) jwks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &f.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
}

// redeem checks the token request and returns the grant of its code, or answers the request with an error.
func (f *fakeIDP) redeem(w http.ResponseWriter, r *http.Request) (grant, bool) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return grant{}, false
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	code := r.PostForm.Get("code")
	g, found := f.codes[code]
	delete(f.codes, code)
	switch {
	case id != clientID || secret != clientSecret:
		writeError(w, "invalid_client")
	case !found:
		writeError(w, "invalid_grant")
	case r.PostForm.Get("redirect_uri") != f.redirectURL:
		writeError(w, "invalid_grant")
	case challengeOf(r.PostForm.Get("code_verifier")) != g.challenge:
		writeError(w, "invalid_grant")
	default:
		f.exchanged++
		return g, true
	}
	return grant{}, false
}

func (f *fakeIDP) oidcToken(w http.ResponseWriter, r *http.Request) {
	g, ok := f.redeem(w, r)
	if !ok {
		return
	}
	raw := f.sign(g.claims)
	if f.forgeKey != nil {
		raw = signWith(f.t, f.forgeKey, g.claims)
	}
	writeJSON(w, map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 300, "id_token": raw})
}

func (f *fakeIDP) githubToken(w http.ResponseWriter, r *http.Request) {
	g, ok := f.redeem(w, r)
	if !ok {
		return
	}
	token := "gho_" + base64.RawURLEncoding.EncodeToString([]byte(g.login))
	f.mu.Lock()
	f.tokens[token] = g.login
	f.mu.Unlock()
	writeJSON(w, map[string]any{"access_token": token, "token_type": "bearer", "scope": ""})
}

func (f *fakeIDP) githubUser(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	login, ok := f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	f.mu.Unlock()
	if !ok {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{"login": login, "id": 7})
}

// sign makes a compact RS256 JWT of the claims with the fake's key.
func (f *fakeIDP) sign(claims map[string]any) string {
	return signWith(f.t, f.key, claims)
}

func signWith(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	require.NoError(t, err)
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	jws, err := signer.Sign(payload)
	require.NoError(t, err)
	raw, err := jws.CompactSerialize()
	require.NoError(t, err)
	return raw
}

func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
