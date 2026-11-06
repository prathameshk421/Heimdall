package ports

import (
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/stretchr/testify/require"
)

var digestImage = "registry.example/go@sha256:" + strings.Repeat("a", 64)

func validProfileSpec() ProfileSpec {
	return ProfileSpec{
		Languages:  []string{"go"},
		Image:      digestImage,
		Acceptance: "go test -json {dir}",
		Seam:       "backend.main:app",
		ACTag:      "// heimdall:{ac}",
		FetchHosts: []string{"proxy.golang.org"},
		TestsDir:   "tests/heimdall",
	}
}

func TestProfileSpecValidation(t *testing.T) {
	t.Run("a complete spec validates", func(t *testing.T) {
		require.NoError(t, validProfileSpec().Validate())
	})
	t.Run("every required presence problem is named", func(t *testing.T) {
		empty := ProfileSpec{}
		err := empty.Validate()
		require.Error(t, err)
		for _, key := range []string{"languages", "acceptance"} {
			require.Contains(t, err.Error(), key)
		}
	})
	t.Run("the acceptance command must carry exactly one dir placeholder", func(t *testing.T) {
		spec := validProfileSpec()
		spec.Acceptance = "go test ./..."
		require.Error(t, spec.Validate())
		spec.Acceptance = "go test {dir} {dir}"
		require.Error(t, spec.Validate())
	})
	t.Run("an unfixed image is refused", func(t *testing.T) {
		spec := validProfileSpec()
		spec.Image = "registry.example/go:latest"
		require.Error(t, spec.Validate())
		spec.Image = "registry.example/go@sha256:" + strings.Repeat("z", 63)
		require.Error(t, spec.Validate())
	})
	t.Run("fetch hosts are bounded", func(t *testing.T) {
		spec := validProfileSpec()
		spec.FetchHosts = make([]string, 9)
		for i := range spec.FetchHosts {
			spec.FetchHosts[i] = "registry.example"
		}
		err := spec.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "eight")
	})
	t.Run("tests_dir must stay inside the repository", func(t *testing.T) {
		spec := validProfileSpec()
		spec.TestsDir = "../escape"
		require.Error(t, spec.Validate())
	})
}

func validProposal() ProfileProposal {
	return ProfileProposal{
		ID:        strings.Repeat("a", 64),
		Repo:      "acme/pay",
		State:     ProfileProposed,
		Author:    "pay-lead",
		Artifact:  []byte("languages: [go]\n"),
		Spec:      validProfileSpec(),
		CreatedAt: time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC),
	}
}

func TestProfileProposalValidation(t *testing.T) {
	t.Run("a proposal validates and states round trip", func(t *testing.T) {
		require.NoError(t, validProposal().Validate())
		require.False(t, ProfileState("rejected").Valid())
	})
	t.Run("the id must be 64 lowercase hex", func(t *testing.T) {
		p := validProposal()
		p.ID = strings.Repeat("A", 64)
		err := p.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "id")
	})
	t.Run("approved and promoted profiles name their approver", func(t *testing.T) {
		p := validProposal()
		p.State = ProfileApproved
		require.Error(t, p.Validate())
		p.Approver = "sec-reviewer"
		p.ApprovedAt = time.Date(2026, 11, 6, 8, 30, 0, 0, time.UTC)
		require.NoError(t, p.Validate())
	})
	t.Run("a promoted profile is stamped", func(t *testing.T) {
		p := validProposal()
		p.State = ProfilePromoted
		p.Approver = "sec-reviewer"
		require.Error(t, p.Validate())
		p.ApprovedAt = time.Date(2026, 11, 6, 8, 30, 0, 0, time.UTC)
		p.PromotedAt = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
		p.Trial = ProfileTrial{Outcome: TrialPassed, SHA: id.SHA(strings.Repeat("9", 40)), LogRef: workflow.BlobRef("blob:sha256:" + strings.Repeat("b", 64))}
		require.NoError(t, p.Validate())
	})
	t.Run("artifact stays bounded", func(t *testing.T) {
		p := validProposal()
		p.Artifact = nil
		require.Error(t, p.Validate())
	})
}
