package verify

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// RequiredRuns is how many runs the flake check needs (TECH_SPEC 12.1: 3 runs).
const RequiredRuns = 3

// Outcome is how one test behaved across the runs.
type Outcome string

// The three outcomes of the flake check. A stable failure is not flaky: it is the same result every time.
const (
	StablePass Outcome = "stable_pass"
	StableFail Outcome = "stable_fail"
	Flaky      Outcome = "flaky"
)

// TestStability is one test's behaviour across the runs. Suite is kept because two packages can hold a test with
// the same name.
type TestStability struct {
	Suite     string
	Test      string
	Stability Outcome
}

// FlakeReport is the outcome of the flake check, sorted by suite and test.
type FlakeReport struct {
	Runs  int
	Tests []TestStability
}

// Rejected lists the flaky tests, the ones quarantined and excluded from the gate.
func (r FlakeReport) Rejected() []TestStability {
	var out []TestStability
	for _, t := range r.Tests {
		if t.Stability == Flaky {
			out = append(out, t)
		}
	}
	return out
}

// ErrRunCount says the flake check was given the wrong number of runs. Two runs cannot tell flaky from stable.
var ErrRunCount = errors.New("the flake check needs exactly 3 runs")

type testKey struct {
	suite string
	test  string
}

// Stability compares the runs and reports each test as a stable pass, a stable failure or flaky. A test that is
// missing from one run is flaky too: a run whose build broke is not evidence that the test is stable.
func Stability(runs []Report) (FlakeReport, error) {
	if len(runs) != RequiredRuns {
		return FlakeReport{}, fmt.Errorf("got %d run(s): %w", len(runs), ErrRunCount)
	}
	indexes := make([]map[testKey]Status, 0, len(runs))
	seen := make(map[testKey]bool)
	for _, r := range runs {
		idx := make(map[testKey]Status)
		for _, suite := range r.Suites {
			for _, c := range suite.Cases {
				key := testKey{suite: suite.Name, test: c.Name}
				idx[key] = c.Status
				seen[key] = true
			}
		}
		indexes = append(indexes, idx)
	}

	keys := make([]testKey, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b testKey) int {
		if c := strings.Compare(a.suite, b.suite); c != 0 {
			return c
		}
		return strings.Compare(a.test, b.test)
	})

	rep := FlakeReport{Runs: RequiredRuns, Tests: make([]TestStability, 0, len(keys))}
	for _, k := range keys {
		rep.Tests = append(rep.Tests, TestStability{Suite: k.suite, Test: k.test, Stability: classify(indexes, k)})
	}
	return rep, nil
}

// classify compares one test's status across every run. Presence counts: a test that did not appear in a run is not
// the same result as appearing and passing.
func classify(indexes []map[testKey]Status, k testKey) Outcome {
	first, ok := indexes[0][k]
	if !ok {
		return Flaky
	}
	for _, idx := range indexes[1:] {
		status, present := idx[k]
		if !present || status != first {
			return Flaky
		}
	}
	if first == StatusPass {
		return StablePass
	}
	return StableFail
}
