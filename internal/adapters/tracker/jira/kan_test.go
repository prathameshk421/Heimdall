package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// The KAN golden files live in testdata/kan. <KEY>.issue.json is the issue reply Jira sends (REST v3, ADF
// description). <KEY>.ticket.json is the ticket the adapter must produce from it: the summary, the status, the
// assignee and the acceptance criteria with their ids and SHA-256 hashes (D50). The hashes were computed with
// sha256sum, not with this package. The files are not recorded from a live Jira yet (T93), so they stand in for the
// KAN payloads until then.
func TestKANGoldenTicketsParseToTheRightCriteria(t *testing.T) {
	for _, key := range []string{"KAN-1", "KAN-2", "KAN-3"} {
		t.Run(key, func(t *testing.T) {
			issue := readGolden(t, key+".issue.json")
			want := readGolden(t, key+".ticket.json")
			f := newFake(t, func(w http.ResponseWriter, _ *http.Request) { reply(w, http.StatusOK, string(issue)) })
			cl := newClient(t, f, &clock{at: now})

			got, err := cl.Ticket(context.Background(), id.ChangeID(key))

			require.NoError(t, err)
			require.NoError(t, got.Validate())
			gotJSON, err := json.Marshal(got)
			require.NoError(t, err)
			require.JSONEq(t, string(want), string(gotJSON))
		})
	}
}

// readGolden reads one file of testdata/kan. The package directory is four levels below the repository root.
func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "kan", name))
	require.NoError(t, err)
	return raw
}
