// Package switchchat is the Switch client of the coordinator agent: rooms, references, posts, history reads and the
// event stream (TECH_SPEC 13, D46). It implements ports.Chat.
package switchchat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Secrets reads one secret by file name. config.Config satisfies it.
type Secrets interface {
	Secret(name string) (string, error)
}

// Config is what New needs. BaseURL is the bridge base URL (the agent bridge, not the Gateway). Secrets is read on
// every call, so a rotated key file is picked up without a restart. HTTPClient must not set a Timeout: the event
// stream is long-lived, and every other call has its own 10 s deadline (TECH_SPEC 2.5).
type Config struct {
	BaseURL   string
	AgentID   string
	AgentName string
	BridgeID  string
	// SharedRoom, when set, is the one standing channel every change opens into: OpenRoom returns it instead of making
	// a room per change.
	SharedRoom change.RoomID
	// InternalRooms makes each change's room an agent-only working room with no chat channel, so the agent has one
	// session per ticket while people read the story in the standing channels.
	InternalRooms      bool
	Secrets            Secrets
	APIKeySecret       string
	RegistrationSecret string
	HTTPClient         *http.Client
	Clock              ports.Clock
	Wait               func(ctx context.Context, d time.Duration) error
}

const (
	// requestDeadline bounds every call except the event stream itself (TECH_SPEC 2.5).
	requestDeadline = 10 * time.Second
	// breakerLimit unavailable outcomes in a row open the breaker, which then refuses calls for breakerCooldown.
	breakerLimit    = 5
	breakerCooldown = 30 * time.Second
	// maxReply bounds one reply body. A larger reply is a fault, not data.
	maxReply = 1 << 20
)

// Client talks to one Switch bridge as the coordinator agent. It is safe for concurrent use.
type Client struct {
	http          *http.Client
	baseURL       string
	agentID       string
	agentName     string
	bridgeID      string
	sharedRoom    change.RoomID
	internalRooms bool
	secrets       Secrets
	apiKeySecret  string
	regSecret     string
	clock         ports.Clock
	wait          func(ctx context.Context, d time.Duration) error
	breaker       breaker

	// postMu serialises Post, so the check of posted and the send cannot interleave.
	postMu sync.Mutex
	posted map[change.EffectKey]bool
}

var _ ports.Chat = (*Client)(nil)
var _ ports.ChatStream = (*Client)(nil)

// StatusOf returns the HTTP status of a non-2xx Switch reply that err carries.
func StatusOf(err error) (int, bool) {
	var re *replyError
	if errors.As(err, &re) {
		return re.status, true
	}
	return 0, false
}

// New builds the client. It fails on a missing dependency or a base URL that is not absolute. Nothing is sent.
func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("switch: base url must be an absolute http or https url")
	}
	switch {
	case cfg.AgentID == "":
		return nil, errors.New("switch: agent id is required")
	case cfg.Secrets == nil:
		return nil, errors.New("switch: secrets are required")
	case cfg.APIKeySecret == "" || cfg.RegistrationSecret == "":
		return nil, errors.New("switch: api key and registration secret names are required")
	case cfg.HTTPClient == nil:
		return nil, errors.New("switch: http client is required")
	case cfg.Clock == nil:
		return nil, errors.New("switch: clock is required")
	case cfg.Wait == nil:
		return nil, errors.New("switch: wait is required")
	}
	return &Client{
		http:          cfg.HTTPClient,
		baseURL:       strings.TrimRight(cfg.BaseURL, "/"),
		agentID:       cfg.AgentID,
		agentName:     cfg.AgentName,
		bridgeID:      cfg.BridgeID,
		sharedRoom:    cfg.SharedRoom,
		internalRooms: cfg.InternalRooms,
		secrets:       cfg.Secrets,
		apiKeySecret:  cfg.APIKeySecret,
		regSecret:     cfg.RegistrationSecret,
		clock:         cfg.Clock,
		wait:          cfg.Wait,
		posted:        map[change.EffectKey]bool{},
	}, nil
}

// key reads the API key file. It runs before every request, so a rotated key is used at once.
func (c *Client) key() (string, error) {
	v, err := c.secrets.Secret(c.apiKeySecret)
	if err != nil {
		return "", fmt.Errorf("switch: read api key: %w", err)
	}
	return v, nil
}

// guarded runs one operation under the breaker and the 10 s deadline. The breaker records the outcome of the whole
// operation, so a successful read cannot hide a failed write made in the same call.
func (c *Client) guarded(ctx context.Context, op string, fn func(ctx context.Context) error) error {
	if err := c.breaker.allow(c.clock.Now()); err != nil {
		return fmt.Errorf("switch %s: %w", op, err)
	}
	callCtx, cancel := context.WithTimeout(ctx, requestDeadline)
	defer cancel()
	err := fn(callCtx)
	if err != nil && ctx.Err() != nil {
		// The caller stopped the call. That is not a Switch failure, so the breaker does not count it.
		err = fmt.Errorf("switch %s: %w", op, ctx.Err())
	}
	c.breaker.record(c.clock.Now(), err)
	return err
}

