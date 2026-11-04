package jira

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// scripted is one canned reply.
type scripted struct {
	status  int
	body    string
	headers map[string]string
}

// script answers the n-th request with the n-th step; past the end it repeats the last step.
func script(steps ...scripted) http.HandlerFunc {
	var mu sync.Mutex
	n := 0
	return func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		i := min(n, len(steps)-1)
		n++
		mu.Unlock()
		s := steps[i]
		for k, v := range s.headers {
			w.Header().Set(k, v)
		}
		reply(w, s.status, s.body)
	}
}

// rep returns n copies of s.
func rep(s scripted, n int) []scripted {
	out := make([]scripted, 0, n)
	for range n {
		out = append(out, s)
	}
	return out
}

// cat joins step lists.
func cat(parts ...[]scripted) []scripted {
	var out []scripted
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

var (
	unavailable = scripted{status: http.StatusInternalServerError, body: `{"errorMessages":["down"]}`}
	okTicket    = scripted{status: http.StatusOK, body: kan7}
)

func TestA429WaitsForRetryAfterThenSucceeds(t *testing.T) {
	f := newFake(t, script(
		scripted{status: http.StatusTooManyRequests, body: `{}`, headers: map[string]string{"Retry-After": "7"}},
		okTicket,
	))
	w := &waits{}
	cl := newClientWait(t, f, &clock{at: now}, w)

	got, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))

	require.NoError(t, err)
	require.Equal(t, id.ChangeID("KAN-7"), got.Key)
	require.Len(t, f.requests(), 2)
	require.Equal(t, []time.Duration{7 * time.Second}, w.pauses(), "the pause is the Retry-After of the reply")
}

func TestA5xxIsRetriedEightTimesWithCappedExponentialBackoff(t *testing.T) {
	f := newFake(t, script(unavailable))
	w := &waits{}
	cl := newClientWait(t, f, &clock{at: now}, w)

	_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))

	require.ErrorIs(t, err, ports.ErrUnavailable)
	require.Len(t, f.requests(), 8, "the resilience policy permits eight attempts in all")
	require.Len(t, w.pauses(), 7)
}

func TestRetryDelayUsesCappedDoublingAndInjectedFullJitter(t *testing.T) {
	for attempt, upper := range []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 32 * time.Second, 64 * time.Second, 128 * time.Second,
		256 * time.Second, 512 * time.Second, 10 * time.Minute,
	} {
		got := retryDelay(attempt+1, halfRandom{})
		require.Equal(t, (upper.Nanoseconds()+1)/2, got.Nanoseconds(), "attempt %d", attempt+1)
	}
}

func TestAuthAndNotFoundAreNotRetried(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, ports.ErrUnauthorized},
		{http.StatusNotFound, ports.ErrNotFound},
	}
	for _, tc := range cases {
		f := newFake(t, script(scripted{status: tc.status, body: `{}`}))
		w := &waits{}
		cl := newClientWait(t, f, &clock{at: now}, w)

		_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))

		require.ErrorIs(t, err, tc.want, "status %d", tc.status)
		require.Len(t, f.requests(), 1, "status %d is not retried", tc.status)
		require.Empty(t, w.pauses())
	}
}

func TestARetryAfterLongerThanTenMinutesReturnsAtOnce(t *testing.T) {
	f := newFake(t, script(scripted{status: http.StatusTooManyRequests, body: `{}`,
		headers: map[string]string{"Retry-After": "601"}}))
	w := &waits{}
	cl := newClientWait(t, f, &clock{at: now}, w)

	_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))

	require.ErrorIs(t, err, ports.ErrUnavailable, "the outbox retries it later")
	require.Len(t, f.requests(), 1)
	require.Empty(t, w.pauses())
}

func TestRetryAfterHTTPDateUsesTheInjectedClock(t *testing.T) {
	c := &clock{at: now}
	f := newFake(t, script(
		scripted{status: http.StatusTooManyRequests, body: `{}`,
			headers: map[string]string{"Retry-After": now.Add(7 * time.Second).Format(http.TimeFormat)}},
		okTicket,
	))
	w := &waits{}
	cl := newClientWait(t, f, c, w)

	_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))

	require.NoError(t, err)
	require.Equal(t, []time.Duration{7 * time.Second}, w.pauses())
}

func TestRetryAfterHTTPDateInThePastRetriesAtOnce(t *testing.T) {
	f := newFake(t, script(
		scripted{status: http.StatusTooManyRequests, body: `{}`,
			headers: map[string]string{"Retry-After": now.Add(-time.Second).Format(http.TimeFormat)}},
		okTicket,
	))
	w := &waits{}
	cl := newClientWait(t, f, &clock{at: now}, w)

	_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))

	require.NoError(t, err)
	require.Equal(t, []time.Duration{0}, w.pauses())
}

func TestBadRetryAfterUsesTheJitteredBackoff(t *testing.T) {
	f := newFake(t, script(
		scripted{status: http.StatusTooManyRequests, body: `{}`, headers: map[string]string{"Retry-After": "later"}},
		okTicket,
	))
	w := &waits{}
	cl := newClientWait(t, f, &clock{at: now}, w)

	_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))

	require.NoError(t, err)
	require.Equal(t, []time.Duration{(time.Second + 1) / 2}, w.pauses())
}

