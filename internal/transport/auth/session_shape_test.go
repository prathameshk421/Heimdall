package auth_test

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// sealForTest seals a payload as the gate does: AES-256-GCM, the cookie name as additional data. It lets a test make
// a cookie of a shape this version never writes, with a key the gate accepts.
func sealForTest(t *testing.T, key []byte, name string, payload map[string]any) *http.Cookie {
	t.Helper()
	plain, err := json.Marshal(payload)
	require.NoError(t, err)
	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	aead, err := cipher.NewGCM(block)
	require.NoError(t, err)
	nonce := make([]byte, aead.NonceSize())
	_, err = rand.Read(nonce)
	require.NoError(t, err)
	return &http.Cookie{Name: name, Value: base64.RawURLEncoding.EncodeToString(aead.Seal(nonce, nonce, plain, []byte(name)))}
}

// A session sealed before sign-in matched a stable id carried a login. It must never open as a person now, even with
// the right key, an unexpired time and a person who exists.
func TestSessionOfTheOldShapeDoesNotGrantAccess(t *testing.T) {
	t.Parallel()
	for _, github := range []bool{false, true} {
		r := newRig(t, rigOptions{github: github})
		person, login := "ana", "ana@example.com"
		now := r.clock.Now()
		base := map[string]any{
			"sid": "old-session-id", "person": person, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
			"csrf": "old-csrf-token",
		}
		current := map[string]any{"identity": "1002", "display": "ana-gh"}
		if !github {
			current = map[string]any{"identity": login, "display": login}
		}

		// Control: the current shape, sealed this way, opens. Without it a refusal below proves nothing.
		ok := map[string]any{}
		for k, v := range base {
			ok[k] = v
		}
		for k, v := range current {
			ok[k] = v
		}
		require.Equal(t, http.StatusOK, r.get("/", sealForTest(t, sessionKey, sessionCookie, ok)).Status, "github=%v control", github)

		// A payload of the right shape sealed for another cookie name does not open as a session.
		foreign := sealForTest(t, sessionKey, loginCookie, ok)
		foreign.Name = sessionCookie
		requireRefused(t, r.get("/", foreign), "sealed for the login cookie")

		// The old shape: a login and no stable identity.
		old := map[string]any{"login": login}
		for k, v := range base {
			old[k] = v
		}
		requireRefused(t, r.get("/", sealForTest(t, sessionKey, sessionCookie, old)), "old shape")
		require.Equal(t, http.StatusUnauthorized, r.get("/test/whoami", sealForTest(t, sessionKey, sessionCookie, old)).Status)

		// A cookie that keeps the old login key beside the new fields is not accepted either.
		mixed := map[string]any{"login": login}
		for k, v := range ok {
			mixed[k] = v
		}
		requireRefused(t, r.get("/", sealForTest(t, sessionKey, sessionCookie, mixed)), "old and new keys")

		// And a current-shape cookie with no identity, or an identity of another person, grants nothing.
		noID := map[string]any{}
		for k, v := range ok {
			noID[k] = v
		}
		noID["identity"] = ""
		requireRefused(t, r.get("/", sealForTest(t, sessionKey, sessionCookie, noID)), "empty identity")
		other := map[string]any{}
		for k, v := range ok {
			other[k] = v
		}
		other["identity"] = "not-ana"
		require.Equal(t, http.StatusForbidden, r.get("/", sealForTest(t, sessionKey, sessionCookie, other)).Status, "identity of no listed person")
	}
}
