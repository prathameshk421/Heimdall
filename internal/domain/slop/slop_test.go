package slop_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/slop"
)

const testsDir = "tests/heimdall"

func rules() policy.Slop {
	return policy.Slop{MaxChangedLines: 400, RequireTicket: true, Ignore: []string{"go.sum"}}
}

// passing is a pull request that satisfies all four rules. Each test flips one thing from it.
func passing() slop.Facts {
	return slop.Facts{
		Title:              "PAY-142 refunds keep the ledger balanced",
		Branch:             "feature/pay-142-refunds",
		Body:               "Adds the refund path.",
		Files:              []slop.File{{Path: "internal/refunds/refunds.go", Added: 10, Deleted: 2}},
		TicketKey:          "PAY-142",
		TestsChanged:       true,
		HeimdallTestsExist: false,
		ExpectedPaths:      []string{"internal/refunds/**"},
	}
}

func failed(r slop.Result) []slop.Rule {
	var out []slop.Rule
	for _, f := range r.Failures {
		out = append(out, f.Rule)
	}
	return out
}

func TestTheBaseFactsPass(t *testing.T) {
	r := slop.Evaluate(passing(), rules(), testsDir)
	require.True(t, r.Pass)
	require.Empty(t, r.Failures)
}

func TestEachRuleHasAStableIdentifier(t *testing.T) {
	require.Equal(t, slop.RuleTicketLinked, slop.Rule("ticket_linked"))
	require.Equal(t, slop.RuleSize, slop.Rule("size"))
	require.Equal(t, slop.RuleTestsPresent, slop.Rule("tests_present"))
	require.Equal(t, slop.RuleScope, slop.Rule("scope"))
}

func TestTicketLinkedMatchesTheWholeKeyAndNothingLonger(t *testing.T) {
	cases := map[string]struct {
		key, title, branch, body string
		ok                       bool
	}{
		"key in the title":                 {"PAY-142", "PAY-142 refunds", "x", "", true},
		"key in the branch, lowercase":     {"PAY-142", "refunds", "feature/pay-142-refunds", "", true},
		"key in the body":                  {"PAY-142", "refunds", "x", "Closes PAY-142.", true},
		"PAY-14 does not match PAY-142":    {"PAY-14", "PAY-142 refunds", "pay-142", "PAY-142", false},
		"PAY-142 does not match PAY-1420":  {"PAY-142", "PAY-1420", "pay-1420", "PAY-1420", false},
		"a longer prefix does not match":   {"PAY-14", "XPAY-14 refunds", "x", "", false},
		"a trailing letter does not match": {"PAY-14", "PAY-14x", "x", "", false},
		"a key with no mention fails":      {"PAY-142", "refunds", "refunds", "refunds", false},
		"an empty key fails":               {"", "PAY-142", "pay-142", "PAY-142", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := passing()
			f.TicketKey, f.Title, f.Branch, f.Body = c.key, c.title, c.branch, c.body
			r := slop.Evaluate(f, rules(), testsDir)
			require.Equal(t, c.ok, !contains(failed(r), slop.RuleTicketLinked), "%s", name)
		})
	}
}

func TestTicketLinkedIsNotRequiredWhenThePolicySaysSo(t *testing.T) {
	f := passing()
	f.TicketKey, f.Title, f.Branch, f.Body = "PAY-142", "refunds", "refunds", "refunds"
	p := rules()
	p.RequireTicket = false
	require.False(t, contains(failed(slop.Evaluate(f, p, testsDir)), slop.RuleTicketLinked))
}

func TestSizeCountsAddedAndDeletedLinesOutsideIgnoredPathsAndTests(t *testing.T) {
	cases := map[string]struct {
		files []slop.File
		ok    bool
	}{
		"exactly the limit passes":            {[]slop.File{{Path: "internal/a.go", Added: 300, Deleted: 100}}, true},
		"one line over the limit fails":       {[]slop.File{{Path: "internal/a.go", Added: 301, Deleted: 100}}, false},
		"the limit is summed across files":    {[]slop.File{{Path: "internal/a.go", Added: 200}, {Path: "internal/b.go", Added: 201}}, false},
		"an ignored lock file does not count": {[]slop.File{{Path: "internal/a.go", Added: 10}, {Path: "go.sum", Added: 5000}}, true},
		"the ignored file in a subdirectory":  {[]slop.File{{Path: "sub/go.sum", Added: 5000}}, true},
		"test lines do not count":             {[]slop.File{{Path: "internal/a.go", Added: 10}, {Path: testsDir + "/PAY-142/x_test.go", Added: 5000}}, true},
		"a sibling of the tests dir counts":   {[]slop.File{{Path: "tests/heimdallx/a.go", Added: 401}}, false},
		"negative counts are refused":         {[]slop.File{{Path: "internal/a.go", Added: -1}}, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := passing()
			f.Files = c.files
			f.ExpectedPaths = []string{"**"}
			r := slop.Evaluate(f, rules(), testsDir)
			require.Equal(t, c.ok, !contains(failed(r), slop.RuleSize), "%s: %+v", name, r.Failures)
		})
	}
}

