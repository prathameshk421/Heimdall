package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "__Host-heimdall-session"
	loginCookie   = "__Host-heimdall-login"
	// maxCookieValue bounds what open decodes. A sealed session is about 300 bytes.
	maxCookieValue = 4096
	// clockSkew is how far in the future an issue time may be before the cookie is refused.
	clockSkew = time.Minute
)

// session is the sealed content of the session cookie. Times are Unix seconds. Identity is the stable key the
// provider proved (a GitHub id or an OIDC claim); Require checks it against the role map on every request. Display
// is only what the page may show. A session of an earlier shape carried a "login" key: it does not decode here
// (unknown fields are refused), and so it never opens as any person.
type session struct {
	ID       string `json:"sid"`
	Person   string `json:"person"`
	Identity string `json:"identity"`
	Display  string `json:"display"`
	Issued   int64  `json:"iat"`
	Expires  int64  `json:"exp"`
	CSRF     string `json:"csrf"`
}

// loginState is the sealed content of the login cookie, from /auth/start to /auth/callback.
type loginState struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	Next     string `json:"next"`
	Expires  int64  `json:"exp"`
}

// ParseSessionKey reads the session key from the text of its secret file: 32 bytes, base64 (standard or URL
// alphabet, padded or not). The error never holds the text.
func ParseSessionKey(text string) ([]byte, error) {
	text = strings.TrimSpace(text)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if key, err := enc.DecodeString(text); err == nil && len(key) == 32 {
			return key, nil
		}
	}
	return nil, errors.New("the session key must be 32 bytes in base64 (make one with: openssl rand -base64 32)")
}

// sealer encrypts and authenticates cookie values with AES-256-GCM. The cookie name is the additional data, so a
// value sealed for one cookie never opens as another.
type sealer struct {
	aead cipher.AEAD
}

func newSealer(key []byte) (*sealer, error) {
	if len(key) != 32 {
		return nil, errors.New("the session key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("session cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("session cipher: %w", err)
	}
	return &sealer{aead: aead}, nil
}

func (s *sealer) seal(name string, v any) (string, error) {
	plain, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode %s: %w", name, err)
	}
	nonce := make([]byte, s.aead.NonceSize(), s.aead.NonceSize()+len(plain)+s.aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("seal %s: %w", name, err)
	}
	return base64.RawURLEncoding.EncodeToString(s.aead.Seal(nonce, nonce, plain, []byte(name))), nil
}

// open decrypts a cookie value into v. Any change to the value, or a value sealed with another key or for another
// cookie, is an error.
func (s *sealer) open(name, value string, v any) error {
	if value == "" || len(value) > maxCookieValue {
		return errors.New("cookie value is empty or too long")
	}
	// Strict: the spare bits of the last character must be zero, so each sealed value has exactly one spelling.
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(raw) < s.aead.NonceSize()+s.aead.Overhead() {
		return errors.New("cookie value is not sealed")
	}
	n := s.aead.NonceSize()
	plain, err := s.aead.Open(nil, raw[:n], raw[n:], []byte(name))
	if err != nil {
		return errors.New("cookie value does not open")
	}
	dec := json.NewDecoder(bytes.NewReader(plain))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	return nil
}

// randomToken is 32 random bytes as unpadded base64url: 43 characters.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// setCookie writes a host-only, HttpOnly, Secure, SameSite=Lax cookie. Lax lets the provider's top-level redirect
// back carry the login cookie, and keeps every cross-site subrequest and POST without it.
func setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
}

func clearCookie(w http.ResponseWriter, name string) {
	setCookie(w, name, "", -1)
}

// session returns the open, unexpired and unrevoked session of the request.
func (g *Gate) session(r *http.Request) (session, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return session{}, false
	}
	var s session
	if err := g.sealer.open(sessionCookie, c.Value, &s); err != nil {
		return session{}, false
	}
	now := g.now()
	switch {
	case s.ID == "" || s.CSRF == "" || s.Person == "" || s.Identity == "":
		return session{}, false
	case now.Unix() >= s.Expires || s.Issued > now.Add(clockSkew).Unix():
		return session{}, false
	case s.Expires-s.Issued > int64(maxSessionTTL/time.Second):
		return session{}, false
	case g.revoked.has(s.ID):
		return session{}, false
	}
	return s, true
}

// issue seals a new session for p and sets its cookie. Every sign-in gets a new id and CSRF token.
func (g *Gate) issue(w http.ResponseWriter, p Person, id identity) error {
	sid, err := randomToken()
	if err != nil {
		return err
	}
	csrf, err := randomToken()
	if err != nil {
		return err
	}
	now := g.now()
	value, err := g.sealer.seal(sessionCookie, session{
		ID: sid, Person: p.Name, Identity: id.key, Display: id.display, Issued: now.Unix(), Expires: now.Add(g.ttl).Unix(), CSRF: csrf,
	})
	if err != nil {
		return err
	}
	setCookie(w, sessionCookie, value, int(g.ttl/time.Second))
	return nil
}

// revocations holds the ids of sessions signed out before they expired. It is a cache in this process: an entry
// leaves once its session would have expired anyway.
type revocations struct {
	mu    sync.Mutex
	until map[string]time.Time
}

func newRevocations() *revocations {
	return &revocations{until: map[string]time.Time{}}
}

func (r *revocations) add(id string, until, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, t := range r.until {
		if !t.After(now) {
			delete(r.until, k)
		}
	}
	r.until[id] = until
}

func (r *revocations) has(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.until[id]
	return ok
}
