package record_test

import (
	"bytes"
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

// shape is a realistic credential literal with the value that must not survive scrubbing. Names around the value
// (a JSON key, a URL host) are not secret and are allowed to stay.
type shape struct{ text, value string }

// awsDocValue is the secret key from the AWS documentation, kept apart from the name that precedes it in the
// literals below so no single line looks like a leaked key.
const awsDocValue = "wJalrXUtnFEMI/K7MDENG/bPx" + "RfiCYEXAMPLEKEY"

// shapes are built from two halves so the test file itself does not look like a leaked key to gitleaks. The values
// are made up but have the real prefixes and lengths.
var shapes = map[string]shape{
	"stripe live secret":      {"sk_li" + "ve_" + "51Hx9aBcDeFgHiJkLmNoPq", "sk_li" + "ve_" + "51Hx9aBcDeFgHiJkLmNoPq"},
	"stripe test secret":      {"sk_te" + "st_" + "51Hx9aBcDeFgHiJkLmNoPq", "sk_te" + "st_" + "51Hx9aBcDeFgHiJkLmNoPq"},
	"stripe restricted live":  {"rk_li" + "ve_" + "51Hx9aBcDeFgHiJkLmNoPq", "rk_li" + "ve_" + "51Hx9aBcDeFgHiJkLmNoPq"},
	"aws secret access key":   {"aws_secret_" + "access_key = " + awsDocValue, awsDocValue},
	"aws secret in json":      {`{"aws_secret_access_key":"` + "wJalrXUtnFEMI/K7MDENG/bPx" + "RfiCYEXAMPLEKEY" + `"}`, "wJalrXUtnFEMI/K7MDENG/bPx" + "RfiCYEXAMPLEKEY"},
	"aws session access key":  {"ASIA" + "IOSFODNN7EXAMPLE", "ASIA" + "IOSFODNN7EXAMPLE"},
	"google api key":          {"AIza" + "SyD9Lm3Qz7Rt5Vx1Bn8Kp2Wc4Hj6Yf0GdEa", "AIza" + "SyD9Lm3Qz7Rt5Vx1Bn8Kp2Wc4Hj6Yf0GdEa"},
	"npm token":               {"npm_" + "a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8", "npm_" + "a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8"},
	"gitlab pat":              {"glpat-" + "Ab3dEf6gHi9jKl2mNo5p", "Ab3dEf6gHi9jKl2mNo5p"},
	"pgp private key block":   {"-----BEGIN PGP PRIVATE KEY BLOCK-----\nlQOYBF" + "abc123\n-----END PGP PRIVATE KEY BLOCK-----", "lQOYBF" + "abc123"},
	"basic auth header value": {`{"headers":{"Authorization":"Basic ` + "dXNlcjpzM2NyM3RQYXNzdzByZA==" + `"}}`, "dXNlcjpzM2NyM3RQYXNzdzByZA"},
	"raw basic auth line":     {"Authorization: Basic " + "dXNlcjpzM2NyM3RQYXNzdzByZA==", "dXNlcjpzM2NyM3RQYXNzdzByZA"},
	"url access_token query":  {"https://example.test/hook?access_token=" + "Zq8Lm2Xv9Rt4Kp7Wn3Hb", "Zq8Lm2Xv9Rt4Kp7Wn3Hb"},
	"form password":           {"username=dana&password=" + "hunter2hunter2hunter2", "hunter2hunter2hunter2"},
	"url signature query":     {"https://example.test/cb?signature=" + "Ab12Cd34Ef56Gh78Ij90Kl", "Ab12Cd34Ef56Gh78Ij90Kl"},
	"url api_key query":       {"https://example.test/x?api_key=" + "Qw3Er5Ty7Ui9Op1As3Df", "Qw3Er5Ty7Ui9Op1As3Df"},
	"json github_token":       {`{"github_token":"` + "Qw3Er5Ty7Ui9Op1As3DfGh" + `"}`, "Qw3Er5Ty7Ui9Op1As3DfGh"},
	"json signing_secret":     {`{"signing_secret":"` + "Qw3Er5Ty7Ui9Op1As3DfGh" + `"}`, "Qw3Er5Ty7Ui9Op1As3DfGh"},
	"json db_passwd":          {`{"db_passwd":"` + "hunter2hunter2hunter2" + `"}`, "hunter2hunter2hunter2"},
}

func TestScrubRemovesNewSecretShapes(t *testing.T) {
	for name, sh := range shapes {
		t.Run(name, func(t *testing.T) {
			in := "before keep me " + sh.text + " after keep me too"
			got := string(record.Scrub([]byte(in)))
			require.NotContains(t, got, sh.value, "the secret value survived scrubbing")
			require.Contains(t, got, "keep me")
			require.Contains(t, got, "keep me too")
			require.Contains(t, got, "[REDACTED]")
		})
	}
}

func TestScrubKeepsTheNameOfAWSSecretAndDropsTheValue(t *testing.T) {
	got := string(record.Scrub([]byte("aws_secret_access_key = " + "wJalrXUtnFEMI/K7MDENG/bPx" + "RfiCYEXAMPLEKEY")))
	require.Equal(t, "aws_secret_access_key = [REDACTED]", got)
}

// Non-secrets that look like secrets must pass through unchanged: a 40-character git SHA is the case the AWS rule
// must not catch, which is why that rule needs the key name in front of the value.
func TestScrubDoesNotMangleLookalikeNonSecrets(t *testing.T) {
	for name, in := range map[string]string{
		"git sha-1 with no key name": "commit 9f1c2ab0c3d4e5f60718293a4b5c6d7e8f901234 merged",
		"uuid":                       "run 3f2504e0-4f89-11d3-9a0c-0305e82c3301 done",
		"base64 image reference":     `<img src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=">`,
		"basic authentication prose": "Use basic authentication over TLS only.",
		"token word in prose":        "the token bucket refills every second",
		"password policy prose":      "password policy: at least twelve characters",
	} {
		require.Equal(t, in, string(record.Scrub([]byte(in))), name)
	}
}

func TestScrubIsIdempotentForNewShapes(t *testing.T) {
	for name, secret := range shapes {
		once := record.Scrub([]byte("x " + secret.text + " y a@b.example"))
		require.Equal(t, string(once), string(record.Scrub(once)), name)
	}
}

func TestScrubRedactsSubstringMatchedJSONKeys(t *testing.T) {
	in := `{"github_token":"aaa111","signing_secret":"bbb222","db_passwd":"ccc333","X-Credential-Id":"ddd444","name":"visible"}`
	got := string(record.Scrub([]byte(in)))
	for _, leaked := range []string{"aaa111", "bbb222", "ccc333", "ddd444"} {
		require.NotContains(t, got, leaked)
	}
	require.Contains(t, got, `"name":"visible"`)
}

func TestScrubRedactsEveryParameterInAFormBody(t *testing.T) {
	got := string(record.Scrub([]byte("user=dana&password=hunter2hunter2&note=keep")))
	require.NotContains(t, got, "hunter2hunter2")
	require.Contains(t, got, "note=keep")
	require.Contains(t, got, "user=dana")
}

func recordedFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "github", "*.json"))
	require.NoError(t, err)
	return files
}

