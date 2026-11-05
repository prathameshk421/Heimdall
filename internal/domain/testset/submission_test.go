package testset_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
)

func readGolden(t testing.TB) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/test_submission.golden.json")
	require.NoError(t, err)
	return b
}

func goldenSubmission(t testing.TB) testset.Submission {
	t.Helper()
	var s testset.Submission
	require.NoError(t, json.Unmarshal(readGolden(t), &s))
	return s
}

// expect is the ticket and the test directory the golden submission is checked against.
var expect = testset.Expect{TestsDir: "tests/heimdall", ACIDs: []string{"AC1", "AC2"}}

func TestSubmissionGoldenDecodesValidatesAndEncodesItBack(t *testing.T) {
	s := goldenSubmission(t)
	require.NoError(t, s.Validate(expect))
	require.Len(t, s.Files, 2)
	require.Equal(t, []string{"TestRefundResponseKeepsRefundRef"}, s.AcceptanceCriteria["AC1"])
	out, err := json.Marshal(s)
	require.NoError(t, err)
	require.JSONEq(t, string(readGolden(t)), string(out), "golden JSON: the wire format is frozen")
}

func TestSubmissionValidateRefusesWhatCannotBeCommitted(t *testing.T) {
	long := strings.Repeat("n", 2001)
	cases := []struct {
		name   string
		mutate func(*testset.Submission)
		want   string
	}{
		{"no files", func(s *testset.Submission) { s.Files = nil }, "files needs at least 1 entry"},
		{"a file outside the tests directory", func(s *testset.Submission) { s.Files[0].Path = "internal/refunds/refunds_test.go" }, "files[0].path"},
		{"a file that only shares the prefix", func(s *testset.Submission) { s.Files[0].Path = "tests/heimdallx/a_test.go" }, "files[0].path"},
		{"a file that is the tests directory itself", func(s *testset.Submission) { s.Files[0].Path = "tests/heimdall" }, "files[0].path"},
		{"a file path that escapes", func(s *testset.Submission) { s.Files[0].Path = "tests/heimdall/../../etc/x_test.go" }, "files[0].path"},
		{"an absolute file path", func(s *testset.Submission) { s.Files[0].Path = "/tests/heimdall/a_test.go" }, "files[0].path"},
		{"the same file twice", func(s *testset.Submission) { s.Files[1].Path = s.Files[0].Path }, "files[1].path"},
		{"a file with no content", func(s *testset.Submission) { s.Files[0].Content = " \n " }, "files[0].content"},
		{"a file over the size limit", func(s *testset.Submission) { s.Files[0].Content = strings.Repeat("x", testset.MaxFileBytes+1) }, "files[0].content"},
		{"too many files", func(s *testset.Submission) {
			s.Files = nil
			for i := range testset.MaxFiles + 1 {
				s.Files = append(s.Files, testset.File{Path: "tests/heimdall/PAY-142/f" + string(rune('a'+i%26)) + strings.Repeat("x", i) + "_test.go", Content: "package x"})
			}
		}, "files has 33 entries, at most 32"},
		{"an AC with no test", func(s *testset.Submission) { s.AcceptanceCriteria["AC2"] = nil }, "acceptance_criteria[AC2]"},
		{"an AC that the ticket does not have", func(s *testset.Submission) { s.AcceptanceCriteria["AC9"] = []string{"TestX"} }, "acceptance_criteria[AC9]"},
		{"an AC id with a bad form", func(s *testset.Submission) { s.AcceptanceCriteria["ac1"] = []string{"TestX"} }, "acceptance_criteria[ac1]"},
		{"a missing AC", func(s *testset.Submission) { delete(s.AcceptanceCriteria, "AC1") }, "acceptance_criteria[AC1]"},
		{"a blank test name", func(s *testset.Submission) { s.AcceptanceCriteria["AC1"] = []string{"  "} }, "acceptance_criteria[AC1][0]"},
		{"a test name over 200 characters", func(s *testset.Submission) { s.AcceptanceCriteria["AC1"] = []string{strings.Repeat("T", 201)} }, "acceptance_criteria[AC1][0]"},
		{"an expected path under the tests directory", func(s *testset.Submission) { s.ExpectedPaths = []string{"tests/heimdall/PAY-142/x_test.go"} }, "expected_paths[0]"},
		{"an expected path that escapes", func(s *testset.Submission) { s.ExpectedPaths = []string{"../secrets"} }, "expected_paths[0]"},
		{"a duplicate expected path", func(s *testset.Submission) { s.ExpectedPaths = []string{"api/openapi.yaml", "api/openapi.yaml"} }, "expected_paths[1]"},
		{"notes over 2000 characters", func(s *testset.Submission) { s.Notes = long }, "notes must be at most 2000 characters, got 2001"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := goldenSubmission(t)
			c.mutate(&s)
			err := s.Validate(expect)
			require.Error(t, err)
			require.Contains(t, err.Error(), c.want)
		})
	}
}

