package agenttools

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

type dedupIDs struct{ n int }

func (d *dedupIDs) NewEvent() change.EventID {
	d.n++
	return change.EventID(fmt.Sprintf("3f1c9a52-8d2b-4e7a-9c11-%012d", d.n))
}
func (d *dedupIDs) NewTask() id.TaskID       { return "tsk_01HZX3K9QW7M2P5R8T0V4YB6CD" }
func (d *dedupIDs) NewFinding() id.FindingID { return "fnd_01HZX3K9QW7M2P5R8T0V4YB6CD" }
func (d *dedupIDs) NewRun() id.RunID         { return "run_01HZX3K9QW7M2P5R8T0V4YB6CD" }
func (d *dedupIDs) NewCase() id.CaseID       { return "cas_01HZX3K9QW7M2P5R8T0V4YB6CD" }

type dedupClock struct{}

func (dedupClock) Now() time.Time { return time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC) }

func TestCrossSeatAndCrossHeadDuplicateMergesIntoOneFinding(t *testing.T) {
	svc := &Service{ids: &dedupIDs{}, clock: dedupClock{}}
	cid := id.ChangeID("KAN-5")
	a1 := Assignment{ChangeID: cid, TaskID: "tsk_01HZX3K9QW7M2P5R8T0V4YB6C1", Agent: "reviewer-1", Role: workflow.RoleReviewer}
	a2 := Assignment{ChangeID: cid, TaskID: "tsk_01HZX3K9QW7M2P5R8T0V4YB6C2", Agent: "reviewer-2", Role: workflow.RoleReviewer}

	f1 := validFinding{
		tier: finding.TierE2,
		input: finding.Input{
			Claim:      "Refund ignores error returned by refunds.record",
			Category:   finding.CategoryCorrectness,
			Severity:   finding.SeverityHigh,
			Confidence: finding.ConfidenceHigh,
			Location:   finding.Location{File: "internal/pay/refund.go", Lines: [2]int{40, 45}},
		},
	}
	evs1, err := svc.buildReviewEvents(a1, nil, []validFinding{f1}, nil)
	require.NoError(t, err)
	require.Len(t, evs1, 2, "first filing emits finding.filed + review.submitted")
	require.Equal(t, change.EventFindingFiled, evs1[0].Type)
	require.Equal(t, change.EventReviewSubmitted, evs1[1].Type)

	// L2: second seat files the same defect with overlapping lines and similar claim -> reuses F1, no second finding.filed.
	f2 := validFinding{
		tier: finding.TierE2,
		input: finding.Input{
			Claim:      "Refund ignores error returned by refunds.record call",
			Category:   finding.CategoryCorrectness,
			Severity:   finding.SeverityHigh,
			Confidence: finding.ConfidenceHigh,
			Location:   finding.Location{File: "internal/pay/refund.go", Lines: [2]int{42, 46}},
		},
	}
	evs2, err := svc.buildReviewEvents(a2, evs1, []validFinding{f2}, nil)
	require.NoError(t, err)
	require.Len(t, evs2, 1, "cross-seat duplicate emits only review.submitted, no duplicate finding.filed")
	require.Equal(t, change.EventReviewSubmitted, evs2[0].Type)
	p, err := change.Decode(evs2[0])
	require.NoError(t, err)
	rs := p.(change.ReviewSubmitted)
	require.Equal(t, []id.FindingRef{1}, rs.FindingRefs, "second seat cites existing F1")

	// Fold records reviewer-2 as an additional reporter on F1.
	all := append(append([]change.Event{}, evs1...), evs2...)
	v, err := change.Fold(all)
	require.NoError(t, err)
	require.Len(t, v.Findings, 1)
	require.Equal(t, []change.ActorID{"reviewer-2"}, v.Findings["F1"].Reporters)

	// L1: same seat on a later head re-reporting the exact same open finding also reuses F1.
	evs3, err := svc.buildReviewEvents(a1, all, []validFinding{f1}, nil)
	require.NoError(t, err)
	require.Len(t, evs3, 1, "cross-head re-report emits only review.submitted, keeping F1")
}

