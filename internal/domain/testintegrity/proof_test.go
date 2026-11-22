package testintegrity_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	ti "github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testintegrity"
)

const (
	headTree  = "aaa111"
	fixedTree = "bbb222"
)

func run(id, tree string, o ti.RunOutcome) ti.Run { return ti.Run{ID: id, Tree: tree, Outcome: o} }

func TestProve(t *testing.T) {
	fail, pass, build := ti.OutcomeFail, ti.OutcomePass, ti.OutcomeBuildError
	tests := []struct {
		name  string
		runs  []ti.Run
		trees [2]string
		class ti.ProofClass
		why   ti.ProofWhy
	}{
		{"red twice on head and green on the fixed tree", []ti.Run{
			run("1", headTree, fail), run("2", headTree, fail), run("3", fixedTree, pass)},
			[2]string{headTree, fixedTree}, ti.ClassProven, ti.WhyProven},
		{"green on the fixed tree twice", []ti.Run{
			run("1", headTree, fail), run("2", headTree, fail), run("3", fixedTree, pass), run("4", fixedTree, pass)},
			[2]string{headTree, fixedTree}, ti.ClassProven, ti.WhyProven},
		{"one red run is not proof", []ti.Run{
			run("1", headTree, fail), run("3", fixedTree, pass)},
			[2]string{headTree, fixedTree}, ti.ClassNotReproduced, ti.WhyOneRedRun},
		{"the same run reported twice is one run", []ti.Run{
			run("1", headTree, fail), run("1", headTree, fail), run("3", fixedTree, pass)},
			[2]string{headTree, fixedTree}, ti.ClassNotReproduced, ti.WhyOneRedRun},
		{"runs with no id do not count", []ti.Run{
			run("", headTree, fail), run("", headTree, fail), run("3", fixedTree, pass)},
			[2]string{headTree, fixedTree}, ti.ClassNotReproduced, ti.WhyNoHeadRun},
		{"fail then pass on head is a flake", []ti.Run{
			run("1", headTree, fail), run("2", headTree, pass), run("3", fixedTree, pass)},
			[2]string{headTree, fixedTree}, ti.ClassFlaky, ti.WhyFlakyHead},
		{"pass then fail on head is a flake", []ti.Run{
			run("1", headTree, pass), run("2", headTree, fail), run("3", headTree, fail), run("4", fixedTree, pass)},
			[2]string{headTree, fixedTree}, ti.ClassFlaky, ti.WhyFlakyHead},
		{"fails twice on head but flaky on the fixed tree", []ti.Run{
			run("1", headTree, fail), run("2", headTree, fail), run("3", fixedTree, fail), run("4", fixedTree, pass)},
			[2]string{headTree, fixedTree}, ti.ClassFlaky, ti.WhyFlakyFixed},
		{"passes on head", []ti.Run{
			run("1", headTree, pass), run("2", headTree, pass), run("3", fixedTree, pass)},
			[2]string{headTree, fixedTree}, ti.ClassNotReproduced, ti.WhyPassesOnHead},
		{"build error on head is not a fail", []ti.Run{
			run("1", headTree, build), run("2", headTree, build), run("3", fixedTree, pass)},
			[2]string{headTree, fixedTree}, ti.ClassBuildError, ti.WhyBuildHead},
		{"a build error mixed with fails is not red twice", []ti.Run{
			run("1", headTree, fail), run("2", headTree, build), run("3", fixedTree, pass)},
			[2]string{headTree, fixedTree}, ti.ClassFlaky, ti.WhyFlakyHead},
		{"build error on the fixed tree", []ti.Run{
			run("1", headTree, fail), run("2", headTree, fail), run("3", fixedTree, build)},
			[2]string{headTree, fixedTree}, ti.ClassBuildError, ti.WhyBuildFixed},
		{"still red on the fixed tree", []ti.Run{
			run("1", headTree, fail), run("2", headTree, fail), run("3", fixedTree, fail)},
			[2]string{headTree, fixedTree}, ti.ClassNotReproduced, ti.WhyFailsOnFixed},
		{"no run on the fixed tree", []ti.Run{
			run("1", headTree, fail), run("2", headTree, fail)},
			[2]string{headTree, fixedTree}, ti.ClassNotReproduced, ti.WhyNoFixedRun},
		{"no run on head", []ti.Run{run("3", fixedTree, pass)},
			[2]string{headTree, fixedTree}, ti.ClassNotReproduced, ti.WhyNoHeadRun},
		{"runs on a stale tree do not count", []ti.Run{
			run("1", "old999", fail), run("2", "old999", fail), run("3", fixedTree, pass)},
			[2]string{headTree, fixedTree}, ti.ClassNotReproduced, ti.WhyNoHeadRun},
		{"head tree unknown", []ti.Run{run("1", "", fail), run("2", "", fail), run("3", fixedTree, pass)},
			[2]string{"", fixedTree}, ti.ClassNotReproduced, ti.WhyBadTrees},
		{"fixed tree unknown", []ti.Run{run("1", headTree, fail), run("2", headTree, fail)},
			[2]string{headTree, ""}, ti.ClassNotReproduced, ti.WhyBadTrees},
		{"fixed tree is the head", []ti.Run{run("1", headTree, fail), run("2", headTree, fail), run("3", headTree, pass)},
			[2]string{headTree, headTree}, ti.ClassNotReproduced, ti.WhyBadTrees},
		{"an outcome nobody defined is not a fail", []ti.Run{
			run("1", headTree, "crashed"), run("2", headTree, "crashed"), run("3", fixedTree, pass)},
			[2]string{headTree, fixedTree}, ti.ClassBuildError, ti.WhyBuildHead},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ti.Prove(ti.ProofFacts{HeadTree: tc.trees[0], FixedTree: tc.trees[1], Runs: tc.runs})
			require.Equal(t, tc.class, got.Class)
			require.Equal(t, tc.why, got.Why)
			require.NotEmpty(t, got.Reason)
		})
	}
}

