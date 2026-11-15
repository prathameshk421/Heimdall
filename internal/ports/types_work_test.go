package ports

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

func TestCheckAndTestJobsValidate(t *testing.T) {
	sha := id.SHA(strings.Repeat("a", 40))
	check := SandboxJob{Kind: SandboxCheck, Repo: "acme/pay", SHA: sha, BaseSHA: id.SHA(strings.Repeat("b", 40)), CheckID: "patterns", Timeout: time.Minute}
	test := SandboxJob{Kind: SandboxTest, Repo: "acme/pay", SHA: sha, Dir: "tests/heimdall/PAY-1", TestKind: "api", Timeout: time.Minute}
	require.NoError(t, check.Validate(), "a check names no directory")
	require.NoError(t, test.Validate())

	tests := []struct {
		name   string
		job    func() SandboxJob
		field  string
		reason string
	}{
		{"a check with no id", func() SandboxJob { j := check; j.CheckID = ""; return j }, "check_id", "lowercase"},
		{"a check id with a shell character", func() SandboxJob { j := check; j.CheckID = "a;rm -rf /"; return j }, "check_id", "lowercase"},
		{"a test with no kind", func() SandboxJob { j := test; j.TestKind = ""; return j }, "test_kind", "lowercase"},
		{"a test with no directory", func() SandboxJob { j := test; j.Dir = ""; return j }, "dir", "relative path"},
		{"a test directory that climbs out", func() SandboxJob { j := test; j.Dir = "../x"; return j }, "dir", "relative path"},
		{"a check id on a test job", func() SandboxJob { j := test; j.CheckID = "patterns"; return j }, "check_id", "only a check job"},
		{"a base tree on a red-first job", func() SandboxJob {
			return SandboxJob{Kind: SandboxRedFirst, Repo: "acme/pay", SHA: sha, BaseSHA: sha, Dir: "tests", Timeout: time.Minute}
		}, "base_sha", "only a check or test"},
		{"a malformed base sha", func() SandboxJob { j := check; j.BaseSHA = "xyz"; return j }, "base_sha", ""},
		{"the removed contract kind", func() SandboxJob { j := check; j.Kind = "contract"; return j }, "kind", "known"},
		{"the removed mutation kind", func() SandboxJob { j := check; j.Kind = "mutation"; return j }, "kind", "known"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.job().Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.field)
			require.Contains(t, err.Error(), tt.reason)
		})
	}
}

func TestSandboxResultBoundsTheSarif(t *testing.T) {
	res := SandboxResult{LogRef: workflow.BlobRef("blob:sha256:" + strings.Repeat("a", 64)), Sarif: make([]byte, MaxJobResult+1)}
	require.ErrorContains(t, res.Validate(), "sarif")
}

func TestTheQueuePriorityIsDataOnTheJobKind(t *testing.T) {
	tests := []struct {
		name string
		job  SandboxJob
		want Priority
	}{
		{"red-first", SandboxJob{Kind: SandboxRedFirst}, PriorityGate},
		{"repro", SandboxJob{Kind: SandboxRepro}, PriorityGate},
		{"a test kind", SandboxJob{Kind: SandboxTest}, PriorityGate},
		{"a profile trial", SandboxJob{Kind: SandboxProfileTrial}, PriorityGate},
		{"a cache warm", SandboxJob{Kind: SandboxWarm}, PriorityOnDemand},
		{"a pipeline check", SandboxJob{Kind: SandboxCheck}, PriorityBackground},
		{"a check asked for on demand", SandboxJob{Kind: SandboxCheck, OnDemand: true}, PriorityOnDemand},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, tt.job.Priority(), tt.name)
	}
	require.Less(t, PriorityGate, PriorityOnDemand)
	require.Less(t, PriorityOnDemand, PriorityBackground)
	require.Less(t, PriorityBackground, PriorityPreview)

	sha := id.SHA(strings.Repeat("a", 40))
	require.Equal(t, sha, SandboxJob{SHA: sha}.HeadSHA())
	require.Equal(t, id.SHA(strings.Repeat("b", 40)), SandboxJob{SHA: sha, Head: id.SHA(strings.Repeat("b", 40))}.HeadSHA())

	job := SandboxJob{Kind: SandboxTest, Repo: "acme/pay", SHA: sha, Dir: "tests", TestKind: "api", Timeout: time.Minute, OnDemand: true}
	require.ErrorContains(t, job.Validate(), "on_demand", "only a check job is asked for on demand")
}
