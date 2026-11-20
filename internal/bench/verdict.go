package bench

import (
	"errors"
	"fmt"
	"strings"
)

// Verdict is the fail-to-pass decision for a bug case.
type Verdict struct {
	Valid  bool
	Reason string
}

// Decide applies the fail-to-pass rule: every target passes after the fix, and at least one target fails before it.
// A build error before the fix is not a red test, because it shows a missing API, not the bug.
func Decide(targets []string, before, after Run) Verdict {
	if len(targets) == 0 {
		return Verdict{Reason: "the fix changes no top-level Go test"}
	}
	if after.BuildFailed {
		return Verdict{Reason: "the fix's tests do not build after the fix"}
	}
	for _, t := range targets {
		if r, ok := after.Tests[t]; !ok {
			return Verdict{Reason: t + " did not run after the fix"}
		} else if r != ResultPass {
			return Verdict{Reason: fmt.Sprintf("%s %s after the fix", t, r)}
		}
	}
	if before.BuildFailed {
		return Verdict{Reason: "the fix's tests do not build before the fix; a build error is not a red test"}
	}
	red := failing(targets, before)
	if len(red) == 0 {
		return Verdict{Reason: "every target passes before the fix (or does not run)"}
	}
	return Verdict{Valid: true, Reason: "red before the fix: " + strings.Join(red, ", ") + "; green after"}
}

func failing(targets []string, r Run) []string {
	var out []string
	for _, t := range targets {
		if r.Tests[t] == ResultFail {
			out = append(out, t)
		}
	}
	return out
}

// Evidence is the result of the extra intro check: the fix's tests on the intro commit and on its parent.
type Evidence struct {
	Confirmed bool   `json:"confirmed"`
	Note      string `json:"note"`
}

// IntroEvidence says whether the intro change brought in the bug: a target fails at the intro commit and every target
// passes at its parent. It is evidence for the manifest reader, not part of the verdict.
func IntroEvidence(targets []string, intro, parent Run) Evidence {
	if intro.BuildFailed {
		return Evidence{Note: "intro: the fix's tests do not build"}
	}
	red := failing(targets, intro)
	if len(red) == 0 {
		return Evidence{Note: "intro: no target fails"}
	}
	if parent.BuildFailed {
		return Evidence{Note: "intro parent: the fix's tests do not build"}
	}
	for _, t := range targets {
		if r, ok := parent.Tests[t]; !ok {
			return Evidence{Note: "intro parent: " + t + " did not run"}
		} else if r != ResultPass {
			return Evidence{Note: fmt.Sprintf("intro parent: %s %s", t, r)}
		}
	}
	return Evidence{Confirmed: true, Note: "red at the intro: " + strings.Join(red, ", ") + "; green at its parent"}
}

// Outcome is the result of checking one case.
type Outcome string

// The case outcomes. An error is a failure to check (network, timeout of a fetch), never a verdict on the case.
const (
	OutcomeValid   Outcome = "valid"
	OutcomeInvalid Outcome = "invalid"
	OutcomeError   Outcome = "error"
	OutcomeSkipped Outcome = "skipped" // no runner for the case's language
)

// Reconcile returns nil when the run agrees with the status the manifest records, and an error that says what to do
// otherwise. A pending case that a run decided must be updated in the manifest.
func Reconcile(status Status, outcome Outcome) error {
	switch {
	case outcome == OutcomeError:
		return errors.New("the case could not be checked")
	case outcome == OutcomeSkipped:
		if status == StatusPending {
			return nil
		}
		return fmt.Errorf("status %s cannot be checked: no runner for the language", status)
	case status == StatusPending:
		return fmt.Errorf("the case is pending: set status: %s", outcome)
	case string(status) != string(outcome):
		return fmt.Errorf("the manifest says %s, the run says %s", status, outcome)
	}
	return nil
}
