package bench

import (
	"os"
	"strings"
	"testing"
	"time"

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

func TestParseAcceptsValidBugAndCleanCases(t *testing.T) {
	m, err := Parse(manifest(bugCase(), cleanCase()))
	require.NoError(t, err)
	require.Len(t, m.Cases, 2)
	bug := m.Cases[0]
	require.Equal(t, KindBug, bug.Kind)
	require.Equal(t, StatusValid, bug.Status)
	require.Equal(t, 5*time.Minute, bug.Timeout)
	require.NotNil(t, bug.Fix)
	require.Equal(t, shaC, bug.Fix.SHA)
	require.Equal(t, []FileTruth{{File: "mock/mock.go", Lines: []string{"510-511"}}}, bug.Truth)
	require.Equal(t, "stretchr/testify", bug.RepoSlug())
	require.Nil(t, m.Cases[1].Fix)
}

func TestParseRejectsBrokenRules(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"missing change sha", strings.Replace(bugCase(), "sha: "+shaA+", ", "", 1), "change.sha"},
		{"short sha", strings.Replace(bugCase(), shaC, "48391ba", 1), "fix.sha"},
		{"upper-case sha", strings.Replace(bugCase(), shaD, strings.ToUpper(shaD), 1), "fix.parent"},
		{"change is its own parent", strings.Replace(bugCase(), shaB, shaA, 1), "own parent"},
		{"unknown kind", strings.Replace(bugCase(), "kind: bug", "kind: flaky", 1), `kind "flaky"`},
		{"bug case without fix", strings.Replace(bugCase(), "    fix: {sha: "+shaC+", parent: "+shaD+", url: https://github.com/stretchr/testify/pull/1207}\n", "", 1), "bug case needs a fix"},
		{"clean case with fix", strings.Replace(cleanCase(), "    test_command", "    fix: {sha: "+shaC+", parent: "+shaD+"}\n    test_command", 1), "clean case has no fix"},
		{"clean case with truth", strings.Replace(cleanCase(), "    sources", "    truth: [{file: a.go, lines: [\"1\"]}]\n    sources", 1), "clean case has no truth"},
		{"unknown status", strings.Replace(bugCase(), "status: valid", "status: done", 1), `status "done"`},
		{"invalid without reason", strings.Replace(bugCase(), "status: valid", "status: invalid", 1), "invalid case needs a reason"},
		{"valid with reason", strings.Replace(bugCase(), "status: valid", "status: valid\n    reason: because", 1), "only an invalid case has a reason"},
		{"valid bug without truth", strings.Replace(bugCase(), "    truth:\n      - file: mock/mock.go\n        lines: [\"510-511\"]\n", "", 1), "valid bug case needs truth"},
		{"bad truth span", strings.Replace(bugCase(), `"510-511"`, `"511-510"`, 1), "line span"},
		{"zero truth line", strings.Replace(bugCase(), `"510-511"`, `"0"`, 1), "line span"},
		{"truth without file", strings.Replace(bugCase(), "file: mock/mock.go", "file: \"\"", 1), "truth file"},
		{"unknown language", strings.Replace(bugCase(), "language: go", "language: cobol", 1), `language "cobol"`},
		{"repo not on GitHub", strings.Replace(bugCase(), "repo: https://github.com/stretchr/testify", "repo: git@github.com:stretchr/testify", 1), "repo"},
		{"no timeout", strings.Replace(bugCase(), "    timeout: 5m\n", "", 1), "timeout"},
		{"timeout too long", strings.Replace(bugCase(), "timeout: 5m", "timeout: 2h", 1), "timeout"},
		{"no test command", strings.Replace(bugCase(), "test_command: go test ./...", "test_command: \"\"", 1), "test_command"},
		{"no description", strings.Replace(bugCase(), "description: AssertExpectations panics on a mock with no expectations.", "description: \"\"", 1), "description"},
		{"no sources", strings.Replace(bugCase(), "sources: [https://github.com/stretchr/testify/pull/1207]", "sources: []", 1), "sources"},
		{"source not https", strings.Replace(bugCase(), "sources: [https://github.com/stretchr/testify/pull/1207]", "sources: [ftp://x]", 1), "source"},
		{"bad id", strings.Replace(bugCase(), "id: testify-1207", "id: Testify 1207", 1), "id"},
		{"unknown field", strings.Replace(bugCase(), "    timeout: 5m\n", "    timeout: 5m\n    budget: 3\n", 1), "budget"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(manifest(tt.body))
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestParseRejectsDuplicateIDsAndWrongVersion(t *testing.T) {
	_, err := Parse(manifest(bugCase(), bugCase()))
	require.ErrorContains(t, err, `duplicate id "testify-1207"`)

	_, err = Parse([]byte("version: 2\ncases:" + bugCase()))
	require.ErrorContains(t, err, "version")

	_, err = Parse([]byte("version: 1\ncases: []\n"))
	require.ErrorContains(t, err, "no cases")
}

func TestParseListsEveryBrokenRuleAtOnce(t *testing.T) {
	body := strings.Replace(bugCase(), "kind: bug", "kind: flaky", 1)
	body = strings.Replace(body, "status: valid", "status: invalid", 1)
	_, err := Parse(manifest(body))
	require.ErrorContains(t, err, `kind "flaky"`)
	require.ErrorContains(t, err, "invalid case needs a reason")
}

func TestPendingBugCaseMayLackTruth(t *testing.T) {
	body := strings.Replace(bugCase(), "status: valid", "status: pending", 1)
	body = strings.Replace(body, "    truth:\n      - file: mock/mock.go\n        lines: [\"510-511\"]\n", "", 1)
	m, err := Parse(manifest(body))
	require.NoError(t, err)
	require.Empty(t, m.Cases[0].Truth)
}

// TestRealManifestParses: the dataset in testdata/bench passes every rule.
func TestRealManifestParses(t *testing.T) {
	b, err := os.ReadFile("../../testdata/bench/cases.yaml")
	require.NoError(t, err)
	m, err := Parse(b)
	require.NoError(t, err)
	require.NotEmpty(t, m.Cases)
}
