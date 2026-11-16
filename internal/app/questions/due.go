// Package questions decides what an open question needs next (AGENT_INTERACTION_ARCH 5.2, H8). It is one pure
// function of the facts and the clock: nothing is scheduled or stored, and a reminder, an escalation or an assumption
// that was already sent is a fact the caller passes back in.
package questions

import (
	"sort"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// Action is something an open question needs.
type Action string

// The actions of 5.2.
const (
	Remind   Action = "remind"   // one reminder in the card's thread
	Escalate Action = "escalate" // the card mentions a policy owner
	Assume   Action = "assume"   // the asking agent is woken and proceeds on a written assumption
)

// Question is the facts about one question.
type Question struct {
	AskedAt  time.Time
	Answered bool
	Done     map[Action]bool // actions already taken
}

// Due returns the actions due at now that were not taken yet, in the order of their deadlines. An answered question
// needs nothing.
func Due(q Question, t policy.Timeouts, now time.Time) []Action {
	if q.Answered {
		return nil
	}
	deadlines := []struct {
		a     Action
		after time.Duration
	}{{Remind, time.Duration(t.RemindAfter)}, {Escalate, time.Duration(t.EscalateAfter)}, {Assume, time.Duration(t.Question)}}
	sort.SliceStable(deadlines, func(i, j int) bool { return deadlines[i].after < deadlines[j].after })
	var out []Action
	for _, d := range deadlines {
		if d.after > 0 && !q.Done[d.a] && now.Sub(q.AskedAt) >= d.after {
			out = append(out, d.a)
		}
	}
	return out
}