// Two seats' paraphrases of one defect merge; two different defects on the same lines stay apart. The claims are the real
// ones (PR #20 F2 and F3, Nov 22: same defect, claim overlap 0.36; PR #18 F39 and F41: a race and an overflow on
// overlapping lines, 0.29). On PR #18, 105 of 133 same-defect pairs scored under the old 0.6 threshold and no different-
// defect pair reached 0.30.
func TestParaphrasesOfOneDefectMergeAndDifferentDefectsOnTheSameLinesDoNot(t *testing.T) {
	at := func(file string, from, to int) *finding.Location {
		return &finding.Location{File: file, Lines: [2]int{from, to}}
	}
	ref := func(s string) id.FindingRef {
		r, err := id.ParseFindingRef(s)
		require.NoError(t, err)
		return r
	}
	known := []knownFinding{{ref: ref("F2"), claim: "os.WriteFile's error is discarded, so ExportPayment returns nil even when the reconciliation file could not be written (disk full, permission denied, bad path). The nightly job then believes the export succeeded while the file is missing or truncated, silently losing reconciliation data.", category: finding.Category("correctness"),
		location: at("internal/payments/export.go", 19, 19), state: finding.StateOpen}}
	in := finding.Input{Claim: "The result of os.WriteFile is discarded and the function returns nil unconditionally, so a failed write (permissions, full disk, bad path) is reported as a successful export and the nightly reconciliation file is silently missing.", Category: finding.Category("correctness"),
		Location: *at("internal/payments/export.go", 19, 20)}
	got, ok := matchFinding(known, in, "")
	require.True(t, ok, "a paraphrase of the same defect is one finding")
	require.Equal(t, ref("F2"), got.ref)

	in.Category = finding.Category("security")
	_, ok = matchFinding(known, in, "")
	require.False(t, ok, "another category is another finding")

	race := []knownFinding{{ref: ref("F39"), claim: "The refund cap is not atomic: Refund reads s.refunds.total at internal/payments/refund.go:67 and records at :71 under two separate lock acquisitions. Two concurrent refunds of 600 on a 1000 payment both see total 0, both pass the check, and both record, so 1200 is refunded against a 1000 payment.", category: finding.Category("correctness"),
		location: at("internal/payments/refund.go", 67, 71), state: finding.StateOpen}}
	overflow := finding.Input{Claim: "The cap check `s.refunds.total(p.ID)+amount > p.Amount` at internal/payments/refund.go:67 overflows int64. On a payment of 9223372036854775807 a full refund followed by a refund of 1 wraps the sum negative, passes the check and returns 201 for money beyond the payment.", Category: finding.Category("correctness"),
		Location: *at("internal/payments/refund.go", 67, 68)}
	_, ok = matchFinding(race, overflow, "")
	require.False(t, ok, "a race and an overflow on the same lines are two findings")
}

// A later head moves the code: the same defect one line down is still the same finding, and an E4 finding is a merge
// candidate too (live, PR #20 Nov 22: F2 at line 19 re-filed as F5 at line 20 after a one-line comment; F4 re-filed as F8).
func TestAMovedDefectOnALaterHeadAndAnE4DuplicateMerge(t *testing.T) {
	at := func(file string, from, to int) *finding.Location {
		return &finding.Location{File: file, Lines: [2]int{from, to}}
	}
	ref := func(s string) id.FindingRef {
		r, err := id.ParseFindingRef(s)
		require.NoError(t, err)
		return r
	}
	writeErr := []knownFinding{{ref: ref("F2"), claim: "os.WriteFile's error is discarded, so ExportPayment returns nil even when the reconciliation file could not be written (disk full, permission denied, bad path). The nightly job then believes the export succeeded while the file is missing or truncated, silently losing reconciliation data.", category: finding.Category("correctness"),
		location: at("internal/payments/export.go", 19, 19), state: finding.StateOpen}}
	moved := finding.Input{Claim: "The error from os.WriteFile is discarded, so ExportPayment returns nil even when the reconciliation file could not be written (disk full, permission denied, nonexistent directory). The nightly job then treats a failed export as success and the morning finance job reads a missing or truncated file.", Category: finding.Category("correctness"), Location: *at("internal/payments/export.go", 20, 20)}
	got, ok := matchFinding(writeErr, moved, "")
	require.True(t, ok, "one line down is the same defect")
	require.Equal(t, ref("F2"), got.ref)

	far := moved
	far.Location = *at("internal/payments/export.go", 40, 40)
	_, ok = matchFinding(writeErr, far, "")
	require.False(t, ok, "twenty lines away is somewhere else")
}

// An E4 finding is filed unverified; it can never block, but a second seat filing the same thing is still noise, so it is
// a merge candidate. A withdrawn or dismissed finding is not.
func TestUnverifiedFindingsAreMergeCandidatesAndTerminalOnesAreNot(t *testing.T) {
	ref, err := id.ParseFindingRef("F4")
	require.NoError(t, err)
	filed := func(state finding.State) []change.Event {
		raw, err := json.Marshal(change.FindingFiled{FindingID: "fnd_01HZX3K9QW7M2P5R8T0V4YB6CD", Ref: ref, Tier: finding.TierE4,
			Severity: finding.SeverityLow, State: state, By: "reviewer-3", Claim: "the test never checks the exported contents",
			Category: finding.Category("tests"), Location: &finding.Location{File: "tests/heimdall/E2E-1122/export_test.go", Lines: [2]int{20, 25}}})
		require.NoError(t, err)
		return []change.Event{{Type: change.EventFindingFiled, Payload: raw}}
	}
	require.Len(t, liveFindingsFrom(filed(finding.StateUnverified)), 1)
	require.Empty(t, liveFindingsFrom(filed(finding.StateWithdrawn)))
}
