package review_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/review"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

const (
	changeID = id.ChangeID("PAY-142")
	caller   = id.AgentName("rhea")
	headSHA  = id.SHA("9f1c2ab0c3d4e5f60718293a4b5c6d7e8f901234")
)

type refs struct {
	files     map[string]int
	checks    []change.ProfileCheck
	memory    map[string]bool
	packs     map[string]bool
	lookupErr error
}

func newRefs() *refs {
	return &refs{
		files:  map[string]int{"internal/refunds/refunds.go": 8, "internal/compat/legacy.go": 4},
		checks: []change.ProfileCheck{{ID: "patterns", When: []string{"**/*.go"}}, {ID: "migrations", When: []string{"**/*.sql"}}},
		memory: map[string]bool{"memory:refund-limit": true},
		packs:  map[string]bool{"pack:refunds:17": true},
	}
}

func (r *refs) FileExists(_ context.Context, _ workflow.ChangeRef, path string) (bool, error) {
	if r.lookupErr != nil {
		return false, r.lookupErr
	}
	_, ok := r.files[path]
	return ok, nil
}

func (r *refs) LineCount(_ context.Context, _ workflow.ChangeRef, path string) (int, error) {
	if r.lookupErr != nil {
		return 0, r.lookupErr
	}
	return r.files[path], nil
}

func (r *refs) ProfileChecks(_ context.Context, _ workflow.ChangeRef) ([]change.ProfileCheck, error) {
	return r.checks, r.lookupErr
}

func (r *refs) MemoryExists(_ context.Context, _ workflow.ChangeRef, _ id.AgentName, ref string) (bool, error) {
	return r.memory[ref], r.lookupErr
}

func TestSubmitReturnsAllProblemsWithFieldPaths(t *testing.T) {
	f := validFinding()
	f.Location.File = "missing.go"
	f.Location.Lines = [2]int{2, 99}
	f.Evidence = []finding.Evidence{
		{Type: finding.EvidenceToolResult, ToolResult: &finding.ToolResult{CheckID: "missing-check", Location: finding.Location{File: "missing-tool.go", Lines: [2]int{1, 2}}}},
		{Type: finding.EvidenceMemory, MemoryRef: ptr("memory:missing")},
		{Type: finding.EvidencePackRef, PackRef: ptr("pack:missing")},
	}
	got, err := review.Submit(context.Background(), newRefs(), submission(f))
	require.NoError(t, err)
	require.Empty(t, got.Findings)
	text := problemText(got)
	for _, want := range []string{
		"findings[0].location.file", "findings[0].location.lines",
		"findings[0].evidence[0].tool_result.check_id", "findings[0].evidence[1].memory_ref",
		"findings[0].evidence[2].pack_ref",
	} {
		require.Contains(t, text, want)
	}
}

func TestSubmitRequiresReasonedAllowedCategoryOutsideDiff(t *testing.T) {
	f := validFinding()
	f.Location.File = "internal/compat/legacy.go"
	f.Category = finding.CategoryCorrectness
	f.Evidence = []finding.Evidence{{Type: finding.EvidenceMemory, MemoryRef: ptr("memory:refund-limit")}}
	got, err := review.Submit(context.Background(), newRefs(), submission(f))
	require.NoError(t, err)
	require.Empty(t, got.Findings)
	require.Contains(t, problemText(got), "findings[0].category")
	require.Contains(t, problemText(got), "findings[0].evidence")
}

func TestSubmitAllowsAnOutsideDiffFindingWithAllowedCategoryAndReasoning(t *testing.T) {
	f := validFinding()
	f.Location.File = "internal/compat/legacy.go"
	f.Category = finding.CategoryCompat
	f.Evidence = []finding.Evidence{{Type: finding.EvidenceReasoning, Reasoning: ptr("The old client contract remains compatible only when this branch keeps the legacy field.")}}
	got, err := review.Submit(context.Background(), newRefs(), submission(f))
	require.NoError(t, err)
	require.Empty(t, got.Problems)
	require.Len(t, got.Findings, 1)
}

func TestSubmitMergesOverlappingSimilarFindingsAndKeepsStrongestEvidence(t *testing.T) {
	existing := review.FiledFinding{
		Input: finding.Input{Claim: "The refund limit can overflow before the validation branch.", Category: finding.CategoryCorrectness, Severity: finding.SeverityHigh, Confidence: finding.ConfidenceMedium, Location: finding.Location{File: "internal/refunds/refunds.go", Lines: [2]int{2, 5}}, Evidence: []finding.Evidence{{Type: finding.EvidenceReasoning, Reasoning: ptr("The arithmetic adds an unbounded amount before checking the limit.")}}},
		Tier:  finding.TierE4, Reporters: []review.Reporter{{Agent: id.AgentName("noah"), Confidence: finding.ConfidenceMedium}},
	}
	f := validFinding()
	f.Claim = "The refund limit can overflow before the validation branch delta echo."
	f.Location.Lines = [2]int{4, 7}
	in := submission(f)
	in.Existing = []review.FiledFinding{existing}
	got, err := review.Submit(context.Background(), newRefs(), in)
	require.NoError(t, err)
	require.Len(t, got.Findings, 1)
	require.Equal(t, finding.TierE3, got.Findings[0].Tier)
	require.Equal(t, []review.Reporter{{Agent: id.AgentName("noah"), Confidence: finding.ConfidenceMedium}, {Agent: caller, Confidence: finding.ConfidenceHigh}}, got.Findings[0].Reporters)
}

