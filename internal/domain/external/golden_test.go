package external_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

type golden struct {
	Comment apiComment `json:"comment"`
	Want    struct {
		Outcome   string                         `json:"outcome"`
		Tool      external.Tool                  `json:"tool"`
		Candidate external.Candidate             `json:"candidate"`
		Drop      struct{ Reason, Cause string } `json:"drop"`
	} `json:"want"`
}

// TestGoldens: each hand-made comment of each tool parses to exactly the hand-written candidate or drop.
func TestGoldens(t *testing.T) {
	files, err := filepath.Glob("testdata/*/*.json")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 10)
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			var g golden
			readFile(t, f, &g)
			got := newPR().Ingest(g.Comment.comment())
			require.Equal(t, external.Outcome(g.Want.Outcome), got.Outcome, "outcome: %+v", got)
			require.Equal(t, g.Want.Tool, got.Tool)
			require.Equal(t, g.Comment.ID, got.CommentID)
			switch got.Outcome {
			case external.OutcomeCandidate:
				require.Equal(t, g.Want.Candidate, got.Candidate)
			case external.OutcomeDropped:
				require.Equal(t, proof.DropReason(g.Want.Drop.Reason), got.Drop.Reason)
				require.Equal(t, external.Cause(g.Want.Drop.Cause), got.Drop.Cause)
			case external.OutcomeIgnored:
				t.Fatalf("golden %s was ignored: %s", f, got.Ignore)
			}
		})
	}
}

// realGolden is one comment captured from a public pull request, with the part of the result that a reader of the
// comment can check by hand. The bodies were read with read-only API calls from pingdotgg/t3code and are listed in
// docs/product/research/13-t3code-bot-comments.md.
type realGolden struct {
	Comment apiComment `json:"comment"`
	Want    struct {
		Tool           external.Tool `json:"tool"`
		Severity       string        `json:"severity"`
		SeverityStated bool          `json:"severity_stated"`
		Outdated       bool          `json:"outdated"`
		ClaimContains  []string      `json:"claim_contains"`
		ClaimExcludes  []string      `json:"claim_excludes"`
	} `json:"want"`
}

// TestRealGoldens: a comment that a tool really posted on a public pull request parses to the tool, the severity the
// body states, the anchor it deserves, a claim that keeps the finding and loses the tool's own noise. This is the
// evidence that the formats were read and not guessed.
func TestRealGoldens(t *testing.T) {
	files, err := filepath.Glob("testdata/real/bodies/*/*.json")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 10, "one captured comment per tool")
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			var g realGolden
			readFile(t, f, &g)
			got := newPR().Ingest(g.Comment.comment())
			require.Equal(t, external.OutcomeCandidate, got.Outcome, "%+v", got)
			require.Equal(t, g.Want.Tool, got.Tool)
			require.Equal(t, g.Want.Severity, string(got.Candidate.Severity))
			require.Equal(t, g.Want.SeverityStated, got.Candidate.SeverityStated)
			require.Equal(t, g.Want.Outdated, got.Candidate.Outdated)
			for _, want := range g.Want.ClaimContains {
				require.Contains(t, got.Candidate.Claim, want)
			}
			for _, bad := range g.Want.ClaimExcludes {
				require.NotContains(t, got.Candidate.Claim, bad)
			}
			require.NotContains(t, got.Candidate.Claim, "\n", "a claim is one line")
			require.Equal(t, strings.TrimSpace(got.Candidate.Claim), got.Candidate.Claim)
		})
	}
}
