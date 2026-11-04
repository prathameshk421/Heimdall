package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	apiVersion      = "2022-11-28"
	acceptJSON      = "application/vnd.github+json"
	acceptRaw       = "application/vnd.github.raw"
	requestTimeout  = 30 * time.Second
	maxAttempts     = 3
	maxInlineWait   = 2 * time.Minute
	maxBody         = 8 << 20
	breakerLimit    = 5
	breakerCooldown = 30 * time.Second
)

// RateLimitError says GitHub refused for rate-limit reasons and the adapter will not hold the call any longer. It
// wraps ports.ErrUnavailable, so the outbox retries it after RetryAfter.
type RateLimitError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited (status %d), retry after %s", e.Status, e.RetryAfter)
}

// Unwrap makes a rate limit an unavailable outcome.
func (e *RateLimitError) Unwrap() error { return ports.ErrUnavailable }

// request is one GitHub call. Either path and query, or an absolute target from a next-page link, names the URL.
type request struct {
	method string
	path   string
	query  url.Values
	target string
	auth   string
	accept string
	etag   string
	body   []byte
	// conflictMissing maps a 409 reply to ports.ErrNotFound. GitHub answers 409 for a commit lookup in an empty
	// repository. Only calls that set it read 409 that way; a 409 on a write stays an ordinary error.
	conflictMissing bool
}

// response is a fully read reply. Only statuses that statusError accepts reach it.
type response struct {
	status int
	header http.Header
	body   []byte
}

// cachedPRs is an open-PR list served again when GitHub answers 304 to its ETag.
type cachedPRs struct {
	etag string
	prs  []ports.PR
}

// client is the one path to GitHub. It is safe for concurrent use.
type client struct {
	http    *http.Client
	root    string
	origin  *url.URL
	clock   ports.Clock
	wait    func(ctx context.Context, d time.Duration) error
	breaker breaker

	cacheMu sync.Mutex
	prCache map[string]cachedPRs
}

// target builds the URL of a path with its query.
func (c *client) target(path string, query url.Values) string {
	u := c.root + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// call sends one request with a deadline and returns the whole reply. The circuit breaker guards it.
func (c *client) call(ctx context.Context, r request) (*response, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var out *response
	err := c.guarded(func() error {
		resp, err := c.send(ctx, r)
		if err != nil {
			return err
		}
		defer closeBody(resp)
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
		if err != nil {
			return fmt.Errorf("%w: github %s %s: read body: %w", ports.ErrUnavailable, r.method, r.path, err)
		}
		if len(raw) > maxBody {
			return fmt.Errorf("github %s %s: response larger than %d bytes", r.method, r.path, maxBody)
		}
		out = &response{status: resp.StatusCode, header: resp.Header, body: raw}
		return nil
	})
	return out, err
}

// open sends one request and hands back the unread body, for a tarball. The caller closes it. The breaker guards
// only the request, and no deadline is set, because a tarball read outlives any fixed timeout.
func (c *client) open(ctx context.Context, r request) (io.ReadCloser, error) {
	var body io.ReadCloser
	err := c.guarded(func() error {
		resp, err := c.send(ctx, r)
		if err != nil {
			return err
		}
		body = handOver(resp)
		return nil
	})
	return body, err
}

// handOver gives the open body of an accepted reply to the caller, who closes it.
func handOver(resp *http.Response) io.ReadCloser { return resp.Body }

// guarded runs fn unless the breaker is open, and records the outcome.
func (c *client) guarded(fn func() error) error {
	if err := c.breaker.allow(c.clock.Now()); err != nil {
		return err
	}
	err := fn()
	c.breaker.record(c.clock.Now(), err)
	return err
}

// send makes the request and retries rate limits. It returns the open reply of an accepted status, or an error that
// names the method and path but never the body.
func (c *client) send(ctx context.Context, r request) (*http.Response, error) {
	if r.target == "" {
		r.target = c.target(r.path, r.query)
	}
	for attempt := 0; ; attempt++ {
		resp, err := c.once(ctx, r)
		if err != nil {
			return nil, err
		}
		if !isRateLimited(resp) {
			if err := statusError(r, resp.StatusCode); err != nil {
				closeBody(resp)
				return nil, err
			}
			return resp, nil
		}
		wait := rateWait(resp.Header, c.clock.Now(), attempt)
		status := resp.StatusCode
		closeBody(resp)
		if attempt+1 >= maxAttempts || wait > maxInlineWait {
			return nil, fmt.Errorf("github %s %s: %w", r.method, r.path, &RateLimitError{Status: status, RetryAfter: wait})
		}
		if err := c.wait(ctx, wait); err != nil {
			return nil, fmt.Errorf("github %s %s: waiting out rate limit: %w", r.method, r.path, err)
		}
	}
}

// once makes a single HTTP request. A transport failure is unavailable.
func (c *client) once(ctx context.Context, r request) (*http.Response, error) {
	var body io.Reader
	if r.body != nil {
		body = bytes.NewReader(r.body)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, r.target, body)
	if err != nil {
		return nil, fmt.Errorf("github %s %s: build request: %w", r.method, r.path, err)
	}
	accept := r.accept
	if accept == "" {
		accept = acceptJSON
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	if r.auth != "" {
		req.Header.Set("Authorization", "Bearer "+r.auth)
	}
	if r.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.etag != "" {
		req.Header.Set("If-None-Match", r.etag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: github %s %s: %w", ports.ErrUnavailable, r.method, r.path, err)
	}
	return resp, nil
}

// nextLink returns the rel="next" target of a Link header, or "" on the last page. A link that leaves the API origin
// is an error: the adapter never sends its token to another host.
func (c *client) nextLink(link string) (string, error) {
	for _, part := range strings.Split(link, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if len(fields) < 2 || !strings.Contains(fields[1], `rel="next"`) {
			continue
		}
		raw := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(fields[0]), "<"), ">")
		u, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("github: next page link: %w", err)
		}
		if u.Scheme != c.origin.Scheme || u.Host != c.origin.Host {
			return "", errors.New("github: next page link leaves the API host")
		}
		return raw, nil
	}
	return "", nil
}

