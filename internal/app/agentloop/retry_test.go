package agentloop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// fakeWaiter moves the fake clock by each wait and records it. cancel, when set, is called instead of waiting, as a
// caller that gives up during the wait.
type fakeWaiter struct {
	clock  *fakeClock
	waits  []time.Duration
	cancel context.CancelFunc
}

func (w *fakeWaiter) wait(ctx context.Context, d time.Duration) error {
	if w.cancel != nil {
		w.cancel()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	w.waits = append(w.waits, d)
	w.clock.now = w.clock.now.Add(d)
	return nil
}

// topJitter always draws the top of the range, so each backoff wait is its ceiling and the test can name it.
type topJitter struct{ asked []int64 }

func (j *topJitter) Int63n(n int64) int64 {
	j.asked = append(j.asked, n)
	return n - 1
}

func failure(kind ports.ModelErrorKind, status int, retryAfter time.Duration) modelStep {
	return modelStep{err: &ports.ModelError{Kind: kind, Status: status, RetryAfter: retryAfter,
		Message: `{"status_code":500,"message":"Internal Server Error"}`}}
}

func serverError() modelStep {
	return failure(ports.ModelErrorServer, http.StatusInternalServerError, 0)
}

// withRetry gives the harness a retry rule on a fake waiter and the top-of-range jitter.
func (h *harness) withRetry() (*fakeWaiter, *topJitter) {
	w := &fakeWaiter{clock: h.clock}
	j := &topJitter{}
	h.cfg.Retry = Retry{Wait: w.wait, Random: j}
	return w, j
}

func submitted(cost float64) modelStep {
	return answer(cost, call("c1", "submit_plan", `{"plan":"p"}`))
}

func TestRetryAfterIsCappedAtOneMinute(t *testing.T) {
	h := newHarness(t, failure(ports.ModelErrorRateLimited, http.StatusTooManyRequests, 10*time.Minute), submitted(0.02))
	w, _ := h.withRetry()
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopSubmitted, res.Reason)
	assert.Equal(t, []time.Duration{time.Minute}, w.waits)
}

func TestSixServerErrorsEndTheRunWithTheWrappedError(t *testing.T) {
	h := newHarness(t, serverError(), serverError(), serverError(), serverError(), serverError(), serverError())
	w, j := h.withRetry()
	res, err := h.run(context.Background(), t)
	require.Error(t, err)
	var modelErr *ports.ModelError
	require.ErrorAs(t, err, &modelErr)
	assert.Equal(t, ports.ModelErrorServer, modelErr.Kind)
	require.ErrorIs(t, err, ports.ErrUnavailable)
	assert.Equal(t, StopModelError, res.Reason)
	assert.Len(t, h.model.requests, 6, "at most 6 attempts per call")
	assert.Equal(t, []int64{int64(2*time.Second) + 1, int64(4*time.Second) + 1, int64(8*time.Second) + 1,
		int64(12*time.Second) + 1, int64(18*time.Second) + 1}, j.asked, "each wait draws from its step, plus or minus 20%")
	assert.Equal(t, []time.Duration{6 * time.Second, 12 * time.Second, 24 * time.Second, 36 * time.Second, 54 * time.Second}, w.waits)
	assert.Equal(t, 5, res.Retries)
	assert.Equal(t, []ports.AgentAttempt{
		{Kind: ports.ModelErrorServer, Status: 500, Wait: 6 * time.Second},
		{Kind: ports.ModelErrorServer, Status: 500, Wait: 12 * time.Second},
		{Kind: ports.ModelErrorServer, Status: 500, Wait: 24 * time.Second},
		{Kind: ports.ModelErrorServer, Status: 500, Wait: 36 * time.Second},
		{Kind: ports.ModelErrorServer, Status: 500, Wait: 54 * time.Second},
		{Kind: ports.ModelErrorServer, Status: 500},
	}, res.FailedCall)
	assert.Zero(t, res.Turns)
}

func TestRetryWaitsNeverPassTheDeadline(t *testing.T) {
	// 6 s and 12 s fit in 30 s; the third wait (24 s) would end past it, so the run stops at the deadline unsent.
	h := newHarness(t, serverError(), serverError(), serverError(), submitted(0.02))
	w, _ := h.withRetry()
	deadline := h.clock.now.Add(30 * time.Second)
	h.cfg.Deadline = deadline
	res, err := h.run(context.Background(), t)
	require.NoError(t, err, "the deadline is an outcome of the run")
	assert.Equal(t, StopDeadline, res.Reason)
	assert.Equal(t, []time.Duration{6 * time.Second, 12 * time.Second}, w.waits)
	assert.True(t, h.clock.now.Before(deadline), "no wait ran past the deadline")
	assert.Len(t, h.model.requests, 3)
}

func TestTheBudgetGuardAppliesBeforeEveryRetry(t *testing.T) {
	// Each failed attempt bills $0.15 of the $0.50 budget. The first two retries fit; after the third bill the worst
	// case of another attempt does not, so the fourth attempt is never sent.
	billed := func() modelStep {
		s := serverError()
		s.resp = ports.ModelResponse{CostUSD: 0.15, Usage: ports.ModelUsage{InputTokens: 10, OutputTokens: 10}}
		return s
	}
	h := newHarness(t, billed(), billed(), billed(), submitted(0.02))
	h.cfg.Price = ports.ModelPrice{InputPerM: 10, CachedInputPerM: 10, OutputPerM: 20}
	w, _ := h.withRetry()
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopBudget, res.Reason)
	assert.Equal(t, ports.AgentBudgetUSD, res.BudgetCause, "the retry guard names the cap it hit")
	assert.Len(t, h.model.requests, 3, "the fourth attempt is not sent")
	assert.Len(t, w.waits, 2, "no wait for a retry the budget refuses")
	assert.InDelta(t, 0.45, res.CostUSD, 1e-12)
}

