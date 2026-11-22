package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func serverFailure() []ports.AgentAttempt {
	return []ports.AgentAttempt{
		{Kind: ports.ModelErrorServer, Status: 500, Wait: 2 * time.Second},
		{Kind: ports.ModelErrorServer, Status: 500, Wait: 4 * time.Second},
		{Kind: ports.ModelErrorServer, Status: 500},
	}
}

// failedCase is a case whose loop ended on a call that still failed after its retries, after ok good turns.
func failedCase(ok int) ports.AgentResult {
	return ports.AgentResult{Stop: ports.AgentStopError, Turns: make([]ports.AgentTurn, ok), Retries: 2, FailedCall: serverFailure()}
}

func TestTheBreakerTripsAfterFiveConsecutiveFailedCalls(t *testing.T) {
	var b providerBreaker
	for i := range breakerLimit - 1 {
		b.observe(failedCase(0))
		require.False(t, b.tripped(), "call %d", i+1)
	}
	b.observe(failedCase(0))
	assert.True(t, b.tripped(), "the fifth failed call in a row trips it")
}

func TestAGoodCallResetsTheBreaker(t *testing.T) {
	var b providerBreaker
	for range breakerLimit - 1 {
		b.observe(failedCase(0))
	}
	b.observe(failedCase(3))
	assert.False(t, b.tripped(), "the case's 3 good calls come before its failed one, so the run of failures is 1")
	for range breakerLimit - 2 {
		b.observe(failedCase(0))
	}
	assert.False(t, b.tripped())
	b.observe(failedCase(0))
	assert.True(t, b.tripped())
}

func TestTheBreakerCountsOnlyProviderFailures(t *testing.T) {
	var b providerBreaker
	for range 2 * breakerLimit {
		b.observe(ports.AgentResult{Stop: ports.AgentStopError,
			FailedCall: []ports.AgentAttempt{{Kind: ports.ModelErrorInvalid, Status: 400}}})
		b.observe(ports.AgentResult{Stop: ports.AgentStopBudget})
	}
	assert.False(t, b.tripped(), "a 400 is the request's fault, and a budget stop made no failed call")
	for range breakerLimit {
		b.observe(ports.AgentResult{Stop: ports.AgentStopDeadline,
			FailedCall: []ports.AgentAttempt{{Kind: ports.ModelErrorRateLimited, Status: 429}}})
	}
	assert.True(t, b.tripped(), "a 429 that had no time left for its retry is a failed call too")
}

func TestTheCaseRecordsTheRetriesOfItsLoop(t *testing.T) {
	var rc reviewCase
	rc.recordLoop(ports.AgentResult{Stop: ports.AgentStopError, Retries: 3,
		Turns: []ports.AgentTurn{{Attempts: 1}, {Attempts: 2, Failed: []ports.AgentAttempt{
			{Kind: ports.ModelErrorRateLimited, Status: 429, Wait: 1500 * time.Millisecond}}}},
		FailedCall: serverFailure()}, newCaseProgress(newProgress(nil), "a"))
	assert.Equal(t, 3, rc.Retries)
	require.Len(t, rc.Transcript, 2)
	assert.Equal(t, 1, rc.Transcript[0].Attempts)
	assert.Empty(t, rc.Transcript[0].Failed)
	assert.Equal(t, 2, rc.Transcript[1].Attempts)
	assert.Equal(t, []attemptRecord{{Kind: "rate_limited", Status: 429, WaitSeconds: 1.5}}, rc.Transcript[1].Failed)
	assert.Equal(t, []attemptRecord{{Kind: "server", Status: 500, WaitSeconds: 2}, {Kind: "server", Status: 500, WaitSeconds: 4},
		{Kind: "server", Status: 500}}, rc.FailedCall)
}

func TestTheMarkdownShowsRetriesPerCaseAndTheBreakerStop(t *testing.T) {
	rep := reviewReport{Stopped: stopProviderFailing, Cases: []reviewCase{
		{CaseScore: bench.CaseScore{ID: "a", Kind: "bug", Turns: 4}, Stop: ports.AgentStopDone, Retries: 2},
		{CaseScore: bench.CaseScore{ID: "b", Kind: "bug"}, Error: "not run: " + stopProviderFailing},
	}}
	md := reviewMarkdown(rep)
	assert.Contains(t, md, "| Case | Kind | Scored | Stop | Turns | Retries |")
	assert.Contains(t, md, "| a | bug | no | done | 4 | 2 |")
	assert.Contains(t, md, "| Stopped | provider failing |")
	assert.Contains(t, md, "| Retries | 2 |")
}

// failingModel answers every call with an HTTP 500 and counts the calls.
type failingModel struct{ calls int }

func (m *failingModel) Complete(context.Context, ports.ModelRequest) (ports.ModelResponse, error) {
	m.calls++
	return ports.ModelResponse{}, &ports.ModelError{Kind: ports.ModelErrorServer, Status: 500, Message: "Internal Server Error"}
}

func TestTheReviewerRetriesThroughTheBenchWiring(t *testing.T) {
	// runReviewer builds the product loop with the retry rule. A model that always fails is sent 3 times, and the
	// waits go through the injected waiter, so the test does not wait.
	model := &failingModel{}
	var waits []time.Duration
	wait := func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	res, err := reviewLoop(context.Background(), model, retryWiring{wait: wait, random: halfJitter{}},
		bench.Price{InputPerM: 0.09, CachedInputPerM: 0.018, OutputPerM: 0.18}, oneFinalRun())
	require.ErrorIs(t, err, ports.ErrUnavailable)
	assert.Equal(t, ports.AgentStopError, res.Stop)
	assert.Equal(t, 3, model.calls)
	assert.Equal(t, 2, res.Retries)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, waits)
}

type halfJitter struct{}

func (halfJitter) Int63n(n int64) int64 { return n / 2 }

// acceptAll accepts every call, the final one included.
type acceptAll struct{}

func (acceptAll) Dispatch(context.Context, string, json.RawMessage) (string, error) { return "ok", nil }

func oneFinalRun() ports.AgentRun {
	return ports.AgentRun{Model: "m", User: "go", Tools: []ports.ModelTool{{Name: finalTool}}, FinalTools: []string{finalTool},
		Dispatcher: acceptAll{},
		Budget:     ports.AgentBudget{USD: 0.5, InputTokens: 800_000, OutputTokens: 40_000}, MaxOutputTokens: 4096,
		MaxTurns: 5, Deadline: time.Now().Add(time.Minute)}
}

func TestCryptoJitterStaysInRange(t *testing.T) {
	for _, n := range []int64{1, 2, int64(2*time.Second) + 1} {
		for range 50 {
			v := cryptoJitter{}.Int63n(n)
			require.GreaterOrEqual(t, v, int64(0))
			require.Less(t, v, n)
		}
	}
}

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
