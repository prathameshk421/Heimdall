package e2e

import (
	"fmt"
	"strings"
)

// Beat is one moment of the three-minute demo script and the board steps that prove it is real (MVP_SCENARIOS
// section 6). If a beat cannot be asserted, it is cut from the video: anything on screen must be real.
type Beat struct {
	At    string
	Shows string
	Steps []string
}

// DemoBeats is the demo script, in the order of the video.
func DemoBeats() []Beat {
	return []Beat{
		{"1:10", "A ticket moves and a room opens in chat", []string{"P1.1", "P1.2"}},
		{"1:20", "The test-author asks one question and the developer answers", []string{"P1.3"}},
		{"1:35", "Failing tests fail for the right reason", []string{"P1.4"}},
		{"1:45", "The oversized pull request is rejected in seconds, naming the rule", []string{"P2.1", "P2.2"}},
		{"1:55", "Three reviewers from three teams each report a different finding", []string{"P3.2", "P3.3"}},
		{"2:00", "One finding is backed by an incident or decision from another team's repository", []string{"P3.4"}},
		{"2:05", "The author disputes one finding, the evidence holds, and the approver upholds it in the room", []string{"P4.1", "P4.3"}},
		{"2:15", "Fixes land with tests and the blocking set drains", []string{"P5.1"}},
		{"2:25", "The GitHub check turns green after the human approval on the exact commit", []string{"P5.2"}},
	}
}

// BeatResult is the outcome of one beat.
type BeatResult struct {
	Beat
	Status Status
	Why    string
}

// DemoCheck judges each beat from the board's results. A beat is green only when every one of its steps passed; a
// step that is not on the board cannot be claimed, so it makes the beat red. A step whose scenario has a failed
// quality check is red, and one whose scenario has an unmeasured check is blocked: its beat is not real until the
// check runs. The worst outcome among a beat's steps is the beat's outcome.
func DemoCheck(results []ScenarioResult) []BeatResult {
	byStep := map[string]Result{}
	scenarioOf := map[string]ScenarioResult{}
	for _, sc := range results {
		for _, r := range sc.Results {
			byStep[r.Step] = r
			scenarioOf[r.Step] = sc
		}
	}
	var out []BeatResult
	for _, b := range DemoBeats() {
		br := BeatResult{Beat: b, Status: StatusPass}
		for _, id := range b.Steps {
			r, ok := byStep[id]
			if !ok {
				r = Result{Step: id, Status: StatusRed, Why: "it is not on the board"}
			} else {
				r = withScenarioChecks(r, scenarioOf[id])
			}
			if severity(r.Status) > severity(br.Status) {
				br.Status, br.Why = r.Status, fmt.Sprintf("%s is %s: %s", id, r.Status, r.Why)
			}
		}
		out = append(out, br)
	}
	return out
}

// withScenarioChecks lets a scenario's own quality and unmeasured checks decide a step that passed on its own.
func withScenarioChecks(r Result, sc ScenarioResult) Result {
	if r.Status != StatusPass {
		return r
	}
	if len(sc.Quality) > 0 {
		return Result{Step: r.Step, Status: StatusRed, Why: "quality: " + sc.Quality[0].String() + " (in " + sc.ID + ")"}
	}
	if len(sc.Unmeasured) > 0 {
		return Result{Step: r.Step, Status: StatusBlocked, Why: "not measured: " + sc.Unmeasured[0]}
	}
	return r
}

// severity orders outcomes from best to worst: pass, then the ones that did not run, then blocked, then red.
func severity(s Status) int {
	switch s {
	case StatusPass:
		return 0
	case StatusNotReached, StatusManual:
		return 1
	case StatusBlocked:
		return 2
	case StatusRed:
		return 3
	}
	return 3
}

// DemoLine is the demo check's last line.
func DemoLine(rs []BeatResult) string {
	proven := 0
	for _, r := range rs {
		if r.Status == StatusPass {
			proven++
		}
	}
	return fmt.Sprintf("demo beats %d/%d real", proven, len(rs))
}

// RenderDemo prints the demo check, one line per beat.
func RenderDemo(rs []BeatResult) string {
	var b strings.Builder
	for _, r := range rs {
		mark := "✔"
		if r.Status != StatusPass {
			mark = "✘ " + r.Status.String() + ": " + r.Why
		}
		fmt.Fprintf(&b, "%s  %-82s %s\n", r.At, r.Shows, mark)
	}
	b.WriteString(DemoLine(rs) + "\n")
	return b.String()
}