func TestSubmissionValidateRefusesABadTestsDirectory(t *testing.T) {
	s := goldenSubmission(t)
	require.Error(t, s.Validate(testset.Expect{TestsDir: "../tests", ACIDs: []string{"AC1", "AC2"}}))
	require.Error(t, s.Validate(testset.Expect{TestsDir: "", ACIDs: []string{"AC1", "AC2"}}))
}

func TestSubmissionValidateReportsEveryProblemAtOnce(t *testing.T) {
	s := goldenSubmission(t)
	s.Files[0].Path = "api/x_test.go"
	s.AcceptanceCriteria["AC2"] = nil
	s.ExpectedPaths = []string{"../x"}
	err := s.Validate(expect)
	require.Error(t, err)
	for _, want := range []string{"files[0].path", "acceptance_criteria[AC2]", "expected_paths[0]"} {
		require.Contains(t, err.Error(), want)
	}
}

func FuzzDecodeSubmission(f *testing.F) {
	f.Add(readGolden(f))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"files":[{"path":"tests/heimdall/a_test.go","content":"x"}],"acceptance_criteria":{"AC1":["TestA"],"AC2":["TestB"]}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var s testset.Submission
		if json.Unmarshal(data, &s) != nil || s.Validate(expect) != nil {
			return
		}
		out, err := json.Marshal(s)
		require.NoError(t, err)
		var again testset.Submission
		require.NoError(t, json.Unmarshal(out, &again), "accepted input must re-decode: %s", out)
		require.NoError(t, again.Validate(expect), "accepted input must re-validate: %s", out)
		require.Equal(t, s, again)
	})
}

func TestSubmissionLimitsAreInclusive(t *testing.T) {
	s := goldenSubmission(t)
	s.Files = nil
	for i := range testset.MaxFiles {
		s.Files = append(s.Files, testset.File{Path: "tests/heimdall/PAY-142/f" + strings.Repeat("x", i) + "_test.go", Content: "package x"})
	}
	require.NoError(t, s.Validate(expect), "32 files is the maximum")

	s = goldenSubmission(t)
	s.Files[0].Content = strings.Repeat("x", testset.MaxFileBytes)
	require.NoError(t, s.Validate(expect), "a file of exactly the size limit is accepted")

	s = goldenSubmission(t)
	s.Notes = strings.Repeat("n", testset.MaxNotesChars)
	require.NoError(t, s.Validate(expect), "notes of exactly the limit are accepted")

	s = goldenSubmission(t)
	s.AcceptanceCriteria["AC1"] = []string{strings.Repeat("T", testset.MaxTestNameChars)}
	require.NoError(t, s.Validate(expect), "a test name of exactly the limit is accepted")
}

func TestAnAcceptanceCriterionIdMustLookLikeAC2EvenWhenTheTicketHasIt(t *testing.T) {
	s := goldenSubmission(t)
	s.AcceptanceCriteria = map[string][]string{"AC01": {"TestX"}}
	err := s.Validate(testset.Expect{TestsDir: "tests/heimdall", ACIDs: []string{"AC01"}})
	require.ErrorContains(t, err, "acceptance_criteria[AC01]: the id must look like AC2")
}
