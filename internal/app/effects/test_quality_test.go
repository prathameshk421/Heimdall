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

const (
	tqBase = id.SHA("0123456789abcdef0123456789abcdef01234567")
	tqHead = id.SHA("fedcba9876543210fedcba9876543210fedcba98")
	tqBlob = workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	tqDir  = "tests/heimdall/PAY-142"
)

func failRun() ports.SandboxResult {
	return ports.SandboxResult{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomeFailure}}}
}

func qualitySubmission(content string) testset.Submission {
	return testset.Submission{
		Files:              []testset.File{{Path: tqDir + "/refund_test.go", Content: content}},
		AcceptanceCriteria: map[string][]string{"AC1": {"TestA"}},
	}
}

type qualityKit struct {
	exec    effects.TestQuality
	source  *fakeSource
	code    *fakeTestQualityCode
	sandbox *fakeSandbox
	ingest  *fakeIngest
	sub     testset.Submission
}

func newQualityKit(t *testing.T, sub testset.Submission, results []ports.SandboxResult, forbidden []string) *qualityKit {
	t.Helper()
	subBytes, err := json.Marshal(sub)
	require.NoError(t, err)
	k := &qualityKit{
		source:  &fakeSource{},
		code:    &fakeTestQualityCode{tree: id.SHA("abcdef0123456789abcdef0123456789abcdef01")},
		sandbox: &fakeSandbox{results: results},
		ingest:  &fakeIngest{},
		sub:     sub,
	}
	k.exec, err = effects.NewTestQuality(effects.TestQualityDeps{
		Blobs:  &fakeBlobStore{blobs: map[workflow.BlobRef][]byte{tqBlob: subBytes}},
		Source: k.source, Sandbox: k.sandbox, Ingest: k.ingest, IDs: &fakeIDs{}, Clock: fixedClock{},
		Code: k.code, ForbiddenImports: forbidden,
	})
	require.NoError(t, err)
	return k
}

func (k *qualityKit) run(t *testing.T, head id.SHA) error {
	t.Helper()
	p := change.TestQualityPayload{Repo: "acme/pay-demo", Branch: "PAY-142-refunds", Ticket: "PAY-142", SubmissionRef: tqBlob, BaseSHA: tqBase, HeadSHA: head}
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	return k.exec.Execute(context.Background(), change.Effect{Key: "test_quality:PAY-142:1:test_qa", Kind: change.EffectTestQuality, Target: "PAY-142", Payload: raw})
}

// Red-first: the submitted tests must fail stably on the base (3 runs), and pass on the head when there is one. The
// pinned tree is computed from the submitted files, never read from the base commit (live KAN-13).
func TestTestQualityVerifiesRedFirst(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		head    id.SHA
		results []ports.SandboxResult
	}{
		"base only": {"", []ports.SandboxResult{failRun(), failRun(), failRun()}},
		"with head": {tqHead, []ports.SandboxResult{failRun(), failRun(), failRun(),
			{Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomePassed}}}}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			k := newQualityKit(t, qualitySubmission("package refund_test\n"), c.results, nil)
			require.NoError(t, k.run(t, c.head))

			require.Len(t, k.sandbox.overlays, len(c.results), "every run evaluates the submitted test overlay")
			for _, overlay := range k.sandbox.overlays {
				require.Equal(t, tqDir+"/refund_test.go", overlay[0].Path)
				require.Equal(t, "package refund_test\n", string(overlay[0].Content))
			}
			require.Len(t, k.sandbox.runs, len(c.results))
			require.Equal(t, change.Repo("acme/pay-demo"), k.source.fetchedRepo)
			require.Equal(t, tqBase, k.source.fetchedSHA)
			require.Empty(t, k.code.dir, "verification must not read the tests directory from the base commit")

			require.Len(t, k.ingest.written, 1)
			require.Equal(t, change.EventTestsVerified, k.ingest.written[0].Type)
			var verified change.TestsVerified
			require.NoError(t, json.Unmarshal(k.ingest.written[0].Payload, &verified))
			wantTree, err := k.sub.TreeHash(tqDir)
			require.NoError(t, err)
			require.Equal(t, id.SHA(wantTree), verified.TreeHash)
			require.Len(t, verified.RunIDs, len(c.results))
			if c.head == "" {
				require.Equal(t, tqBase, verified.SHA)
			}
		})
	}
}

func TestTestQualityRejections(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		sub       testset.Submission
		results   []ports.SandboxResult
		head      id.SHA
		forbidden []string
		wantErr   string // Execute fails and nothing is ingested
		wantWhy   string // tests.rejected with this reason
	}{
		"flaky on base": {qualitySubmission("package refund_test\n"),
			[]ports.SandboxResult{failRun(), {Tests: []ports.TestResult{{Name: "TestA", Outcome: ports.OutcomePassed}}}, failRun()},
			"", nil, "", "flaky"},
		"still fails on head": {qualitySubmission("package refund_test\n"),
			[]ports.SandboxResult{failRun(), failRun(), failRun(), failRun()},
			tqHead, nil, "", "does not pass with the feature"},
		"imports the product seam": {qualitySubmission("package refund_test\nimport \"acme/internal/secret\"\n"),
			[]ports.SandboxResult{failRun(), failRun(), failRun()},
			"", []string{"acme/internal"}, "", "imports forbidden"},
		"file outside the ticket's tests directory": {testset.Submission{
			Files:              []testset.File{{Path: "tests/heimdall/PAY-999/refund_test.go", Content: "package refund_test\n"}},
			AcceptanceCriteria: map[string][]string{"AC1": {"TestA"}},
		}, []ports.SandboxResult{failRun(), failRun(), failRun()}, "", nil, "pin the submitted tests tree", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			k := newQualityKit(t, c.sub, c.results, c.forbidden)
			err := k.run(t, c.head)
			if c.wantErr != "" {
				require.ErrorContains(t, err, c.wantErr)
				require.Empty(t, k.ingest.written, "nothing is recorded for a tree that cannot be pinned")
				return
			}
			require.NoError(t, err)
			require.Len(t, k.ingest.written, 1)
			require.Equal(t, change.EventTestsRejected, k.ingest.written[0].Type)
			var rejected change.TestsRejected
			require.NoError(t, json.Unmarshal(k.ingest.written[0].Payload, &rejected))
			require.NotEmpty(t, rejected.Reasons)
			require.Contains(t, rejected.Reasons[0], c.wantWhy)
		})
	}
}
