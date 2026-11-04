package e2e_test

import (
	"context"
	"errors"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

// fakeWorld is a hand-written double of the simulator's World, used only to test the board's own logic.
// It is never a product adapter: the board's job is to judge the product, and these tests judge the board.
type fakeWorld struct {
	missing  map[e2e.Need]string // prerequisites that are not available, with the reason
	counts   []e2e.Counts        // successive Counts() answers; the last repeats
	replayed int
	probes   map[e2e.Need]int // how many times each prerequisite was probed
	spend    []float64        // successive Spend() answers; empty means "not built"
	now      time.Time
}

func (f *fakeWorld) Tracker() e2e.TrackerUser     { return nil }
func (f *fakeWorld) GitHub() e2e.GitHubUser       { return nil }
func (f *fakeWorld) Chat() e2e.ChatUser           { return nil }
func (f *fakeWorld) Server() e2e.Observer         { return fakeObserver{f} }
func (f *fakeWorld) Heimdall() e2e.CLI            { return nil }
func (f *fakeWorld) Clock() e2e.Wait              { return fakeClock{f} }
func (f *fakeWorld) Agents() e2e.AgentControl     { return nil }
func (f *fakeWorld) Scripted() e2e.ScriptedAgents { return nil }
func (f *fakeWorld) Faults() e2e.Faults           { return nil }
func (f *fakeWorld) Runner() e2e.TestRunner       { return nil }
func (f *fakeWorld) Clean() e2e.CleanRoom         { return nil }

func (f *fakeWorld) Prereq(_ context.Context, n e2e.Need) e2e.Availability {
	if f.probes == nil {
		f.probes = map[e2e.Need]int{}
	}
	f.probes[n]++
	if why, gone := f.missing[n]; gone {
		return e2e.Availability{Why: why}
	}
	return e2e.Availability{OK: true}
}

type fakeObserver struct{ f *fakeWorld }

func (o fakeObserver) Gate(context.Context, e2e.PR) (e2e.GateState, error) {
	return e2e.GateState{}, errors.New("not used")
}
func (o fakeObserver) Checks(context.Context, e2e.PR) ([]e2e.Check, error) { return nil, nil }
func (o fakeObserver) Findings(context.Context, e2e.PR) ([]e2e.Finding, error) {
	return nil, nil
}
func (o fakeObserver) Dashboard(context.Context, string) (string, error) { return "", nil }
func (o fakeObserver) Health(context.Context) error                      { return nil }
func (o fakeObserver) Spend(context.Context) (float64, error) {
	if len(o.f.spend) == 0 {
		return 0, &e2e.NotBuiltError{Task: "T19", What: "the spend reader"}
	}
	v := o.f.spend[0]
	if len(o.f.spend) > 1 {
		o.f.spend = o.f.spend[1:]
	}
	return v, nil
}
func (o fakeObserver) Counts(context.Context) (e2e.Counts, error) {
	if len(o.f.counts) == 0 {
		return e2e.Counts{}, nil
	}
	c := o.f.counts[0]
	if len(o.f.counts) > 1 {
		o.f.counts = o.f.counts[1:]
	}
	return c, nil
}
func (o fakeObserver) Replay(context.Context) error { o.f.replayed++; return nil }

type fakeClock struct{ f *fakeWorld }

func (c fakeClock) Until(context.Context, string, time.Duration, func(context.Context) (bool, string, error)) error {
	return nil
}
func (c fakeClock) Since(t time.Time) time.Duration { return c.f.now.Sub(t) }
func (c fakeClock) Now() time.Time                  { return c.f.now }
