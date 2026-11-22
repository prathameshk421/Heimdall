package agentloop

import (
	"context"
	"errors"
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

func TestRetryAServerErrorThenTheCallSucceeds(t *testing.T) {
	h := newHarness(t, serverError(), submitted(0.02))
	w, j := h.withRetry()
	start := h.clock.now
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopSubmitted, res.Reason)
	assert.Len(t, h.model.requests, 2, "one retry")
	assert.Equal(t, h.model.requests[0], h.model.requests[1], "the retry sends the same request")
	assert.Equal(t, []int64{int64(2*time.Second) + 1}, j.asked, "the first backoff draws from [0, 2 s]")
	assert.Equal(t, []time.Duration{2 * time.Second}, w.waits)
	assert.Equal(t, start.Add(2*time.Second), h.clock.now, "the wait goes through the clock")
	assert.Equal(t, 1, res.Retries)
	assert.Nil(t, res.FailedCall, "the call that ended the run did not fail")
	require.Len(t, res.Turn, 1)
	assert.Equal(t, 2, res.Turn[0].Attempts)
	assert.Equal(t, []ports.AgentAttempt{{Kind: ports.ModelErrorServer, Status: 500, Wait: 2 * time.Second}}, res.Turn[0].Failed)
	assert.InDelta(t, 0.02, res.CostUSD, 1e-12, "the failed attempt reported no usage, so only the answer is charged")
	assert.Equal(t, 1, res.Turns, "a retried call is one turn")
}

func TestRetryARateLimitHonoursRetryAfter(t *testing.T) {
	h := newHarness(t, failure(ports.ModelErrorRateLimited, http.StatusTooManyRequests, 7*time.Second), submitted(0.02))
	w, j := h.withRetry()
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopSubmitted, res.Reason)
	assert.Equal(t, []time.Duration{7 * time.Second}, w.waits, "the provider's hint is the wait")
	assert.Empty(t, j.asked, "a hint takes the place of the jitter")
	assert.Equal(t, []ports.AgentAttempt{{Kind: ports.ModelErrorRateLimited, Status: 429, Wait: 7 * time.Second}}, res.Turn[0].Failed)
}

func TestRetryAfterIsCappedAtOneMinute(t *testing.T) {
	h := newHarness(t, failure(ports.ModelErrorRateLimited, http.StatusTooManyRequests, 10*time.Minute), submitted(0.02))
	w, _ := h.withRetry()
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopSubmitted, res.Reason)
	assert.Equal(t, []time.Duration{time.Minute}, w.waits)
}

func TestThreeServerErrorsEndTheRunWithTheWrappedError(t *testing.T) {
	h := newHarness(t, serverError(), serverError(), serverError())
	w, j := h.withRetry()
	res, err := h.run(context.Background(), t)
	require.Error(t, err)
	var modelErr *ports.ModelError
	require.ErrorAs(t, err, &modelErr)
	assert.Equal(t, ports.ModelErrorServer, modelErr.Kind)
	require.ErrorIs(t, err, ports.ErrUnavailable)
	assert.Equal(t, StopModelError, res.Reason)
	assert.Len(t, h.model.requests, 3, "at most 3 attempts per call")
	assert.Equal(t, []int64{int64(2*time.Second) + 1, int64(4*time.Second) + 1}, j.asked, "the ceiling doubles")
	assert.Equal(t, []time.Duration{2 * time.Second, 4 * time.Second}, w.waits)
	assert.Equal(t, 2, res.Retries)
	assert.Equal(t, []ports.AgentAttempt{
		{Kind: ports.ModelErrorServer, Status: 500, Wait: 2 * time.Second},
		{Kind: ports.ModelErrorServer, Status: 500, Wait: 4 * time.Second},
		{Kind: ports.ModelErrorServer, Status: 500},
	}, res.FailedCall)
	assert.Zero(t, res.Turns)
}

func TestTheBackoffCeilingStopsAtThirtySeconds(t *testing.T) {
	assert.Equal(t, 2*time.Second, backoffCeiling(1))
	assert.Equal(t, 4*time.Second, backoffCeiling(2))
	assert.Equal(t, 16*time.Second, backoffCeiling(4))
	assert.Equal(t, 30*time.Second, backoffCeiling(5))
	assert.Equal(t, 30*time.Second, backoffCeiling(63), "no overflow on a large attempt number")
}

func TestARetryThatCannotFinishBeforeTheDeadlineStopsWithoutWaiting(t *testing.T) {
	h := newHarness(t, failure(ports.ModelErrorRateLimited, http.StatusTooManyRequests, 30*time.Second))
	w, _ := h.withRetry()
	h.cfg.Deadline = h.clock.now.Add(10 * time.Second)
	res, err := h.run(context.Background(), t)
	require.NoError(t, err, "the deadline is an outcome of the run")
	assert.Equal(t, StopDeadline, res.Reason)
	assert.Empty(t, w.waits, "no wait past the deadline")
	assert.Len(t, h.model.requests, 1)
	assert.Equal(t, []ports.AgentAttempt{{Kind: ports.ModelErrorRateLimited, Status: 429}}, res.FailedCall)
	assert.Zero(t, res.Retries)
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
	h := newHarness(t, modelStep{err: errors.New("dial tcp: connection refused")})
	w, _ := h.withRetry()
	res, err := h.run(context.Background(), t)
	require.Error(t, err)
	assert.Equal(t, StopModelError, res.Reason)
	assert.Empty(t, w.waits)
	assert.Equal(t, []ports.AgentAttempt{{}}, res.FailedCall)
}

func TestWithoutARetryRuleAServerErrorEndsTheRun(t *testing.T) {
	h := newHarness(t, serverError())
	res, err := h.run(context.Background(), t)
	require.ErrorIs(t, err, ports.ErrUnavailable)
	assert.Equal(t, StopModelError, res.Reason)
	assert.Len(t, h.model.requests, 1)
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
	assert.Equal(t, []time.Duration{2 * time.Second}, w.waits)
}
