package openrouter

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

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/liveness"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.ModelHealth = Prober{}

// Prober asks an OpenAI-compatible endpoint whether it answers, with one minimal request (HARNESS_DESIGN B3). It sends
// the server's key when KeyName is set. An empty KeyName sends no Authorization header, for a local endpoint.
type Prober struct {
	BaseURL string
	Secrets Secrets
	KeyName string
	Model   string
	// Headers are extra, non-secret request headers.
	Headers map[string]string
	HTTP    *http.Client
}

// Probe implements ports.ModelHealth. A provider that refuses (401, 402, 429, a 5xx, or no answer at all) is
// liveness.Refusing with no error; only a probe that could not be made (no key, no model) is an error.
func (p Prober) Probe(parent context.Context) (liveness.Health, error) {
	if strings.TrimSpace(p.Model) == "" {
		return liveness.Unprobed, errors.New("model probe configuration is incomplete")
	}
	key, err := bearerKey(p.Secrets, p.KeyName)
	if err != nil {
		return liveness.Unprobed, errors.New("model probe credential is unavailable")
	}
	body, err := json.Marshal(map[string]any{
		"model":      p.Model,
		"max_tokens": 1,
		"messages":   []map[string]string{{"role": "user", "content": "ok"}},
	})
	if err != nil {
		return liveness.Unprobed, fmt.Errorf("encode model probe: %w", err)
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint(), bytes.NewReader(body))
	if err != nil {
		return liveness.Unprobed, fmt.Errorf("build model probe: %w", err)
	}
	setModelHeaders(req, key, p.Headers)
	client := p.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return liveness.Refusing, nil
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return probeHealth(resp.StatusCode), nil
}

// probeHealth maps the provider's status to health: any 2xx answers; quota, auth, rate limit and outage refuse.
func probeHealth(code int) liveness.Health {
	switch {
	case code >= 200 && code < 300:
		return liveness.Healthy
	default:
		return liveness.Refusing
	}
}

func (p Prober) endpoint() string {
	base := strings.TrimRight(p.BaseURL, "/")
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	return base + "/chat/completions"
}
