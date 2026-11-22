package agentloop

import (
	"errors"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The retry rule of a model call (AM12c). Live run 2 lost 14 of 18 cases to single HTTP 500 answers.
const (
	// modelAttempts is the number of times one model call is sent at most.
	modelAttempts = 3
	// retryBase and retryCap bound the exponential backoff: attempt n waits a random time in
	// [0, min(retryCap, retryBase * 2^(n-1))] (full jitter).
	retryBase = 2 * time.Second
	retryCap  = 30 * time.Second
	// retryAfterCap bounds the provider's Retry-After hint.
	retryAfterCap = time.Minute
)

// Retry is how the loop waits before it sends a failed model call again. The zero value retries nothing. A rule that
// retries needs both fields: Wait is the only way the loop pauses (never time.Sleep), and Random draws the jitter.
type Retry struct {
	Wait   ports.Waiter
	Random ports.Random
}

func (rt Retry) check() error {
	if (rt.Wait == nil) != (rt.Random == nil) {
		return errors.New("agent loop retry needs both a waiter and a jitter source")
	}
	return nil
}

// delay returns the wait before attempt+1, and false when the call must not be sent again: the rule is off, the
// attempts are used up, or the failure is not one that may pass. Only a rate limit (429) and a server error (5xx) may
// pass; any other 4xx is a fault of the request and is never retried. Retry-After, when given, is the wait.
func (rt Retry) delay(err error, attempt int) (time.Duration, bool) {
	if rt.Wait == nil || attempt >= modelAttempts {
		return 0, false
	}
	var modelErr *ports.ModelError
	if !errors.As(err, &modelErr) {
		return 0, false
	}
	if modelErr.Kind != ports.ModelErrorRateLimited && modelErr.Kind != ports.ModelErrorServer {
		return 0, false
	}
	if modelErr.RetryAfter > 0 {
		return min(modelErr.RetryAfter, retryAfterCap), true
	}
	ceiling := backoffCeiling(attempt)
	return time.Duration(rt.Random.Int63n(int64(ceiling) + 1)), true
}

// backoffCeiling is min(retryCap, retryBase * 2^(attempt-1)), computed without overflow.
func backoffCeiling(attempt int) time.Duration {
	ceiling := retryBase
	for i := 1; i < attempt && ceiling < retryCap; i++ {
		ceiling *= 2
	}
	return min(ceiling, retryCap)
}

// attemptOf is the record of one failed attempt: the kind, the status, the request size and the request hash of a
// *ports.ModelError, empty otherwise. SameRequest is set when the hash matches the failed attempt before it. Live run 3
// met an HTTP 500 that repeated on the identical body three times: a refusal of the request, not a passing fault. The
// retry rule stays as it is; the record only makes the repeat plain.
func attemptOf(err error, before []ports.AgentAttempt) ports.AgentAttempt {
	var modelErr *ports.ModelError
	if !errors.As(err, &modelErr) {
		return ports.AgentAttempt{}
	}
	a := ports.AgentAttempt{Kind: modelErr.Kind, Status: modelErr.Status, RequestBytes: modelErr.RequestBytes,
		RequestHash: modelErr.RequestHash}
	if n := len(before); n > 0 && a.RequestHash != "" {
		a.SameRequest = before[n-1].RequestHash == a.RequestHash
	}
	return a
}
