package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The added lines keep their head line numbers across hunks; removed lines and context are read but not numbered.
func TestParseFileDiffNumbersTheAddedLines(t *testing.T) {
	d := parseFileDiff(ports.ChangedFile{Path: "a.go", Status: ports.FileModified, Additions: 2, Deletions: 1,
		Patch: "@@ -1,3 +1,3 @@\n ctx\n-old\n+new\n ctx\n@@ -20 +40,2 @@\n+x\n\\ No newline at end of file"})
	require.True(t, d.known)
	require.Equal(t, []diffLine{{line: 2, text: "new"}, {line: 40, text: "x"}}, d.added)
	require.Equal(t, []string{"old"}, d.removed)

	d = parseFileDiff(ports.ChangedFile{Path: "big.bin", Status: ports.FileModified, Additions: 9})
	require.False(t, d.known, "a change with no patch is not known")
	d = parseFileDiff(ports.ChangedFile{Path: "x", Status: ports.FileModified, Additions: 1, Patch: "@@ bad @@\n+x"})
	require.False(t, d.known, "a hunk header that does not parse is not guessed")
}

func TestTheSecretScanFindsAnAddedKeyAndIsUnknownWithoutAPatch(t *testing.T) {
	d, err := newDiffFacts(nil)
	require.NoError(t, err)
	key := "AKIA" + "Q3EGRZ7BHT5LWXMP"
	files := []ports.ChangedFile{{Path: "cfg.go", Status: ports.FileModified, Additions: 1,
		Patch: "@@ -1 +7 @@\n+const k = \"" + key + "\""}}
	got, err := d.FastFacts(context.Background(), action.FactRequest{Files: files})
	require.NoError(t, err)
	require.True(t, got.Secrets.Known)
	require.NotEmpty(t, got.Secrets.Findings)
	require.Equal(t, 7, got.Secrets.Findings[0].Line)
	for _, f := range got.Secrets.Findings {
		require.NotContains(t, f.Rule, key, "a finding never holds the value")
	}

	files = append(files, ports.ChangedFile{Path: "blob.bin", Status: ports.FileModified, Additions: 3})
	got, err = d.FastFacts(context.Background(), action.FactRequest{Files: files})
	require.NoError(t, err)
	require.False(t, got.Secrets.Known)
}

func TestPinnedTestsAreUnknownWhenARenameCouldHideAPin(t *testing.T) {
	pins := []string{"pay/**/*_test.go"}
	diffs := []fileDiff{parseFileDiff(ports.ChangedFile{Path: "pay/a_test.go", Status: ports.FileModified, Patch: "@@ -1 +1 @@\n+x"}),
		parseFileDiff(ports.ChangedFile{Path: "pay/b_test.go", Status: ports.FileRemoved})}
	got := pinnedChanges(diffs, pins)
	require.True(t, got.Known)
	require.Equal(t, []fastcheck.TestChange{{Path: "pay/a_test.go", Kind: fastcheck.TestEdited},
		{Path: "pay/b_test.go", Kind: fastcheck.TestDeleted}}, got.Changes)

	diffs = append(diffs, parseFileDiff(ports.ChangedFile{Path: "elsewhere.go", Status: ports.FileRenamed}))
	require.False(t, pinnedChanges(diffs, pins).Known)
	require.True(t, pinnedChanges(diffs, nil).Known, "no pins: nothing to hide")
}

func TestIntegrityIsUnknownWhenATestFilePatchIsMissing(t *testing.T) {
	diffs := []fileDiff{parseFileDiff(ports.ChangedFile{Path: "pay/pay_test.go", Status: ports.FileModified, Additions: 400})}
	require.False(t, integrity(diffs, policy.Tests{}).Known)
	diffs = []fileDiff{parseFileDiff(ports.ChangedFile{Path: "img.png", Status: ports.FileModified, Additions: 1})}
	require.True(t, integrity(diffs, policy.Tests{}).Known, "a non-test binary does not hide an assertion")
}

func TestIsTestPath(t *testing.T) {
	for _, p := range []string{"a/b_test.go", "t/test_x.py", "src/x.spec.ts", "FooTest.java", "tests/unit/a.rb"} {
		require.True(t, isTestPath(p, "tests"), p)
	}
	for _, p := range []string{"a/b.go", "testdata.go", "src/x.ts"} {
		require.False(t, isTestPath(p, "tests"), p)
	}
}
