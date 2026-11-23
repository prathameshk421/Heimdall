package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
)

func TestStageBLocalToRemoteLifecycle(t *testing.T) {
	tmp := t.TempDir()
	cardsDir := filepath.Join(tmp, "cards")
	keyFile := filepath.Join(tmp, "keys", "owner.key")
	agentsFile := filepath.Join(tmp, "AGENTS.md")
	require.NoError(t, os.WriteFile(agentsFile, []byte("# Existing\n\nKeep me.\n"), 0o644))

	env := func(stdin string) (cardsEnv, *bytes.Buffer) {
		var out bytes.Buffer
		return cardsEnv{
			stdin: strings.NewReader(stdin),
			out:   &out,
			errw:  &out,
			home:  tmp,
			now:   func() time.Time { return time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC) },
		}, &out
	}

	// 1. Import from stdin with a secret -> scrubbed locally into StatusCandidate
	e, out := env("- Always enforce auth in `internal/auth/**` (never commit AKIAABCDEFGHIJKLMNOP).\n")
	require.Equal(t, 0, runCardsWith([]string{"import", "--from", "-", "--dir", cardsDir}, e), out.String())
	require.Contains(t, out.String(), "imported 1 candidate cards (1 local redactions)")
	names, err := cardFileNames(cardsDir)
	require.NoError(t, err)
	require.Len(t, names, 1)
	cardName := names[0]
	raw, err := os.ReadFile(filepath.Join(cardsDir, cardName))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "AKIAABCDEFGHIJKLMNOP")
	require.Contains(t, string(raw), "status: candidate")

	// 2. Approve locally -> status: approved with approver & timestamp
	e, out = env("")
	require.Equal(t, 0, runCardsWith([]string{"approve", cardName, "--dir", cardsDir, "--approver", "alice"}, e), out.String())

	// 3. Sign (--init-key) & verify
	e, out = env("")
	require.Equal(t, 0, runCardsWith([]string{"sign", cardName, "--dir", cardsDir, "--key-file", keyFile, "--key-id", "alice-1", "--init-key"}, e), out.String())
	e, out = env("")
	require.Equal(t, 0, runCardsWith([]string{"verify", cardName, "--dir", cardsDir, "--pub-file", keyFile + ".pub", "--key-id", "alice-1"}, e), out.String())
	require.Contains(t, out.String(), "verified "+cardName)

	// 4. Export to AGENTS.md idempotently preserving human text
	e, out = env("")
	require.Equal(t, 0, runCardsWith([]string{"export", "--target", "agents", "--dir", cardsDir, "--out", agentsFile}, e), out.String())
	exported, err := os.ReadFile(agentsFile)
	require.NoError(t, err)
	require.Contains(t, string(exported), "Keep me.")
	require.Contains(t, string(exported), "internal/auth/**")

	// 5. Local MCP stdio serves the approved signed card
	e, out = env(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_cards","arguments":{}}}` + "\n")
	require.Equal(t, 0, runCardsWith([]string{"mcp", "--dir", cardsDir}, e), out.String())
	require.Contains(t, out.String(), `\"signed\":true`)

	// 6. Push uploads card + DSSE sidecar; outcomes prints owner table
	var uploaded []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut:
			uploaded = append(uploaded, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/cards/outcomes":
			_ = json.NewEncoder(w).Encode(map[string]any{"outcomes": []rulecard.CardOutcome{{CardName: cardName, Status: rulecard.OutcomeStatusStrong, Weight: 1.45, Kept: 3, Proven: 2}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	tokFile := filepath.Join(tmp, "tok")
	require.NoError(t, os.WriteFile(tokFile, []byte("alice-token\n"), 0o600))

	e, out = env("")
	e.client = srv.Client()
	require.Equal(t, 0, runCardsWith([]string{"push", "--server", srv.URL, "--token-file", tokFile, "--dir", cardsDir, "--yes"}, e), out.String())
	require.Equal(t, []string{"/cards/" + cardName, "/cards/" + cardName + ".dsse.json"}, uploaded)

	e, out = env("")
	e.client = srv.Client()
	require.Equal(t, 0, runCardsWith([]string{"outcomes", "--server", srv.URL, "--token-file", tokFile}, e), out.String())
	require.Contains(t, out.String(), cardName)
	require.Contains(t, out.String(), "strong")
}
