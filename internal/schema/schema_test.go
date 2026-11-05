package schema_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/deliberation"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/schema"
)

var wantIDs = []string{"FindingIn@1", "Review@1", "TaskEnvelope@1", "TestSubmission@1", "Verdict@1"}

// goldenExamples maps each schema id to the domain golden file whose JSON must be a valid instance of it.
var goldenExamples = map[string][]string{
	"FindingIn@1":      {"../domain/finding/testdata/finding_in.golden.json"},
	"Review@1":         {"../domain/finding/testdata/review.golden.json", "../domain/finding/testdata/review_clean.golden.json"},
	"TaskEnvelope@1":   {"../domain/workflow/testdata/task_envelope.golden.json"},
	"TestSubmission@1": {"../domain/testset/testdata/test_submission.golden.json"},
	"Verdict@1":        {"../domain/deliberation/testdata/verdict.golden.json"},
}

func mustSchema(t *testing.T, id string) map[string]any {
	t.Helper()
	b, err := schema.Schema(id)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}

// at follows a path of object keys, failing the test if one is missing.
func at(t *testing.T, m map[string]any, path ...string) map[string]any {
	t.Helper()
	cur := m
	for _, k := range path {
		next, ok := cur[k].(map[string]any)
		require.True(t, ok, "schema has no object at %s", strings.Join(path, "."))
		cur = next
	}
	return cur
}

func TestRegisteredIDsAreExactlyTheFiveContracts(t *testing.T) {
	require.Equal(t, wantIDs, schema.IDs())
}

func TestAnUnknownIDIsRefused(t *testing.T) {
	_, err := schema.Schema("Nope@1")
	require.Error(t, err)
}

func TestEverySchemaMatchesItsGoldenFile(t *testing.T) {
	for _, id := range wantIDs {
		t.Run(id, func(t *testing.T) {
			got, err := schema.Schema(id)
			require.NoError(t, err)
			want, err := os.ReadFile("testdata/" + id + ".schema.json")
			require.NoError(t, err, "golden file missing; generate it once on purpose and review it")
			require.Equal(t, string(want), string(got), "schema drifted from the reviewed golden file")
		})
	}
}

func TestSchemaGenerationIsDeterministic(t *testing.T) {
	for _, id := range wantIDs {
		first, err := schema.Schema(id)
		require.NoError(t, err)
		for range 5 {
			again, err := schema.Schema(id)
			require.NoError(t, err)
			require.Equal(t, string(first), string(again), id)
		}
	}
}

func TestSchemasAreDraft202012Objects(t *testing.T) {
	for _, id := range wantIDs {
		m := mustSchema(t, id)
		require.Equal(t, "https://json-schema.org/draft/2020-12/schema", m["$schema"], id)
		require.Equal(t, "object", m["type"], id)
	}
}

func TestLimitsInTheSchemaEqualTheGoConstants(t *testing.T) {
	f := mustSchema(t, "FindingIn@1")
	props := at(t, f, "properties")
	require.EqualValues(t, finding.ClaimMinChars, at(t, props, "claim")["minLength"])
	require.EqualValues(t, finding.ClaimMaxChars, at(t, props, "claim")["maxLength"])
	require.EqualValues(t, finding.MaxContextRefs, at(t, props, "context_refs")["maxItems"])
	require.EqualValues(t, finding.MaxEvidence, at(t, props, "evidence")["maxItems"])
	require.EqualValues(t, 1, at(t, props, "evidence")["minItems"])
	require.EqualValues(t, finding.MaxPathBytes, at(t, props, "location", "properties", "file")["maxLength"])

	ev := at(t, props, "evidence", "items", "properties")
	require.EqualValues(t, finding.ReasoningMinChars, at(t, ev, "reasoning")["minLength"])
	require.EqualValues(t, finding.ReasoningMaxChars, at(t, ev, "reasoning")["maxLength"])
	require.EqualValues(t, finding.MaxRefBytes, at(t, ev, "memory_ref")["maxLength"])
	require.EqualValues(t, finding.MaxRefBytes, at(t, ev, "pack_ref")["maxLength"])

	r := mustSchema(t, "Review@1")
	require.EqualValues(t, finding.MaxFindings, at(t, r, "properties", "findings")["maxItems"])

	v := mustSchema(t, "Verdict@1")
	vp := at(t, v, "properties")
	require.EqualValues(t, deliberation.RationaleMinChars, at(t, vp, "rationale")["minLength"])
	require.EqualValues(t, deliberation.RationaleMaxChars, at(t, vp, "rationale")["maxLength"])
	require.EqualValues(t, deliberation.MaxCitations, at(t, vp, "citations")["maxItems"])
	require.EqualValues(t, deliberation.MaxQuoteChars, at(t, vp, "citations", "items", "properties", "quote")["maxLength"])

	ts := mustSchema(t, "TestSubmission@1")
	tp := at(t, ts, "properties")
	require.EqualValues(t, testset.MaxFiles, at(t, tp, "files")["maxItems"])
	require.EqualValues(t, testset.MaxFileBytes, at(t, tp, "files", "items", "properties", "content")["maxLength"])
	require.EqualValues(t, testset.MaxExpectedPaths, at(t, tp, "expected_paths")["maxItems"])
	require.EqualValues(t, testset.MaxNotesChars, at(t, tp, "notes")["maxLength"])
}

