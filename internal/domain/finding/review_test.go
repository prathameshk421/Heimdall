package finding_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
)

func goldenReview(t testing.TB, name string) finding.Review {
	t.Helper()
	var r finding.Review
	require.NoError(t, json.Unmarshal(read(t, name), &r))
	return r
}

func TestReviewGoldensDecodeValidateAndEncodeBack(t *testing.T) {
	for _, name := range []string{"review.golden.json", "review_clean.golden.json"} {
		t.Run(name, func(t *testing.T) {
			r := goldenReview(t, name)
			require.NoError(t, r.Validate())
			out, err := json.Marshal(r)
			require.NoError(t, err)
			require.JSONEq(t, string(read(t, name)), string(out), "golden JSON: the wire format is frozen")
		})
	}
}

func TestReviewValidateRefusesWhatCannotBeJudged(t *testing.T) {
	cases := []struct {
		name   string
		golden string
		mutate func(*finding.Review)
		want   string
	}{
		{"findings and clean together", "review.golden.json", func(r *finding.Review) { r.Clean = true }, "clean is true but findings has 1 entry"},
		{"neither findings nor clean", "review_clean.golden.json", func(r *finding.Review) { r.Clean = false }, "findings is empty and clean is false"},
		{"no files reviewed", "review.golden.json", func(r *finding.Review) { r.FilesReviewed = nil }, "files_reviewed needs at least 1 entry"},
		{"a file path that escapes", "review.golden.json", func(r *finding.Review) { r.FilesReviewed = []string{"../x.go"} }, "files_reviewed[0]"},
		{"a duplicate file", "review.golden.json", func(r *finding.Review) { r.FilesReviewed = []string{"a.go", "a.go"} }, "files_reviewed[1]"},
		{"no head sha", "review.golden.json", func(r *finding.Review) { r.HeadSHA = "" }, "head_sha"},
		{"a bad finding", "review.golden.json", func(r *finding.Review) { r.Findings[0].Claim = "x" }, "findings[0].claim"},
		{"a bad rule verdict", "review.golden.json", func(r *finding.Review) {
			r.RuleChecks = []finding.RuleCheck{{Card: "refunds.md", Verdict: "maybe"}}
		}, "rule_checks[0]"},
		{"a blank rule card", "review.golden.json", func(r *finding.Review) {
			r.RuleChecks = []finding.RuleCheck{{Card: " ", Verdict: finding.RuleHolds}}
		}, "rule_checks[0].card"},
		{"too many rule checks", "review.golden.json", func(r *finding.Review) {
			r.RuleChecks = make([]finding.RuleCheck, 13)
			for i := range r.RuleChecks {
				r.RuleChecks[i] = finding.RuleCheck{Card: "a.md", Verdict: finding.RuleHolds}
			}
		}, "rule_checks has 13 entries, at most 12"},
		{"too many findings", "review.golden.json", func(r *finding.Review) {
			r.Findings = make([]finding.Input, 51)
			for i := range r.Findings {
				r.Findings[i] = goldenFinding(t)
			}
		}, "findings has 51 entries, at most 50"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := goldenReview(t, c.golden)
			c.mutate(&r)
			err := r.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), c.want)
		})
	}
}

func TestReviewRefusesABadHeadSHAWhenDecoded(t *testing.T) {
	g := strings.Replace(string(read(t, "review.golden.json")), "9f1c2ab0c3d4e5f60718293a4b5c6d7e8f901234", "XYZ", 1)
	var r finding.Review
	err := json.Unmarshal([]byte(g), &r)
	require.Error(t, err)
}

func TestReviewCoversReportsWhetherAFileWasReviewed(t *testing.T) {
	r := goldenReview(t, "review.golden.json")
	require.True(t, r.Reviewed("api/openapi.yaml"))
	require.False(t, r.Reviewed("main.go"))
}
