package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/openrouter"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// keyFile reads the model key through internal/platform/config, from the directory and file name of the -key-file
// flag. The key is read on each call and never kept, printed or logged.
type keyFile struct{ cfg config.Config }

func (k keyFile) Get(name string) (string, error) { return k.cfg.Secret(name) }

// liveModel builds the OpenAI-compatible chat client for the -model, -key-file and price flags. It reads the key once
// to fail early on a missing or malformed file; the error names the file but never holds its content.
func liveModel(o reviewOptions) (ports.ModelChat, error) {
	if strings.TrimSpace(o.model) == "" || o.keyFile == "" {
		return nil, errors.New("review: -model and -key-file are required")
	}
	if !(o.price.InputPerM > 0) || !(o.price.OutputPerM > 0) || o.price.CachedInputPerM < 0 {
		return nil, errors.New("review: set -price-input and -price-output (USD per million tokens), or name a model in the " +
			"price table; without a price the cost cap cannot see the spend")
	}
	abs, err := filepath.Abs(o.keyFile)
	if err != nil {
		return nil, fmt.Errorf("key file path: %w", err)
	}
	keys := keyFile{cfg: config.Config{SecretsDir: filepath.Dir(abs)}}
	name := filepath.Base(abs)
	if _, err := keys.Get(name); err != nil {
		return nil, fmt.Errorf("model key file: %w", err)
	}
	return openrouter.ChatClient{
		BaseURL: o.baseURL, Secrets: keys, KeyName: name,
		Prices: map[string]openrouter.Price{o.model: {Input: o.price.InputPerM, CachedInput: o.price.CachedInputPerM, Output: o.price.OutputPerM}},
	}, nil
}

// cappedModel holds the run's hard cost cap in front of any model. Before each call it bounds the call's cost
// (bench.Price.CallBound) and refuses with bench.ErrCostCap when the spend could pass the cap. After each call it adds
// the cost the adapter reports.
type cappedModel struct {
	inner ports.ModelChat
	cap   *bench.CostCap
	price bench.Price
}

func (m cappedModel) Complete(ctx context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return ports.ModelResponse{}, fmt.Errorf("size the model request: %w", err)
	}
	if err := m.cap.Allow(m.price.CallBound(len(body), req.MaxOutputTokens)); err != nil {
		return ports.ModelResponse{}, err
	}
	resp, err := m.inner.Complete(ctx, req)
	if err != nil {
		return resp, err
	}
	if err := m.cap.Add(resp.CostUSD); err != nil {
		return ports.ModelResponse{}, fmt.Errorf("%w: %w", bench.ErrCostCap, err)
	}
	return resp, nil
}
