package testintegrity_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	ti "github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testintegrity"
)

func rulesOf(vs []ti.Violation) []ti.Rule {
	var out []ti.Rule
	for _, v := range vs {
		out = append(out, v.Rule)
	}
	return out
}

func TestSamples(t *testing.T) {
	tests := []struct {
		name string
		want []ti.Rule
	}{
		{"go_tautology", []ti.Rule{ti.RuleTautology}},
		{"go_fake_subject", []ti.Rule{ti.RuleMocksChangedSource}},
		{"go_mockery_subject", []ti.Rule{ti.RuleMocksChangedSource}},
		{"py_mock_subject", []ti.Rule{ti.RuleMocksChangedSource, ti.RuleTautology}},
		{"ts_mock_subject", []ti.Rule{ti.RuleMocksChangedSource, ti.RuleTautology}},
		{"ts_skip", []ti.Rule{ti.RuleSkipAdded}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := ti.Evaluate(loadSample(t, tc.name), ti.Options{})
			require.ElementsMatch(t, tc.want, rulesOf(res.Violations))
			require.Empty(t, res.Blocking(), "static heuristics warn and do not block by default")
			for _, v := range res.Violations {
				require.NotEmpty(t, v.Message)
				require.NotEmpty(t, v.ID)
			}
		})
	}
}

func TestMockLevelCanBlock(t *testing.T) {
	res := ti.Evaluate(loadSample(t, "py_mock_subject"), ti.Options{MockLevel: ti.LevelBlock})
	require.Equal(t, []ti.Rule{ti.RuleMocksChangedSource}, rulesOf(res.Blocking()))
	require.Equal(t, []ti.Rule{ti.RuleTautology}, rulesOf(res.Warnings()))
}

