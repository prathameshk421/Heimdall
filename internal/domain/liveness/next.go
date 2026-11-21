// Package liveness decides what to do about a quiet agent (AGENT_INTERACTION_ARCH 6.3, H6). Next is a pure function
// of facts: when the task began, when this agent last called Heimdall, what the board already did, and the model
// provider's health. No ladder is stored; the reconciler carries out what Next returns on each Run (TECH_SPEC 6.2).
package liveness

import "time"

// Action is the board's next step for one task.
type Action string

// The actions of 6.3.
const (
	Wait    Action = "wait"
	Repeat  Action = "repeat_wake"      // post the wake line again
	Probe   Action = "probe_provider"   // ask the model provider once, with the server's own key
	Blocked Action = "provider_blocked" // the provider refuses: say so in every status, post no wake
	Rewake  Action = "stall_rewake"     // post the wake line once more after a stall (Switch offers no session reset, D172)
	Fail    Action = "fail"             // task.failed{unavailable}; Missing names the seat
)

// Health is what the last provider probe found.
type Health string

// Provider health.
const (
	Unprobed Health = ""
	Healthy  Health = "ok"
	Refusing Health = "refusing" // quota, auth or outage
)

// Facts about one open task.
type Facts struct {
	Since        time.Time // when the task was assigned
	Acked        bool
	LastCall     time.Time // this agent's last Heimdall call; zero if none
	Repeated     bool      // the wake line was repeated
	StallWokenAt time.Time // when the agent was woken again after a stall; zero if never
	Provider     Health
}

// Windows are the two waits of 6.3.
type Windows struct {
	Ack   time.Duration // how long a wake may go unacknowledged
	Stall time.Duration // how long an acknowledged agent may make no call
}

// Next returns what the board does now for f. The rows are 6.3's table, in order.
func Next(f Facts, w Windows, now time.Time) Action {
	quietSince := latest(f.Since, f.LastCall)
	switch {
	case f.Provider == Refusing:
		return Blocked
	case !f.Acked && !f.Repeated && now.Sub(f.Since) >= w.Ack:
		return Repeat
	case now.Sub(quietSince) < w.Stall:
		return Wait
	case f.Provider == Unprobed:
		return Probe
	case f.StallWokenAt.IsZero():
		return Rewake
	case now.Sub(latest(f.StallWokenAt, f.LastCall)) >= w.Stall:
		return Fail
	}
	return Wait
}

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
