package effects_test

import (
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

type fakeSource struct {
	fetchedRepo change.Repo
	fetchedSHA  id.SHA
	err         error
}

type fakeTestQualityCode struct {
	repo change.Repo
	sha  id.SHA
	dir  string
	tree id.SHA
	err  error
}

func (f *fakeTestQualityCode) TreeHash(_ context.Context, repo change.Repo, sha id.SHA, dir string) (id.SHA, error) {
	f.repo, f.sha, f.dir = repo, sha, dir
	return f.tree, f.err
}
func (*fakeTestQualityCode) ReadFile(context.Context, change.Repo, id.SHA, string) ([]byte, error) {
	return nil, ports.ErrNotFound
}
func (*fakeTestQualityCode) Tarball(context.Context, change.Repo, id.SHA) (io.ReadCloser, error) {
	return nil, ports.ErrNotFound
}

func (f *fakeSource) Fetch(_ context.Context, repo change.Repo, sha id.SHA) error {
	f.fetchedRepo = repo
	f.fetchedSHA = sha
	return f.err
}

type fakeSandbox struct {
	runs     []ports.SandboxJob
	overlays [][]ports.File
	results  []ports.SandboxResult
	idx      int
}

func (f *fakeSandbox) RunOverlay(ctx context.Context, j ports.SandboxJob, overlays []ports.File) (ports.SandboxResult, error) {
	f.overlays = append(f.overlays, overlays)
	return f.Run(ctx, j)
}

func (f *fakeSandbox) Run(_ context.Context, j ports.SandboxJob) (ports.SandboxResult, error) {
	f.runs = append(f.runs, j)
	if f.idx < len(f.results) {
		res := f.results[f.idx]
		f.idx++
		return res, nil
	}
	return ports.SandboxResult{
		Failed: 1,
		Tests: []ports.TestResult{
			{Name: "TestA", Outcome: ports.OutcomeFailure},
		},
	}, nil
}

func TestTestQualityExecutorSuccess(t *testing.T) {
	ctx := context.Background()
	blobRef := workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	baseSHA := id.SHA("0123456789abcdef0123456789abcdef01234567")

	sub := testset.Submission{
		Files: []testset.File{
			{Path: "tests/heimdall/PAY-142/refund_test.go", Content: "package refund_test\n"},
		},
		AcceptanceCriteria: map[string][]string{
			"AC1": {"TestA"},
		},
	}
	subBytes, err := json.Marshal(sub)
	require.NoError(t, err)

	blobs := &fakeBlobStore{blobs: map[workflow.BlobRef][]byte{blobRef: subBytes}}
	source := &fakeSource{}
	code := &fakeTestQualityCode{tree: id.SHA("abcdef0123456789abcdef0123456789abcdef01")}
	sandbox := &fakeSandbox{
		results: []ports.SandboxResult{
			// 3 flake runs on base: all fail on TestA (stable failure on base)
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
		},
	}
	ingest := &fakeIngest{}
	ids := &fakeIDs{}
	clock := fixedClock{}

	store := &fakeStore{
		events: []change.Event{
			{
				ID:       "ev-1",
				ChangeID: "PAY-142",
				Type:     change.EventChangeTriggered,
				Payload:  json.RawMessage(`{"ticket":"PAY-142","repo":"acme/pay-demo","assignee":"ana","source":"jira"}`),
			},
			{
				ID:       "ev-2",
				ChangeID: "PAY-142",
				Type:     change.EventBranchEnsured,
				Payload:  json.RawMessage(`{"repo":"acme/pay-demo","branch":"PAY-142-refunds","base_sha":"0123456789abcdef0123456789abcdef01234567"}`),
			},
			{
				ID:       "ev-3",
				ChangeID: "PAY-142",
				Type:     change.EventTestsSubmitted,
				BlobRefs: []workflow.BlobRef{blobRef},
				Payload:  json.RawMessage(`{"refs":["tests/heimdall/PAY-142/refund_test.go"],"ac_map":{"AC1":["TestA"]},"sha":"0123456789abcdef0123456789abcdef01234567","tree_hash":"0123456789abcdef0123456789abcdef01234567"}`),
			},
		},
	}

	exec, err := effects.NewTestQuality(effects.TestQualityDeps{
		Blobs:   blobs,
		Source:  source,
		Sandbox: sandbox,
		Store:   store,
		Ingest:  ingest,
		IDs:     ids,
		Clock:   clock,
		Code:    code,
	})
	require.NoError(t, err)

	payload := change.TestQualityPayload{
		Repo:          change.Repo("acme/pay-demo"),
		Branch:        "PAY-142-refunds",
		Ticket:        id.ChangeID("PAY-142"),
		SubmissionRef: blobRef,
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	fx := change.Effect{
		Key:     "test_quality:PAY-142:1:test_qa",
		Kind:    change.EffectTestQuality,
		Target:  "PAY-142",
		Payload: raw,
	}

	err = exec.Execute(ctx, fx)
	require.NoError(t, err)
	require.Len(t, sandbox.overlays, 3, "each flake run evaluates the submitted test overlay")
	for _, overlay := range sandbox.overlays {
		require.Equal(t, "tests/heimdall/PAY-142/refund_test.go", overlay[0].Path)
		require.Equal(t, "package refund_test\n", string(overlay[0].Content))
	}

	// Source was fetched
	require.Equal(t, change.Repo("acme/pay-demo"), source.fetchedRepo)
	require.Equal(t, baseSHA, source.fetchedSHA)

	// Ingested tests.verified
	require.Len(t, ingest.written, 1)
	require.Equal(t, change.EventTestsVerified, ingest.written[0].Type)

	var verified change.TestsVerified
	require.NoError(t, json.Unmarshal(ingest.written[0].Payload, &verified))
	require.Equal(t, baseSHA, verified.SHA)
	require.Equal(t, code.tree, verified.TreeHash)
	require.Equal(t, change.Repo("acme/pay-demo"), code.repo)
	require.Equal(t, "tests/heimdall/PAY-142", code.dir)
	require.Len(t, verified.RunIDs, 3)
}

func TestTestQualityExecutorWithHeadPassesRedFirst(t *testing.T) {
	ctx := context.Background()
	blobRef := workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	baseSHA := id.SHA("0123456789abcdef0123456789abcdef01234567")
	headSHA := id.SHA("fedcba9876543210fedcba9876543210fedcba98")

	sub := testset.Submission{
		Files: []testset.File{
			{Path: "tests/heimdall/PAY-142/refund_test.go", Content: "package refund_test\n"},
		},
		AcceptanceCriteria: map[string][]string{"AC1": {"TestA"}},
	}
	subBytes, err := json.Marshal(sub)
	require.NoError(t, err)

	blobs := &fakeBlobStore{blobs: map[workflow.BlobRef][]byte{blobRef: subBytes}}
	source := &fakeSource{}
	code := &fakeTestQualityCode{tree: id.SHA("abcdef0123456789abcdef0123456789abcdef01")}
	sandbox := &fakeSandbox{
		results: []ports.SandboxResult{
			// 3 runs on base: fail
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
			// 1 run on head: pass
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomePassed}}},
		},
	}
	ingest := &fakeIngest{}

	store := &fakeStore{
		events: []change.Event{
			{
				ID:       "ev-1",
				ChangeID: "PAY-142",
				Type:     change.EventBranchEnsured,
				Payload:  json.RawMessage(`{"repo":"acme/pay-demo","branch":"PAY-142-refunds","base_sha":"` + string(baseSHA) + `"}`),
			},
			{
				ID:       "ev-2",
				ChangeID: "PAY-142",
				Type:     change.EventPROpened,
				Payload:  json.RawMessage(`{"repo":"acme/pay-demo","number":42,"head_sha":"` + string(headSHA) + `","author":"ana","ticket":"PAY-142"}`),
			},
			{
				ID:       "ev-3",
				ChangeID: "PAY-142",
				Type:     change.EventTestsSubmitted,
				BlobRefs: []workflow.BlobRef{blobRef},
				Payload:  json.RawMessage(`{"refs":["tests/heimdall/PAY-142/refund_test.go"],"ac_map":{"AC1":["TestA"]},"sha":"` + string(baseSHA) + `","tree_hash":"` + string(baseSHA) + `"}`),
			},
		},
	}

	exec, err := effects.NewTestQuality(effects.TestQualityDeps{
		Blobs:   blobs,
		Source:  source,
		Sandbox: sandbox,
		Store:   store,
		Ingest:  ingest,
		IDs:     &fakeIDs{},
		Clock:   fixedClock{},
		Code:    code,
	})
	require.NoError(t, err)

	fx := change.Effect{
		Key:     "test_quality:PAY-142:1:test_qa",
		Kind:    change.EffectTestQuality,
		Target:  "PAY-142",
		Payload: json.RawMessage(`{"repo":"acme/pay-demo","branch":"PAY-142-refunds","ticket":"PAY-142","submission_ref":"` + string(blobRef) + `"}`),
	}

	err = exec.Execute(ctx, fx)
	require.NoError(t, err)
	require.Len(t, sandbox.overlays, 4, "base and head checks both evaluate the submitted tests")
	require.Equal(t, change.Repo("acme/pay-demo"), code.repo)
	require.Equal(t, "tests/heimdall/PAY-142", code.dir)

	require.Len(t, ingest.written, 1)
	require.Equal(t, change.EventTestsVerified, ingest.written[0].Type)
	require.Len(t, sandbox.runs, 4) // 3 base + 1 head
}

