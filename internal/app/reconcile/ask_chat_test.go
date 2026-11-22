package reconcile

import (
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/stretchr/testify/require"
)

// An ask in chat names a ticketless pull request by its own address, never the pr-... stream id.
func TestFormatAskNamesATicketlessPullRequestReadably(t *testing.T) {
	got := formatAsk(id.ChangeID("pr-aarush323-heimdall-fixture-pay-15"), []string{"ann"}, "pick reviewers with heimdall review")
	require.Equal(t, "🔔 aarush323/heimdall-fixture-pay #15 · @ann: pick reviewers with heimdall review", got)
	require.NotContains(t, got, "`")
}

// A ticket key stays as it is, because people know it.
func TestFormatAskKeepsATicketKey(t *testing.T) {
	require.Equal(t, "🔔 KAN-5 · @ann: uphold F1", formatAsk(id.ChangeID("KAN-5"), []string{"ann"}, "uphold F1"))
	require.Equal(t, "🔔 KAN-5: pick reviewers", formatAsk(id.ChangeID("KAN-5"), nil, "pick reviewers"))
}
