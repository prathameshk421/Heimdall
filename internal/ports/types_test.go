package ports_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const sha = "9f1c2ab"

func validPR() ports.PR {
	return ports.PR{
		Repo: "acme/pay-demo", Number: 7, Title: "PAY-142: refunds", Body: "does things", Branch: "PAY-142-refunds",
		Author: "aarush323", BaseSHA: "a01b2c3", HeadSHA: sha, State: ports.PRStateOpen,
	}
}

func TestPRValidateAcceptsAWellFormedPullRequest(t *testing.T) {
	require.NoError(t, validPR().Validate())
}

func TestPRValidateNamesEveryBrokenField(t *testing.T) {
	p := ports.PR{Repo: "acme", Number: 0, Branch: "", Author: "", BaseSHA: "xyz", HeadSHA: "", State: "maybe"}
	err := p.Validate()
	require.Error(t, err)
	for _, f := range []string{"repo", "number", "branch", "author", "base_sha", "head_sha", "state"} {
		require.Contains(t, err.Error(), f)
	}
}

func TestPRBodyIsBoundedBecauseItIsUntrustedText(t *testing.T) {
	p := validPR()
	p.Body = strings.Repeat("x", ports.MaxUntrustedText+1)
	require.Error(t, p.Validate())
}

func TestChangedFileRefusesPathsOutsideTheRepository(t *testing.T) {
	ok := ports.ChangedFile{Path: "internal/refunds/refunds.go", Status: ports.FileModified, Additions: 3, Deletions: 1}
	require.NoError(t, ok.Validate())
	for _, bad := range []string{"", "/etc/passwd", "../x", "a/../../b", "a//b", "./a"} {
		f := ok
		f.Path = bad
		require.Error(t, f.Validate(), bad)
	}
	f := ok
	f.Status = "exploded"
	f.Additions = -1
	err := f.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "status")
	require.Contains(t, err.Error(), "additions")
}

func TestFileValidatesItsPath(t *testing.T) {
	require.NoError(t, ports.File{Path: "tests/heimdall/PAY-142/refunds_test.go", Content: []byte("package x")}.Validate())
	require.Error(t, ports.File{Path: "../escape.go"}.Validate())
}

func TestCheckRequiresAConclusionOnlyWhenCompleted(t *testing.T) {
	done := ports.Check{Name: "heimdall/gate", Status: ports.CheckCompleted, Conclusion: change.CISuccess}
	require.NoError(t, done.Validate())
	require.Error(t, ports.Check{Name: "heimdall/gate", Status: ports.CheckCompleted}.Validate(), "completed needs a conclusion")
	require.Error(t, ports.Check{Name: "heimdall/gate", Status: ports.CheckQueued, Conclusion: change.CISuccess}.Validate(), "queued has none")
	require.Error(t, ports.Check{Name: "", Status: ports.CheckQueued}.Validate())
}

func TestAgentRefValidatesNameAndRole(t *testing.T) {
	require.NoError(t, ports.AgentRef{Name: "bob-reviewer", Role: workflow.RoleReviewer}.Validate())
	err := ports.AgentRef{Name: "Bob/Reviewer", Role: "wizard"}.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "name")
	require.Contains(t, err.Error(), "role")
}

func TestLivenessValues(t *testing.T) {
	for _, l := range []ports.Liveness{ports.LivenessLive, ports.LivenessDormant, ports.LivenessOffline} {
		require.True(t, l.Valid(), l)
	}
	require.False(t, ports.Liveness("asleep").Valid())
}

func TestSandboxJobBoundsTheTimeAndThePath(t *testing.T) {
	job := ports.SandboxJob{Kind: ports.SandboxRedFirst, Repo: "acme/pay-demo", SHA: sha, Dir: "tests/heimdall/PAY-142", Timeout: 3 * time.Minute}
	require.NoError(t, job.Validate())
	job.Timeout = 0
	require.Error(t, job.Validate())
	job.Timeout = time.Hour
	require.Error(t, job.Validate(), "TECH_SPEC 12.2 caps every job")
	job.Timeout = time.Minute
	job.Dir = "/abs"
	job.Kind = "smoke"
	err := job.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "kind")
	require.Contains(t, err.Error(), "dir")
}

