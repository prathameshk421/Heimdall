package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

func TestMaskSecretsCoversRawAndEncodedForms(t *testing.T) {
	modelKey := "sk-test+key/val=1&2\"3\\4"
	ghToken := "ghs_" + "maskTestTokenValue012345"

	variants := []string{
		modelKey,
		url.QueryEscape(modelKey),
		jsonEscapeString(modelKey, true),
		jsonEscapeString(modelKey, false),
		base64.StdEncoding.EncodeToString([]byte(modelKey)),
		base64.RawStdEncoding.EncodeToString([]byte(modelKey)),
		base64.URLEncoding.EncodeToString([]byte(modelKey)),
		base64.RawURLEncoding.EncodeToString([]byte(modelKey)),
		ghToken,
		url.QueryEscape(ghToken),
		base64.StdEncoding.EncodeToString([]byte(ghToken)),
		base64.RawStdEncoding.EncodeToString([]byte(ghToken)),
	}

	raw := "keep-prefix "
	for _, v := range variants {
		raw += "[" + v + "] "
	}
	raw += "keep-suffix"

	got := maskSecrets([]byte(raw), modelKey, ghToken, "short7")
	require.Len(t, got, len(raw), "byte-level masking preserves length")
	text := string(got)
	require.Contains(t, text, "keep-prefix")
	require.Contains(t, text, "keep-suffix")
	for _, v := range variants {
		require.NotContains(t, text, v)
	}

	// Secrets shorter than minMaskSecretLen must not corrupt normal output.
	untouched := maskSecrets([]byte("status short7 ok"), "short7", "")
	require.Equal(t, "status short7 ok", string(untouched))
}

func TestMaskJSONSecretsPreservesValidJSONAndNumbers(t *testing.T) {
	specialKey := `sk-key"with\slash<and>&12345`
	numericSecret := "1234567890"
	ghToken := "ghs_" + "jsonMaskTokenValue987654"

	payload, err := json.Marshal(map[string]any{
		"workflow_run": 1234567890,
		"detail": "raw=" + specialKey +
			" json=" + jsonEscapeString(specialKey, false) +
			" url=" + url.QueryEscape(specialKey) +
			" b64=" + base64.StdEncoding.EncodeToString([]byte(specialKey)) +
			" tok=" + ghToken +
			" num=" + numericSecret,
	})
	require.NoError(t, err)

	got := maskJSONSecrets(payload, specialKey, ghToken, numericSecret)
	require.True(t, json.Valid(got), "masked JSON must remain valid: %s", string(got))
	require.LessOrEqual(t, len(got), len(payload))

	var decoded struct {
		WorkflowRun int64  `json:"workflow_run"`
		Detail      string `json:"detail"`
	}
	require.NoError(t, json.Unmarshal(got, &decoded))
	require.Equal(t, int64(1234567890), decoded.WorkflowRun, "numeric JSON fields must not be corrupted")
	require.NotContains(t, decoded.Detail, numericSecret)

	for _, forbidden := range []string{
		specialKey,
		jsonEscapeString(specialKey, true),
		jsonEscapeString(specialKey, false),
		url.QueryEscape(specialKey),
		base64.StdEncoding.EncodeToString([]byte(specialKey)),
		ghToken,
	} {
		require.NotContains(t, string(got), forbidden)
		require.NotContains(t, decoded.Detail, forbidden)
	}
}

func TestProofStepSecretsIncludesConfiguredAndEnvToken(t *testing.T) {
	envToken := "ghs_" + "processEnvToken0123456789"
	s := &proofStep{
		env: config.ActionEnv{
			ModelKey: "sk-model-secret-value",
			Token:    "ghs_" + "configuredToken0123456789",
		},
		getenv: func(k string) string {
			if k == "GITHUB_TOKEN" {
				return envToken
			}
			return ""
		},
	}
	got := s.secrets()
	require.ElementsMatch(t, []string{s.env.ModelKey, s.env.Token, envToken}, got)
}