func TestSizeMessageNamesTheCountAndTheLimit(t *testing.T) {
	f := passing()
	f.Files = []slop.File{{Path: "internal/a.go", Added: 450}}
	f.ExpectedPaths = []string{"**"}
	r := slop.Evaluate(f, rules(), testsDir)
	require.Len(t, r.Failures, 1)
	require.Contains(t, r.Failures[0].Message, "450")
	require.Contains(t, r.Failures[0].Message, "400")
}

func TestTestsPresentNeedsChangedTestsOrHeimdallTests(t *testing.T) {
	cases := map[string]struct {
		changed, exist, ok bool
	}{
		"neither":         {false, false, false},
		"tests changed":   {true, false, true},
		"heimdall exists": {false, true, true},
		"both":            {true, true, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := passing()
			f.TestsChanged, f.HeimdallTestsExist = c.changed, c.exist
			r := slop.Evaluate(f, rules(), testsDir)
			require.Equal(t, c.ok, !contains(failed(r), slop.RuleTestsPresent))
		})
	}
}

func TestScopeAllowsPathsInTheTestsDirOrUnderExpectedPaths(t *testing.T) {
	cases := map[string]struct {
		files    []slop.File
		expected []string
		body     string
		ok       bool
	}{
		"only tests changed":                    {[]slop.File{{Path: testsDir + "/PAY-142/a_test.go", Added: 3}}, nil, "", true},
		"a path under an expected glob":         {[]slop.File{{Path: "internal/refunds/x.go", Added: 3}}, []string{"internal/refunds/**"}, "", true},
		"a path outside every expected glob":    {[]slop.File{{Path: "internal/auth/x.go", Added: 3}}, []string{"internal/refunds/**"}, "", false},
		"outside, with a justification":         {[]slop.File{{Path: "internal/auth/x.go", Added: 3}}, nil, "## Justification\nThe auth fix is needed for the refund.\n", true},
		"outside, justification is blank":       {[]slop.File{{Path: "internal/auth/x.go", Added: 3}}, nil, "## Justification\n   \n## Notes\nx", false},
		"outside, justification is absent":      {[]slop.File{{Path: "internal/auth/x.go", Added: 3}}, nil, "Justification: it is needed", false},
		"heading of another level counts":       {[]slop.File{{Path: "internal/auth/x.go", Added: 3}}, nil, "### Justification\nreason", true},
		"justification is not the last section": {[]slop.File{{Path: "internal/auth/x.go", Added: 3}}, nil, "## Justification\n\n## Other\nreason", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := passing()
			f.Files, f.ExpectedPaths, f.Body = c.files, c.expected, c.body
			r := slop.Evaluate(f, rules(), testsDir)
			require.Equal(t, c.ok, !contains(failed(r), slop.RuleScope), "%s: %+v", name, r.Failures)
		})
	}
}

func TestTheJustificationHeadingIsNamed(t *testing.T) {
	require.Equal(t, "Justification", slop.JustificationHeading)
}

func TestTheFailureListNamesEveryFailingRuleNotJustTheFirst(t *testing.T) {
	f := slop.Facts{
		Title: "refunds",
		Files: []slop.File{{Path: "internal/auth/x.go", Added: 500}},
	}
	r := slop.Evaluate(f, rules(), testsDir)
	require.False(t, r.Pass)
	require.Equal(t, []slop.Rule{slop.RuleTicketLinked, slop.RuleSize, slop.RuleTestsPresent, slop.RuleScope}, failed(r))
	for _, fl := range r.Failures {
		require.NotEmpty(t, fl.Message)
	}
}

func TestAZeroFactsFailsClosed(t *testing.T) {
	r := slop.Evaluate(slop.Facts{}, rules(), testsDir)
	require.False(t, r.Pass)
	require.Contains(t, failed(r), slop.RuleTicketLinked)
	require.Contains(t, failed(r), slop.RuleTestsPresent)
}

func contains(rs []slop.Rule, want slop.Rule) bool {
	for _, r := range rs {
		if r == want {
			return true
		}
	}
	return false
}

// FuzzEvaluate feeds arbitrary titles, paths and counts to the rule. It must never panic, and Pass must agree with
// the failure list.
func FuzzEvaluate(f *testing.F) {
	f.Add("PAY-142 refunds", "feature/pay-142", "body", "internal/a.go", 10, 2, "PAY-142", true)
	f.Add("", "", "", "", 0, 0, "", false)
	f.Add("x", "y", "## Justification\nreason", "/../etc", -5, 1<<40, "PAY-1", false)
	f.Fuzz(func(t *testing.T, title, branch, body, path string, added, deleted int, key string, tests bool) {
		facts := slop.Facts{
			Title: title, Branch: branch, Body: body,
			Files:         []slop.File{{Path: path, Added: added, Deleted: deleted}},
			TicketKey:     key,
			TestsChanged:  tests,
			ExpectedPaths: []string{"internal/**"},
		}
		r := slop.Evaluate(facts, rules(), testsDir)
		require.Equal(t, len(r.Failures) == 0, r.Pass)
	})
}
