package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const testPin = "0123456789abcdef0123456789abcdef01234567"

// Every `uses:` marked heimdall-pin gets the SHA, and nothing else in the file changes.
func TestPinWorkflowReplacesOnlyMarkedLines(t *testing.T) {
	raw := "a: 1\n      - uses: o/r/deploy/action@0000000000000000000000000000000000000000 # heimdall-pin\n" +
		"      - uses: actions/x@0000000000000000000000000000000000000000 # v1\n"
	got, err := pinWorkflow([]byte(raw), testPin)
	require.NoError(t, err)
	require.Contains(t, string(got), "o/r/deploy/action@"+testPin+" # heimdall-pin")
	require.Contains(t, string(got), "actions/x@0000000000000000000000000000000000000000 # v1", "an unmarked line stays")

	_, err = pinWorkflow([]byte("a: 1\n"), testPin)
	require.Error(t, err, "a workflow with no pin line is refused")
}

func TestModelSettingsDefaultsAndValidation(t *testing.T) {
	for _, tt := range []struct {
		name                   string
		model, in, cached, out string
		wantModel, wantOut     string
		wantErr                string
	}{
		{name: "default model gets its prices", wantModel: defaultModel, wantOut: "2.00"},
		{name: "own model with prices", model: "x/y", in: "1", cached: "0.1", out: "4", wantModel: "x/y", wantOut: "4"},
		{name: "own model without prices", model: "x/y", wantErr: "needs -price-input"},
		{name: "negative price", model: "x/y", in: "-1", cached: "0", out: "1", wantErr: "price"},
		{name: "price not a number", model: "x/y", in: "cheap", cached: "0", out: "1", wantErr: "price"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, err := modelSettings(tt.model, "", tt.in, tt.cached, tt.out)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantModel, m.model)
			require.Equal(t, tt.wantOut, m.priceOutput)
			require.Equal(t, defaultModelBaseURL, m.baseURL)
		})
	}
}

// -dir writes the three pinned workflows and a shadow-mode policy, and a second run changes nothing. An existing
// heimdall.yaml is never overwritten.
func TestWriteActionFilesIsRepeatableAndKeepsAPolicy(t *testing.T) {
	dir := t.TempDir()
	msg, err := writeActionFiles(dir, testPin)
	require.NoError(t, err)
	require.Contains(t, msg, "wrote")
	for _, name := range []string{"heimdall-trigger.yml", "heimdall-proof.yml", "heimdall-gate.yml"} {
		raw, err := os.ReadFile(filepath.Join(dir, ".github", "workflows", name))
		require.NoError(t, err)
		require.Contains(t, string(raw), "@"+testPin+" # heimdall-pin")
		require.NotContains(t, string(raw), "@0000000000000000000000000000000000000000 # heimdall-pin")
	}
	policy, err := os.ReadFile(filepath.Join(dir, "heimdall.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(policy), "mode: shadow")

	msg, err = writeActionFiles(dir, testPin)
	require.NoError(t, err)
	require.NotContains(t, msg, "wrote")

	other := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(other, "heimdall.yaml"), []byte("people: []\n"), 0o600))
	msg, err = writeActionFiles(other, testPin)
	require.NoError(t, err)
	kept, err := os.ReadFile(filepath.Join(other, "heimdall.yaml"))
	require.NoError(t, err)
	require.Equal(t, "people: []\n", string(kept), "an existing policy file is not touched")
	require.Contains(t, msg, "add an action area")
}

func TestPinRefMustBeAFullSHA(t *testing.T) {
	_, err := resolvePinRef("v1.0")
	require.Error(t, err)
	got, err := resolvePinRef(testPin)
	require.NoError(t, err)
	require.Equal(t, testPin, got)
}