func TestSubmitRejectsReviewFromAnotherHead(t *testing.T) {
	in := submission(validFinding())
	in.Review.HeadSHA = id.SHA("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	got, err := review.Submit(context.Background(), newRefs(), in)

	require.NoError(t, err)
	require.Empty(t, got.Findings)
	require.Contains(t, problemText(got), "head_sha")
}

func TestSubmitPropagatesReferenceFailureWithoutAccepting(t *testing.T) {
	r := newRefs()
	r.lookupErr = errors.New("reference database unavailable")
	_, err := review.Submit(context.Background(), r, submission(validFinding()))
	require.ErrorIs(t, err, r.lookupErr)
}

type auditLog struct{ causes []string }

func (l *auditLog) RejectedOutput(_ context.Context, cause string) {
	l.causes = append(l.causes, cause)
}

func TestOutputFilterRejectsSecretCanaryAndOversizeWithoutLeakingCause(t *testing.T) {
	log := &auditLog{}
	filter, err := review.NewOutputFilter([]string{"canary-refund-17"}, log)
	require.NoError(t, err)
	for _, text := range []string{"please use gh" + "p_abcdefghijklmnopqrstuvwxyz0123456789", "canary-refund-17", strings.Repeat("a", 401)} {
		_, err = filter.Filter(context.Background(), text)
		require.ErrorIs(t, err, review.ErrOutputFiltered)
		require.Equal(t, "output rejected", err.Error())
	}
	require.Len(t, log.causes, 3)
}

func submission(f finding.Input) review.Submission {
	return review.Submission{Change: workflow.ChangeRef{ID: changeID, Repo: "acme/pay-demo", HeadSHA: headSHA}, Caller: caller, FilesChanged: []string{"internal/refunds/refunds.go"}, Review: finding.Review{HeadSHA: headSHA, FilesReviewed: []string{"internal/refunds/refunds.go"}, Findings: []finding.Input{f}}}
}

func validFinding() finding.Input {
	return finding.Input{Claim: "Refund validation permits an amount that exceeds the requested refund limit.", Category: finding.CategoryCorrectness, Severity: finding.SeverityHigh, Confidence: finding.ConfidenceHigh, Location: finding.Location{File: "internal/refunds/refunds.go", Lines: [2]int{2, 4}}, Evidence: []finding.Evidence{{Type: finding.EvidenceMemory, MemoryRef: ptr("memory:refund-limit")}}, ContextRefs: []string{"card:refund-cap.md"}}
}

func ptr(s string) *string { return &s }
func problemText(result review.Result) string {
	parts := make([]string, 0, len(result.Problems))
	for _, problem := range result.Problems {
		parts = append(parts, problem.Error())
	}
	return strings.Join(parts, "\n")
}

func TestSubmitRefusesABadCardRef(t *testing.T) {
	for _, ref := range []string{"card:", "card:../secret.md", "card:a/b.md", "card:.hidden", "card:" + strings.Repeat("a", 300)} {
		f := validFinding()
		f.ContextRefs = []string{ref}
		got, err := review.Submit(context.Background(), newRefs(), submission(f))
		require.NoError(t, err)
		require.Contains(t, problemText(got), "context_refs[0]", ref)
	}
}

func TestSubmitRefusesACardCitationForAnotherFile(t *testing.T) {
	f := validFinding()
	ref := "card:cap.md#some/other/file.go"
	f.Evidence = []finding.Evidence{{Type: finding.EvidenceMemory, MemoryRef: &ref}}
	got, err := review.Submit(context.Background(), newRefs(), submission(f))
	require.NoError(t, err)
	require.Contains(t, problemText(got), "must name the file of location.file")
}

func toolResultFinding(checkID string) finding.Input {
	f := validFinding()
	f.Evidence = []finding.Evidence{{Type: finding.EvidenceToolResult, ToolResult: &finding.ToolResult{CheckID: checkID, Location: f.Location}}}
	return f
}

// E2 evidence cites a profile check that runs on this change: verify_evidence re-runs exactly that check on the head.
func TestSubmitAcceptsAToolResultThatCitesACheckOfferedOnTheChange(t *testing.T) {
	got, err := review.Submit(context.Background(), newRefs(), submission(toolResultFinding("patterns")))
	require.NoError(t, err)
	require.Empty(t, got.Problems)
	require.Equal(t, finding.TierE2, got.Findings[0].Tier)
}

// A check the profile does not hold, or one whose when matches no changed file, cannot be repeated on this head. The
// refusal lists the checks the reviewer may cite.
func TestSubmitRefusesAToolResultWhoseCheckIsNotOfferedAndListsTheOffered(t *testing.T) {
	for _, checkID := range []string{"go vet", "migrations"} {
		got, err := review.Submit(context.Background(), newRefs(), submission(toolResultFinding(checkID)))
		require.NoError(t, err)
		require.Empty(t, got.Findings, checkID)
		text := problemText(got)
		require.Contains(t, text, "findings[0].evidence[0].tool_result.check_id")
		require.Contains(t, text, "use one of patterns", checkID)
	}
}

// Without a promoted profile no check runs, so no finding can claim E2 and the refusal says to use other evidence.
func TestSubmitRefusesAToolResultWhenTheRepositoryHasNoProfileChecks(t *testing.T) {
	r := newRefs()
	r.checks = nil
	got, err := review.Submit(context.Background(), r, submission(toolResultFinding("patterns")))
	require.NoError(t, err)
	require.Empty(t, got.Findings)
	require.Contains(t, problemText(got), "no profile check runs on this change")
}