// request is one call to the bridge. The bearer is the API key or the registration token. It never reaches an error
// message or a log.
type request struct {
	method string
	path   string
	bearer string
	in     any
	header map[string]string
}

// send makes one request and returns the reply body of an accepted status. A transport failure is unavailable. No
// error carries the bearer or a reply body.
func (c *Client) send(ctx context.Context, r request) ([]byte, error) {
	var body io.Reader
	if r.in != nil {
		raw, err := json.Marshal(r.in)
		if err != nil {
			return nil, fmt.Errorf("switch %s: encode request: %w", r.path, err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, c.baseURL+r.path, body)
	if err != nil {
		return nil, fmt.Errorf("switch %s: build request: %w", r.path, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.bearer)
	if r.in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range r.header {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: switch %s %s: %w", ports.ErrUnavailable, r.method, r.path, err)
	}
	defer closeBody(resp)
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReply+1))
	if err != nil {
		return nil, fmt.Errorf("%w: switch %s: read reply: %w", ports.ErrUnavailable, r.path, err)
	}
	if len(raw) > maxReply {
		return nil, fmt.Errorf("switch %s: reply larger than %d bytes", r.path, maxReply)
	}
	if err := statusError(r.path, resp.StatusCode); err != nil {
		return nil, err
	}
	return raw, nil
}

// operation calls one Switch operation and decodes its result into out. Operations reply {"result": ...}.
func (c *Client) operation(ctx context.Context, name string, args any, out any) error {
	bearer, err := c.key()
	if err != nil {
		return err
	}
	raw, err := c.send(ctx, request{
		method: http.MethodPost, path: "/agents/" + c.agentID + "/ops/" + name, bearer: bearer, in: args,
	})
	if err != nil {
		return err
	}
	var env struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("switch %s: decode reply: %w", name, err)
	}
	if len(env.Result) == 0 || string(env.Result) == "null" {
		return fmt.Errorf("switch %s: reply has no result", name)
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return fmt.Errorf("switch %s: decode result: %w", name, err)
	}
	return nil
}

// replyError is a non-2xx reply. It wraps the ports sentinel for its status, and StatusOf reads the status.
type replyError struct {
	status int
	path   string
	cause  error
}

func (e *replyError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%v: switch %s: status %d", e.cause, e.path, e.status)
	}
	return fmt.Sprintf("switch %s: status %d", e.path, e.status)
}

// Unwrap returns the ports sentinel for the status, or nil when the status has none.
func (e *replyError) Unwrap() error { return e.cause }

// statusError maps a reply status to a ports error. Nil means the status is 2xx.
func statusError(path string, code int) error {
	var cause error
	switch {
	case code >= 200 && code < 300:
		return nil
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		cause = ports.ErrUnauthorized
	case code == http.StatusNotFound:
		cause = ports.ErrNotFound
	case code == http.StatusBadRequest:
		// Switch answers a call on a deleted room with 400 "Room not found".
		cause = ports.ErrInvalidRequest
	case code == http.StatusTooManyRequests || code >= 500:
		cause = ports.ErrUnavailable
	}
	return &replyError{status: code, path: path, cause: cause}
}

// closeBody discards the rest of a reply. A close error changes nothing the caller can act on.
func closeBody(resp *http.Response) {
	_ = resp.Body.Close()
}

// breaker opens after breakerLimit unavailable outcomes in a row and refuses calls for breakerCooldown. After the
// cooldown one call probes: a success closes the breaker, an unavailable outcome opens it again. Client errors are
// neutral: they neither count nor reset the count.
type breaker struct {
	mu        sync.Mutex
	failures  int
	openUntil time.Time
	probing   bool
}

// allow admits a call, or refuses it while the breaker is open or a probe is already in flight.
func (b *breaker) allow(now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openUntil.IsZero() {
		return nil
	}
	if now.Before(b.openUntil) || b.probing {
		return fmt.Errorf("%w: circuit open until %s", ports.ErrUnavailable, b.openUntil.Format(time.RFC3339))
	}
	b.probing = true
	return nil
}

// record applies the outcome of an admitted call.
func (b *breaker) record(now time.Time, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case err == nil:
		b.failures = 0
		b.openUntil = time.Time{}
		b.probing = false
	case errors.Is(err, ports.ErrUnavailable):
		b.probing = false
		b.failures++
		if b.failures >= breakerLimit || !b.openUntil.IsZero() {
			b.openUntil = now.Add(breakerCooldown)
		}
	default:
		b.probing = false
	}
}