func TestSandboxResultOutcomesSeparateFailureFromError(t *testing.T) {
	res := ports.SandboxResult{
		Passed: 1, Failed: 1, Errored: 1,
		Tests: []ports.TestResult{
			{Name: "TestA", Outcome: ports.OutcomePassed},
			{Name: "TestB", Outcome: ports.OutcomeFailure},
			{Name: "TestC", Outcome: ports.OutcomeError},
		},
		LogRef: workflow.BlobRef("blob:sha256:" + strings.Repeat("b", 64)),
	}
	require.NoError(t, res.Validate())
	res.Tests[2].Outcome = "crashed"
	require.Error(t, res.Validate())
	res.Tests[2].Outcome = ports.OutcomeError
	res.LogRef = "not-a-blob"
	require.Error(t, res.Validate())
}

func TestEpisodeAndItsStatus(t *testing.T) {
	ep := ports.Episode{Key: "memory_episode:PAY-142:1:decision-9", Source: "decision", Status: ports.EpisodeConfirmed, GroupID: "payments", Body: "{}"}
	require.NoError(t, ep.Validate())
	ep.Status = "rumour"
	ep.GroupID = ""
	err := ep.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "status")
	require.Contains(t, err.Error(), "group_id")
}

func TestMemoryQueryNeedsServerSetGroupsAndABoundedLimit(t *testing.T) {
	q := ports.MemoryQuery{Question: "refund idempotency", Groups: []string{"payments"}, Limit: 20}
	require.NoError(t, q.Validate())
	q.Groups = nil
	require.Error(t, q.Validate(), "groups are set by the server, so none is a refusal")
	q.Groups = []string{"payments"}
	q.Limit = 21
	require.Error(t, q.Validate())
	q.Limit = 0
	require.Error(t, q.Validate())
}

func TestMemoryItemLabelsAreTheSpecLabels(t *testing.T) {
	for _, l := range []ports.MemoryLabel{ports.LabelConfirmed, ports.LabelSuperseded, ports.LabelContested, ports.LabelObserved} {
		require.True(t, l.Valid(), l)
	}
	require.False(t, ports.MemoryLabel("true").Valid())
}

func TestCriterionIDAndTicketKey(t *testing.T) {
	ticket := ports.Ticket{
		Key: "PAY-142", Title: "Add refunds", Assignee: "bob", Status: "In Progress",
		Criteria: []ports.Criterion{{ID: "AC1", Hash: strings.Repeat("c", 64), Text: "refund cannot exceed payment"}},
	}
	require.NoError(t, ticket.Validate())
	ticket.Key = "pr-acme-pay-demo-7"
	ticket.Criteria[0].ID = "AC0"
	err := ticket.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "key")
	require.Contains(t, err.Error(), "criteria[0]")
}

func TestRoomRequestAndMessage(t *testing.T) {
	require.NoError(t, ports.RoomRequest{ChangeID: "PAY-142", Title: "PAY-142 refunds", Key: "room_open:PAY-142:1:room"}.Validate())
	require.Error(t, ports.RoomRequest{ChangeID: "PAY-142", Title: "", Key: "x"}.Validate())
	m := ports.Message{ID: "msg_1", Room: "room-42", Sender: "sw-88", Text: "heimdall uphold F1", At: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)}
	require.NoError(t, m.Validate())
	m.Sender = ""
	require.Error(t, m.Validate())
}

func TestBlobClassValues(t *testing.T) {
	for _, c := range []ports.BlobClass{ports.BlobLog, ports.BlobDiff, ports.BlobBundle, ports.BlobPack, ports.BlobEvidence} {
		require.True(t, c.Valid(), c)
	}
	require.False(t, ports.BlobClass("video").Valid())
}

func TestTicketChangeNeedsAKeyAndAChangelogEntry(t *testing.T) {
	c := ports.TicketChange{Key: "KAN-7", ChangelogID: "10042", Fields: []string{"status"}, At: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)}
	require.NoError(t, c.Validate())
	c.ChangelogID = ""
	require.Error(t, c.Validate())
}
