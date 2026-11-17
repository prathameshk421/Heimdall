package change_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

const (
	fTask  = "tsk_01HZX3K9QW7M2P5R8T0V4YB6CD"
	fTask2 = "tsk_01HZX3K9QW7M2P5R8T0V4YB6CE"
	fCase  = "case_01HZX3K9QW7M2P5R8T0V4YB6CD"
	fHead  = "9f1c2ab"
	fHead2 = "1a2b3c4"
	fBase  = "7d3e9a1"
	fBlob  = "blob:sha256:0000000000000000000000000000000000000000000000000000000000000001"
)

func tk(s string) id.TaskID { return id.TaskID(s) }
func cs(s string) id.CaseID { return id.CaseID(s) }

// ev builds the n-th event of a stream: payload is any value that marshals to the event's payload.
func ev(t *testing.T, n int, typ change.EventType, payload any, blobs ...workflow.BlobRef) change.Event {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	e := envelopeOf(typ)
	e.ID = change.EventID(fmt.Sprintf("3f1c9a52-8d2b-4e7a-9c11-0b6f5d4e%04x", n))
	e.Version = int64(n)
	e.OccurredAt = time.Date(2026, 11, 4, 9, n, 0, 0, time.UTC)
	e.Payload = raw
	e.BlobRefs = blobs
	return e
}

func fold(t *testing.T, evs ...change.Event) change.View {
	t.Helper()
	v, err := change.Fold(evs)
	require.NoError(t, err)
	return v
}

func opened(t *testing.T) change.Event {
	n := 1
	return ev(t, n, change.EventPROpened, change.PROpened{Repo: "acme/pay", Number: 7, HeadSHA: fHead, Author: "ann",
		BaseSHA: fBase, SuggestedReviewers: []change.ActorID{"rev-1"}})
}

func assigned(t *testing.T, n int, task string) change.Event {
	return ev(t, n, change.EventTaskAssigned, change.TaskAssigned{TaskID: tk(task), Agent: "rev-1", Role: "reviewer",
		Deadline: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC), Kind: "review", Subject: "rev-1", Input: "head-" + fHead})
}

func TestTestsInfoCountsAttemptsAndKeepsTheSubmissionRef(t *testing.T) {
	sub := change.TestsSubmitted{ACMap: map[string][]string{"AC1": {"T"}}, Files: []change.TestFile{{Path: "tests/a_test.go", Blob: fBlob}}, TaskID: tk(fTask), BaseSHA: fBase}
	v := fold(t,
		ev(t, 1, change.EventTestsSubmitted, sub, fBlob),
		ev(t, 2, change.EventTestsRejected, change.TestsRejected{Reasons: []string{"x"}, SHA: fHead}),
		ev(t, 3, change.EventTestsSubmitted, sub, fBlob))
	require.Equal(t, 2, v.Tests.Attempts, "attempts count every submission and never reset")
	require.Equal(t, workflow.BlobRef(fBlob), v.Tests.Ref)
	require.Equal(t, fTask, string(v.Tests.TaskID))
	require.Equal(t, fBase, string(v.Tests.BaseSHA))
	require.Empty(t, v.Tests.Rejected, "a new submission clears the rejection")
}

func TestTaskInfoCarriesKindInputAndLiveness(t *testing.T) {
	v := fold(t,
		assigned(t, 1, fTask),
		ev(t, 2, change.EventTaskRewoken, change.TaskRewoken{TaskID: tk(fTask), N: 1}),
		ev(t, 3, change.EventTaskRewoken, change.TaskRewoken{TaskID: tk(fTask), N: 2, Reset: true}))
	task := v.Tasks[tk(fTask)]
	require.Equal(t, "review", task.Kind)
	require.Equal(t, "head-"+fHead, task.Input)
	require.Equal(t, 2, task.Wakes)
	require.Equal(t, time.Date(2026, 11, 4, 9, 1, 0, 0, time.UTC), task.Since)
	require.Equal(t, time.Date(2026, 11, 4, 9, 3, 0, 0, time.UTC), task.ResetAt)
	require.True(t, task.Live())
}

