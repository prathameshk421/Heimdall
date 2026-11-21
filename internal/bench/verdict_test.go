package bench

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func results(pairs ...string) Run {
	r := Run{Tests: map[string]TestResult{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		r.Tests[pairs[i]] = TestResult(pairs[i+1])
	}
	return r
}

func buildFailed() Run { return Run{BuildFailed: true, BuildOutput: "undefined: NewAPI"} }

func TestDecideFailToPass(t *testing.T) {
	targets := []string{"TestA", "TestB"}
	tests := []struct {
		name          string
		targets       []string
		before, after Run
		valid         bool
		reason        string
	}{
		{"red before, green after", targets, results("TestA", "fail", "TestB", "pass"), results("TestA", "pass", "TestB", "pass"), true, "red before the fix: TestA"},
		{"every target red before", targets, results("TestA", "fail", "TestB", "fail"), results("TestA", "pass", "TestB", "pass"), true, "red before the fix: TestA, TestB"},
		{"no targets", nil, results(), results(), false, "changes no top-level Go test"},
		{"green before", targets, results("TestA", "pass", "TestB", "pass"), results("TestA", "pass", "TestB", "pass"), false, "every target passes before the fix"},
		{"skipped before is not red", targets, results("TestA", "skip", "TestB", "pass"), results("TestA", "pass", "TestB", "pass"), false, "every target passes before the fix"},
		{"build error before", targets, buildFailed(), results("TestA", "pass", "TestB", "pass"), false, "a build error is not a red test"},
		{"red after", targets, results("TestA", "fail", "TestB", "fail"), results("TestA", "pass", "TestB", "fail"), false, "TestB fail after the fix"},
		{"skipped after", targets, results("TestA", "fail", "TestB", "fail"), results("TestA", "pass", "TestB", "skip"), false, "TestB skip after the fix"},
		{"missing after", targets, results("TestA", "fail", "TestB", "fail"), results("TestA", "pass"), false, "TestB did not run after the fix"},
		{"build error after", targets, results("TestA", "fail"), buildFailed(), false, "tests do not build after the fix"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Decide(tt.targets, tt.before, tt.after)
			require.Equal(t, tt.valid, v.Valid, v.Reason)
			require.Contains(t, v.Reason, tt.reason)
		})
	}
}

func TestIntroEvidence(t *testing.T) {
	targets := []string{"TestA", "TestB"}
	tests := []struct {
		name          string
		intro, parent Run
		confirmed     bool
		note          string
	}{
		{"red at intro, green at its parent", results("TestA", "fail", "TestB", "pass"), results("TestA", "pass", "TestB", "pass"), true, "red at the intro: TestA; green at its parent"},
		{"intro does not build", buildFailed(), results("TestA", "pass", "TestB", "pass"), false, "intro: the fix's tests do not build"},
		{"green at intro", results("TestA", "pass", "TestB", "pass"), results("TestA", "pass", "TestB", "pass"), false, "intro: no target fails"},
		{"parent does not build", results("TestA", "fail"), buildFailed(), false, "intro parent: the fix's tests do not build"},
		{"red at parent too", results("TestA", "fail"), results("TestA", "fail", "TestB", "pass"), false, "intro parent: TestA fail"},
		{"missing at parent", results("TestA", "fail"), results("TestA", "pass"), false, "intro parent: TestB did not run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := IntroEvidence(targets, tt.intro, tt.parent)
			require.Equal(t, tt.confirmed, e.Confirmed, e.Note)
			require.Contains(t, e.Note, tt.note)
		})
	}
}

func TestReconcile(t *testing.T) {
	tests := []struct {
		status  Status
		outcome Outcome
		want    string // empty: agrees
	}{
		{StatusValid, OutcomeValid, ""},
		{StatusInvalid, OutcomeInvalid, ""},
		{StatusPending, OutcomeSkipped, ""},
		{StatusValid, OutcomeInvalid, "manifest says valid, the run says invalid"},
		{StatusInvalid, OutcomeValid, "manifest says invalid, the run says valid"},
		{StatusPending, OutcomeValid, "set status: valid"},
		{StatusPending, OutcomeInvalid, "set status: invalid"},
		{StatusValid, OutcomeError, "could not be checked"},
		{StatusPending, OutcomeError, "could not be checked"},
		{StatusValid, OutcomeSkipped, "no runner"},
	}
	for _, tt := range tests {
		t.Run(string(tt.status)+"/"+string(tt.outcome), func(t *testing.T) {
			err := Reconcile(tt.status, tt.outcome)
			if tt.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.want)
		})
	}
}
