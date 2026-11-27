package bench

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	shaA = "3c33e07c4c233bf61e1414f5acb3cda06ffef1d1"
	shaB = "e2b56b3a384eb40136b2cff13e07e932dd95bf28"
	shaC = "48391ba5eb8c5f49132138e67ec112d180b88f63"
	shaD = "840cb801497147a6d30a568e0874dfefb10867a9"
)

// bugCase is a valid bug case; each test breaks one rule in it.
func bugCase() string {
	return `
  - id: testify-1207
    repo: https://github.com/stretchr/testify
    language: go
    kind: bug
    status: valid
    change: {sha: ` + shaA + `, parent: ` + shaB + `, url: https://github.com/stretchr/testify/pull/1182}
    fix: {sha: ` + shaC + `, parent: ` + shaD + `, url: https://github.com/stretchr/testify/pull/1207}
    test_command: go test ./...
    timeout: 5m
    description: AssertExpectations panics on a mock with no expectations.
    truth:
      - file: mock/mock.go
        lines: ["510-511"]
    sources: [https://github.com/stretchr/testify/pull/1207]
`
}

func cleanCase() string {
	return `
  - id: chi-1051
    repo: https://github.com/go-chi/chi
    language: go
    kind: clean
    status: pending
    change: {sha: ` + shaA + `, parent: ` + shaB + `, url: https://github.com/go-chi/chi/pull/1051}
    test_command: go test ./...
    timeout: 2m
    description: Modernizer refactor to strings.CutPrefix.
    sources: [https://github.com/go-chi/chi/pull/1051]
`
}

func manifest(cases ...string) []byte {
	return []byte("version: 1\ncases:" + strings.Join(cases, ""))
}

func TestParseListsEveryBrokenRuleAtOnce(t *testing.T) {
	body := strings.Replace(bugCase(), "kind: bug", "kind: flaky", 1)
	body = strings.Replace(body, "status: valid", "status: invalid", 1)
	_, err := Parse(manifest(body))
	require.ErrorContains(t, err, `kind "flaky"`)
	require.ErrorContains(t, err, "invalid case needs a reason")
}
