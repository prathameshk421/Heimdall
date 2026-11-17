package effects

import (
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"path"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Source fetches source code for a commit.
type Source interface {
	Fetch(ctx context.Context, repo change.Repo, sha id.SHA) error
}

// TestQualityDeps are the dependencies of TestQuality.
type TestQualityDeps struct {
	Blobs   ports.BlobStore
	Source  Source
	Sandbox ports.Sandbox
	Ingest  Ingest
	IDs     ports.IDs
	Clock   ports.Clock
	// Code is not used by verification any more: the pinned tree comes from the submission.
	Code             ports.CodeReader
	ForbiddenImports []string
	// NeedsSignoff says whether the submission touches a sensitive domain, from the policy at the base commit.
	NeedsSignoff func(ctx context.Context, repo change.Repo, base id.SHA, sub testset.Submission) bool
}

// TestQuality performs test_quality effects: runs sandbox checks (red-first, flake, seam imports),
// and ingests tests.verified or tests.rejected.
type TestQuality struct {
	deps TestQualityDeps
}

// NewTestQuality builds the executor, or refuses when a dependency is missing.
func NewTestQuality(d TestQualityDeps) (TestQuality, error) {
	var missing []string
	if d.Blobs == nil {
		missing = append(missing, "blobs")
	}
	if d.Source == nil {
		missing = append(missing, "source")
	}
	if d.Sandbox == nil {
		missing = append(missing, "sandbox")
	}
	if d.Ingest == nil {
		missing = append(missing, "ingest")
	}
	if d.IDs == nil {
		missing = append(missing, "ids")
	}
	if d.Clock == nil {
		missing = append(missing, "clock")
	}
	if len(missing) > 0 {
		return TestQuality{}, fmt.Errorf("test_quality executor: missing %v", missing)
	}
	return TestQuality{deps: d}, nil
}

// Execute performs the test quality checks and ingests the result.
func (t TestQuality) Execute(ctx context.Context, fx change.Effect) error {
	if fx.Kind != change.EffectTestQuality {
		return failf(nil, "test_quality executor: effect %s has kind %s", fx.Key, fx.Kind)
	}
	p, err := change.DecodeTestQualityPayload(fx.Payload)
	if err != nil {
		return failf(err, "test_quality effect %s: payload refused: %v", fx.Key, err)
	}
	if fx.Target != string(p.Ticket) {
		return failf(nil, "test_quality effect %s: target %q is not ticket %s", fx.Key, fx.Target, p.Ticket)
	}

	sub, err := t.readSubmission(ctx, p.Ticket, p.SubmissionRef)
	if err != nil {
		return err
	}

	baseSHA := p.BaseSHA
	if baseSHA == "" {
		return failf(nil, "test_quality %s: the job names no base SHA", p.Ticket)
	}
	if err := t.deps.Source.Fetch(ctx, p.Repo, baseSHA); err != nil {
		return failf(err, "test_quality %s: fetch base source", p.Ticket)
	}

	allReasons, runIDs, err := t.runChecks(ctx, p, sub, baseSHA)
	if err != nil {
		return err
	}

	return t.ingestOutcome(ctx, p, sub, runIDs, allReasons)
}

func (t TestQuality) readSubmission(ctx context.Context, ticket id.ChangeID, ref workflow.BlobRef) (testset.Submission, error) {
	rc, err := t.deps.Blobs.Get(ctx, ref)
	if err != nil {
		return testset.Submission{}, failf(err, "test_quality %s: get submission blob %s", ticket, ref)
	}
	var sub testset.Submission
	err = json.NewDecoder(rc).Decode(&sub)
	_ = rc.Close()
	if err != nil {
		return testset.Submission{}, failf(err, "test_quality %s: decode submission blob %s", ticket, ref)
	}
	return sub, nil
}

func (t TestQuality) runChecks(ctx context.Context, p change.TestQualityPayload, sub testset.Submission, baseSHA id.SHA) ([]string, []id.RunID, error) {
	var allReasons []string

	seamReasons, err := t.checkSeam(sub)
	if err != nil {
		return nil, nil, failf(err, "test_quality %s: seam imports check", p.Ticket)
	}
	allReasons = append(allReasons, seamReasons...)
	allReasons = append(allReasons, placeholderReasons(sub)...)

	flakeReports, flakeRunIDs, flakeReasons, err := t.checkFlake(ctx, p.Repo, p.Ticket, baseSHA, sub)
	if err != nil {
		return nil, nil, err
	}
	allReasons = append(allReasons, flakeReasons...)

	redFirstRunIDs, redFirstReasons, err := t.checkRedFirst(ctx, p, sub, baseSHA, flakeReports[0])
	if err != nil {
		return nil, nil, err
	}
	allReasons = append(allReasons, redFirstReasons...)

	runIDs := make([]id.RunID, 0, len(flakeRunIDs)+len(redFirstRunIDs))
	runIDs = append(runIDs, flakeRunIDs...)
	runIDs = append(runIDs, redFirstRunIDs...)
	return allReasons, runIDs, nil
}

// placeholderReasons refuses a test package (a directory) none of whose files imports anything but the standard
// library. It cannot reach the product, so it can only fail for a made-up reason, and red-first alone would pass it.
// The unit is the package, as in Go: on live KAN-25 one file used the helpers of a sibling that imports the product,
// and a per-file check refused it.
func placeholderReasons(sub testset.Submission) []string {
	reaches, imports := map[string]bool{}, map[string]bool{}
	var dirs []string
	for _, f := range sub.Files {
		parsed, err := parser.ParseFile(token.NewFileSet(), f.Path, f.Content, parser.ImportsOnly)
		if err != nil {
			continue // submit_tests already refuses Go that does not parse
		}
		dir := path.Dir(f.Path)
		if _, seen := reaches[dir]; !seen {
			dirs = append(dirs, dir)
			reaches[dir] = false
		}
		for _, imp := range parsed.Imports {
			imports[dir] = true
			if strings.Contains(strings.Trim(imp.Path.Value, `"`), ".") {
				reaches[dir] = true
			}
		}
	}
	var reasons []string
	for _, dir := range dirs {
		if imports[dir] && !reaches[dir] {
			reasons = append(reasons, dir+" never imports the product, so its tests cannot test it. Call the service through its test seam.")
		}
	}
	return reasons
}

func (t TestQuality) checkSeam(sub testset.Submission) ([]string, error) {
	if len(t.deps.ForbiddenImports) == 0 {
		return nil, nil
	}
	filesMap := make(map[string]string, len(sub.Files))
	for _, f := range sub.Files {
		filesMap[f.Path] = f.Content
	}
	violations, err := verify.SeamImports(filesMap, t.deps.ForbiddenImports)
	if err != nil {
		return nil, err
	}
	var reasons []string
	for _, v := range violations {
		reasons = append(reasons, fmt.Sprintf("%s:%d imports forbidden %s", v.File, v.Line, v.Path))
	}
	return reasons, nil
}

func (t TestQuality) checkFlake(ctx context.Context, repo change.Repo, ticket id.ChangeID, baseSHA id.SHA, sub testset.Submission) ([]verify.Report, []id.RunID, []string, error) {
	dir := "tests/heimdall/" + string(ticket)
	overlays := overlayFiles(sub)
	var flakeReports []verify.Report
	var runIDs []id.RunID

	for i := 0; i < verify.RequiredRuns; i++ {
		runID := t.deps.IDs.NewRun()
		runIDs = append(runIDs, runID)
		res, err := t.runSandbox(ctx, ports.SandboxJob{
			Kind:    ports.SandboxRedFirst,
			Repo:    repo,
			SHA:     baseSHA,
			Dir:     dir,
			Timeout: 3 * time.Minute,
		}, overlays)
		if err != nil {
			return nil, nil, nil, failf(err, "test_quality %s: sandbox run %d", ticket, i+1)
		}
		flakeReports = append(flakeReports, toReport(res))
	}

	stability, err := verify.Stability(flakeReports)
	if err != nil {
		return nil, nil, nil, failf(err, "test_quality %s: stability check", ticket)
	}
	var reasons []string
	for _, rej := range stability.Rejected() {
		reasons = append(reasons, fmt.Sprintf("%s is flaky across %d runs", rej.Test, verify.RequiredRuns))
	}
	return flakeReports, runIDs, reasons, nil
}

func (t TestQuality) checkRedFirst(ctx context.Context, p change.TestQualityPayload, sub testset.Submission, baseSHA id.SHA, beforeRep verify.Report) ([]id.RunID, []string, error) {
	repo, ticket, headSHA := p.Repo, p.Ticket, p.HeadSHA
	if headSHA != "" && headSHA != baseSHA {
		dir := "tests/heimdall/" + string(ticket)
		runID := t.deps.IDs.NewRun()
		resHead, err := t.runSandbox(ctx, ports.SandboxJob{
			Kind:    ports.SandboxRedFirst,
			Repo:    repo,
			SHA:     headSHA,
			Dir:     dir,
			Timeout: 3 * time.Minute,
		}, overlayFiles(sub))
		if err != nil {
			return nil, nil, failf(err, "test_quality %s: sandbox head run", ticket)
		}
		afterRep := toReport(resHead)
		verdict := verify.Judge(beforeRep, afterRep, sub.AcceptanceCriteria)
		var reasons []string
		for _, rej := range verdict.Rejected {
			reasons = append(reasons, rej.Reason)
		}
		for _, ac := range verdict.Uncovered {
			reasons = append(reasons, fmt.Sprintf("acceptance criterion %s has no passing red-first test", ac))
		}
		return []id.RunID{runID}, reasons, nil
	}

	var reasons []string
	for _, c := range beforeRep.Cases() {
		switch c.Status {
		case verify.StatusError:
			reasons = append(reasons, fmt.Sprintf("%s failed with compile or setup error on base: %s", c.Name, verify.ErrorDetail(c)))
		case verify.StatusPass:
			reasons = append(reasons, fmt.Sprintf("%s passed without the feature on base", c.Name))
		case verify.StatusFail, verify.StatusSkipped:
		}
	}
	return nil, reasons, nil
}

type overlaySandbox interface {
	RunOverlay(context.Context, ports.SandboxJob, []ports.File) (ports.SandboxResult, error)
}

func (t TestQuality) runSandbox(ctx context.Context, job ports.SandboxJob, overlays []ports.File) (ports.SandboxResult, error) {
	if len(overlays) == 0 {
		return t.deps.Sandbox.Run(ctx, job)
	}
	sandbox, ok := t.deps.Sandbox.(overlaySandbox)
	if !ok {
		return ports.SandboxResult{}, fmt.Errorf("sandbox does not support test-submission overlays")
	}
	return sandbox.RunOverlay(ctx, job, overlays)
}

func overlayFiles(sub testset.Submission) []ports.File {
	files := make([]ports.File, 0, len(sub.Files))
	for _, file := range sub.Files {
		files = append(files, ports.File{Path: file.Path, Content: []byte(file.Content)})
	}
	return files
}

func (t TestQuality) ingestOutcome(ctx context.Context, p change.TestQualityPayload, sub testset.Submission, runIDs []id.RunID, reasons []string) error {
	if len(reasons) > 0 {
		return t.ingestRejected(ctx, p.Ticket, p.BaseSHA, runIDs, reasons)
	}
	return t.ingestVerified(ctx, p, sub, runIDs)
}

func (t TestQuality) ingestRejected(ctx context.Context, ticket id.ChangeID, baseSHA id.SHA, runIDs []id.RunID, reasons []string) error {
	payload := change.TestsRejected{
		Reasons: boundReasons(reasons),
		RunIDs:  runIDs,
		SHA:     baseSHA,
	}
	if err := payload.Validate(); err != nil {
		return failf(err, "test_quality %s: tests.rejected refused", ticket)
	}
	ev, err := newFact(t.deps.IDs, t.deps.Clock, change.EventTestsRejected,
		change.Actor{Kind: change.ActorSystem, ID: "heimdall"}, payload)
	if err != nil {
		return failf(err, "test_quality %s: build event", ticket)
	}
	if err := t.deps.Ingest.Ingest(ctx, ticket, []change.Event{ev}); err != nil {
		return failf(err, "test_quality %s: ingest tests.rejected", ticket)
	}
	return nil
}

func (t TestQuality) ingestVerified(ctx context.Context, p change.TestQualityPayload, sub testset.Submission, runIDs []id.RunID) error {
	ticket := p.Ticket
	dir := "tests/heimdall/" + string(ticket)
	// The tests exist only as a sandbox overlay until they are committed, so the base commit has no such directory.
	// The pinned tree is the one git gives the directory once these files are committed; the gate compares it with
	// the tree it reads at the head.
	tree, err := sub.TreeHash(dir)
	if err != nil {
		return failf(err, "test_quality %s: pin the submitted tests tree", ticket)
	}
	treeHash := id.SHA(tree)
	needsSignoff := false
	if t.deps.NeedsSignoff != nil {
		needsSignoff = t.deps.NeedsSignoff(ctx, p.Repo, p.BaseSHA, sub)
	}

	payload := change.TestsVerified{
		RunIDs:       runIDs,
		SHA:          p.BaseSHA,
		TreeHash:     treeHash,
		NeedsSignoff: needsSignoff,
	}
	if err := payload.Validate(); err != nil {
		return failf(err, "test_quality %s: tests.verified refused", ticket)
	}
	ev, err := newFact(t.deps.IDs, t.deps.Clock, change.EventTestsVerified,
		change.Actor{Kind: change.ActorSystem, ID: "heimdall"}, payload)
	if err != nil {
		return failf(err, "test_quality %s: build event", ticket)
	}
	if err := t.deps.Ingest.Ingest(ctx, ticket, []change.Event{ev}); err != nil {
		return failf(err, "test_quality %s: ingest tests.verified", ticket)
	}
	return nil
}

func toReport(res ports.SandboxResult) verify.Report {
	cases := make([]verify.Case, 0, len(res.Tests))
	for _, t := range res.Tests {
		var st verify.Status
		switch t.Outcome {
		case ports.OutcomePassed:
			st = verify.StatusPass
		case ports.OutcomeFailure:
			st = verify.StatusFail
		case ports.OutcomeError:
			st = verify.StatusError
		case ports.OutcomeSkipped:
			st = verify.StatusSkipped
		default:
			st = verify.StatusFail
		}
		cases = append(cases, verify.Case{
			Name:   t.Name,
			Status: st,
		})
	}
	if len(cases) == 0 {
		switch {
		case res.Failed > 0:
			cases = append(cases, verify.Case{Name: "suite", Status: verify.StatusFail})
		case res.Errored > 0:
			cases = append(cases, verify.Case{Name: "suite", Status: verify.StatusError})
		case res.Passed > 0:
			cases = append(cases, verify.Case{Name: "suite", Status: verify.StatusPass})
		}
	}
	return verify.Report{Suites: []verify.Suite{{Name: "default", Cases: cases}}}
}

func boundReasons(in []string) []string {
	const maxReasons = 8
	const maxLen = 256
	out := make([]string, 0, min(len(in), maxReasons))
	for _, r := range in {
		if len(out) >= maxReasons {
			break
		}
		s := strings.TrimSpace(r)
		if s == "" {
			continue
		}
		if len(s) > maxLen {
			s = s[:maxLen]
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		out = append(out, "tests rejected without a reason")
	}
	return out
}
