package agentloop

import (
	"errors"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The retry rule of a model call (AM12c, AM12h). Live run 2 lost 14 of 18 cases to single HTTP 500 answers. Live run 8
// showed that Deploy's 500s come and go over minutes and do not depend on the body: one saved refused body got 200, 500,
// 500, 200 over three minutes. Three attempts within about 2 s retried inside the same bad window, so the waits now
// spread over about two minutes.
const (
	// modelAttempts is the number of times one model call is sent at most.
	modelAttempts = 6
	// retryJitterPercent is how far a wait may move from its step (retryStep), each way.
	retryJitterPercent = 20
	// retryAfterCap bounds the provider's Retry-After hint.
	retryAfterCap = time.Minute
)

// retrySteps are the waits before attempts 2 to 6: about 110 s in all.
var retrySteps = [...]time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second, 45 * time.Second}

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
// pass; any other 4xx is a fault of the request and is never retried. Retry-After, when given, is the wait. Else the
// wait is the attempt's step (retryStep) moved by a random amount of up to retryJitterPercent each way, so workers that
// failed together do not send again together. The caller checks the deadline and the budget before it waits.
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
	step := retryStep(attempt)
	spread := step * retryJitterPercent / 100
	return step - spread + time.Duration(rt.Random.Int63n(int64(2*spread)+1)), true
}

// retryStep is the wait before the attempt after attempt; past the list it is the last step.
func retryStep(attempt int) time.Duration {
	return retrySteps[min(max(attempt, 1), len(retrySteps))-1]
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
