package bench

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return b
}

// TestParseDiffOfARealFix: the real git diff -U0 of stretchr/testify#1207 (840cb80..48391ba).
func TestParseDiffOfARealFix(t *testing.T) {
	files, err := ParseDiff(readFixture(t, "testify-1207.diff"))
	require.NoError(t, err)
	want := []FileDiff{
		{Path: "mock/mock.go", Status: FileModified, Hunks: []Hunk{{OldStart: 510, OldCount: 0, NewStart: 511, NewCount: 4}}},
		{Path: "mock/mock_test.go", Status: FileModified, Hunks: []Hunk{
			{OldStart: 917, OldCount: 0, NewStart: 918, NewCount: 1},
			{OldStart: 927, OldCount: 2, NewStart: 928, NewCount: 2},
		}},
	}
	require.Empty(t, cmp.Diff(want, files))
	require.Equal(t, []string{"mock/mock_test.go"}, GoTestFiles(files))
	require.Equal(t, []FileTruth{{File: "mock/mock.go", Lines: []string{"510-511"}}}, GoTruth(files))
	require.Equal(t, []int{918, 928, 929}, AddedLines(files[1]))
}

func TestParseDiffAddedDeletedAndQuotedPaths(t *testing.T) {
	diff := `diff --git a/new_test.go b/new_test.go
new file mode 100644
index 0000000..1111111
--- /dev/null
+++ b/new_test.go
@@ -0,0 +1,3 @@
+package x
+
+func TestNew(t *testing.T) {}
diff --git a/old.go b/old.go
deleted file mode 100644
index 2222222..0000000
--- a/old.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package x
-var y = 1
diff --git a/lib/a b.go b/lib/a b.go
index 3333333..4444444 100644
--- a/lib/a b.go
+++ b/lib/a b.go
@@ -0,0 +1 @@
+// top
@@ -7 +8 @@ func f() {
-	return 1
+	return 2
diff --git a/logo.png b/logo.png
index 5555555..6666666 100644
Binary files a/logo.png and b/logo.png differ
`
	files, err := ParseDiff([]byte(diff))
	require.NoError(t, err)
	want := []FileDiff{
		{Path: "new_test.go", Status: FileAdded, Hunks: []Hunk{{OldStart: 0, OldCount: 0, NewStart: 1, NewCount: 3}}},
		{Path: "old.go", Status: FileDeleted, Hunks: []Hunk{{OldStart: 1, OldCount: 2, NewStart: 0, NewCount: 0}}},
		{Path: "lib/a b.go", Status: FileModified, Hunks: []Hunk{
			{OldStart: 0, OldCount: 0, NewStart: 1, NewCount: 1},
			{OldStart: 7, OldCount: 1, NewStart: 8, NewCount: 1},
		}},
		{Path: "logo.png", Status: FileModified},
	}
	require.Empty(t, cmp.Diff(want, files))
	require.Equal(t, []string{"new_test.go"}, GoTestFiles(files))
	// An added file has no old lines, so it is not truth. An insertion at the top is line 1.
	require.Equal(t, []FileTruth{
		{File: "old.go", Lines: []string{"1-2"}},
		{File: "lib/a b.go", Lines: []string{"1", "7"}},
	}, GoTruth(files))
	require.Equal(t, []int{1, 2, 3}, AddedLines(files[0]))
}

func TestGoTruthSkipsTestsTestdataVendorAndOtherFiles(t *testing.T) {
	files := []FileDiff{
		{Path: "a_test.go", Status: FileModified, Hunks: []Hunk{{OldStart: 3, OldCount: 1, NewStart: 3, NewCount: 1}}},
		{Path: "testdata/x.go", Status: FileModified, Hunks: []Hunk{{OldStart: 3, OldCount: 1, NewStart: 3, NewCount: 1}}},
		{Path: "vendor/y/y.go", Status: FileModified, Hunks: []Hunk{{OldStart: 3, OldCount: 1, NewStart: 3, NewCount: 1}}},
		{Path: "CHANGELOG.md", Status: FileModified, Hunks: []Hunk{{OldStart: 3, OldCount: 1, NewStart: 3, NewCount: 1}}},
		{Path: "src/z.go", Status: FileModified, Hunks: []Hunk{{OldStart: 3, OldCount: 0, NewStart: 4, NewCount: 2}}},
		{Path: "sub/testdata_helper_test.go", Status: FileDeleted, Hunks: []Hunk{{OldStart: 1, OldCount: 9}}},
	}
	require.Equal(t, []FileTruth{{File: "src/z.go", Lines: []string{"3-4"}}}, GoTruth(files))
	require.Equal(t, []string{"a_test.go"}, GoTestFiles(files), "a deleted test cannot fail before the fix")
}

func TestParseDiffRejectsBrokenHunkHeader(t *testing.T) {
	_, err := ParseDiff([]byte("diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -a +1 @@\n"))
	require.ErrorContains(t, err, "hunk header")
}

func TestCommitParents(t *testing.T) {
	raw := "tree da0d785f4719df0b83843bfd793f03471f636368\nparent " + shaD + "\nparent " + shaB +
		"\nauthor A <a@example.com> 1 +0000\ncommitter B <b@example.com> 1 +0000\n\nparent " + shaC + " in the message\n"
	require.Equal(t, []string{shaD, shaB}, CommitParents([]byte(raw)))
	require.Empty(t, CommitParents([]byte("tree x\nauthor A\n\nmsg\n")))
}
