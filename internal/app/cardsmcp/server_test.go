package cardsmcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/cardsmcp"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
)

type staticSource struct{ entries []cardsmcp.CardEntry }

func (s staticSource) Cards(context.Context) ([]cardsmcp.CardEntry, error) { return s.entries, nil }

type rpcToolReply struct {
	Result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"result"`
}

func decodeToolText(t *testing.T, line string) (string, bool) {
	t.Helper()
	var rep rpcToolReply
	require.NoError(t, json.Unmarshal([]byte(line), &rep))
	require.NotEmpty(t, rep.Result.Content)
	return rep.Result.Content[0].Text, rep.Result.IsError
}

func TestServeExposesOnlyApprovedCardsAndBlocksTraversal(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	srv, err := cardsmcp.New(staticSource{entries: []cardsmcp.CardEntry{
		{Card: rulecard.Card{Name: "auth.md", Kind: rulecard.KindInvariant, AppliesTo: []string{"internal/auth/**"}, Check: "Require auth", Body: "No bypass.", Status: rulecard.StatusApproved, ApprovedBy: "alice", ApprovedAt: now}, Signed: true},
		{Card: rulecard.Card{Name: "draft.md", Body: "Hidden candidate", Status: rulecard.StatusCandidate}},
		{Card: rulecard.Card{Name: "rejected.md", Body: "Hidden rejected", Status: rulecard.StatusRejected}},
	}})
	require.NoError(t, err)

	reqs := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_cards","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"match_cards","arguments":{"files":["internal/auth/jwt.go"]}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_card","arguments":{"name":"../secret.md"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"export_preview","arguments":{"target":"agents"}}}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	require.NoError(t, srv.Serve(context.Background(), strings.NewReader(reqs), &out))

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 6, "notification must not emit a response")

	listText, isErr := decodeToolText(t, lines[2])
	require.False(t, isErr)
	require.Contains(t, listText, `"auth.md"`)
	require.Contains(t, listText, `"signed":true`)
	require.NotContains(t, listText, "draft.md")
	require.NotContains(t, listText, "rejected.md")

	matchText, isErr := decodeToolText(t, lines[3])
	require.False(t, isErr)
	require.Contains(t, matchText, `"auth.md"`)

	_, isErr = decodeToolText(t, lines[4])
	require.True(t, isErr, "path traversal must be rejected")

	exportText, isErr := decodeToolText(t, lines[5])
	require.False(t, isErr)
	require.Contains(t, exportText, "heimdall:cards:begin")
}
