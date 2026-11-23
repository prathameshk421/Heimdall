package external_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
)

// TestClaimStripsBidiAndControlCharacters: a bot comment is untrusted text. Format characters (BiDi overrides,
// zero-width characters) are removed and control characters become spaces before the claim is read, so the claim
// cannot reorder what a person reads or hide text (SEC-12). Newlines and tabs stay: they are the line structure.
func TestClaimStripsBidiAndControlCharacters(t *testing.T) {
	t.Parallel()
	pr := external.PR{
		Head:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Sources: []string{"copilot"},
	}
	cases := []struct{ name, body, want string }{
		{"bidi override is removed, logical order stays",
			"**Check the error.**\n\nIt is \u202edropped\u202c here.",
			"Check the error. It is dropped here."},
		{"zero-width and soft hyphen are removed",
			"**Check the error.**\n\nIt is dr\u200bop\u200cped\u00ad here.",
			"Check the error. It is dropped here."},
		{"control characters become spaces",
			"**Check the error.**\n\nIt is\x07dropped\x1bhere.",
			"Check the error. It is dropped here."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := pr.Ingest(external.Comment{
				ID:        1,
				Kind:      external.KindReviewComment,
				UserType:  "Bot",
				UserLogin: "copilot-pull-request-reviewer[bot]",
				Path:      "pkg/a.go",
				Line:      10,
				Side:      string(external.SideRight),
				CommitID:  pr.Head,
				Body:      tc.body,
			})
			require.Equal(t, external.OutcomeCandidate, res.Outcome)
			require.Equal(t, tc.want, res.Candidate.Claim)
		})
	}
}
