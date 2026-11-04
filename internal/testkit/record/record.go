package record

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
)

// Transport records the exchanges that pass through it.
type Transport struct {
	dir  string
	next http.RoundTripper
	seq  atomic.Int64
}

// New returns a Transport that forwards to next and writes each scrubbed exchange under dir/system.
func New(dir, system string, next http.RoundTripper) *Transport {
	return &Transport{dir: filepath.Join(dir, system), next: next}
}

type exchange struct {
	Method   string `json:"method"`
	URL      string `json:"url"`
	Status   int    `json:"status"`
	Request  side   `json:"request"`
	Response side   `json:"response"`
}

type side struct {
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// credentialHeaders are never written, scrubbed or not.
func credentialHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "x-auth-token":
		return true
	}
	return false
}

// keptResponseHeader limits response headers to the ones contract and drift checks look at.
func keptResponseHeader(name string) bool {
	n := strings.ToLower(name)
	return n == "content-type" || n == "etag" || n == "link" || n == "retry-after" || strings.HasPrefix(n, "x-ratelimit-")
}

// RoundTrip forwards the request, hands the caller the real response, and records a scrubbed copy.
func (t *Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	var reqBody []byte
	if r.Body != nil {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, fmt.Errorf("record: read request body: %w", err)
		}
		if err := r.Body.Close(); err != nil {
			return nil, fmt.Errorf("record: close request body: %w", err)
		}
		reqBody = b
		r.Body = io.NopCloser(bytes.NewReader(b))
	}
	resp, err := t.next.RoundTrip(r)
	if err != nil {
		return nil, err // nothing was exchanged, so nothing is recorded
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("record: read response body: %w", err)
	}
	if err := resp.Body.Close(); err != nil {
		return nil, fmt.Errorf("record: close response body: %w", err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))

	ex := exchange{
		Method:   r.Method,
		URL:      string(Scrub([]byte(r.URL.String()))),
		Status:   resp.StatusCode,
		Request:  side{Headers: requestHeaders(r.Header), Body: body(reqBody)},
		Response: side{Headers: responseHeaders(resp.Header), Body: body(respBody)},
	}
	if err := t.write(r, ex); err != nil {
		return nil, err
	}
	return resp, nil
}

func requestHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for name, vals := range h {
		if !credentialHeader(name) {
			out[name] = string(Scrub([]byte(strings.Join(vals, ", "))))
		}
	}
	return out
}

func responseHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for name, vals := range h {
		if keptResponseHeader(name) && !credentialHeader(name) {
			out[name] = string(Scrub([]byte(strings.Join(vals, ", "))))
		}
	}
	return out
}

// body scrubs b and returns it as JSON: as-is when it is JSON, otherwise as a string.
func body(b []byte) json.RawMessage {
	if len(b) == 0 {
		return nil
	}
	clean := Scrub(b)
	if json.Valid(clean) {
		return clean
	}
	quoted, err := json.Marshal(string(clean))
	if err != nil {
		return nil // a string always marshals; this keeps the signature honest
	}
	return quoted
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func (t *Transport) write(r *http.Request, ex exchange) error {
	if err := os.MkdirAll(t.dir, 0o750); err != nil {
		return fmt.Errorf("record: create %s: %w", t.dir, err)
	}
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(r.URL.Path), "-"), "-")
	if len(slug) > 80 {
		slug = slug[:80]
	}
	name := fmt.Sprintf("%04d-%s-%s.json", t.seq.Add(1), strings.ToLower(r.Method), slug)
	data, err := json.MarshalIndent(ex, "", "  ")
	if err != nil {
		return fmt.Errorf("record: encode %s: %w", name, err)
	}
	if err := os.WriteFile(filepath.Join(t.dir, filepath.Base(name)), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("record: write %s: %w", name, err)
	}
	return nil
}
