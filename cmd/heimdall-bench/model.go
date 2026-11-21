package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

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
		BaseURL: o.baseURL, Secrets: keys, KeyName: name, RequiredToolChoice: o.requiredToolChoice,
		Prices: map[string]openrouter.Price{o.model: {Input: o.price.InputPerM, CachedInput: o.price.CachedInputPerM, Output: o.price.OutputPerM}},
	}, nil
}

// cappedModel holds the run's hard cost cap in front of any model. Before each call it reserves the call's bound
// (bench.Price.CallBound) and refuses with bench.ErrCostCap when the spend could pass the cap. After each call it
// settles the cost the adapter reports, also for a failed call that the provider billed. The cap is shared by every
// worker of a parallel run, and the reservation holds until the call settles, so two workers can never both pass the
// check and together pass the cap. It sees every attempt of a retried call, so a retry passes the same cap.
//
// prog, when set, gets one line per answer: the turn, the tools, the tokens, the spend and the elapsed time.
type cappedModel struct {
	inner ports.ModelChat
	cap   *bench.CostCap
	price bench.Price
	prog  *caseProgress
}

func (m cappedModel) Complete(ctx context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return ports.ModelResponse{}, fmt.Errorf("size the model request: %w", err)
	}
	settle, err := m.cap.Reserve(m.price.CallBound(len(body), req.MaxOutputTokens))
	if err != nil {
		return ports.ModelResponse{}, err
	}
	start := time.Now()
	resp, callErr := m.inner.Complete(ctx, req)
	seconds := time.Since(start).Seconds()
	if callErr != nil {
		// A failed call settles with what the provider billed for it, often zero. The rest of its hold returns to the
		// cap for the other workers.
		if err := settle(resp.CostUSD); err != nil {
			return ports.ModelResponse{}, fmt.Errorf("%w: %w", bench.ErrCostCap, err)
		}
		return resp, callErr
	}
	if err := settle(resp.CostUSD); err != nil {
		return ports.ModelResponse{}, fmt.Errorf("%w: %w", bench.ErrCostCap, err)
	}
	if m.prog != nil {
		m.prog.modelTurn(benchTurn{tools: callNames(resp.ToolCalls), input: resp.Usage.InputTokens,
			output: resp.Usage.OutputTokens, cost: resp.CostUSD, seconds: seconds}, m.cap.Spent(), m.prog.elapsed())
	}
	return resp, nil
}

// callNames names the tools of one answer, in the order the model asked for them.
func callNames(calls []ports.ModelToolCall) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Name
	}
	return out
}
