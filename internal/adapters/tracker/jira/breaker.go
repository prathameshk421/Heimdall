package jira

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// breaker opens after breakerLimit consecutive unavailable calls and refuses calls for breakerCooldown (TECH_SPEC
// 2.5). When the cooldown ends, the next call is the only probe: it moves the cooldown forward before it is sent, so
// a second caller in the same cooldown is refused. A probe that succeeds closes the breaker; one that fails opens it
// for another cooldown. Client errors (401, 404) are neutral. It is safe for concurrent use.
type breaker struct {
	mu        sync.Mutex
	failures  int
	openUntil time.Time
}

// allow reports whether a call may be sent, and whether it is the probe. An open breaker returns an error that wraps
// ports.ErrUnavailable and names the instant it closes for a probe.
func (b *breaker) allow(now time.Time) (probe bool, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures < breakerLimit {
		return false, nil
	}
	if now.Before(b.openUntil) {
		return false, fmt.Errorf("%w: jira circuit open until %s", ports.ErrUnavailable, b.openUntil.Format(time.RFC3339))
	}
	b.openUntil = now.Add(breakerCooldown)
	return true, nil
}

// record counts the outcome of one call. Only ErrUnavailable counts as a failure; nil closes the breaker.
func (b *breaker) record(now time.Time, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case err == nil:
		b.failures = 0
		b.openUntil = time.Time{}
	case errors.Is(err, ports.ErrUnavailable):
		b.failures++
		if b.failures >= breakerLimit {
			b.openUntil = now.Add(breakerCooldown)
		}
	}
}