func TestRequestHeadersAreAnAllowlist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	tr, err := record.New(dir, "github", http.DefaultTransport, record.Options{})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/repos/o/r", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Private-Token", "glpat-"+"Ab3dEf6gHi9jKl2mNo5p")
	req.Header.Set("X-Hub-Signature-256", "sha256="+"Qw3Er5Ty7Ui9Op1As3DfGhJk")
	req.Header.Set("X-Custom-Thing", "visible-only-if-allowed")
	resp, err := (&http.Client{Transport: tr}).Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	files := recordedFiles(t, dir)
	require.Len(t, files, 1)
	stored, err := os.ReadFile(files[0]) // #nosec G304 -- path from a glob of the test's own temp dir
	require.NoError(t, err)
	text := string(stored)
	for _, leaked := range []string{"Ab3dEf6gHi9jKl2mNo5p", "Qw3Er5Ty7Ui9Op1As3DfGhJk", "visible-only-if-allowed"} {
		require.NotContains(t, text, leaked)
	}
	require.Contains(t, text, `"Private-Token": "[REDACTED]"`, "a non-allowlisted header keeps its name")
	require.Contains(t, text, `"X-Hub-Signature-256": "[REDACTED]"`)
	require.Contains(t, text, `"Accept": "application/json"`)
}

