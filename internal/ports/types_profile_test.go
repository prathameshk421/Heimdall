package ports

import (
	"strconv"
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

func validExtendedSpec() ProfileSpec {
	spec := validProfileSpec()
	spec.Runs = 3
	spec.Checks = []ProfileCheck{{
		ID: "patterns", Lens: "security", When: []string{"**/*"}, Image: "checks",
		Run: "opengrep scan --sarif-output /out/result.sarif /work", Baseline: true,
	}}
	spec.Kinds = []ProfileKind{
		{Name: "api", Run: "pytest {dir} --junitxml=/out/junit.xml"},
		{Name: "ui", Image: "browser", Serve: "uvicorn app:app --port 8000", Ready: "http://127.0.0.1:8000/healthz", Run: "npx playwright test {dir}"},
	}
	spec.Services = []ProfileService{{Name: "db", Image: "postgres", Env: map[string]string{"POSTGRES_PASSWORD": "test-only"}}}
	return spec
}

func TestProfileExtensionsValidate(t *testing.T) {
	require.NoError(t, validExtendedSpec().Validate())
	require.NoError(t, validProfileSpec().ValidateExtensions(), "a profile with no extensions stays valid")

	tests := []struct {
		name   string
		mutate func(*ProfileSpec)
		field  string
	}{
		{"a kind run with no {dir}", func(s *ProfileSpec) { s.Kinds[0].Run = "pytest tests" }, "kinds[0]: run"},
		{"a kind run with two {dir}", func(s *ProfileSpec) { s.Kinds[0].Run = "pytest {dir} {dir}" }, "kinds[0]: run"},
		{"serve without ready", func(s *ProfileSpec) { s.Kinds[1].Ready = "" }, "kinds[1]: serve"},
		{"ready without serve", func(s *ProfileSpec) { s.Kinds[0].Ready = "http://127.0.0.1:8000/" }, "kinds[0]: serve"},
		{"ready on another host", func(s *ProfileSpec) { s.Kinds[1].Ready = "http://example.com:8000/" }, "kinds[1]: ready"},
		{"ready on a private address", func(s *ProfileSpec) { s.Kinds[1].Ready = "http://10.0.0.5:8000/" }, "kinds[1]: ready"},
		{"ready on the metadata address", func(s *ProfileSpec) { s.Kinds[1].Ready = "http://169.254.169.254:80/" }, "kinds[1]: ready"},
		{"ready with userinfo trick", func(s *ProfileSpec) { s.Kinds[1].Ready = "http://127.0.0.1@evil.example:8000/" }, "kinds[1]: ready"},
		{"ready with no port", func(s *ProfileSpec) { s.Kinds[1].Ready = "http://127.0.0.1/" }, "kinds[1]: ready"},
		{"ready that is not http", func(s *ProfileSpec) { s.Kinds[1].Ready = "ftp://127.0.0.1:21/" }, "kinds[1]: ready"},
		{"a run with a line break", func(s *ProfileSpec) { s.Kinds[0].Run = "pytest {dir}\nrm -rf /" }, "kinds[0]: run"},
		{"runs missing while kinds exist", func(s *ProfileSpec) { s.Runs = 0 }, "runs"},
		{"runs too large", func(s *ProfileSpec) { s.Runs = 11 }, "runs"},
		{"a check with {dir}", func(s *ProfileSpec) { s.Checks[0].Run = "scan {dir}" }, "checks[0]: run"},
		{"a check glob that climbs out", func(s *ProfileSpec) { s.Checks[0].When = []string{"../**"} }, "checks[0]: when[0]"},
		{"an absolute check glob", func(s *ProfileSpec) { s.Checks[0].When = []string{"/etc/**"} }, "checks[0]: when[0]"},
		{"a check with no glob", func(s *ProfileSpec) { s.Checks[0].When = nil }, "checks[0]: when"},
		{"a check image that is neither name nor digest", func(s *ProfileSpec) { s.Checks[0].Image = "checks:latest" }, "checks[0]: image"},
		{"a bad lens", func(s *ProfileSpec) { s.Checks[0].Lens = "Security!" }, "checks[0]: lens"},
		{"a duplicate check id", func(s *ProfileSpec) { s.Checks = append(s.Checks, s.Checks[0]) }, "is used twice"},
		{"a duplicate kind name", func(s *ProfileSpec) { s.Kinds[1].Name = "api" }, "is used twice"},
		{"a service env name in lower case", func(s *ProfileSpec) { s.Services[0].Env = map[string]string{"password": "x"} }, "services[0]: env.password"},
		{"a service env value with a line break", func(s *ProfileSpec) { s.Services[0].Env["POSTGRES_PASSWORD"] = "a\nb" }, "services[0]: env.POSTGRES_PASSWORD"},
		{"too many checks", func(s *ProfileSpec) {
			s.Checks = nil
			for i := range 33 {
				c := validExtendedSpec().Checks[0]
				c.ID = "c" + strconv.Itoa(i)
				s.Checks = append(s.Checks, c)
			}
		}, "at most 32"},
		{"too many services", func(s *ProfileSpec) {
			s.Services = nil
			for i := range 5 {
				s.Services = append(s.Services, ProfileService{Name: "s" + strconv.Itoa(i), Image: "postgres"})
			}
		}, "at most 4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := validExtendedSpec()
			tt.mutate(&spec)
			err := spec.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.field)
		})
	}
}