// listAll collects every page of a list. etag is sent with the first page; a 304 comes back as notModified. The
// returned etag is set only when the list is one page, so a cached list can never be stale on a later page.
func listAll[T any](ctx context.Context, c *client, auth, path string, query url.Values, etag string) (page[T], error) {
	var out page[T]
	req := request{method: http.MethodGet, path: path, query: query, auth: auth, etag: etag}
	for first := true; ; first = false {
		resp, err := c.call(ctx, req)
		if err != nil {
			return page[T]{}, err
		}
		if resp.status == http.StatusNotModified {
			return page[T]{notModified: true}, nil
		}
		items, err := decode[[]T](resp, path)
		if err != nil {
			return page[T]{}, err
		}
		out.items = append(out.items, items...)
		next, err := c.nextLink(resp.header.Get("Link"))
		if err != nil {
			return page[T]{}, err
		}
		if next == "" {
			if first {
				out.etag = resp.header.Get("ETag")
			}
			return out, nil
		}
		req = request{method: http.MethodGet, path: path, auth: auth, target: next}
	}
}

// page is the result of listAll.
type page[T any] struct {
	items       []T
	etag        string
	notModified bool
}

// getPRs and putPRs guard the open-PR cache.
func (c *client) getPRs(key string) (cachedPRs, bool) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	v, ok := c.prCache[key]
	return v, ok
}

func (c *client) putPRs(key string, v cachedPRs) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.prCache[key] = v
}

// breaker opens after breakerLimit consecutive unavailable outcomes and refuses calls for breakerCooldown. After the
// cooldown the next call is a probe: a success closes it, a failure opens it again. Client errors are neutral.
type breaker struct {
	mu        sync.Mutex
	failures  int
	openUntil time.Time
}

func (b *breaker) allow(now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if now.Before(b.openUntil) {
		return fmt.Errorf("%w: github circuit open until %s", ports.ErrUnavailable, b.openUntil.Format(time.RFC3339))
	}
	return nil
}

func (b *breaker) record(now time.Time, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case err == nil:
		b.failures = 0
		b.openUntil = time.Time{}
	case errors.Is(err, ports.ErrUnavailable):
		b.failures++
		if b.failures >= breakerLimit {
			b.openUntil = now.Add(breakerCooldown)
		}
	}
}

// isRateLimited is GitHub's signal for a rate limit: 429, or a 403 with Retry-After or an exhausted quota.
func isRateLimited(resp *http.Response) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	return resp.Header.Get("Retry-After") != "" || resp.Header.Get("X-RateLimit-Remaining") == "0"
}

// rateWait is how long to hold a call before the next attempt: Retry-After, then the reset time, then one minute
// doubling on each attempt.
func rateWait(h http.Header, now time.Time, attempt int) time.Duration {
	if secs, err := strconv.Atoi(h.Get("Retry-After")); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		if d := time.Unix(reset, 0).Sub(now); d > 0 {
			return d
		}
		return 0
	}
	return time.Minute << attempt
}

// statusError maps a reply status to a ports error. Nil means the status is accepted (2xx, or 304 for a cached list).
// The message carries no body, because a body can echo a token.
func statusError(r request, code int) error {
	label := fmt.Sprintf("github %s %s: status %d", r.method, r.path, code)
	switch {
	case code == http.StatusNotModified || (code >= 200 && code < 300):
		return nil
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return fmt.Errorf("%w: %s", ports.ErrUnauthorized, label)
	case code == http.StatusNotFound:
		return fmt.Errorf("%w: %s", ports.ErrNotFound, label)
	case code == http.StatusConflict && r.conflictMissing:
		return fmt.Errorf("%w: %s", ports.ErrNotFound, label)
	case code >= 500:
		return fmt.Errorf("%w: %s", ports.ErrUnavailable, label)
	}
	return errors.New(label)
}

// closeBody discards the rest of a reply. A close error changes nothing the caller can act on.
func closeBody(resp *http.Response) {
	_ = resp.Body.Close()
}

// decode reads a reply body as JSON into T.
func decode[T any](resp *response, path string) (T, error) {
	var v T
	if err := json.Unmarshal(resp.body, &v); err != nil {
		return v, fmt.Errorf("github %s: decode: %w", path, err)
	}
	return v, nil
}
