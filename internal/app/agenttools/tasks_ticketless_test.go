package agenttools

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// A ticketless pull request has no trigger and no branch events, so its task takes the repository and base from
// pr.opened (live, Nov 21: every read_code on PR #18 failed with `repository "" is not owner/name`, D189).
func TestATicketlessPullRequestTaskNamesTheRepositoryAndBase(t *testing.T) {
	raw, err := json.Marshal(change.PROpened{Repo: "acme/pay", Number: 18, HeadSHA: "9f1c2ab", BaseSHA: "7d3e9a1", Author: "ann"})
	require.NoError(t, err)
	events := []change.Event{{Type: change.EventPROpened, Payload: raw}}
	a := buildAssignment(ports.ActiveTaskRef{ChangeID: "pr-acme-pay-18"}, &change.TaskAssigned{TaskID: "tsk_1", Agent: "reviewer-1"}, time.Time{}, true, events)
	require.Equal(t, "acme/pay", a.Task.Change.Repo)
	require.Equal(t, "7d3e9a1", string(a.Task.Change.BaseSHA))
	require.Equal(t, "9f1c2ab", string(a.Task.Change.HeadSHA))
}