func TestRetryAfterHTTPDateAboveTheMaximumReturnsAtOnce(t *testing.T) {
	f := newFake(t, script(scripted{status: http.StatusTooManyRequests, body: `{}`,
		headers: map[string]string{"Retry-After": now.Add(maxRetryAfter + time.Second).Format(http.TimeFormat)}}))
	w := &waits{}
	cl := newClientWait(t, f, &clock{at: now}, w)

	_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))

	require.ErrorIs(t, err, ports.ErrUnavailable)
	require.Len(t, f.requests(), 1)
	require.Empty(t, w.pauses())
}

func TestReadSecretNeverWrapsOrEchoesAProviderError(t *testing.T) {
	const canary = "credential-canary"
	_, err := readSecret(errorSecrets{err: errors.New(canary)}, "jira_api_token")

	require.Error(t, err)
	for current := err; current != nil; current = errors.Unwrap(current) {
		require.NotContains(t, current.Error(), canary)
	}
	require.EqualError(t, err, "jira: could not read secret jira_api_token")
}

type errorSecrets struct{ err error }

func (s errorSecrets) Secret(string) (string, error) { return "", s.err }

func TestAPauseCancelledByTheContextStopsTheRetries(t *testing.T) {
	f := newFake(t, script(unavailable))
	w := &waits{stop: true}
	cl := newClientWait(t, f, &clock{at: now}, w)

	_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))

	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, f.requests(), 1, "no attempt after a cancelled pause")
}

func TestTheBreakerOpensAfterFiveUnavailableCallsAndSendsNothingWhileOpen(t *testing.T) {
	f := newFake(t, script(unavailable))
	cl := newClientWait(t, f, &clock{at: now}, &waits{})
	for range 5 {
		_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))
		require.ErrorIs(t, err, ports.ErrUnavailable)
	}
	require.Len(t, f.requests(), 40, "five calls of eight attempts each")

	_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))

	require.ErrorIs(t, err, ports.ErrUnavailable)
	require.Contains(t, err.Error(), "circuit open")
	require.Len(t, f.requests(), 40, "an open breaker sends no request")
}

func TestAfterTheCooldownOneProbeIsSentAndASuccessCloses(t *testing.T) {
	f := newFake(t, script(cat(rep(unavailable, 40), []scripted{okTicket})...))
	c := &clock{at: now}
	cl := newClientWait(t, f, c, &waits{})
	for range 5 {
		_, _ = cl.Ticket(context.Background(), id.ChangeID("KAN-7"))
	}
	require.Len(t, f.requests(), 40)

	c.at = now.Add(29 * time.Second)
	_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))
	require.ErrorIs(t, err, ports.ErrUnavailable, "still open 29 s in")
	require.Len(t, f.requests(), 40)

	c.at = now.Add(30 * time.Second)
	got, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))
	require.NoError(t, err, "the probe succeeds")
	require.Equal(t, id.ChangeID("KAN-7"), got.Key)
	require.Len(t, f.requests(), 41, "the probe is one request, not eight attempts")

	_, err = cl.Ticket(context.Background(), id.ChangeID("KAN-7"))
	require.NoError(t, err, "a successful probe closes the breaker")
	require.Len(t, f.requests(), 42)
}

func TestAFailedProbeIsOneRequestAndOpensTheBreakerAgain(t *testing.T) {
	f := newFake(t, script(unavailable))
	c := &clock{at: now}
	cl := newClientWait(t, f, c, &waits{})
	for range 5 {
		_, _ = cl.Ticket(context.Background(), id.ChangeID("KAN-7"))
	}
	require.Len(t, f.requests(), 40)

	c.at = now.Add(30 * time.Second)
	_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))
	require.ErrorIs(t, err, ports.ErrUnavailable)
	require.Len(t, f.requests(), 41, "one probe request, no retries inside it")

	_, err = cl.Ticket(context.Background(), id.ChangeID("KAN-7"))
	require.ErrorIs(t, err, ports.ErrUnavailable)
	require.Len(t, f.requests(), 41, "the failed probe opened the breaker for another cooldown")
}

func TestClientErrorsDoNotCountTowardsTheBreaker(t *testing.T) {
	f := newFake(t, script(cat(rep(scripted{status: http.StatusNotFound, body: `{}`}, 5), []scripted{okTicket})...))
	cl := newClientWait(t, f, &clock{at: now}, &waits{})
	for range 5 {
		_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))
		require.ErrorIs(t, err, ports.ErrNotFound)
	}

	_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))

	require.NoError(t, err, "five 404s do not open the breaker")
	require.Len(t, f.requests(), 6)
}

func TestASuccessResetsTheUnavailableStreak(t *testing.T) {
	// Call 1 fails (eight attempts), call 2 succeeds, calls 3 to 6 fail (four calls). Only four in a row, so call 7
	// is still sent.
	f := newFake(t, script(cat(
		rep(unavailable, 8),
		[]scripted{okTicket},
		rep(unavailable, 32),
		[]scripted{okTicket},
	)...))
	cl := newClientWait(t, f, &clock{at: now}, &waits{})
	_, _ = cl.Ticket(context.Background(), id.ChangeID("KAN-7"))
	_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))
	require.NoError(t, err)
	for range 4 {
		_, _ = cl.Ticket(context.Background(), id.ChangeID("KAN-7"))
	}
	require.Len(t, f.requests(), 8+1+32)

	_, err = cl.Ticket(context.Background(), id.ChangeID("KAN-7"))

	require.NoError(t, err, "four unavailable calls in a row do not open the breaker")
	require.Len(t, f.requests(), 8+1+32+1)
}
