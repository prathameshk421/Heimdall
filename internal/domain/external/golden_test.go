package external_test

import (
	"path/filepath"
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