func TestFileNameIsBuiltFromTheScrubbedPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	tr, err := record.New(dir, "slack", http.DefaultTransport, record.Options{})
	require.NoError(t, err)
	webhook := "/services/T0123/B0456/xxxxsecretwebhooktokenzz"
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+webhook, strings.NewReader(`{"text":"hi"}`))
	require.NoError(t, err)
	resp, err := (&http.Client{Transport: tr}).Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	files, err := filepath.Glob(filepath.Join(dir, "slack", "*.json"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	name := filepath.Base(files[0])
	require.NotContains(t, name, "xxxxsecretwebhooktoken", "the path must not leak into a file name")
	require.NotContains(t, name, "t0123", "a segment that mixes letters and digits is not kept")
	require.Contains(t, name, "post-services")
	stored, err := os.ReadFile(files[0]) // #nosec G304 -- path from a glob of the test's own temp dir
	require.NoError(t, err)
	require.NotContains(t, string(stored), "xxxxsecretwebhooktoken")
}

func TestNewRefusesToOverwriteAnEarlierRun(t *testing.T) {
	dir := t.TempDir()
	first, err := record.New(dir, "github", http.DefaultTransport, record.Options{})
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{}`) }))
	t.Cleanup(srv.Close)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/a", nil)
	require.NoError(t, err)
	resp, err := (&http.Client{Transport: first}).Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	_, err = record.New(dir, "github", http.DefaultTransport, record.Options{})
	require.Error(t, err, "an earlier run's recordings must not be silently overwritten")

	_, err = record.New(dir, "github", http.DefaultTransport, record.Options{Overwrite: true})
	require.NoError(t, err, "Overwrite is the explicit opt-in")
}

// capturing is a stand-in for the real transport that remembers the request it was handed.
type capturing struct{ got *http.Request }

func (c *capturing) RoundTrip(r *http.Request) (*http.Response, error) {
	c.got = r
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
}

func TestRoundTripDoesNotMutateTheCallersRequest(t *testing.T) {
	next := &capturing{}
	dir := t.TempDir()
	tr, err := record.New(dir, "github", next, record.Options{})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://api.example.test/p", bytes.NewReader([]byte(`{"title":"x"}`)))
	require.NoError(t, err)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.NotSame(t, req, next.got, "the transport must send a clone, not the caller's request")
	require.NotNil(t, next.got.GetBody, "the clone must be replayable so retries and redirects resend the body")
	again, err := next.got.GetBody()
	require.NoError(t, err)
	b, err := io.ReadAll(again)
	require.NoError(t, err)
	require.JSONEq(t, `{"title":"x"}`, string(b))
}

func TestRedirectReplaysTheRequestBody(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, string(b))
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/next", http.StatusTemporaryRedirect)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	tr, err := record.New(dir, "github", http.DefaultTransport, record.Options{})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/start", strings.NewReader(`{"v":1}`))
	require.NoError(t, err)
	resp, err := (&http.Client{Transport: tr}).Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, []string{`{"v":1}`, `{"v":1}`}, seen, "a 307 redirect must send the same body again")
}

func TestOversizedResponseIsAnErrorNotATruncation(t *testing.T) {
	big := strings.Repeat("a", record.MaxBodyBytes+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, big) }))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	tr, err := record.New(dir, "github", http.DefaultTransport, record.Options{})
	require.NoError(t, err)
	resp, err := tr.RoundTrip(mustGet(t, srv.URL+"/big"))
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.Error(t, err, "a body over the cap must fail loudly")
	require.Nil(t, resp)
	require.Empty(t, recordedFiles(t, dir), "nothing is recorded for an exchange that failed")
}

func mustGet(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	return req
}
