package commands_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// Every case of testdata/plan/address (F5, H4): the ticket of a message and who it is for.
func TestRouteMatchesTheAddressGoldens(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "testdata", "plan", "address")
	raw, err := os.ReadFile(filepath.Join(dir, "policy.yaml"))
	require.NoError(t, err)
	var p policy.Policy
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	require.NoError(t, dec.Decode(&p))

	raw, err = os.ReadFile(filepath.Join(dir, "cases.json"))
	require.NoError(t, err)
	var cases []struct {
		ID       string           `json:"id"`
		Input    commands.Message `json:"input"`
		Expected commands.Address `json:"expected"`
	}
	require.NoError(t, json.Unmarshal(raw, &cases))
	require.NotEmpty(t, cases)
	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			require.Equal(t, c.Expected, commands.Route(c.Input, p.Seats))
		})
	}
}

func TestEveryVerbHasAClass(t *testing.T) {
	for _, use := range []string{"uphold", "dismiss", "start", "review", "challenge", "remember", "forget",
		"spec-change", "reject", "override", "waive-reviewer", "incident", "resume", "answer", "approve-tests"} {
		_, ok := commands.Classes()[use]
		require.True(t, ok, use)
	}
}
