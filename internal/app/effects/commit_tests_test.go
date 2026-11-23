package effects_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type fakeBlobStore struct {
	blobs map[workflow.BlobRef][]byte
}

func (f *fakeBlobStore) Put(_ context.Context, _ ports.BlobClass, r io.Reader) (workflow.BlobRef, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	ref := workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if f.blobs == nil {
		f.blobs = make(map[workflow.BlobRef][]byte)
	}
	f.blobs[ref] = b
	return ref, nil
}

func (f *fakeBlobStore) Get(_ context.Context, ref workflow.BlobRef) (io.ReadCloser, error) {
	b, ok := f.blobs[ref]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

type fakeBranchWriter struct {
	committedFiles []ports.File
	commitSHA      id.SHA
}

type fakeCommitCode struct {
	tree id.SHA
	err  error
}

func (f fakeCommitCode) TreeHash(context.Context, change.Repo, id.SHA, string) (id.SHA, error) {
	return f.tree, f.err
}
func (fakeCommitCode) ReadFile(context.Context, change.Repo, id.SHA, string) ([]byte, error) {
	return nil, ports.ErrNotFound
}
func (fakeCommitCode) Tarball(context.Context, change.Repo, id.SHA) (io.ReadCloser, error) {
	return nil, ports.ErrNotFound
}

func (f *fakeBranchWriter) EnsureBranch(_ context.Context, _ change.Repo, _ id.SHA, _ string, _ string) error {
	return nil
}

func (f *fakeBranchWriter) CommitFiles(_ context.Context, _ change.Repo, _, _ string, files []ports.File) (id.SHA, error) {
	f.committedFiles = files
	return f.commitSHA, nil
}

const ctBlob = workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

// commitKit wires the executor over a stored submission bundle; commitPath is the file the effect asks to commit.
type commitKit struct {
	exec     effects.CommitTests
	branches *fakeBranchWriter
	ingest   *fakeIngest
}

func newCommitKit(t *testing.T, bundle testset.Submission, tree id.SHA) *commitKit {
	t.Helper()
	raw, err := json.Marshal(bundle)
	require.NoError(t, err)
	k := &commitKit{
		branches: &fakeBranchWriter{commitSHA: id.SHA("0123456789abcdef0123456789abcdef01234567")},
		ingest:   &fakeIngest{},
	}
	k.exec, err = effects.NewCommitTests(effects.CommitTestsDeps{
		Blobs:    &fakeBlobStore{blobs: map[workflow.BlobRef][]byte{ctBlob: raw}},
		Branches: k.branches, Ingest: k.ingest, IDs: &fakeIDs{}, Clock: fixedClock{}, Code: fakeCommitCode{tree: tree},
	})
	require.NoError(t, err)
	return k
}

func (k *commitKit) run(t *testing.T, path string) error {
	t.Helper()
	raw, err := json.Marshal(change.CommitTestsPayload{
		Repo: "acme/pay-demo", Branch: "PAY-142-refunds", Dir: "tests/heimdall/PAY-142",
		Files: []change.CommitFile{{Path: path, BlobRef: ctBlob}}, Message: "tests: add tests",
	})
	require.NoError(t, err)
	return k.exec.Execute(context.Background(), change.Effect{
		Key: "github_commit_tests:PAY-142:1:commit:treehash", Kind: change.EffectGitHubCommitTests, Target: "acme/pay-demo", Payload: raw,
	})
}

// The blob a commit file names is the stored submission, as the engine writes it. The executor commits the file's own
// content, not the bundle (live KAN-13 committed the whole JSON as refund_test.go).
func TestCommitTestsCommitsTheSubmittedFileAndIngestsTestsCommitted(t *testing.T) {
	t.Parallel()
	treeHash := id.SHA("abcdef0123456789abcdef0123456789abcdef01")
	k := newCommitKit(t, testset.Submission{Files: []testset.File{
		{Path: "tests/heimdall/PAY-142/refund_test.go", Content: "package test\n"},
		{Path: "tests/heimdall/PAY-142/other_test.go", Content: "package other\n"},
	}}, treeHash)

	require.NoError(t, k.run(t, "tests/heimdall/PAY-142/refund_test.go"))
	require.Len(t, k.branches.committedFiles, 1)
	require.Equal(t, "tests/heimdall/PAY-142/refund_test.go", k.branches.committedFiles[0].Path)
	require.Equal(t, []byte("package test\n"), k.branches.committedFiles[0].Content)

	require.Len(t, k.ingest.written, 1)
	require.Equal(t, change.EventTestsCommitted, k.ingest.written[0].Type)
	var committed change.TestsCommitted
	require.NoError(t, json.Unmarshal(k.ingest.written[0].Payload, &committed))
	require.Equal(t, k.branches.commitSHA, committed.SHA)
	require.Equal(t, treeHash, committed.TreeHash)
}

func TestCommitTestsRefusesWhatItCannotPinOrDoesNotHold(t *testing.T) {
	t.Parallel()
	bundle := testset.Submission{Files: []testset.File{{Path: "tests/heimdall/PAY-142/a_test.go", Content: "package a\n"}}}
	cases := map[string]struct {
		tree     id.SHA
		path     string
		want     string
		noCommit bool
	}{
		"unpinned tree hash":                {"", "tests/heimdall/PAY-142/a_test.go", "pin tests tree", false},
		"file the submission does not hold": {"abcdef0123456789abcdef0123456789abcdef01", "tests/heimdall/PAY-142/missing_test.go", "has no file", true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			k := newCommitKit(t, bundle, c.tree)
			require.ErrorContains(t, k.run(t, c.path), c.want)
			require.Empty(t, k.ingest.written)
			if c.noCommit {
				require.Empty(t, k.branches.committedFiles, "nothing but the submitted test source is ever committed")
			}
		})
	}
}