func TestProvenIsTheOnlyShowableAnswer(t *testing.T) {
	// Every other class must leave the finding hidden, so one test names the single proven path.
	proven := ti.Prove(ti.ProofFacts{HeadTree: headTree, FixedTree: fixedTree, Runs: []ti.Run{
		run("1", headTree, ti.OutcomeFail), run("2", headTree, ti.OutcomeFail), run("3", fixedTree, ti.OutcomePass)}})
	require.Equal(t, ti.ClassProven, proven.Class)
}

func TestRetries(t *testing.T) {
	p, f, b := ti.OutcomePass, ti.OutcomeFail, ti.OutcomeBuildError
	tests := []struct {
		name string
		in   []ti.RunOutcome
		want ti.Stability
	}{
		{"no runs", nil, ti.StabilityNone},
		{"one pass", []ti.RunOutcome{p}, ti.StabilityPass},
		{"two passes", []ti.RunOutcome{p, p}, ti.StabilityPass},
		{"one fail", []ti.RunOutcome{f}, ti.StabilityFail},
		{"fail then pass on retry is flaky, not a clean pass", []ti.RunOutcome{f, p}, ti.StabilityFlaky},
		{"fail, fail, pass is flaky", []ti.RunOutcome{f, f, p}, ti.StabilityFlaky},
		{"pass then fail is flaky", []ti.RunOutcome{p, f}, ti.StabilityFlaky},
		{"build error then pass is not a clean pass", []ti.RunOutcome{b, p}, ti.StabilityFlaky},
		{"only build errors", []ti.RunOutcome{b, b}, ti.StabilityBuildError},
		{"an unknown outcome is a build error", []ti.RunOutcome{"weird"}, ti.StabilityBuildError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ti.Retries(tc.in))
		})
	}
}
