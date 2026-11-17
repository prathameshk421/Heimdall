package schema_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/deliberation"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
)

// placeholder matches the words a skill must never carry into a run.
var placeholder = regexp.MustCompile(`(?i)\b(todo|tbd)\b`)

// readText reads a file the skills test needs and fails the test when it is missing.
func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	return string(b)
}

// exampleFiles returns the JSON examples of one skill, and insists there are at least three of them.
func exampleFiles(t *testing.T, skill string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "examples", skill, "*.json"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 3, "testdata/examples/%s needs at least 3 JSON examples", skill)
	return files
}

// decodeExample decodes one example the way the MCP server decodes an agent's submission: an undeclared field is an
// error, not something silently dropped. It returns the raw bytes for the schema check.
func decodeExample(t *testing.T, file string, v any) []byte {
	t.Helper()
	b, err := os.ReadFile(file)
	require.NoError(t, err)
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	require.NoError(t, dec.Decode(v), "%s does not decode as its contract", file)
	return b
}

// fitsTopLevel applies the check the package's own golden test uses: every key is a declared property of the schema,
// every required property is present, and every enum field holds a listed value. It is a structural check, not JSON
// Schema validation: no validator is in go.sum (see doc.go). The domain Validate methods carry the rest.
func fitsTopLevel(t *testing.T, id string, b []byte) {
	t.Helper()
	var inst map[string]any
	require.NoError(t, json.Unmarshal(b, &inst))
	m := mustSchema(t, id)
	props := at(t, m, "properties")
	req, _ := m["required"].([]any)
	for k := range inst {
		require.Contains(t, props, k, "example key %q is not a property of %s", k, id)
	}
	for _, r := range anyStrings(req) {
		require.Contains(t, inst, r, "example lacks required property %q of %s", r, id)
	}
	for k, v := range inst {
		if enum, ok := props[k].(map[string]any)["enum"].([]any); ok {
			require.Contains(t, anyStrings(enum), v, "%s=%v is not in the schema enum", k, v)
		}
	}
}

func TestReviewerExamplesAreValidReviewsOfTheSampleDiff(t *testing.T) {
	sample := readText(t, filepath.Join("testdata", "examples", "reviewer", "sample.md"))
	for _, file := range exampleFiles(t, "reviewer") {
		t.Run(filepath.Base(file), func(t *testing.T) {
			var review finding.Review
			raw := decodeExample(t, file, &review)
			require.NoError(t, review.Validate(), "%s is not a valid Review@1", file)
			fitsTopLevel(t, "Review@1", raw)
			require.Contains(t, sample, string(review.HeadSHA), "%s must review the sample diff's head commit", file)
			for _, name := range review.FilesReviewed {
				require.Contains(t, sample, name, "%s reviewed %s, which the sample does not describe", file, name)
			}
			for _, f := range review.Findings {
				require.Contains(t, sample, f.Location.File, "%s files a finding on %s, which the sample does not describe", file, f.Location.File)
			}
		})
	}
}

func TestResolverExamplesAreValidVerdictsOnTheSampleCase(t *testing.T) {
	sample := readText(t, filepath.Join("testdata", "examples", "resolver", "sample.md"))
	seen := map[deliberation.Decision]bool{}
	for _, file := range exampleFiles(t, "resolver") {
		t.Run(filepath.Base(file), func(t *testing.T) {
			var verdict deliberation.Verdict
			raw := decodeExample(t, file, &verdict)
			require.NoError(t, verdict.Validate(), "%s is not a valid Verdict@1", file)
			fitsTopLevel(t, "Verdict@1", raw)
			seen[verdict.Decision] = true
			require.Contains(t, sample, string(verdict.CaseID), "%s must settle the sample case", file)
			for _, c := range verdict.Citations {
				require.Contains(t, sample, c.Ref, "%s cites %s, which the sample does not describe", file, c.Ref)
			}
		})
	}
	for _, want := range []deliberation.Decision{deliberation.DecisionUphold, deliberation.DecisionDismiss, deliberation.DecisionNeedsHuman} {
		require.True(t, seen[want], "the resolver examples must include a %s verdict", want)
	}
}

// The skill files of the kind table carry no em dash and no placeholder words. That they stay short and name only the
// tools of their kind is checked by workflow.CheckKinds (internal/transport/mcp).
func TestSkillFilesCarryNoEmDashAndNoPlaceholderWords(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "skills", "*.md"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, f := range files {
		body := readText(t, f)
		require.NotContains(t, body, "\u2014", "%s must not use an em dash", f)
		require.NotRegexp(t, placeholder, body, "%s must not contain TODO or TBD", f)
	}
}