func TestMockMatching(t *testing.T) {
	file := func(p string, test bool) ti.FileChange {
		return ti.FileChange{Path: p, Status: ti.StatusModified, IsTest: test}
	}
	tests := []struct {
		name    string
		testAt  string
		mock    ti.MockFact
		changed []ti.FileChange
		flagged bool
	}{
		{"go fake of the changed interface file", "internal/pay/a_test.go",
			ti.MockFact{Kind: ti.MockGoFake, Target: "internal/pay/gateway.go"},
			[]ti.FileChange{file("internal/pay/gateway.go", false)}, true},
		{"go fake of an unchanged file", "internal/pay/a_test.go",
			ti.MockFact{Kind: ti.MockGoFake, Target: "internal/pay/gateway.go"},
			[]ti.FileChange{file("internal/pay/other.go", false)}, false},
		{"go mocks package of a changed package", "internal/pay/a_test.go",
			ti.MockFact{Kind: ti.MockGoGenerated, Target: "github.com/acme/shop/internal/pay/mocks"},
			[]ti.FileChange{file("internal/pay/ledger.go", false)}, true},
		{"go mocks package of another package", "internal/pay/a_test.go",
			ti.MockFact{Kind: ti.MockGoGenerated, Target: "github.com/acme/shop/internal/ledger/mocks"},
			[]ti.FileChange{file("internal/pay/ledger.go", false)}, false},
		{"go package whose name only ends alike", "internal/pay/a_test.go",
			ti.MockFact{Kind: ti.MockGoGenerated, Target: "github.com/acme/shop/internal/prepay/mocks"},
			[]ti.FileChange{file("internal/pay/ledger.go", false)}, false},
		{"go mock source file from mockery", "internal/pay/a_test.go",
			ti.MockFact{Kind: ti.MockGoGenerated, Target: "internal/pay/ledger.go"},
			[]ti.FileChange{file("internal/pay/ledger.go", false)}, true},
		{"go changed file is a test file", "internal/pay/a_test.go",
			ti.MockFact{Kind: ti.MockGoFake, Target: "internal/pay/helpers_test.go"},
			[]ti.FileChange{file("internal/pay/helpers_test.go", true)}, false},
		{"python patch of a function in a changed module", "tests/test_refund.py",
			ti.MockFact{Kind: ti.MockPythonPatch, Target: "shop.billing.refund.compute_refund"},
			[]ti.FileChange{file("shop/billing/refund.py", false)}, true},
		{"python patch of a method of a class in a changed module", "tests/test_refund.py",
			ti.MockFact{Kind: ti.MockPythonPatch, Target: "shop.billing.refund.Refunder.run"},
			[]ti.FileChange{file("shop/billing/refund.py", false)}, true},
		{"python patch under a source root", "tests/test_refund.py",
			ti.MockFact{Kind: ti.MockPythonPatch, Target: "shop.billing.refund.compute_refund"},
			[]ti.FileChange{file("src/shop/billing/refund.py", false)}, true},
		{"python patch of a package function, init changed", "tests/test_refund.py",
			ti.MockFact{Kind: ti.MockPythonPatch, Target: "shop.billing.compute"},
			[]ti.FileChange{file("shop/billing/__init__.py", false)}, true},
		{"python patch of an external library", "tests/test_refund.py",
			ti.MockFact{Kind: ti.MockPythonPatch, Target: "requests.get"},
			[]ti.FileChange{file("shop/billing/refund.py", false)}, false},
		{"python patch of an unchanged sibling module", "tests/test_refund.py",
			ti.MockFact{Kind: ti.MockPythonPatch, Target: "shop.billing.ledger.post"},
			[]ti.FileChange{file("shop/billing/refund.py", false)}, false},
		{"python deep name does not meet a top-level module", "tests/test_refund.py",
			ti.MockFact{Kind: ti.MockPythonPatch, Target: "shop.billing.ledger.post"},
			[]ti.FileChange{file("shop.py", false)}, false},
		{"js relative mock of a changed module", "test/refund.test.ts",
			ti.MockFact{Kind: ti.MockJSMock, Target: "../src/refund"},
			[]ti.FileChange{file("src/refund.ts", false)}, true},
		{"js relative mock with extension", "test/refund.test.ts",
			ti.MockFact{Kind: ti.MockJSMock, Target: "../src/refund.js"},
			[]ti.FileChange{file("src/refund.ts", false)}, true},
		{"js mock of a directory index", "test/refund.test.ts",
			ti.MockFact{Kind: ti.MockJSMock, Target: "../src/refund"},
			[]ti.FileChange{file("src/refund/index.ts", false)}, true},
		{"js alias mock of a changed module", "test/refund.test.ts",
			ti.MockFact{Kind: ti.MockJSMock, Target: "@/lib/refund"},
			[]ti.FileChange{file("src/lib/refund.ts", false)}, true},
		{"js mock of an npm package", "test/refund.test.ts",
			ti.MockFact{Kind: ti.MockJSMock, Target: "axios"},
			[]ti.FileChange{file("src/axios.ts", false)}, false},
		{"js mock of an unchanged module", "test/refund.test.ts",
			ti.MockFact{Kind: ti.MockJSMock, Target: "../src/ledger"},
			[]ti.FileChange{file("src/refund.ts", false)}, false},
		{"js relative path that leaves the repository", "refund.test.ts",
			ti.MockFact{Kind: ti.MockJSMock, Target: "../outside/refund"},
			[]ti.FileChange{file("refund.ts", false)}, false},
		{"mock that the change did not add", "test/refund.test.ts",
			ti.MockFact{Kind: ti.MockJSMock, Target: "../src/refund", Added: false},
			[]ti.FileChange{file("src/refund.ts", false)}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.mock
			if tc.name != "mock that the change did not add" {
				m.Added = true
			}
			f := ti.Facts{OwnerKnown: true, Files: tc.changed, Tests: []ti.TestFile{{Path: tc.testAt, Mocks: []ti.MockFact{m}}}}
			got := ti.Evaluate(f, ti.Options{}).Violations
			if tc.flagged {
				require.Len(t, got, 1)
				require.Equal(t, ti.RuleMocksChangedSource, got[0].Rule)
				require.Equal(t, ti.LevelWarn, got[0].Level)
				require.Equal(t, tc.testAt, got[0].Path)
			} else {
				require.Empty(t, got)
			}
		})
	}
}