func TestTaskStatusesDoneAndSuperseded(t *testing.T) {
	v := fold(t,
		opened(t),
		assigned(t, 2, fTask), assigned(t, 3, fTask2),
		ev(t, 4, change.EventTaskSuperseded, change.TaskSuperseded{TaskID: tk(fTask), Reason: "new input"}),
		ev(t, 5, change.EventReviewSubmitted, change.ReviewSubmitted{Reviewer: "rev-1", PackDate: "2026-11-04", TaskID: tk(fTask2)}))
	require.Equal(t, change.TaskSupersededStatus, v.Tasks[tk(fTask)].Status)
	require.Equal(t, change.TaskDoneStatus, v.Tasks[tk(fTask2)].Status)
	require.False(t, v.Tasks[tk(fTask)].Live())
}

func TestReviewOutcomes(t *testing.T) {
	v := fold(t, opened(t),
		ev(t, 2, change.EventReviewSubmitted, change.ReviewSubmitted{Reviewer: "rev-1", PackDate: "2026-11-04"}),
		ev(t, 3, change.EventReviewUnavailable, change.ReviewUnavailable{Reviewer: "rev-2", Reason: "silent"}),
		ev(t, 4, change.EventReviewWaived, change.ReviewWaived{Reviewer: "rev-3", Reason: "ok", Approver: "bob"}))
	require.Equal(t, change.ReviewSubmittedOutcome, v.Reviews["rev-1"].Outcome)
	require.True(t, v.Reviews["rev-1"].Clean)
	require.Equal(t, change.ReviewUnavailableOutcome, v.Reviews["rev-2"].Outcome)
	require.False(t, v.Reviews["rev-2"].Clean, "a seat that never answered is not a clean review")
	require.Equal(t, change.ReviewWaivedOutcome, v.Reviews["rev-3"].Outcome)
	require.Equal(t, fHead, string(v.Reviews["rev-3"].HeadSHA))
}

func TestPRInfoBaseSlopAtAndSuggested(t *testing.T) {
	v := fold(t, opened(t),
		ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}),
		ev(t, 3, change.EventPRPushed, change.PRPushed{Repo: "acme/pay", Number: 7, HeadSHA: fHead2, BaseSHA: "5555555"}))
	require.Equal(t, "5555555", string(v.PR.Base))
	require.Equal(t, time.Date(2026, 11, 4, 9, 2, 0, 0, time.UTC), v.PR.SlopAt)
	require.Equal(t, []change.ActorID{"rev-1"}, v.PR.Suggested)
}

func TestPRLinkedSetsTheTicket(t *testing.T) {
	v := fold(t, opened(t), ev(t, 2, change.EventPRLinked, change.PRLinked{Repo: "acme/pay", Number: 7, Ticket: "KAN-5"}))
	require.Equal(t, "KAN-5", string(v.PR.Ticket))
}

func TestCasesAreOpenUntilAVerdictIsAppliedOrEscalated(t *testing.T) {
	v := fold(t,
		ev(t, 1, change.EventCaseOpened, change.CaseOpened{CaseID: cs(fCase), Finding: 1}),
		ev(t, 2, change.EventVerdictSubmitted, change.VerdictSubmitted{CaseID: cs(fCase), Verdict: change.VerdictUphold}))
	c := v.Cases[cs(fCase)]
	require.True(t, c.Open)
	require.Equal(t, "F1", c.Finding)
	require.Equal(t, ev(t, 2, change.EventCaseOpened, nil).ID, c.LastEvent, "the last event of the case")

	v = fold(t,
		ev(t, 1, change.EventCaseOpened, change.CaseOpened{CaseID: cs(fCase), Finding: 1}),
		ev(t, 2, change.EventVerdictEscalated, change.VerdictEscalated{CaseID: cs(fCase), Reason: "unsure"}))
	require.False(t, v.Cases[cs(fCase)].Open)
}

func TestResumesCountPerRow(t *testing.T) {
	r := change.StepResumed{Step: "commit", By: "ann"}
	v := fold(t, ev(t, 1, change.EventStepResumed, r), ev(t, 2, change.EventStepResumed, r),
		ev(t, 3, change.EventStepResumed, change.StepResumed{Step: "write_tests", By: "ann"}))
	require.Equal(t, map[string]int{"commit": 2, "write_tests": 1}, v.Resumes)
}

