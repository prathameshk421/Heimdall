package agenttools

import (
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