func lines(s ...string) []string { return s }

func TestWeakened(t *testing.T) {
	tests := []struct {
		name string
		c    ti.FileChange
		want []ti.Rule
	}{
		{"go test function deleted",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusModified,
				Removed: lines("func TestRefund(t *testing.T) {", "\tassert.Equal(t, 1, f())", "}")},
			[]ti.Rule{ti.RuleTestDeleted, ti.RuleAssertionDeleted}},
		{"go test function renamed is also seen as deleted",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusModified,
				Removed: lines("func TestRefund(t *testing.T) {"), Added: lines("func TestRefundV2(t *testing.T) {")},
			[]ti.Rule{ti.RuleTestDeleted}},
		{"go test function kept with a changed signature line",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusModified,
				Removed: lines("func TestRefund(t *testing.T) {"), Added: lines("func TestRefund(t *testing.T) { // new")},
			nil},
		{"whole test file deleted",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusDeleted},
			[]ti.Rule{ti.RuleTestDeleted}},
		{"test file moved out of the test paths",
			ti.FileChange{Path: "docs/a.go", OldPath: "a_test.go", OldIsTest: true, Status: ti.StatusRenamed},
			[]ti.Rule{ti.RuleTestDeleted}},
		{"non-test file deleted is no concern of this rule",
			ti.FileChange{Path: "a.go", Status: ti.StatusDeleted, Removed: lines("func TestX(t *testing.T) {")},
			nil},
		{"assertion removed",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusModified,
				Removed: lines("\trequire.NoError(t, err)", "\tassert.Equal(t, want, got)"), Added: lines("\trequire.NoError(t, err)")},
			[]ti.Rule{ti.RuleAssertionDeleted}},
		{"assertion rewritten keeps the count",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusModified,
				Removed: lines("\tassert.Equal(t, want, got)"), Added: lines("\tassert.Equal(t, want, got2)")},
			nil},
		{"python assert removed",
			ti.FileChange{Path: "test_a.py", IsTest: true, Status: ti.StatusModified,
				Removed: lines("    assert result.refund_ref == 'r1'")},
			[]ti.Rule{ti.RuleAssertionDeleted}},
		{"js test deleted",
			ti.FileChange{Path: "a.test.ts", IsTest: true, Status: ti.StatusModified,
				Removed: lines("  it('keeps the refund ref', () => {", "    expect(a).toBe(b)", "  })")},
			[]ti.Rule{ti.RuleTestDeleted, ti.RuleAssertionDeleted}},
		{"go skip added",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusModified, Added: lines("\tt.Skip(\"flaky\")")},
			[]ti.Rule{ti.RuleSkipAdded}},
		{"go skipf added",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusModified, Added: lines("\tt.Skipf(\"no %s\", x)")},
			[]ti.Rule{ti.RuleSkipAdded}},
		{"skip that already existed and moved",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusModified,
				Added: lines("\tt.Skip(\"needs db\")"), Removed: lines("\tt.Skip(\"needs db\")")},
			nil},
		{"pytest skip added",
			ti.FileChange{Path: "test_a.py", IsTest: true, Status: ti.StatusModified, Added: lines("@pytest.mark.skip(reason=\"later\")")},
			[]ti.Rule{ti.RuleSkipAdded}},
		{"pytest skipif added",
			ti.FileChange{Path: "test_a.py", IsTest: true, Status: ti.StatusModified, Added: lines("@pytest.mark.skipif(sys.platform == 'win32', reason='x')")},
			[]ti.Rule{ti.RuleSkipAdded}},
		{"unittest skip added",
			ti.FileChange{Path: "test_a.py", IsTest: true, Status: ti.StatusModified, Added: lines("    @unittest.skip(\"later\")")},
			[]ti.Rule{ti.RuleSkipAdded}},
		{"jest it.skip added",
			ti.FileChange{Path: "a.test.js", IsTest: true, Status: ti.StatusModified, Added: lines("it.skip('x', () => {})")},
			[]ti.Rule{ti.RuleSkipAdded}},
		{"describe.skip added",
			ti.FileChange{Path: "a.test.ts", IsTest: true, Status: ti.StatusModified, Added: lines("describe.skip('x', () => {})")},
			[]ti.Rule{ti.RuleSkipAdded}},
		{"xit added",
			ti.FileChange{Path: "a.spec.ts", IsTest: true, Status: ti.StatusModified, Added: lines("xit('x', () => {})")},
			[]ti.Rule{ti.RuleSkipAdded}},
		{"java disabled added",
			ti.FileChange{Path: "ATest.java", IsTest: true, Status: ti.StatusModified, Added: lines("    @Disabled(\"later\")")},
			[]ti.Rule{ti.RuleSkipAdded}},
		{"rust ignore added",
			ti.FileChange{Path: "tests/a.rs", IsTest: true, Status: ti.StatusModified, Added: lines("#[ignore]")},
			[]ti.Rule{ti.RuleSkipAdded}},
		{"skip word in a string with no call is not a marker",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusModified, Added: lines("\tname := \"t.Skip is fine\"")},
			nil},
		{"skip call inside a string is a known false positive",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusModified, Added: lines("\tmsg := \"call t.Skip(x) to skip\"")},
			[]ti.Rule{ti.RuleSkipAdded}},
		{"skip in a non-test file is ignored",
			ti.FileChange{Path: "a.go", Status: ti.StatusModified, Added: lines("\tt.Skip(\"x\")")},
			nil},
		{"go timeout raised",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusModified,
				Removed: lines("\tctx, cancel := context.WithTimeout(ctx, 5*time.Second)"), Added: lines("\tctx, cancel := context.WithTimeout(ctx, 60*time.Second)")},
			[]ti.Rule{ti.RuleTimeoutLoosened}},
		{"go timeout lowered",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusModified,
				Removed: lines("\tctx, cancel := context.WithTimeout(ctx, 60*time.Second)"), Added: lines("\tctx, cancel := context.WithTimeout(ctx, 5*time.Second)")},
			nil},
		{"go timeout units compared",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusModified,
				Removed: lines("\ttimeout := 2 * time.Second"), Added: lines("\ttimeout := 1500 * time.Millisecond")},
			nil},
		{"jest timeout raised",
			ti.FileChange{Path: "a.test.ts", IsTest: true, Status: ti.StatusModified,
				Removed: lines("jest.setTimeout(5000)"), Added: lines("jest.setTimeout(30000)")},
			[]ti.Rule{ti.RuleTimeoutLoosened}},
		{"python timeout raised",
			ti.FileChange{Path: "test_a.py", IsTest: true, Status: ti.StatusModified,
				Removed: lines("@pytest.mark.timeout(10)"), Added: lines("@pytest.mark.timeout(120)")},
			[]ti.Rule{ti.RuleTimeoutLoosened}},
		{"new timeout is not loosened",
			ti.FileChange{Path: "test_a.py", IsTest: true, Status: ti.StatusModified, Added: lines("@pytest.mark.timeout(120)")},
			nil},
		{"flaky marker added",
			ti.FileChange{Path: "test_a.py", IsTest: true, Status: ti.StatusModified, Added: lines("@pytest.mark.flaky(reruns=3)")},
			[]ti.Rule{ti.RuleRetryLoosened}},
		{"jest retryTimes added",
			ti.FileChange{Path: "a.test.ts", IsTest: true, Status: ti.StatusModified, Added: lines("jest.retryTimes(3)")},
			[]ti.Rule{ti.RuleRetryLoosened}},
		{"retries raised",
			ti.FileChange{Path: "a.test.ts", IsTest: true, Status: ti.StatusModified,
				Removed: lines("  retries: 1,"), Added: lines("  retries: 5,")},
			[]ti.Rule{ti.RuleRetryLoosened}},
		{"retries set to zero",
			ti.FileChange{Path: "a.test.ts", IsTest: true, Status: ti.StatusModified, Added: lines("  retries: 0,")},
			nil},
		{"retry word in a comment",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusModified, Added: lines("\t// retry later when the queue is empty")},
			nil},
		{"tautology go true",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusAdded, Added: lines("\trequire.True(t, true)")},
			[]ti.Rule{ti.RuleTautology}},
		{"tautology go same value",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusAdded, Added: lines("\tassert.Equal(t, got.ID, got.ID)")},
			[]ti.Rule{ti.RuleTautology}},
		{"go equal of different values",
			ti.FileChange{Path: "a_test.go", IsTest: true, Status: ti.StatusAdded, Added: lines("\tassert.Equal(t, want, got)")},
			nil},
		{"python assert True",
			ti.FileChange{Path: "test_a.py", IsTest: true, Status: ti.StatusAdded, Added: lines("    assert True")},
			[]ti.Rule{ti.RuleTautology}},
		{"python assert same sides",
			ti.FileChange{Path: "test_a.py", IsTest: true, Status: ti.StatusAdded, Added: lines("    assert x == x")},
			[]ti.Rule{ti.RuleTautology}},
		{"python assertEqual same sides",
			ti.FileChange{Path: "test_a.py", IsTest: true, Status: ti.StatusAdded, Added: lines("        self.assertEqual(r.total, r.total)")},
			[]ti.Rule{ti.RuleTautology}},
		{"python assert of different sides",
			ti.FileChange{Path: "test_a.py", IsTest: true, Status: ti.StatusAdded, Added: lines("    assert r.total == 500")},
			nil},
		{"js expect true toBe true",
			ti.FileChange{Path: "a.test.ts", IsTest: true, Status: ti.StatusAdded, Added: lines("  expect(true).toBe(true);")},
			[]ti.Rule{ti.RuleTautology}},
		{"js expect same value",
			ti.FileChange{Path: "a.test.ts", IsTest: true, Status: ti.StatusAdded, Added: lines("  expect(result).toEqual(result);")},
			[]ti.Rule{ti.RuleTautology}},
		{"js expect different values",
			ti.FileChange{Path: "a.test.ts", IsTest: true, Status: ti.StatusAdded, Added: lines("  expect(refundAmount('c')).toBe(500);")},
			nil},
		{"rust assert true",
			ti.FileChange{Path: "tests/a.rs", IsTest: true, Status: ti.StatusAdded, Added: lines("    assert!(true);")},
			[]ti.Rule{ti.RuleTautology}},
		{"java assertEquals same",
			ti.FileChange{Path: "ATest.java", IsTest: true, Status: ti.StatusAdded, Added: lines("        assertEquals(x, x);")},
			[]ti.Rule{ti.RuleTautology}},
		{"unknown language gets no line rules",
			ti.FileChange{Path: "a_test.lua", IsTest: true, Status: ti.StatusModified, Added: lines("t.Skip('x')", "assert True")},
			nil},
		{"java test declared on the line after the marker is deleted",
			ti.FileChange{Path: "ATest.java", IsTest: true, Status: ti.StatusModified,
				Removed: lines("    @Test", "    void keepsRefundRef() {")},
			[]ti.Rule{ti.RuleTestDeleted}},
		{"rust test deleted",
			ti.FileChange{Path: "tests/a.rs", IsTest: true, Status: ti.StatusModified,
				Removed: lines("#[test]", "fn keeps_refund_ref() {")},
			[]ti.Rule{ti.RuleTestDeleted}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := ti.Evaluate(ti.Facts{OwnerKnown: true, Files: []ti.FileChange{tc.c}}, ti.Options{})
			require.ElementsMatch(t, tc.want, rulesOf(res.Violations))
			require.Empty(t, res.Blocking(), "an unpinned test only warns")
		})
	}
}
