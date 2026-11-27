package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
)

func TestParseReviewFlagsNeedsTheModelAndTheKeyBeforeAPrice(t *testing.T) {
	// The brief's live command names the model, the base URL, the key file, the cap, the filter and the output. It
	// names no price flag, so this must parse and take the table's price.
	o, err := parseReviewFlags([]string{
		"-out", "review.json", "-model", "deepseek/deepseek-v4-flash",
		"-base-url", "https://openrouter.ai/api/v1", "-key-file", "/tmp/openrouter/key",
		"-max-cost", "3.00", "-only", "testify-1207,chi-1185",
	}, &bytes.Buffer{})
	require.NoError(t, err)
	require.InDelta(t, 3.00, o.maxCost, 1e-12)
	require.InDelta(t, 0.09, o.price.InputPerM, 1e-12, "the table prices the model the brief names")
	require.InDelta(t, 0.18, o.price.OutputPerM, 1e-12)
	require.Equal(t, []string{"testify-1207", "chi-1185"}, mustIDs(t, o.only))
}

func TestParseReviewFlagsRefusesABadOnlyList(t *testing.T) {
	args := []string{"-out", "r.json", "-model", "deepseek/deepseek-v4-flash", "-key-file", "/tmp/k", "-only", "a,a"}
	_, err := parseReviewFlags(args, &bytes.Buffer{})
	require.ErrorContains(t, err, "-only")
}

func TestParseReviewFlagsRefusesABadParallel(t *testing.T) {
	// The workers share one machine and one cost cap, so a count outside the allowed range is a usage error, not a
	// silent clamp that hides a typo.
	base := []string{"-out", "r.json", "-model", "deepseek/deepseek-v4-flash", "-key-file", "/tmp/k"}
	o, err := parseReviewFlags(base, &bytes.Buffer{})
	require.NoError(t, err)
	require.Equal(t, 1, o.parallel, "one case at a time by default")

	o, err = parseReviewFlags(append(base, "-parallel", "8"), &bytes.Buffer{})
	require.NoError(t, err)
	require.Equal(t, 8, o.parallel)

	for _, n := range []string{"0", "-1", "9"} {
		_, err := parseReviewFlags(append(base, "-parallel", n), &bytes.Buffer{})
		require.ErrorContains(t, err, "-parallel", n)
	}
}

func mustIDs(t *testing.T, s string) []string {
	t.Helper()
	ids, err := parseCaseIDs(s)
	require.NoError(t, err)
	return ids
}

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
