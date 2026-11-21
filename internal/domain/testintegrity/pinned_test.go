package testintegrity_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	ti "github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testintegrity"
)

const pinnedFile = "tests/heimdall/PAY-142/refund_test.go"

func TestPinned(t *testing.T) {
	whole := []ti.Pinned{{Path: pinnedFile}}
	funcs := []ti.Pinned{{Path: pinnedFile, Tests: []string{"TestRefundKeepsRef"}}}
	edit := ti.FileChange{Path: pinnedFile, IsTest: true, Status: ti.StatusModified,
		Removed: lines("\tt.Fatal(\"missing\")"), Added: lines("\tt.Log(\"ok\")")}
	tests := []struct {
		name   string
		pins   []ti.Pinned
		change ti.FileChange
		want   []ti.Rule
	}{
		{"edit of a pinned file", whole, edit, []ti.Rule{ti.RulePinnedEdited, ti.RuleAssertionDeleted}},
		{"mode or whitespace change still reported by status", whole,
			ti.FileChange{Path: pinnedFile, IsTest: true, Status: ti.StatusModified}, []ti.Rule{ti.RulePinnedEdited}},
		{"delete of a pinned file", whole,
			ti.FileChange{Path: pinnedFile, IsTest: true, Status: ti.StatusDeleted}, []ti.Rule{ti.RulePinnedDeleted, ti.RuleTestDeleted}},
		{"rename of a pinned file", whole,
			ti.FileChange{Path: "tests/other_test.go", OldPath: pinnedFile, IsTest: true, OldIsTest: true, Status: ti.StatusRenamed},
			[]ti.Rule{ti.RulePinnedDeleted}},
		{"pinned file is not in the change", whole,
			ti.FileChange{Path: "internal/pay/refund.go", Status: ti.StatusModified, Added: lines("x")}, nil},
		{"pinned file added by this change is not an edit", whole,
			ti.FileChange{Path: pinnedFile, IsTest: true, Status: ti.StatusAdded, Added: lines("func TestRefundKeepsRef(t *testing.T) {")}, nil},
		{"pinned function body edited by name", funcs,
			ti.FileChange{Path: pinnedFile, IsTest: true, Status: ti.StatusModified, EditedTests: lines("TestRefundKeepsRef"), Added: lines("x")},
			[]ti.Rule{ti.RulePinnedEdited}},
		{"pinned function deleted", funcs,
			ti.FileChange{Path: pinnedFile, IsTest: true, Status: ti.StatusModified,
				Removed: lines("func TestRefundKeepsRef(t *testing.T) {", "}")},
			[]ti.Rule{ti.RulePinnedDeleted, ti.RuleTestDeleted}},
		{"pinned function signature edited", funcs,
			ti.FileChange{Path: pinnedFile, IsTest: true, Status: ti.StatusModified,
				Removed: lines("func TestRefundKeepsRef(t *testing.T) {"), Added: lines("func TestRefundKeepsRef(t *testing.T) { // x")},
			[]ti.Rule{ti.RulePinnedEdited}},
		{"another function of the same file is not pinned", funcs,
			ti.FileChange{Path: pinnedFile, IsTest: true, Status: ti.StatusModified, EditedTests: lines("TestOther"), Added: lines("x")},
			nil},
		{"pinned function in a deleted file", funcs,
			ti.FileChange{Path: pinnedFile, IsTest: true, Status: ti.StatusDeleted},
			[]ti.Rule{ti.RulePinnedDeleted, ti.RuleTestDeleted}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := ti.Facts{OwnerKnown: true, Pinned: tc.pins, Files: []ti.FileChange{tc.change}}
			require.ElementsMatch(t, tc.want, rulesOf(ti.Evaluate(f, ti.Options{}).Violations))
		})
	}
}

func TestPinnedBlocksAndOwnerApprovalClearsIt(t *testing.T) {
	edit := ti.FileChange{Path: pinnedFile, IsTest: true, Status: ti.StatusModified,
		Removed: lines("\tt.Fatal(\"missing\")"), Added: lines("\tt.Skip(\"later\")")}
	base := ti.Facts{Pinned: []ti.Pinned{{Path: pinnedFile}}, Files: []ti.FileChange{edit}}

	t.Run("no owner approval blocks the edit and the weakening", func(t *testing.T) {
		f := base
		f.OwnerKnown = true
		res := ti.Evaluate(f, ti.Options{})
		require.ElementsMatch(t, []ti.Rule{ti.RulePinnedEdited, ti.RuleSkipAdded, ti.RuleAssertionDeleted}, rulesOf(res.Blocking()))
		require.Empty(t, res.Warnings())
	})
	t.Run("owner approval on head clears the pinned rule and weakening falls back to a warning", func(t *testing.T) {
		f := base
		f.OwnerKnown, f.OwnerApproved = true, true
		res := ti.Evaluate(f, ti.Options{})
		require.Empty(t, res.Blocking())
		require.ElementsMatch(t, []ti.Rule{ti.RuleSkipAdded, ti.RuleAssertionDeleted}, rulesOf(res.Warnings()))
	})
	t.Run("approval that is not known keeps the block and says so", func(t *testing.T) {
		f := base
		f.OwnerKnown, f.OwnerApproved = false, true // a true flag with an unknown fact must not clear anything
		res := ti.Evaluate(f, ti.Options{})
		require.Contains(t, rulesOf(res.Blocking()), ti.RulePinnedEdited)
		for _, v := range res.Blocking() {
			if v.Rule == ti.RulePinnedEdited {
				require.Contains(t, v.Message, "not known whether a policy owner approved")
			}
		}
	})
	t.Run("weakening a file that only has pinned functions warns", func(t *testing.T) {
		f := ti.Facts{OwnerKnown: true, Pinned: []ti.Pinned{{Path: pinnedFile, Tests: []string{"TestRefundKeepsRef"}}},
			Files: []ti.FileChange{{Path: pinnedFile, IsTest: true, Status: ti.StatusModified, Added: lines("\tt.Skip(\"x\")")}}}
		res := ti.Evaluate(f, ti.Options{})
		require.Empty(t, res.Blocking())
		require.Equal(t, []ti.Rule{ti.RuleSkipAdded}, rulesOf(res.Warnings()))
	})
}

func TestEvaluateIsStableAndHasStableIDs(t *testing.T) {
	f := loadSample(t, "py_mock_subject")
	first := ti.Evaluate(f, ti.Options{})
	second := ti.Evaluate(f, ti.Options{})
	require.Equal(t, first, second)

	var ids []string
	for _, v := range first.Violations {
		ids = append(ids, v.ID)
	}
	require.Equal(t, []string{
		"mocks-changed-file:tests/billing/test_refund.py:shop.billing.refund.compute_refund",
		"tautological-assertion:tests/billing/test_refund.py",
	}, ids)

	// The same fact twice gives one violation.
	f.Tests = append(f.Tests, f.Tests[0])
	require.Len(t, ti.Evaluate(f, ti.Options{}).Violations, 2)
}
