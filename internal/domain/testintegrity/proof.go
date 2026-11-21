package testintegrity

import "strconv"

// RunOutcome is what one run of a test gave.
type RunOutcome string

// The outcomes of a run. A build error is not a fail: the test never ran.
const (
	OutcomePass       RunOutcome = "pass"
	OutcomeFail       RunOutcome = "fail"
	OutcomeBuildError RunOutcome = "build_error"
)

func (o RunOutcome) valid() bool {
	return o == OutcomePass || o == OutcomeFail || o == OutcomeBuildError
}

// Stability is what a set of runs on one tree says about a test (rule 5).
type Stability string

// The five stabilities.
const (
	StabilityNone       Stability = "no_runs"
	StabilityPass       Stability = "pass"
	StabilityFail       Stability = "fail"
	StabilityBuildError Stability = "build_error"
	// StabilityFlaky: the runs on the same tree do not agree. A test that failed and then passed on a retry is here.
	StabilityFlaky Stability = "flaky"
)

// Retries classifies the outcomes of runs of one test on one tree. A rerun that passes is never a clean pass: any
// mix of outcomes, such as fail then pass, is flaky. An outcome this package does not know counts as a build error,
// because the test did not give a usable result.
func Retries(outcomes []RunOutcome) Stability {
	if len(outcomes) == 0 {
		return StabilityNone
	}
	first := normalize(outcomes[0])
	for _, o := range outcomes[1:] {
		if normalize(o) != first {
			return StabilityFlaky
		}
	}
	switch first {
	case OutcomePass:
		return StabilityPass
	case OutcomeFail:
		return StabilityFail
	case OutcomeBuildError:
		return StabilityBuildError
	}
	return StabilityBuildError
}

func normalize(o RunOutcome) RunOutcome {
	if o.valid() {
		return o
	}
	return OutcomeBuildError
}

// Run is one run of the finding's test on one tree. ID tells separate runs apart: the same ID twice is one run
// reported twice. Tree is the git tree hash that was tested.
type Run struct {
	ID      string
	Tree    string
	Outcome RunOutcome
}

// ProofFacts are the runs of a finding's test. HeadTree is the tree of the PR head. FixedTree is the tree where the
// suspected lines are reverted or fixed. A run on any other tree is stale and does not count.
type ProofFacts struct {
	HeadTree  string
	FixedTree string
	Runs      []Run
}

// ProofClass is the answer of the proof rule. Only proven shows a finding. The other three keep it hidden and
// counted, and none of them says the finding is false.
type ProofClass string

// The four answers.
const (
	ClassProven        ProofClass = "proven"
	ClassNotReproduced ProofClass = "not_reproduced"
	ClassFlaky         ProofClass = "flaky"
	ClassBuildError    ProofClass = "build_error"
)

// ProofWhy is the stable reason of an answer.
type ProofWhy string

// The reasons. Each has one sentence in ProofResult.Reason.
const (
	WhyProven       ProofWhy = "fails_twice_passes_fixed"
	WhyBadTrees     ProofWhy = "bad_trees"
	WhyNoHeadRun    ProofWhy = "no_head_run"
	WhyPassesOnHead ProofWhy = "passes_on_head"
	WhyOneRedRun    ProofWhy = "one_red_run"
	WhyFlakyHead    ProofWhy = "flaky_head"
	WhyBuildHead    ProofWhy = "build_error_head"
	WhyNoFixedRun   ProofWhy = "no_fixed_run"
	WhyFlakyFixed   ProofWhy = "flaky_fixed"
	WhyBuildFixed   ProofWhy = "build_error_fixed"
	WhyFailsOnFixed ProofWhy = "fails_on_fixed"
	WhyNoResult     ProofWhy = "no_result"
)

// ProofResult is the answer and why.
type ProofResult struct {
	Class  ProofClass
	Why    ProofWhy
	Reason string
}

