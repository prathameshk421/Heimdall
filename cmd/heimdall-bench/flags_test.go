package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
)

func TestPriceForKnownModelAndFlags(t *testing.T) {
	// A flag wins over the table, so a price change is one flag, not a code change.
	got, err := priceFor("deepseek/deepseek-v4-flash", bench.Price{InputPerM: 9, OutputPerM: 9, CachedInputPerM: 9})
	require.NoError(t, err)
	require.InDelta(t, 9, got.InputPerM, 1e-12)
	require.InDelta(t, 9, got.OutputPerM, 1e-12)
}

func TestPriceForKnownModelWithoutFlags(t *testing.T) {
	// The live command of the owner names no price flag, so the table must cover the model the bench uses.
	got, err := priceFor("deepseek/deepseek-v4-flash", bench.Price{})
	require.NoError(t, err)
	require.Positive(t, got.InputPerM)
	require.Positive(t, got.OutputPerM)
	require.GreaterOrEqual(t, got.CachedInputPerM, 0.0)
	require.LessOrEqual(t, got.CachedInputPerM, got.InputPerM, "a cache read is never dearer than a fresh input token")
}

func TestPriceForUnknownModelNeedsFlags(t *testing.T) {
	// An unknown model with no price would make the cost cap blind, so it is refused with what to add.
	_, err := priceFor("nobody/unknown-model", bench.Price{})
	require.ErrorContains(t, err, "-price-input")
	require.ErrorContains(t, err, "-price-output")

	got, err := priceFor("nobody/unknown-model", bench.Price{InputPerM: 1, OutputPerM: 2, CachedInputPerM: -1})
	require.NoError(t, err)
	require.InDelta(t, 1, got.CachedInputPerM, 1e-12, "a cache price left unset is the input price")

	free, err := priceFor("nobody/unknown-model", bench.Price{InputPerM: 1, OutputPerM: 2})
	require.NoError(t, err)
	require.Zero(t, free.CachedInputPerM, "a cache price of zero is a free cache read, and it stays zero")
}

func TestPriceForRefusesHalfAPrice(t *testing.T) {
	for _, p := range []bench.Price{{InputPerM: 1}, {OutputPerM: 1}, {InputPerM: -1, OutputPerM: 1}} {
		_, err := priceFor("deepseek/deepseek-v4-flash", p)
		require.Error(t, err, "a half or negative price cannot bill a call: %+v", p)
	}
}

func TestParseCaseIDs(t *testing.T) {
	tests := []struct {
		in   string
		want []string
		ok   bool
	}{
		{"", nil, true},
		{"a", []string{"a"}, true},
		{"a,b", []string{"a", "b"}, true},
		{"a, b", []string{"a", "b"}, true},
		{"a b", []string{"a", "b"}, true},
		{" a , , b ", []string{"a", "b"}, true},
		{"a,,a", nil, false},
		{",", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseCaseIDs(tt.in)
			if !tt.ok {
				require.Error(t, err, "a repeated or empty id is a typo, not a filter")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