func TestActionProofMasksSecretsInArtifactTranscriptAndSummary(t *testing.T) {
	gh := newFakeGitHub(t)
	modelKey := "sk-mask+test/key=12345678"
	ghToken := fakeToken

	leakedText := "echo raw=" + modelKey +
		" url=" + url.QueryEscape(modelKey) +
		" b64=" + base64.StdEncoding.EncodeToString([]byte(modelKey)) +
		" gh=" + ghToken +
		" ghb64=" + base64.StdEncoding.EncodeToString([]byte(ghToken))

	proposeArgsRaw, err := json.Marshal(map[string]any{
		"file":        "pay/refund.go",
		"start_line":  2,
		"end_line":    2,
		"severity":    "high",
		"claim":       leakedText,
		"test_path":   "pay/refund_proof_test.go",
		"test_name":   "TestRefundProof",
		"test_source": "package pay\nimport \"testing\"\nfunc TestRefundProof(t *testing.T) {}\n",
	})
	require.NoError(t, err)

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+modelKey {
			writeFakeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad key"})
			return
		}
		if calls.Add(1) == 1 {
			writeFakeJSON(w, http.StatusOK, map[string]any{
				"id": "c1", "model": "test/flash",
				"choices": []any{map[string]any{
					"index": 0, "finish_reason": "tool_calls",
					"message": map[string]any{
						"role": "assistant", "content": leakedText,
						"tool_calls": []any{map[string]any{
							"id": "call_1", "type": "function",
							"function": map[string]any{"name": "propose_finding", "arguments": string(proposeArgsRaw)},
						}},
					},
				}},
				"usage": map[string]any{"prompt_tokens": 500, "completion_tokens": 50},
			})
			return
		}
		writeFakeJSON(w, http.StatusOK, map[string]any{
			"id": "c2", "model": "test/flash",
			"choices": []any{map[string]any{
				"index": 0, "finish_reason": "tool_calls",
				"message": map[string]any{
					"role": "assistant", "content": "done",
					"tool_calls": []any{map[string]any{
						"id": "call_2", "type": "function",
						"function": map[string]any{"name": "finish_review", "arguments": `{"summary":"checked"}`},
					}},
				},
			}},
			"usage": map[string]any{"prompt_tokens": 500, "completion_tokens": 20},
		})
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	out := filepath.Join(dir, proofFile)
	summary := filepath.Join(t.TempDir(), "step_summary.md")
	env := proofEnv(t, gh, srv.URL, false)
	require.NoError(t, os.WriteFile(filepath.Join(env["GITHUB_WORKSPACE"], "pay", "refund.go"),
		[]byte("package pay\nfunc Refund() {}\n"), 0o600))
	env["HEIMDALL_MODEL_KEY"] = modelKey
	env["GITHUB_STEP_SUMMARY"] = summary

	code, _, stderr := runStep(t, smokeDeps(env, &fakeJobs{}), "proof", "-out", out)
	require.Equal(t, exitActionOK, code, stderr)

	artifactRaw, err := os.ReadFile(out)
	require.NoError(t, err)
	require.True(t, json.Valid(artifactRaw), "heimdall-proof.json must be valid JSON")
	var artifact action.ProofArtifact
	require.NoError(t, json.Unmarshal(artifactRaw, &artifact))
	require.NotEmpty(t, artifact.Candidates, "expected candidate carrying the claim in heimdall-proof.json")
	require.Contains(t, artifact.Candidates[0].Claim, "echo raw=*************************")

	transcriptRaw, err := os.ReadFile(filepath.Join(dir, proofTranscriptFile))
	require.NoError(t, err)
	require.True(t, json.Valid(transcriptRaw), "heimdall-proof-transcript.json must be valid JSON")
	var transcript action.ProofTranscript
	require.NoError(t, json.Unmarshal(transcriptRaw, &transcript))

	summaryRaw, err := os.ReadFile(summary)
	require.NoError(t, err)

	forbidden := []string{
		modelKey,
		url.QueryEscape(modelKey),
		base64.StdEncoding.EncodeToString([]byte(modelKey)),
		ghToken,
		base64.StdEncoding.EncodeToString([]byte(ghToken)),
	}
	for _, secret := range forbidden {
		require.NotContains(t, string(artifactRaw), secret, "secret leaked into heimdall-proof.json")
		require.NotContains(t, string(transcriptRaw), secret, "secret leaked into heimdall-proof-transcript.json")
		require.NotContains(t, string(summaryRaw), secret, "secret leaked into step summary")
	}
}
