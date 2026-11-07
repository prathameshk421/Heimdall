// Package openrouter reads OpenRouter account credit without submitting model requests.
package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Secrets supplies a named credential without exposing a storage mechanism to this adapter.
type Secrets interface{ Get(string) (string, error) }

// Client reads available credits from OpenRouter.
type Client struct {
	BaseURL    string
	Secrets    Secrets
	SecretName string
	HTTP       *http.Client
}
type creditsResponse struct {
	Data struct {
		TotalCredits float64 `json:"total_credits"`
		TotalUsage   float64 `json:"total_usage"`
	} `json:"data"`
}

// Balance returns available credits (total credits minus usage).
func (c Client) Balance(parent context.Context) (balance float64, err error) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	key, err := c.Secrets.Get(c.SecretName)
	if err != nil {
		return 0, errors.New("read OpenRouter API key")
	}
	if strings.TrimSpace(key) == "" {
		return 0, errors.New("OpenRouter API key is missing")
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://openrouter.ai"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/credits", nil)
	if err != nil {
		return 0, fmt.Errorf("build OpenRouter balance request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("OpenRouter balance request: %w", ports.ErrUnavailable)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close OpenRouter balance body: %w", closeErr))
		}
	}()
	if err := mapBalanceStatus(resp.StatusCode); err != nil {
		return 0, err
	}
	var body creditsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("decode OpenRouter balance: %w", err)
	}
	return body.Data.TotalCredits - body.Data.TotalUsage, nil
}

func mapBalanceStatus(code int) error {
	switch {
	case code == http.StatusUnauthorized:
		return ports.ErrUnauthorized
	case code >= 500:
		return ports.ErrUnavailable
	case code < 200 || code >= 300:
		return fmt.Errorf("OpenRouter balance returned HTTP %d", code)
	default:
		return nil
	}
}
