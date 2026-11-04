package record_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/record"
)

// secrets are shapes of real credentials. The values are made up but have the real prefixes and lengths.
var secrets = map[string]string{
	"github classic token":    "ghp_R7qXk2mPz9LwT4vNb8YcD1eHf6JsUa3GoIx5",
	"github fine-grained pat": "github_pat_11ABCDEFG0abcdefghijkl_mnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghij",
	"github app token":        "ghs_16C7e42F292c6912E7710c838347Ae178B4a",
	"openrouter key":          "sk-or-v1-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	"generic sk key":          "sk-proj-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789",
	"atlassian token":         "ATATT3xFfGF0AbCdEfGhIjKlMnOpQrStUvWxYz0123456789=AB12CD34",
	"aws access key":          "AKIAIOSFODNN7EXAMPLE",
	"slack token":             "xoxb-123456789012-1234567890123-AbCdEfGhIjKlMnOpQrStUvWx",
	"jwt":                     "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4ifQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
	"bearer header":           "Bearer 9f8e7d6c5b4a39281706f5e4d3c2b1a0",
	"private key":             "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAxyz\nabcdef\n-----END RSA PRIVATE KEY-----",
	"database password":       "postgres://heimdall:s3cr3tPassw0rd@db.internal:5432/heimdall",
}

func TestScrubRemovesEverySecretShape(t *testing.T) {
	for name, secret := range secrets {
		t.Run(name, func(t *testing.T) {
			in := `{"before":"keep me","value":"` + strings.ReplaceAll(secret, "\n", `\n`) + `","after":"keep me too"}`
			got := string(record.Scrub([]byte(in)))
			for _, fragment := range strings.FieldsFunc(secret, func(r rune) bool { return r == ' ' || r == '\n' || r == ':' || r == '@' || r == '/' }) {
				if len(fragment) < 12 || strings.HasPrefix(fragment, "-----") {
					continue
				}
				require.NotContains(t, got, fragment, "a fragment of the secret survived scrubbing")
			}
			require.Contains(t, got, "keep me", "scrubbing must not eat the surrounding payload")
			require.Contains(t, got, "keep me too")
			require.Contains(t, got, "[REDACTED]")
		})
	}
}

func TestScrubRedactsSecretNamedJSONFields(t *testing.T) {
	in := `{"password":"hunter2hunter2","client_secret":"abc123","api_key":"zzz","token":"qqq","name":"visible","private_key":"k"}`
	got := string(record.Scrub([]byte(in)))
	for _, leaked := range []string{"hunter2hunter2", "abc123", "zzz", "qqq"} {
		require.NotContains(t, got, leaked)
	}
	require.Contains(t, got, `"name":"visible"`)
}

func TestScrubReplacesEmailAddresses(t *testing.T) {
	got := string(record.Scrub([]byte(`{"email":"dana.k@acme.example","note":"mail pat@corp.test now"}`)))
	require.NotContains(t, got, "dana.k@acme.example")
	require.NotContains(t, got, "pat@corp.test")
	require.Contains(t, got, "@example.invalid")
}

func TestScrubKeepsOrdinaryPayloadsIntact(t *testing.T) {
	in := `{"id":42,"sha":"9f1c2ab0c3d4e5f60718293a4b5c6d7e8f901234","title":"Add refunds","labels":["heimdall-test"],"url":"https://api.github.com/repos/o/r/pulls/7"}`
	require.JSONEq(t, in, string(record.Scrub([]byte(in))), "a commit sha, ids and urls are not secrets")
}

func TestScrubIsIdempotent(t *testing.T) {
	for _, secret := range secrets {
		once := record.Scrub([]byte(`{"v":"` + strings.ReplaceAll(secret, "\n", `\n`) + `","e":"a@b.example"}`))
		require.Equal(t, string(once), string(record.Scrub(once)))
	}
}

func FuzzScrubNeverLeaksAPlantedSecret(f *testing.F) {
	f.Add("prefix ", " suffix")
	f.Add(`{"a":"`, `"}`)
	f.Add("\n", "\n")
	f.Fuzz(func(t *testing.T, before, after string) {
		for _, secret := range []string{secrets["github classic token"], secrets["openrouter key"], secrets["atlassian token"], secrets["aws access key"]} {
			out := string(record.Scrub([]byte(before + secret + after)))
			if strings.Contains(out, secret) {
				t.Fatalf("secret survived inside %q", before+secret+after)
			}
		}
	})
}

func TestTransportWritesAScrubbedExchangeAndNeverTheAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "session=topsecretcookie")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		_, _ = io.WriteString(w, `{"id":7,"token":"`+secrets["github classic token"]+`","owner":"dana.k@acme.example"}`)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	client := &http.Client{Transport: record.New(dir, "github", http.DefaultTransport)}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/repos/o/r/pulls/7", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+secrets["github classic token"])
	req.Header.Set("Cookie", "session=topsecretcookie")
	resp, err := client.Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Contains(t, string(body), secrets["github classic token"], "the caller must still receive the real response")

	files, err := filepath.Glob(filepath.Join(dir, "github", "*.json"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	stored, err := os.ReadFile(files[0]) // #nosec G304 -- path from a glob of the test's own temp dir
	require.NoError(t, err)
	text := string(stored)
	for _, leaked := range []string{secrets["github classic token"], "topsecretcookie", "dana.k@acme.example", "Authorization", "Cookie"} {
		require.NotContains(t, text, leaked)
	}
	require.Contains(t, text, "/repos/o/r/pulls/7")
	require.Contains(t, text, "4999", "rate-limit headers are kept: drift checks use them")
	require.Contains(t, filepath.Base(files[0]), "get-repos-o-r-pulls-7")
}
