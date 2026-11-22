package announce_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/announce"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/stretchr/testify/require"
)

func TestTicketlessPRHeaderShowsPRNumberNotStreamID(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(change.PROpened{
		Repo:    "aarush323/heimdall-fixture-pay",
		Number:  15,
		HeadSHA: id.SHA("1111111111111111111111111111111111111111"),
		BaseSHA: id.SHA("2222222222222222222222222222222222222222"),
		Author:  "alice",
	})
	require.NoError(t, err)
	ev := change.Event{
		ID:         "01J00000000000000000000001",
		ChangeID:   "pr-aarush323-heimdall-fixture-pay-15",
		Version:    1,
		Type:       change.EventPROpened,
		Actor:      change.Actor{Kind: change.ActorGitHub, ID: "alice"},
		OccurredAt: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC),
		Payload:    raw,
	}

	board, ok := announce.RenderBoard("pr-aarush323-heimdall-fixture-pay-15", []change.Event{ev})
	require.True(t, ok)
	require.Contains(t, board, "# 🛡️ Heimdall Review · PR #15")
	require.NotContains(t, board, "pr-aarush323-heimdall-fixture-pay-15")

	view, err := change.Fold([]change.Event{ev})
	require.NoError(t, err)
	status := announce.Render(view, nil)
	require.Contains(t, status, "PR #15 · 0 needs")
}
