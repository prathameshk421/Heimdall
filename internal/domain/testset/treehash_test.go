package testset_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
)

// The expected value comes from git itself: two files committed under tests/heimdall/KAN-13, then
// `git rev-parse HEAD:tests/heimdall/KAN-13`. The gate compares this hash with the committed tree, so the two must be
// the same bytes.
func TestTreeHashMatchesGit(t *testing.T) {
	sub := testset.Submission{Files: []testset.File{
		{Path: "tests/heimdall/KAN-13/internal/app/a_test.go", Content: "package app\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n"},
		{Path: "tests/heimdall/KAN-13/b_test.go", Content: "package x\n"},
	}}
	got, err := sub.TreeHash("tests/heimdall/KAN-13")
	require.NoError(t, err)
	require.Equal(t, "87ca194b16548db8e80f2e3909cfa03030c2e456", got)

	// The order of the files in the submission does not change the hash.
	sub.Files[0], sub.Files[1] = sub.Files[1], sub.Files[0]
	again, err := sub.TreeHash("tests/heimdall/KAN-13")
	require.NoError(t, err)
	require.Equal(t, got, again)
}

func TestTreeHashRefusesAFileOutsideTheDirectory(t *testing.T) {
	sub := testset.Submission{Files: []testset.File{{Path: "tests/heimdall/KAN-99/a_test.go", Content: "package x\n"}}}
	_, err := sub.TreeHash("tests/heimdall/KAN-13")
	require.ErrorContains(t, err, "outside")
	_, err = testset.Submission{}.TreeHash("tests/heimdall/KAN-13")
	require.ErrorContains(t, err, "no files")
}
