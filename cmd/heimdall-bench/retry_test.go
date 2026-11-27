package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestCappedModelChargesABilledFailureOnce(t *testing.T) {
	req, _ := request(t)
	costCap, err := bench.NewCostCap(3)
	require.NoError(t, err)
	inner := billedFailure{cost: 0.25}
	m := cappedModel{inner: inner, cap: costCap, price: bench.Price{InputPerM: 1, CachedInputPerM: 1, OutputPerM: 2}}
	_, err = m.Complete(context.Background(), req)
	require.ErrorIs(t, err, ports.ErrUnavailable)
	assert.InDelta(t, 0.25, costCap.Spent(), 1e-15)
}

type billedFailure struct{ cost float64 }

func (f billedFailure) Complete(context.Context, ports.ModelRequest) (ports.ModelResponse, error) {
	return ports.ModelResponse{CostUSD: f.cost}, &ports.ModelError{Kind: ports.ModelErrorServer, Status: 502}
}