func TestACancelDuringTheWaitEndsTheRun(t *testing.T) {
	h := newHarness(t, serverError())
	w, _ := h.withRetry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.cancel = cancel
	res, err := h.run(ctx, t)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, StopCancelled, res.Reason)
	assert.Len(t, h.model.requests, 1)
}

func TestTheBudgetGuardRefusesARetryThatCouldPassTheCap(t *testing.T) {
	// The failed attempt was billed $0.45 of the $0.50 budget. The retry's worst case is larger than what is left, so
	// it is not sent.
	billed := serverError()
	billed.resp = ports.ModelResponse{CostUSD: 0.45, Usage: ports.ModelUsage{InputTokens: 100, OutputTokens: 10}}
	h := newHarness(t, billed)
	h.cfg.Price = ports.ModelPrice{InputPerM: 10, CachedInputPerM: 10, OutputPerM: 20}
	w, _ := h.withRetry()
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopBudget, res.Reason)
	assert.Equal(t, ports.AgentBudgetUSD, res.BudgetCause)
	assert.Len(t, h.model.requests, 1, "the retry is not sent")
	assert.Empty(t, w.waits, "no wait for a retry the budget refuses")
	assert.InDelta(t, 0.45, res.CostUSD, 1e-12, "the billed failure is charged once")
	assert.Equal(t, int64(100), res.Usage.InputTokens)
	assert.Zero(t, res.Retries)
	assert.Len(t, res.FailedCall, 1)
}

func TestABilledFailureThatCannotBeTrueEndsTheRun(t *testing.T) {
	billed := serverError()
	billed.resp = ports.ModelResponse{CostUSD: -1}
	h := newHarness(t, billed)
	h.withRetry()
	res, err := h.run(context.Background(), t)
	require.ErrorIs(t, err, ports.ErrAgentUsage)
	assert.Equal(t, StopBudget, res.Reason)
	assert.Equal(t, ports.AgentBudgetUsage, res.BudgetCause)
}

func TestAnOrdinaryClientErrorIsNotRetried(t *testing.T) {
	for _, kind := range []ports.ModelErrorKind{ports.ModelErrorInvalid, ports.ModelErrorAuth, ports.ModelErrorRefused,
		ports.ModelErrorContextTooLong} {
		t.Run(string(kind), func(t *testing.T) {
			h := newHarness(t, failure(kind, http.StatusBadRequest, 0))
			w, _ := h.withRetry()
			res, err := h.run(context.Background(), t)
			require.Error(t, err)
			assert.Equal(t, StopModelError, res.Reason)
			assert.Len(t, h.model.requests, 1)
			assert.Empty(t, w.waits)
			assert.Equal(t, []ports.AgentAttempt{{Kind: kind, Status: 400}}, res.FailedCall)
		})
	}
}

func TestAnErrorThatIsNotAModelErrorIsNotRetried(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"plain", errors.New("dial tcp: connection refused")},
		{"unexpected end of body", fmt.Errorf("read body: %w", io.ErrUnexpectedEOF)},
		{"a wrapped fault that does not claim unavailable", fmt.Errorf("parse answer: %w", errors.New("unexpected token"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, modelStep{err: tc.err})
			w, _ := h.withRetry()
			res, err := h.run(context.Background(), t)
			require.Error(t, err)
			assert.Equal(t, StopModelError, res.Reason)
			assert.Empty(t, w.waits)
			assert.Len(t, h.model.requests, 1)
			assert.Equal(t, []ports.AgentAttempt{{}}, res.FailedCall)
		})
	}
}

func TestARetryRuleNeedsBothAWaiterAndAJitterSource(t *testing.T) {
	h := newHarness(t)
	h.cfg.Retry = Retry{Wait: (&fakeWaiter{clock: h.clock}).wait}
	_, err := New(h.model, h.clock, h.cfg, nil, []Submitter{h.sub})
	require.Error(t, err)
	h.cfg.Retry = Retry{Random: &topJitter{}}
	_, err = New(h.model, h.clock, h.cfg, nil, []Submitter{h.sub})
	require.Error(t, err)
}

func TestTheRunnerRetriesThroughThePort(t *testing.T) {
	c := newContract(t, serverError(), answer(0.02, call("c1", "finish", `{}`)))
	w := &fakeWaiter{clock: c.clock}
	r, err := NewRunner(c.model, c.clock, testPrice)
	require.NoError(t, err)
	_, err = r.WithRetry(nil, &topJitter{})
	require.Error(t, err)
	r, err = r.WithRetry(w.wait, &topJitter{})
	require.NoError(t, err)
	c.run.Dispatcher = c.disp
	res, err := r.Run(context.Background(), c.run)
	require.NoError(t, err)
	assert.Equal(t, ports.AgentStopDone, res.Stop)
	assert.Equal(t, 1, res.Retries)
	require.Len(t, res.Turns, 1)
	assert.Equal(t, 2, res.Turns[0].Attempts)
	assert.Equal(t, []time.Duration{6 * time.Second}, w.waits)
}