func TestTestQualityRefusesVerificationWithoutPinnedTreeHash(t *testing.T) {
	ctx := context.Background()
	blobRef := workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	baseSHA := id.SHA("0123456789abcdef0123456789abcdef01234567")
	sub := testset.Submission{
		Files:              []testset.File{{Path: "tests/heimdall/PAY-142/refund_test.go", Content: "package refund_test\\n"}},
		AcceptanceCriteria: map[string][]string{"AC1": {"TestA"}},
	}
	subBytes, err := json.Marshal(sub)
	require.NoError(t, err)
	blobs := &fakeBlobStore{blobs: map[workflow.BlobRef][]byte{blobRef: subBytes}}
	sandbox := &fakeSandbox{results: []ports.SandboxResult{
		{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
		{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
		{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
	}}
	store := &fakeStore{events: []change.Event{
		{ID: "ev-1", ChangeID: "PAY-142", Type: change.EventBranchEnsured, Payload: json.RawMessage(`{"repo":"acme/pay-demo","branch":"PAY-142-refunds","base_sha":"` + string(baseSHA) + `"}`)},
		{ID: "ev-2", ChangeID: "PAY-142", Type: change.EventTestsSubmitted, BlobRefs: []workflow.BlobRef{blobRef}, Payload: json.RawMessage(`{"refs":["tests/heimdall/PAY-142/refund_test.go"],"ac_map":{"AC1":["TestA"]},"sha":"` + string(baseSHA) + `","tree_hash":"` + string(baseSHA) + `"}`)},
	}}
	ingest := &fakeIngest{}
	exec, err := effects.NewTestQuality(effects.TestQualityDeps{
		Blobs: blobs, Source: &fakeSource{}, Sandbox: sandbox, Store: store, Ingest: ingest,
		IDs: &fakeIDs{}, Clock: fixedClock{}, Code: &fakeTestQualityCode{},
	})
	require.NoError(t, err)
	err = exec.Execute(ctx, change.Effect{
		Key: "test_quality:PAY-142:1:test_qa", Kind: change.EffectTestQuality, Target: "PAY-142",
		Payload: json.RawMessage(`{"repo":"acme/pay-demo","branch":"PAY-142-refunds","ticket":"PAY-142","submission_ref":"` + string(blobRef) + `"}`),
	})
	require.ErrorContains(t, err, "empty tests tree hash")
	require.Empty(t, ingest.written, "verification must not be recorded without a content tree hash")
}

func TestTestQualityExecutorWithHeadRejectsFailingRedFirst(t *testing.T) {
	ctx := context.Background()
	blobRef := workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	baseSHA := id.SHA("0123456789abcdef0123456789abcdef01234567")
	headSHA := id.SHA("fedcba9876543210fedcba9876543210fedcba98")

	sub := testset.Submission{
		Files: []testset.File{
			{Path: "tests/heimdall/PAY-142/refund_test.go", Content: "package refund_test\n"},
		},
		AcceptanceCriteria: map[string][]string{"AC1": {"TestA"}},
	}
	subBytes, err := json.Marshal(sub)
	require.NoError(t, err)

	blobs := &fakeBlobStore{blobs: map[workflow.BlobRef][]byte{blobRef: subBytes}}
	source := &fakeSource{}
	sandbox := &fakeSandbox{
		results: []ports.SandboxResult{
			// 3 runs on base: fail
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
			// 1 run on head: STILL FAILS! (Does not pass with feature)
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
		},
	}
	ingest := &fakeIngest{}

	store := &fakeStore{
		events: []change.Event{
			{
				ID:       "ev-1",
				ChangeID: "PAY-142",
				Type:     change.EventBranchEnsured,
				Payload:  json.RawMessage(`{"repo":"acme/pay-demo","branch":"PAY-142-refunds","base_sha":"` + string(baseSHA) + `"}`),
			},
			{
				ID:       "ev-2",
				ChangeID: "PAY-142",
				Type:     change.EventPROpened,
				Payload:  json.RawMessage(`{"repo":"acme/pay-demo","number":42,"head_sha":"` + string(headSHA) + `","author":"ana","ticket":"PAY-142"}`),
			},
			{
				ID:       "ev-3",
				ChangeID: "PAY-142",
				Type:     change.EventTestsSubmitted,
				BlobRefs: []workflow.BlobRef{blobRef},
				Payload:  json.RawMessage(`{"refs":["tests/heimdall/PAY-142/refund_test.go"],"ac_map":{"AC1":["TestA"]},"sha":"` + string(baseSHA) + `","tree_hash":"` + string(baseSHA) + `"}`),
			},
		},
	}

	exec, err := effects.NewTestQuality(effects.TestQualityDeps{
		Blobs:   blobs,
		Source:  source,
		Sandbox: sandbox,
		Store:   store,
		Ingest:  ingest,
		IDs:     &fakeIDs{},
		Clock:   fixedClock{},
	})
	require.NoError(t, err)

	fx := change.Effect{
		Key:     "test_quality:PAY-142:1:test_qa",
		Kind:    change.EffectTestQuality,
		Target:  "PAY-142",
		Payload: json.RawMessage(`{"repo":"acme/pay-demo","branch":"PAY-142-refunds","ticket":"PAY-142","submission_ref":"` + string(blobRef) + `"}`),
	}

	err = exec.Execute(ctx, fx)
	require.NoError(t, err)

	require.Len(t, ingest.written, 1)
	require.Equal(t, change.EventTestsRejected, ingest.written[0].Type)

	var rejected change.TestsRejected
	require.NoError(t, json.Unmarshal(ingest.written[0].Payload, &rejected))
	require.NotEmpty(t, rejected.Reasons)
	require.Contains(t, rejected.Reasons[0], "does not pass with the feature")
}

func TestTestQualityExecutorRejectionOnFlake(t *testing.T) {
	ctx := context.Background()
	blobRef := workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	sub := testset.Submission{
		Files: []testset.File{
			{Path: "tests/heimdall/PAY-142/refund_test.go", Content: "package refund_test\n"},
		},
		AcceptanceCriteria: map[string][]string{"AC1": {"TestA"}},
	}
	subBytes, err := json.Marshal(sub)
	require.NoError(t, err)

	blobs := &fakeBlobStore{blobs: map[workflow.BlobRef][]byte{blobRef: subBytes}}
	source := &fakeSource{}
	sandbox := &fakeSandbox{
		results: []ports.SandboxResult{
			// Flaky: fail, pass, fail
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomePassed}}},
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
		},
	}
	ingest := &fakeIngest{}
	ids := &fakeIDs{}
	clock := fixedClock{}

	store := &fakeStore{
		events: []change.Event{
			{
				ID:       "ev-1",
				ChangeID: "PAY-142",
				Type:     change.EventBranchEnsured,
				Payload:  json.RawMessage(`{"repo":"acme/pay-demo","branch":"PAY-142-refunds","base_sha":"0123456789abcdef0123456789abcdef01234567"}`),
			},
			{
				ID:       "ev-2",
				ChangeID: "PAY-142",
				Type:     change.EventTestsSubmitted,
				BlobRefs: []workflow.BlobRef{blobRef},
				Payload:  json.RawMessage(`{"refs":["tests/heimdall/PAY-142/refund_test.go"],"ac_map":{"AC1":["TestA"]},"sha":"0123456789abcdef0123456789abcdef01234567","tree_hash":"0123456789abcdef0123456789abcdef01234567"}`),
			},
		},
	}

	exec, err := effects.NewTestQuality(effects.TestQualityDeps{
		Blobs:   blobs,
		Source:  source,
		Sandbox: sandbox,
		Store:   store,
		Ingest:  ingest,
		IDs:     ids,
		Clock:   clock,
	})
	require.NoError(t, err)

	payload := change.TestQualityPayload{
		Repo:          change.Repo("acme/pay-demo"),
		Branch:        "PAY-142-refunds",
		Ticket:        id.ChangeID("PAY-142"),
		SubmissionRef: blobRef,
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	fx := change.Effect{
		Key:     "test_quality:PAY-142:1:test_qa",
		Kind:    change.EffectTestQuality,
		Target:  "PAY-142",
		Payload: raw,
	}

	err = exec.Execute(ctx, fx)
	require.NoError(t, err)

	// Ingested tests.rejected
	require.Len(t, ingest.written, 1)
	require.Equal(t, change.EventTestsRejected, ingest.written[0].Type)

	var rejected change.TestsRejected
	require.NoError(t, json.Unmarshal(ingest.written[0].Payload, &rejected))
	require.NotEmpty(t, rejected.Reasons)
	require.Contains(t, rejected.Reasons[0], "flaky")
}

func TestTestQualityExecutorRejectionOnSeamImports(t *testing.T) {
	ctx := context.Background()
	blobRef := workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	sub := testset.Submission{
		Files: []testset.File{
			{
				Path:    "tests/heimdall/PAY-142/refund_test.go",
				Content: "package refund_test\nimport \"acme/internal/secret\"\n",
			},
		},
		AcceptanceCriteria: map[string][]string{"AC1": {"TestA"}},
	}
	subBytes, err := json.Marshal(sub)
	require.NoError(t, err)

	blobs := &fakeBlobStore{blobs: map[workflow.BlobRef][]byte{blobRef: subBytes}}
	source := &fakeSource{}
	sandbox := &fakeSandbox{
		results: []ports.SandboxResult{
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}},
		},
	}
	ingest := &fakeIngest{}

	store := &fakeStore{
		events: []change.Event{
			{
				ID:       "ev-1",
				ChangeID: "PAY-142",
				Type:     change.EventBranchEnsured,
				Payload:  json.RawMessage(`{"repo":"acme/pay-demo","branch":"PAY-142-refunds","base_sha":"0123456789abcdef0123456789abcdef01234567"}`),
			},
			{
				ID:       "ev-2",
				ChangeID: "PAY-142",
				Type:     change.EventTestsSubmitted,
				BlobRefs: []workflow.BlobRef{blobRef},
				Payload:  json.RawMessage(`{"refs":["tests/heimdall/PAY-142/refund_test.go"],"ac_map":{"AC1":["TestA"]},"sha":"0123456789abcdef0123456789abcdef01234567","tree_hash":"0123456789abcdef0123456789abcdef01234567"}`),
			},
		},
	}

	exec, err := effects.NewTestQuality(effects.TestQualityDeps{
		Blobs:            blobs,
		Source:           source,
		Sandbox:          sandbox,
		Store:            store,
		Ingest:           ingest,
		IDs:              &fakeIDs{},
		Clock:            fixedClock{},
		ForbiddenImports: []string{"acme/internal"},
	})
	require.NoError(t, err)

	fx := change.Effect{
		Key:     "test_quality:PAY-142:1:test_qa",
		Kind:    change.EffectTestQuality,
		Target:  "PAY-142",
		Payload: json.RawMessage(`{"repo":"acme/pay-demo","branch":"PAY-142-refunds","ticket":"PAY-142","submission_ref":"` + string(blobRef) + `"}`),
	}

	err = exec.Execute(ctx, fx)
	require.NoError(t, err)

	require.Len(t, ingest.written, 1)
	require.Equal(t, change.EventTestsRejected, ingest.written[0].Type)

	var rejected change.TestsRejected
	require.NoError(t, json.Unmarshal(ingest.written[0].Payload, &rejected))
	require.NotEmpty(t, rejected.Reasons)
	require.Contains(t, rejected.Reasons[0], "imports forbidden")
}