func TestRequiredFieldsAppearAsRequired(t *testing.T) {
	cases := map[string][]string{
		"FindingIn@1":      {"claim", "category", "severity", "confidence", "location", "evidence", "context_refs"},
		"Review@1":         {"head_sha", "files_reviewed", "clean", "findings"},
		"Verdict@1":        {"case_id", "decision", "rationale", "citations", "confidence"},
		"TestSubmission@1": {"files", "acceptance_criteria", "expected_paths", "notes"},
	}
	for id, want := range cases {
		req, ok := mustSchema(t, id)["required"].([]any)
		require.True(t, ok, id)
		for _, field := range want {
			require.True(t, slices.Contains(anyStrings(req), field), "%s must require %s", id, field)
		}
	}
}

func anyStrings(xs []any) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.(string))
	}
	return out
}

func TestEnumsInTheSchemaAreTheSetsTheParsersAccept(t *testing.T) {
	f := at(t, mustSchema(t, "FindingIn@1"), "properties")
	require.Equal(t, []string{"correctness", "security", "compat", "ops", "spec_drift", "tests", "style"}, anyStrings(at(t, f, "category")["enum"].([]any)))
	require.Equal(t, []string{"low", "medium", "high", "critical"}, anyStrings(at(t, f, "severity")["enum"].([]any)))
	require.Equal(t, []string{"high", "medium", "low"}, anyStrings(at(t, f, "confidence")["enum"].([]any)))
	ev := at(t, f, "evidence", "items", "properties")
	require.Equal(t, []string{"repro_test", "tool_result", "memory", "pack_ref", "reasoning"}, anyStrings(at(t, ev, "type")["enum"].([]any)))
	for _, c := range anyStrings(at(t, f, "category")["enum"].([]any)) {
		_, err := finding.ParseCategory(c)
		require.NoError(t, err, c)
	}
	dec := at(t, mustSchema(t, "Verdict@1"), "properties", "decision")
	require.Equal(t, []string{"uphold", "dismiss", "amend", "spec_change", "needs_human"}, anyStrings(dec["enum"].([]any)))
}

// TestGoldenExamplesFitTheirSchemas checks the top level of every domain example: each key is a declared property,
// each required property is present, and each enum field holds a listed value. It is NOT full JSON Schema validation:
// no validator is in go.sum, so validation against a real validator is not verified (see doc.go).
func TestGoldenExamplesFitTheirSchemas(t *testing.T) {
	for id, files := range goldenExamples {
		m := mustSchema(t, id)
		props := at(t, m, "properties")
		req, _ := m["required"].([]any)
		for _, file := range files {
			t.Run(id+" "+file, func(t *testing.T) {
				b, err := os.ReadFile(file)
				require.NoError(t, err)
				var inst map[string]any
				require.NoError(t, json.Unmarshal(b, &inst))
				for k := range inst {
					require.Contains(t, props, k, "example key %q is not a property of %s", k, id)
				}
				for _, r := range anyStrings(req) {
					require.Contains(t, inst, r, "example lacks required property %q of %s", r, id)
				}
				for k, v := range inst {
					if enum, ok := props[k].(map[string]any)["enum"].([]any); ok {
						require.True(t, slices.Contains(anyStrings(enum), v.(string)), "%s=%v is not in the schema enum", k, v)
					}
				}
			})
		}
	}
}
