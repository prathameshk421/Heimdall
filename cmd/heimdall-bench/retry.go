package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
	"sync"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agentloop"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// breakerLimit is the number of model calls in a row, across the run, that may fail after their retries before the
// run stops (AM12c). Live run 2 lost 14 of 18 cases to HTTP 500s; a run that keeps going on a failing provider only
// reports the outage.
const breakerLimit = 5

// stopProviderFailing is the run's stop reason when the breaker trips.
const stopProviderFailing = "provider failing"

// providerBreaker counts the model calls in a row that failed after their retries. One case's loop is a run of good
// calls, and then at most one failed call that ends it, so the case's result says where the run of failures stands.
// The workers of a parallel run share one breaker, so it locks itself; "in a row" is then the order the cases end in.
type providerBreaker struct {
	mu     sync.Mutex
	failed int
}

// observe reads one case's loop result. A good call resets the count; a call that failed with a rate limit or a server
// error after its retries adds one. Any other end (an ordinary 4xx, the budget, the turn cap) adds nothing.
func (b *providerBreaker) observe(res ports.AgentResult) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(res.Turns) > 0 {
		b.failed = 0
	}
	if n := len(res.FailedCall); n > 0 {
		switch res.FailedCall[n-1].Kind {
		case ports.ModelErrorRateLimited, ports.ModelErrorServer:
			b.failed++
		case ports.ModelErrorRefused, ports.ModelErrorContextTooLong, ports.ModelErrorAuth, ports.ModelErrorInvalid:
		}
	}
}

// tripped reports whether breakerLimit calls in a row failed.
func (b *providerBreaker) tripped() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failed >= breakerLimit
}

// retryWiring is the waiter and the jitter source of the loop's retry rule.
type retryWiring struct {
	wait   ports.Waiter
	random ports.Random
}

// systemRetry waits on the real clock with crypto/rand jitter.
func systemRetry() retryWiring { return retryWiring{wait: clock.Wait, random: cryptoJitter{}} }

// reviewLoop runs one reviewer conversation through the product's agent loop (ports.AgentLoop) with the retry rule.
func reviewLoop(ctx context.Context, model ports.ModelChat, rw retryWiring, price bench.Price, run ports.AgentRun) (ports.AgentResult, error) {
	loop, err := agentloop.NewRunner(model, clock.System{}, ports.ModelPrice{InputPerM: price.InputPerM,
		CachedInputPerM: price.CachedInputPerM, OutputPerM: price.OutputPerM})
	if err != nil {
		return ports.AgentResult{}, fmt.Errorf("agent loop: %w", err)
	}
	loop, err = loop.WithRetry(rw.wait, rw.random)
	if err != nil {
		return ports.AgentResult{}, fmt.Errorf("agent loop: %w", err)
	}
	return loop.Run(ctx, run)
}

// cryptoJitter is the jitter of the retry delays. It reads crypto/rand, so the generator is not a weak one.
type cryptoJitter struct{}

// Int63n returns a number in [0, n). The loop passes a positive n.
func (cryptoJitter) Int63n(n int64) int64 {
	if n <= 0 {
		return 0
	}
	var b [8]byte
	// Since Go 1.24, crypto/rand.Read never returns an error: it stops the process instead.
	_, _ = rand.Read(b[:])
	return int64(binary.BigEndian.Uint64(b[:])&math.MaxInt64) % n
}

// attemptRecord is one failed attempt of a model call in the results file. RequestBytes and RequestSHA256 name the
// body that was refused, never its content, and SameRequest marks an attempt that sent the same body as the one before
// it: a refusal that repeats on it is a fault of the request, not a passing one.
type attemptRecord struct {
	Kind          string  `json:"kind,omitempty"`
	Status        int     `json:"status,omitempty"`
	RequestBytes  int     `json:"request_bytes,omitempty"`
	RequestSHA256 string  `json:"request_sha256,omitempty"`
	SameRequest   bool    `json:"same_request,omitempty"`
	WaitSeconds   float64 `json:"wait_seconds,omitempty"`
}

func attemptRecords(attempts []ports.AgentAttempt) []attemptRecord {
	var out []attemptRecord
	for _, a := range attempts {
		out = append(out, attemptRecord{Kind: string(a.Kind), Status: a.Status, RequestBytes: a.RequestBytes,
			RequestSHA256: a.RequestHash, SameRequest: a.SameRequest, WaitSeconds: a.Wait.Seconds()})
	}
	return out
}
