package main

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// countingModel answers every call with the same cost and counts the calls.
type countingModel struct {
	cost  float64
	calls int
}

func (m *countingModel) Complete(context.Context, ports.ModelRequest) (ports.ModelResponse, error) {
	m.calls++
	return ports.ModelResponse{CostUSD: m.cost}, nil
}

func request(t *testing.T) (ports.ModelRequest, int) {
	t.Helper()
	req := ports.ModelRequest{Model: "m", MaxOutputTokens: 4000,
		Messages: []ports.ModelMessage{{Role: ports.ModelRoleUser, Content: "review this change"}}}
	b, err := json.Marshal(req)
	require.NoError(t, err)
	return req, len(b)
}

func TestCappedModelRefusesACallThatCouldPassTheCap(t *testing.T) {
	price := bench.Price{InputPerM: 1, CachedInputPerM: 1, OutputPerM: 2}
	req, size := request(t)
	bound := price.CallBound(size, req.MaxOutputTokens)
	costCap, err := bench.NewCostCap(2.5 * bound)
	require.NoError(t, err)
	inner := &countingModel{cost: bound}
	m := cappedModel{inner: inner, cap: costCap, price: price}

	_, err = m.Complete(context.Background(), req)
	require.NoError(t, err)
	_, err = m.Complete(context.Background(), req)
	require.NoError(t, err)
	_, err = m.Complete(context.Background(), req)
	require.ErrorIs(t, err, bench.ErrCostCap, "the third call could reach 3 bounds, over the 2.5 cap")
	require.Equal(t, 2, inner.calls, "the refused call never reaches the model")
	require.InDelta(t, 2*bound, costCap.Spent(), 1e-15)
}

func TestCappedModelStopsOnACostThatCannotBeTrue(t *testing.T) {
	req, _ := request(t)
	costCap, err := bench.NewCostCap(3)
	require.NoError(t, err)
	inner := &countingModel{cost: math.NaN()}
	m := cappedModel{inner: inner, cap: costCap, price: bench.Price{InputPerM: 0.1, OutputPerM: 0.2}}
	_, err = m.Complete(context.Background(), req)
	require.ErrorIs(t, err, bench.ErrCostCap)
	_, err = m.Complete(context.Background(), req)
	require.ErrorIs(t, err, bench.ErrCostCap)
	require.Equal(t, 1, inner.calls)
}

func TestLiveModelNeedsPricesAndAReadableKeyFile(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "model_key")
	require.NoError(t, os.WriteFile(key, []byte("sk-test-not-a-real-key\n"), 0o600))
	ok := reviewOptions{model: "deepseek/deepseek-v4-flash", keyFile: key, price: bench.Price{InputPerM: 0.09, CachedInputPerM: 0.09, OutputPerM: 0.18}}

	_, err := liveModel(ok)
	require.NoError(t, err)

	noPrice := ok
	noPrice.price = bench.Price{}
	_, err = liveModel(noPrice)
	require.ErrorContains(t, err, "-price-input", "a run with no price would be blind to its spend")

	missing := ok
	missing.keyFile = filepath.Join(dir, "absent")
	_, err = liveModel(missing)
	require.Error(t, err)

	noModel := ok
	noModel.model = ""
	_, err = liveModel(noModel)
	require.Error(t, err)

	multi := filepath.Join(dir, "two_lines")
	require.NoError(t, os.WriteFile(multi, []byte("sk-one\nsk-two\n"), 0o600))
	bad := ok
	bad.keyFile = multi
	_, err = liveModel(bad)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "sk-one", "an error never carries the key")
}