// Prove is rule 4 of A6 and the proof rule of A11. A finding's test is proof only when it fails on the PR head in two
// separate runs and passes on the tree where the suspected lines are reverted or fixed. One red run is not proof. A
// flake is not proof. A build error is not a fail. It is a pure function of the runs and never fails.
func Prove(f ProofFacts) ProofResult {
	if f.HeadTree == "" || f.FixedTree == "" || f.HeadTree == f.FixedTree {
		return result(ClassNotReproduced, WhyBadTrees,
			"the head tree and the fixed tree must both be known and must differ, so there is no proof")
	}
	head, fixed := runsOn(f)
	if r, done := headVerdict(head); done {
		return r
	}
	return fixedVerdict(fixed)
}

func result(c ProofClass, w ProofWhy, reason string) ProofResult {
	return ProofResult{Class: c, Why: w, Reason: reason}
}

// runsOn keeps the first run of each ID, drops a blank ID, and splits the outcomes by tree. A run on another tree is
// stale. A run with no ID cannot be told from a repeat, so it does not count.
func runsOn(f ProofFacts) (head, fixed []RunOutcome) {
	seen := map[string]bool{}
	for _, r := range f.Runs {
		if r.ID == "" || seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		switch r.Tree {
		case f.HeadTree:
			head = append(head, r.Outcome)
		case f.FixedTree:
			fixed = append(fixed, r.Outcome)
		}
	}
	return head, fixed
}

// headVerdict decides on the head runs. It returns done when the answer is already known. Two fails in a row are the
// only way on to the fixed tree.
func headVerdict(head []RunOutcome) (ProofResult, bool) {
	switch Retries(head) {
	case StabilityNone:
		return result(ClassNotReproduced, WhyNoHeadRun, "the test has no run on the PR head"), true
	case StabilityFlaky:
		return result(ClassFlaky, WhyFlakyHead,
			"the runs on the PR head do not agree ("+tally(head)+"), so the test is flaky and proves nothing"), true
	case StabilityBuildError:
		return result(ClassBuildError, WhyBuildHead,
			"the test did not build on the PR head, and a build error is not a fail"), true
	case StabilityPass:
		return result(ClassNotReproduced, WhyPassesOnHead, "the test passes on the PR head, so it shows no bug"), true
	case StabilityFail:
		if len(head) < 2 {
			return result(ClassNotReproduced, WhyOneRedRun,
				"the test failed once on the PR head, and one red run is not proof"), true
		}
	}
	return ProofResult{}, false
}

func fixedVerdict(fixed []RunOutcome) ProofResult {
	switch Retries(fixed) {
	case StabilityNone:
		return result(ClassNotReproduced, WhyNoFixedRun,
			"the test failed twice on the PR head, but it has no run on the tree where the suspected lines are fixed")
	case StabilityFlaky:
		return result(ClassFlaky, WhyFlakyFixed,
			"the runs on the fixed tree do not agree ("+tally(fixed)+"), so the test is flaky and proves nothing")
	case StabilityBuildError:
		return result(ClassBuildError, WhyBuildFixed,
			"the test did not build on the fixed tree, and a build error is not a pass")
	case StabilityFail:
		return result(ClassNotReproduced, WhyFailsOnFixed,
			"the test also fails on the fixed tree, so it does not isolate the suspected lines")
	case StabilityPass:
		return result(ClassProven, WhyProven,
			"the test failed in two separate runs on the PR head and passes on the fixed tree")
	}
	return result(ClassNotReproduced, WhyNoResult, "the runs give no usable answer")
}

// tally writes the counts of each outcome, for a reason sentence.
func tally(outcomes []RunOutcome) string {
	var p, f, b int
	for _, o := range outcomes {
		switch normalize(o) {
		case OutcomePass:
			p++
		case OutcomeFail:
			f++
		case OutcomeBuildError:
			b++
		}
	}
	return strconv.Itoa(f) + " fail, " + strconv.Itoa(p) + " pass, " + strconv.Itoa(b) + " build error"
}
