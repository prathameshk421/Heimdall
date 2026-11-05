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

// MaxBodyBytes is the largest request or response body a Transport buffers. A larger body fails the exchange rather
// than being recorded cut short, because a truncated fixture would look like a real response.
const MaxBodyBytes = 8 << 20

// Options configures New.
type Options struct {
	// Overwrite lets New write into a directory that already holds recordings. Files with the same sequence number
	// are replaced and the rest are kept. Without it, New refuses, so an earlier run is never silently mixed in.
	Overwrite bool
}

// Transport records the exchanges that pass through it.
type Transport struct {
	dir  string
	next http.RoundTripper
	seq  atomic.Int64
}

// New returns a Transport that forwards to next and writes each scrubbed exchange under dir/system. It fails when
// dir/system already holds recordings from an earlier run and opts.Overwrite is not set.
func New(dir, system string, next http.RoundTripper, opts Options) (*Transport, error) {
	target := filepath.Join(dir, system)
	if !opts.Overwrite {
		existing, err := filepath.Glob(filepath.Join(target, "[0-9][0-9][0-9][0-9]-*.json"))
		if err != nil {
			return nil, fmt.Errorf("record: look for earlier recordings in %s: %w", target, err)
		}
		if len(existing) > 0 {
			return nil, fmt.Errorf("record: %s already holds %d recordings from an earlier run; set Overwrite to write over them", target, len(existing))
		}
	}
	return &Transport{dir: target, next: next}, nil
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

// credentialHeader names headers whose values are credentials. They are never written, not even as a name, so the
// file does not say that a credential was sent.
func credentialHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "x-auth-token":
		return true
	}
	return false
}

// requestHeaderAllowed lists the request headers a recording keeps. Anything else is recorded as redacted, keeping
// its name. Pagination is read from response Link headers, so no request header is needed for it.
func requestHeaderAllowed(name string) bool {
	switch strings.ToLower(name) {
	case "content-type", "accept", "user-agent", "content-length", "x-github-event", "x-github-delivery":
		return true
	}
	return false
}

// responseHeaderAllowed lists the response headers that contract and drift checks look at: the content type, the
// entity tag, pagination links, retry hints and rate-limit counters.
func responseHeaderAllowed(name string) bool {
	n := strings.ToLower(name)
	return n == "content-type" || n == "etag" || n == "link" || n == "retry-after" || strings.HasPrefix(n, "x-ratelimit-")
}

// RoundTrip forwards a clone of the request, hands the caller the real response, and records a scrubbed copy. The
// caller's request is not changed.
func (t *Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	reqBody, err := readCapped(r.Body)
	if err != nil {
		return nil, fmt.Errorf("record: request body: %w", err)
	}
	out := r.Clone(r.Context())
	if len(reqBody) > 0 {
		out.Body = io.NopCloser(bytes.NewReader(reqBody))
		out.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(reqBody)), nil }
	}
	resp, err := t.next.RoundTrip(out)
	if err != nil {
		return nil, err // nothing was exchanged, so nothing is recorded
	}
	respBody, err := readCapped(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("record: response body: %w", err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))

	ex := exchange{
		Method:   r.Method,
		URL:      recordedURL(r),
		Status:   resp.StatusCode,
		Request:  side{Headers: recordHeaders(r.Header, requestHeaderAllowed), Body: body(reqBody)},
		Response: side{Headers: recordHeaders(resp.Header, responseHeaderAllowed), Body: body(respBody)},
	}
	if err := t.write(r, ex); err != nil {
		return nil, err
	}
	return resp, nil
}

// readCapped reads and closes a body, failing above MaxBodyBytes. A nil body is empty.
func readCapped(rc io.ReadCloser) ([]byte, error) {
	if rc == nil || rc == http.NoBody {
		return nil, nil
	}
	b, err := io.ReadAll(io.LimitReader(rc, MaxBodyBytes+1))
	cerr := rc.Close()
	if err != nil {
		return nil, err
	}
	if cerr != nil {
		return nil, cerr
	}
	if len(b) > MaxBodyBytes {
		return nil, fmt.Errorf("body is larger than the %d byte limit", MaxBodyBytes)
	}
	return b, nil
}

// recordHeaders keeps the allowed headers (scrubbed), records every other header as redacted under its name, and
// omits credential headers entirely.
func recordHeaders(h http.Header, allowed func(string) bool) map[string]string {
	out := map[string]string{}
	for name, vals := range h {
		switch {
		case credentialHeader(name):
		case allowed(name):
			out[name] = string(Scrub([]byte(strings.Join(vals, ", "))))
		default:
			out[name] = redacted
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

// maxPlainSegment is the longest path segment kept as is in a file name. Longer segments are usually tokens.
const maxPlainSegment = 20

// recordedURL is the request URL as it is written: scrubbed, with credential-looking path segments replaced. A
// webhook path such as /services/T0123/B0456/<token> is a credential even though no rule names it.
func recordedURL(r *http.Request) string {
	u := *r.URL
	u.Path = redactPath(r.URL.Path)
	u.RawPath = ""
	return string(Scrub([]byte(u.String())))
}

// redactPath scrubs a path and replaces each segment that looks like a credential with a placeholder.
func redactPath(path string) string {
	parts := strings.Split(string(Scrub([]byte(path))), "/")
	for i, seg := range parts {
		if suspicious(seg) {
			parts[i] = "redacted"
		}
	}
	return strings.Join(parts, "/")
}

// slugFor turns a request path into a file-name part from its redacted form.
func slugFor(path string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(redactPath(path)), "-"), "-")
	if len(slug) > 80 {
		slug = slug[:80]
	}
	return slug
}

// suspicious reports whether a path segment could be a token rather than a resource name.
func suspicious(seg string) bool {
	if len(seg) > maxPlainSegment {
		return true
	}
	var letters, digits bool
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		switch {
		case c >= '0' && c <= '9':
			digits = true
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			letters = true
		}
	}
	return letters && digits
}

func (t *Transport) write(r *http.Request, ex exchange) error {
	if err := os.MkdirAll(t.dir, 0o750); err != nil {
		return fmt.Errorf("record: create %s: %w", t.dir, err)
	}
	name := fmt.Sprintf("%04d-%s-%s.json", t.seq.Add(1), strings.ToLower(r.Method), slugFor(r.URL.Path))
	data, err := json.MarshalIndent(ex, "", "  ")
	if err != nil {
		return fmt.Errorf("record: encode %s: %w", name, err)
	}
	if err := os.WriteFile(filepath.Join(t.dir, filepath.Base(name)), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("record: write %s: %w", name, err)
	}
	return nil
}
