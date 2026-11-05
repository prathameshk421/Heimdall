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

func (f *fakeBranchWriter) EnsureBranch(_ context.Context, _ change.Repo, _ id.SHA, _ string) error {
	return nil
}

func (f *fakeBranchWriter) CommitFiles(_ context.Context, _ change.Repo, _, _ string, files []ports.File) (id.SHA, error) {
	f.committedFiles = files
	return f.commitSHA, nil
}

func TestCommitTestsExecutor(t *testing.T) {
	ctx := context.Background()
	blobRef := workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	blobContent := []byte("package test\n")
	commitSHA := id.SHA("0123456789abcdef0123456789abcdef01234567")

	blobs := &fakeBlobStore{blobs: map[workflow.BlobRef][]byte{blobRef: blobContent}}
	branches := &fakeBranchWriter{commitSHA: commitSHA}
	ingest := &fakeIngest{}
	ids := &fakeIDs{}
	clock := fixedClock{}

	store := &fakeStore{
		events: []change.Event{
			{
				ID:            "ev-1",
				ChangeID:      "PAY-142",
				Type:          change.EventTestsVerified,
				CorrelationID: "corr-1",
				Payload:       json.RawMessage(`{"run_ids":["run_01ARZ3NDEKTSV4RRFFQ69G5FAV"],"sha":"0123456789abcdef0123456789abcdef01234567","tree_hash":"0123456789abcdef0123456789abcdef01234567"}`),
			},
		},
	}

	exec, err := effects.NewCommitTests(effects.CommitTestsDeps{
		Blobs:    blobs,
		Branches: branches,
		Store:    store,
		Ingest:   ingest,
		IDs:      ids,
		Clock:    clock,
	})
	require.NoError(t, err)

	payload := change.CommitTestsPayload{
		Repo:    change.Repo("acme/pay-demo"),
		Branch:  "PAY-142-refunds",
		Dir:     "tests/heimdall/PAY-142",
		Files:   []change.CommitFile{{Path: "tests/heimdall/PAY-142/refund_test.go", BlobRef: blobRef}},
		Message: "tests: add tests",
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	fx := change.Effect{
		Key:     "github_commit_tests:PAY-142:1:commit:treehash",
		Kind:    change.EffectGitHubCommitTests,
		Target:  "acme/pay-demo",
		Payload: raw,
	}

	err = exec.Execute(ctx, fx)
	require.NoError(t, err)

	// Check that branch writer committed the files
	require.Len(t, branches.committedFiles, 1)
	require.Equal(t, "tests/heimdall/PAY-142/refund_test.go", branches.committedFiles[0].Path)
	require.Equal(t, blobContent, branches.committedFiles[0].Content)

	// Check that tests.committed was ingested
	require.Len(t, ingest.written, 1)
	require.Equal(t, change.EventTestsCommitted, ingest.written[0].Type)

	var committedPayload change.TestsCommitted
	require.NoError(t, json.Unmarshal(ingest.written[0].Payload, &committedPayload))
	require.Equal(t, commitSHA, committedPayload.SHA)
}

func TestCommitTestsExecutorSkipsWhenAlreadyCommitted(t *testing.T) {
	ctx := context.Background()
	blobRef := workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	blobs := &fakeBlobStore{blobs: map[workflow.BlobRef][]byte{blobRef: []byte("content")}}
	branches := &fakeBranchWriter{}
	ingest := &fakeIngest{}

	store := &fakeStore{
		events: []change.Event{
			{ID: "ev-1", Type: change.EventTestsCommitted},
		},
	}

	exec, err := effects.NewCommitTests(effects.CommitTestsDeps{
		Blobs:    blobs,
		Branches: branches,
		Store:    store,
		Ingest:   ingest,
		IDs:      &fakeIDs{},
		Clock:    fixedClock{},
	})
	require.NoError(t, err)

	payload := change.CommitTestsPayload{
		Repo:    change.Repo("acme/pay-demo"),
		Branch:  "PAY-142-refunds",
		Dir:     "tests/heimdall/PAY-142",
		Files:   []change.CommitFile{{Path: "tests/heimdall/PAY-142/refund_test.go", BlobRef: blobRef}},
		Message: "tests: add tests",
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	fx := change.Effect{
		Key:     "github_commit_tests:PAY-142:1:commit:treehash",
		Kind:    change.EffectGitHubCommitTests,
		Target:  "acme/pay-demo",
		Payload: raw,
	}

	err = exec.Execute(ctx, fx)
	require.NoError(t, err)
	// Skipped: no commit, no ingest
	require.Empty(t, branches.committedFiles)
	require.Empty(t, ingest.written)
}