func TestChecksReportedIsKeptPerHead(t *testing.T) {
	v := fold(t, ev(t, 1, change.EventChecksReported, change.ChecksReported{Head: fHead, Errors: []change.CheckError{{Check: "lint", Reason: "no image"}}, More: 3}))
	got := v.Checks[fHead]
	require.True(t, got.Ran)
	require.Equal(t, 3, got.More)
	require.Len(t, got.Errors, 1)
	require.False(t, v.Checks[fHead2].Ran)
}

func TestFindingKeepsTheFilingFingerprint(t *testing.T) {
	rec := change.FindingRecord{FindingID: "fnd_01HZX3K9QW7M2P5R8T0V4YB6CD", Ref: 1, Tier: "E1", Severity: "high", State: "open", By: "rev-1", Fingerprint: "abc"}
	v := fold(t, ev(t, 1, change.EventFindingFiled, rec))
	require.Equal(t, "abc", v.Findings["F1"].Fingerprint)
}

// Executors may record a fact twice (a crash after the append, before they learn it): folding the repeat must not
// change the view.
func TestFoldIsIdempotentForExecutorFacts(t *testing.T) {
	head := []change.Event{opened(t), ev(t, 12, change.EventTestsSubmitted, change.TestsSubmitted{ACMap: map[string][]string{"AC1": {"T"}}, Files: []change.TestFile{{Path: "t_test.go", Blob: fBlob}}})}
	rec := change.FindingRecord{FindingID: "fnd_01HZX3K9QW7M2P5R8T0V4YB6CD", Ref: 1, Tier: "E1", Severity: "high", State: "open", By: "rev-1"}
	facts := []change.Event{
		ev(t, 2, change.EventRoomOpened, change.RoomOpened{RoomID: "switch:room-1"}),
		ev(t, 3, change.EventBranchEnsured, change.BranchEnsured{Repo: "acme/pay", Branch: "KAN-5-x", BaseSHA: fBase}),
		ev(t, 5, change.EventTestsVerified, change.TestsVerified{RunIDs: []id.RunID{"run_01HZX3K9QW7M2P5R8T0V4YB6CD"}, SHA: fHead, TreeHash: fBase}),
		ev(t, 6, change.EventTestsRejected, change.TestsRejected{Reasons: []string{"r"}, SHA: fHead}),
		ev(t, 7, change.EventTestsCommitted, change.TestsCommitted{SHA: fHead, TreeHash: fBase}),
		ev(t, 8, change.EventFindingVerified, rec),
		ev(t, 9, change.EventCIReported, change.CIReported{Check: "heimdall/acceptance", SHA: fHead, Conclusion: change.CISuccess}),
		ev(t, 10, change.EventChecksReported, change.ChecksReported{Head: fHead}),
		ev(t, 11, change.EventGateEvaluated, change.GateEvaluated{OK: true, SHA: fHead, TreeHash: fBase, PolicyVersion: 1}),
	}
	for _, f := range facts {
		base := append(append([]change.Event{}, head...), f)
		dup := append(append([]change.Event{}, base...), f)
		require.Equal(t, fold(t, base...), fold(t, dup...), string(f.Type))
	}
}

// Every payload as an older writer stored it still decodes (the log is never rewritten).
func TestOldPayloadsDecode(t *testing.T) {
	old := map[change.EventType]string{
		change.EventTaskAssigned:     `{"task_id":"` + fTask + `","agent":"rev-1","role":"reviewer","deadline":"2026-11-04T12:00:00Z","bundle_ref":"` + fBlob + `"}`,
		change.EventTestsSubmitted:   `{"refs":["tests/a_test.go"],"ac_map":{"AC1":["T"]},"sha":"9f1c2ab","tree_hash":"7d3e9a1"}`,
		change.EventReviewSubmitted:  `{"reviewer":"rev-1","finding_refs":[],"pack_date":"2026-11-04"}`,
		change.EventVerdictSubmitted: `{"case_id":"` + fCase + `","verdict":"uphold","citations":[],"checks":[]}`,
		change.EventPROpened:         `{"repo":"acme/pay","number":7,"head_sha":"9f1c2ab","author":"ann"}`,
		change.EventPRPushed:         `{"repo":"acme/pay","number":7,"head_sha":"9f1c2ab"}`,
	}
	for typ, raw := range old {
		_, err := change.Decode(eventWith(typ, raw))
		require.NoError(t, err, string(typ))
	}
}
