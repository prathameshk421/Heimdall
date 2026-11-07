package effects

import (
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestAcceptancePassesOnlyWhenTestsRanAndAllPassed(t *testing.T) {
	cases := []struct {
		name string
		res  ports.SandboxResult
		want change.CIConclusion
	}{
		{"no tests", ports.SandboxResult{}, change.CIFailure},
		{"all pass", ports.SandboxResult{Tests: []ports.TestResult{{Name: "A", Outcome: ports.OutcomePassed}, {Name: "B", Outcome: ports.OutcomePassed}}}, change.CISuccess},
		{"one fails", ports.SandboxResult{Tests: []ports.TestResult{{Name: "A", Outcome: ports.OutcomePassed}, {Name: "B", Outcome: ports.OutcomeFailure}}}, change.CIFailure},
		{"one errors", ports.SandboxResult{Tests: []ports.TestResult{{Name: "A", Outcome: ports.OutcomeError}}}, change.CIFailure},
	}
	for _, c := range cases {
		if got, _ := acceptanceVerdict(c.res); got != c.want {
			t.Errorf("%s: conclusion %s, want %s", c.name, got, c.want)
		}
	}
}
