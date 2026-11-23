package auth

import (
	"bytes"
	"context"
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

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	sessionCookie = "__Host-heimdall-session"
	loginCookie   = "__Host-heimdall-login"
	// maxCookieValue bounds what open decodes. A sealed session is about 300 bytes.
	maxCookieValue = 4096
	// clockSkew is how far in the future an issue time may be before the cookie is refused.
	clockSkew = time.Minute
	// maxInMemoryRevocations caps the in-memory fallback revocation map so memory stays bounded.
	maxInMemoryRevocations = 10_000
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

// session returns the open, unexpired and unrevoked session of the request. A revocation lookup failure fails closed.
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
	}
	revoked, err := g.revoked.IsRevoked(r.Context(), s.ID, now)
	if err != nil {
		g.logger.WarnContext(r.Context(), "board auth: revocation lookup failed", "error", err)
		return session{}, false
	}
	if revoked {
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

type revocationEntry struct {
	expiresAt time.Time
	seq       uint64
}

// revocations holds the ids of sessions signed out before they expired when no persistent store is configured.
// Expired entries are pruned on every read and write, and the map is bounded by limit (evicting the oldest entry
// if full).
type revocations struct {
	mu      sync.Mutex
	now     func() time.Time
	limit   int
	seq     uint64
	entries map[string]revocationEntry
}

var _ ports.RevocationStore = (*revocations)(nil)

func newRevocations(now func() time.Time, limit int) *revocations {
	if limit <= 0 {
		limit = maxInMemoryRevocations
	}
	return &revocations{now: now, limit: limit, entries: map[string]revocationEntry{}}
}

func (r *revocations) Revoke(_ context.Context, sid string, expiresAt time.Time) error {
	if sid == "" {
		return errors.New("revoke session: sid is empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	r.pruneLocked(now)
	if !expiresAt.After(now) {
		delete(r.entries, sid)
		return nil
	}
	if existing, ok := r.entries[sid]; ok {
		if expiresAt.After(existing.expiresAt) {
			existing.expiresAt = expiresAt
			r.entries[sid] = existing
		}
		return nil
	}
	if len(r.entries) >= r.limit {
		r.evictOldestLocked()
	}
	r.seq++
	r.entries[sid] = revocationEntry{expiresAt: expiresAt, seq: r.seq}
	return nil
}

func (r *revocations) IsRevoked(_ context.Context, sid string, now time.Time) (bool, error) {
	if sid == "" {
		return false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(now)
	_, ok := r.entries[sid]
	return ok, nil
}

func (r *revocations) Prune(_ context.Context, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(now)
	return nil
}

func (r *revocations) pruneLocked(now time.Time) {
	for k, e := range r.entries {
		if !e.expiresAt.After(now) {
			delete(r.entries, k)
		}
	}
}

func (r *revocations) evictOldestLocked() {
	var oldestKey string
	var oldestSeq uint64
	first := true
	for k, e := range r.entries {
		if first || e.seq < oldestSeq {
			oldestKey, oldestSeq, first = k, e.seq, false
		}
	}
	if !first {
		delete(r.entries, oldestKey)
	}
}
