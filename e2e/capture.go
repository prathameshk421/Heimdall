package e2e

import (
	"fmt"
	"time"
)

// Capture is everything a person saw during a scenario: room messages, the gate as shown on GitHub, and
// findings as displayed. Steps add to it; the board runs the quality checks over it.
type Capture struct {
	items []captured
	seen  map[string]bool
	allIn time.Time
}

type captured struct {
	msg     *Message
	gate    *GateState
	finding *FindingView
}

// Violation is one quality-check failure.
type Violation struct {
	Check  string
	Detail string
}

func (v Violation) String() string { return v.Check + ": " + v.Detail }

// The names of the quality checks, as they appear on the board (MVP_SCENARIOS section 4).
const (
	CheckHumanWords  = "human words only"
	CheckRedSaysWhy  = "every red says why"
	CheckCheckable   = "every finding is checkable"
	CheckQuiet       = "quiet"
	CheckAddressed   = "addressed correctly"
	CheckReplay      = "idempotent replay"
	CheckCost        = "cost"
	CheckAgentsStay  = "agents stay out"
	CheckLatencyName = "latency"
)

// AddMessages records chat messages. A message with an id that was already recorded is ignored, so
// reading a transcript twice does not look like the product posting twice.
func (c *Capture) AddMessages(ms ...Message) {
	for _, m := range ms {
		if m.ID != "" {
			if c.seen == nil {
				c.seen = map[string]bool{}
			}
			if c.seen[m.ID] {
				continue
			}
			c.seen[m.ID] = true
		}
		m := m
		c.items = append(c.items, captured{msg: &m})
	}
}

// AddGate records a gate state.
func (c *Capture) AddGate(g GateState) { c.items = append(c.items, captured{gate: &g}) }

// AddFinding records a displayed finding.
func (c *Capture) AddFinding(f FindingView) { c.items = append(c.items, captured{finding: &f}) }

// MarkAllSubmitted records when the last reviewer submitted; before then no agent may show a finding.
func (c *Capture) MarkAllSubmitted(t time.Time) { c.allIn = t }

// AllSubmitted is the time recorded by MarkAllSubmitted, or the zero time.
func (c *Capture) AllSubmitted() time.Time { return c.allIn }

// Len is the number of things captured so far.
func (c *Capture) Len() int { return len(c.items) }

// Messages returns every captured message in order.
func (c *Capture) Messages() []Message {
	var out []Message
	for _, it := range c.items {
		if it.msg != nil {
			out = append(out, *it.msg)
		}
	}
	return out
}

// CheckFrom runs the per-item quality checks over what was captured since item i: human words, reasons on
// every red, checkable findings, and who may be pinged.
func (c *Capture) CheckFrom(i int) []Violation {
	var out []Violation
	var msgs []Message
	raw := func(where, text string) {
		for _, frag := range RawInternals(text) {
			out = append(out, Violation{CheckHumanWords, fmt.Sprintf("%s shows %q", where, frag)})
		}
	}
	for _, it := range c.items[i:] {
		switch {
		case it.msg != nil:
			if it.msg.FromAgent {
				raw("a message from "+it.msg.From, it.msg.Text)
				msgs = append(msgs, *it.msg)
			}
		case it.gate != nil:
			g := it.gate
			raw("the gate summary", g.Summary)
			for _, r := range g.Reasons {
				raw("a gate reason", r.Text)
			}
			for _, p := range GateProblems(*g) {
				out = append(out, Violation{CheckRedSaysWhy, p})
			}
		case it.finding != nil:
			raw("finding "+it.finding.ID, it.finding.Claim)
			for _, p := range FindingProblems(*it.finding) {
				out = append(out, Violation{CheckCheckable, p})
			}
		}
	}
	for _, p := range AddressingProblems(msgs) {
		out = append(out, Violation{CheckAddressed, p})
	}
	return out
}
