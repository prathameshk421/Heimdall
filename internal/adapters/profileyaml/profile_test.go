package profileyaml_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/profileyaml"
)

var goImage = "registry.example/go@sha256:" + strings.Repeat("a", 64)

// allowlist holds the digest reference the base profile uses and the image names the runner resolves.
var allowlist = []string{goImage, "checks", "browser", "postgres"}

const baseProfile = `languages: [go]
image: IMAGE
acceptance: 'go test {dir}'
seam: api.Handler
ac_tag: '// heimdall:{ac}'
paths:
  tests: tests/heimdall
`

const extendedTail = `tests:
  runs: 3
  kinds:
    api: {run: "pytest {dir} --junitxml=/out/junit.xml"}
    ui:
      image: browser
      serve: "uvicorn app:app --port 8000"
      ready: "http://127.0.0.1:8000/"
      run: "npx playwright test {dir}"
services:
  - {name: db, image: postgres, port: 5432, data: /var/lib/postgresql/data, env: {POSTGRES_PASSWORD: test-only}}
checks:
  - {id: patterns, lens: security, when: ["**/*"], image: checks, run: "opengrep scan /work", baseline: true}
`

func profile(tail string) []byte {
	return []byte(strings.Replace(baseProfile, "IMAGE", goImage, 1) + tail)
}

func TestParsedProfileCarriesChecksKindsServicesAndRuns(t *testing.T) {
	p, err := profileyaml.ParseApprovedProfile(profile(extendedTail), allowlist)
	require.NoError(t, err)
	spec := profileyaml.Spec(p)
	require.NoError(t, spec.Validate())
	require.Equal(t, 3, spec.Runs)
	require.Len(t, spec.Checks, 1)
	require.True(t, spec.Checks[0].Baseline)
	require.Equal(t, "patterns", spec.Checks[0].ID)
	require.Equal(t, []string{"api", "ui"}, []string{spec.Kinds[0].Name, spec.Kinds[1].Name}, "kinds come out sorted by name")
	require.Equal(t, "http://127.0.0.1:8000/", spec.Kinds[1].Ready)
	require.Equal(t, "test-only", spec.Services[0].Env["POSTGRES_PASSWORD"])
	require.Equal(t, 5432, spec.Services[0].Port)
	require.Equal(t, "/var/lib/postgresql/data", spec.Services[0].Data)
}

func TestProfileExtensionsAreRefused(t *testing.T) {
	tests := []struct {
		name    string
		tail    string
		wantErr error
		want    string
	}{
		{"a check image off the allowlist", "checks:\n  - {id: c, lens: security, when: ['**/*'], image: evil, run: x}\n", profileyaml.ErrImageNotAllowed, "evil"},
		{"a check digest off the allowlist", "checks:\n  - {id: c, lens: security, when: ['**/*'], image: 'registry.example/x@sha256:" + strings.Repeat("b", 64) + "', run: x}\n", profileyaml.ErrImageNotAllowed, "registry.example/x"},
		{"a kind image off the allowlist", "tests:\n  runs: 1\n  kinds:\n    ui: {image: evil, run: 'x {dir}'}\n", profileyaml.ErrImageNotAllowed, "evil"},
		{"a service image off the allowlist", "services:\n  - {name: db, image: evil, port: 5432}\n", profileyaml.ErrImageNotAllowed, "evil"},
		{"a kind run without {dir}", "tests:\n  runs: 1\n  kinds:\n    api: {run: 'pytest tests'}\n", nil, "{dir}"},
		{"a ready URL that is not loopback", "tests:\n  runs: 1\n  kinds:\n    ui: {serve: 'x', ready: 'http://example.com:80/', run: 'x {dir}'}\n", nil, "loopback"},
		{"serve with no ready", "tests:\n  runs: 1\n  kinds:\n    ui: {serve: 'x', run: 'x {dir}'}\n", nil, "together"},
		{"kinds with no runs", "tests:\n  kinds:\n    api: {run: 'x {dir}'}\n", nil, "runs"},
		{"an unknown key in a check", "checks:\n  - {id: c, lens: security, when: ['**/*'], image: checks, run: x, privileged: true}\n", nil, "privileged"},
		{"an unknown key in a kind", "tests:\n  runs: 1\n  kinds:\n    api: {run: 'x {dir}', network: host}\n", nil, "network"},
		{"an unknown key in a service", "services:\n  - {name: db, image: postgres, ports: ['5432:5432']}\n", nil, "ports"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := profileyaml.ParseApprovedProfile(profile(tt.tail), allowlist)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}