func TestTestQualityExecutorSkipsWhenAlreadyEvaluated(t *testing.T) {
	ctx := context.Background()
	blobRef := workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	blobs := &fakeBlobStore{blobs: map[workflow.BlobRef][]byte{blobRef: []byte("{}")}}
	source := &fakeSource{}
	sandbox := &fakeSandbox{}
	ingest := &fakeIngest{}

	store := &fakeStore{
		events: []change.Event{
			{ID: "ev-1", Type: change.EventTestsVerified},
		},
	}

	exec, err := effects.NewTestQuality(effects.TestQualityDeps{
		Blobs:   blobs,
		Source:  source,
		Sandbox: sandbox,
		Store:   store,
		Ingest:  ingest,
		IDs:     &fakeIDs{},
		Clock:   fixedClock{},
	})
	require.NoError(t, err)

	fx := change.Effect{
		Key:     "test_quality:PAY-142:1:test_qa",
		Kind:    change.EffectTestQuality,
		Target:  "PAY-142",
		Payload: json.RawMessage(`{"repo":"acme/pay-demo","branch":"PAY-142-refunds","ticket":"PAY-142","submission_ref":"` + string(blobRef) + `"}`),
	}

	err = exec.Execute(ctx, fx)
	require.NoError(t, err)
	require.Empty(t, ingest.written)
	require.Empty(t, sandbox.runs)
}
