package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Comment posts one plain-text comment on ticket as an Atlassian Document Format paragraph (F1). The effect key makes
// a repeat in this process a no-op.
func (c *Client) Comment(ctx context.Context, ticket id.ChangeID, key change.EffectKey, text string) error {
	if !reTicketKey.MatchString(string(ticket)) {
		return fmt.Errorf("jira comment: %q is not a ticket key", string(ticket))
	}
	if key == "" {
		return errors.New("jira comment: effect key is required")
	}
	if c.alreadyWritten(key) {
		return nil
	}
	body := map[string]any{"body": adfParagraph(text)}
	if err := c.postJSON(ctx, issuePath+string(ticket)+"/comment", body, nil); err != nil {
		return fmt.Errorf("jira comment %s: %w", ticket, err)
	}
	c.markWritten(key)
	return nil
}

// Transition moves ticket to the named status by looking up its available transitions and posting the matching one (F1).
func (c *Client) Transition(ctx context.Context, ticket id.ChangeID, key change.EffectKey, status string) error {
	if !reTicketKey.MatchString(string(ticket)) {
		return fmt.Errorf("jira transition: %q is not a ticket key", string(ticket))
	}
	if key == "" {
		return errors.New("jira transition: effect key is required")
	}
	tKey := change.EffectKey(string(key) + ":transition")
	if c.alreadyWritten(tKey) {
		return nil
	}
	var avail struct {
		Transitions []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			To   struct {
				Name string `json:"name"`
			} `json:"to"`
		} `json:"transitions"`
	}
	path := issuePath + string(ticket) + "/transitions"
	if err := c.getJSON(ctx, path, nil, &avail); err != nil {
		return fmt.Errorf("jira transition %s: list: %w", ticket, err)
	}
	tid := ""
	for _, tr := range avail.Transitions {
		if strings.EqualFold(tr.Name, status) || strings.EqualFold(tr.To.Name, status) {
			tid = tr.ID
			break
		}
	}
	if tid == "" {
		return fmt.Errorf("jira transition %s: no transition to %q", ticket, status)
	}
	payload := map[string]any{"transition": map[string]string{"id": tid}}
	if err := c.postJSON(ctx, path, payload, nil); err != nil {
		return fmt.Errorf("jira transition %s: %w", ticket, err)
	}
	c.markWritten(tKey)
	return nil
}

// CreateTicket creates a Jira issue in this client's project and returns its key.
func (c *Client) CreateTicket(ctx context.Context, d ports.TicketDraft) (id.ChangeID, error) {
	if err := d.Validate(); err != nil {
		return "", fmt.Errorf("jira create ticket: %w", err)
	}
	body := map[string]any{
		"fields": map[string]any{
			"project":     map[string]string{"key": c.project},
			"summary":     d.Title,
			"description": adfParagraph(d.Body),
			"issuetype":   map[string]string{"name": "Task"},
		},
	}
	var reply struct {
		Key string `json:"key"`
	}
	if err := c.postJSON(ctx, "/rest/api/3/issue", body, &reply); err != nil {
		return "", fmt.Errorf("jira create ticket: %w", err)
	}
	if !reTicketKey.MatchString(reply.Key) {
		return "", fmt.Errorf("jira create ticket: unexpected key %q", reply.Key)
	}
	return id.ChangeID(reply.Key), nil
}

func (c *Client) alreadyWritten(key change.EffectKey) bool {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.written[key]
}

func (c *Client) markWritten(key change.EffectKey) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.written == nil {
		c.written = map[change.EffectKey]bool{}
	}
	c.written[key] = true
}

func adfParagraph(text string) map[string]any {
	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": []map[string]any{
			{
				"type": "paragraph",
				"content": []map[string]any{
					{"type": "text", "text": text},
				},
			},
		},
	}
}

// postJSON sends one authenticated POST with a deadline and optional JSON reply decoding, guarded by the circuit breaker.
func (c *Client) postJSON(ctx context.Context, path string, in any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	payload, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("%s: encode request: %w", path, err)
	}
	probe, err := c.breaker.allow(c.clock.Now())
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	raw, err := c.sendPost(ctx, c.base+path, path, payload, probe)
	c.breaker.record(c.clock.Now(), err)
	if err != nil {
		return err
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%s: decode reply: %w", path, err)
		}
	}
	return nil
}

func (c *Client) sendPost(ctx context.Context, target, path string, payload []byte, probe bool) ([]byte, error) {
	attempts := maxAttempts
	if probe {
		attempts = 1
	}
	for attempt := 1; ; attempt++ {
		raw, after, hasRetryAfter, err := c.postOnce(ctx, target, path, payload)
		if err == nil {
			return raw, nil
		}
		if !errors.Is(err, ports.ErrUnavailable) || attempt >= attempts {
			return nil, err
		}
		pause := after
		if !hasRetryAfter {
			pause = retryDelay(attempt, c.random)
		}
		if pause > maxRetryAfter {
			return nil, err
		}
		if werr := c.wait(ctx, pause); werr != nil {
			return nil, fmt.Errorf("%s: wait before retry: %w", path, werr)
		}
	}
}

func (c *Client) postOnce(ctx context.Context, target, path string, payload []byte) ([]byte, time.Duration, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, false, fmt.Errorf("build request %s: %w", path, err)
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, 0, false, fmt.Errorf("%s: %w: %w", path, ports.ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := statusError(resp.StatusCode, path); err != nil {
		after, hasRetryAfter := retryAfter(resp, c.clock.Now())
		return nil, after, hasRetryAfter, err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, 0, false, fmt.Errorf("%s: read body: %w: %w", path, ports.ErrUnavailable, err)
	}
	if len(raw) > maxBody {
		return nil, 0, false, fmt.Errorf("%s: response larger than %d bytes", path, maxBody)
	}
	return raw, 0, false, nil
}
