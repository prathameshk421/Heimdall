package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testGitHubToken = "ghs_" + "abcdefghijklmnopqrstuvwxyz0123456789"
	testLogKey      = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	testModelKey    = "sk-or-" + "test-model-key-value"
)

// actionEnv is a full environment of a gate or proof step. Each test removes or changes one value.
func actionEnv() map[string]string {
	return map[string]string{
		"GITHUB_REPOSITORY":           "aarush323/heimdall-fixture-pay",
		"GITHUB_API_URL":              "https://api.github.com",
		"GITHUB_TOKEN":                testGitHubToken,
		"GITHUB_EVENT_NAME":           "workflow_run",
		"GITHUB_EVENT_PATH":           "/home/runner/work/_temp/_github_workflow/event.json",
		"GITHUB_RUN_ID":               "44868368873",
		"GITHUB_WORKSPACE":            "/home/runner/work/pay/pay",
		"RUNNER_TEMP":                 "/home/runner/work/_temp",
		"HOME":                        "/home/runner",
		"HEIMDALL_LOG_KEY":            testLogKey,
		"HEIMDALL_MODEL_KEY":          testModelKey,
		"HEIMDALL_MODEL_BASE_URL":     "https://openrouter.ai/api/v1",
		"HEIMDALL_MODEL":              "deepseek/deepseek-v4-flash",
		"HEIMDALL_MODEL_PRICE_INPUT":  "0.30",
		"HEIMDALL_MODEL_PRICE_OUTPUT": "1.20",
	}
}

func getenvOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestTheGateReadsItsEnvironment(t *testing.T) {
	got, err := LoadAction(getenvOf(actionEnv()), ActionGate)
	require.NoError(t, err)
	require.Equal(t, "aarush323/heimdall-fixture-pay", got.Repo)
	require.Equal(t, int64(44868368873), got.RunID)
	require.Equal(t, testGitHubToken, got.Token)
	require.Len(t, got.LogKey, 32)
	require.Equal(t, "github-actions[bot]", got.CommentAuthor, "the default author is the GITHUB_TOKEN's bot login")
}

func TestTheAPIURLDefaultsToGitHub(t *testing.T) {
	env := actionEnv()
	delete(env, "GITHUB_API_URL")
	got, err := LoadAction(getenvOf(env), ActionGate)
	require.NoError(t, err)
	require.Equal(t, "https://api.github.com", got.APIURL)
}

// Each role refuses the values it needs when they are missing or malformed. The error names the variable, never its
// value.
func TestLoadActionRefusals(t *testing.T) {
	cases := []struct {
		name string
		role ActionRole
		key  string
		val  string // "" deletes the variable
		want string
	}{
		{"gate needs the token", ActionGate, "GITHUB_TOKEN", "", "GITHUB_TOKEN"},
		{"a token with a space", ActionGate, "GITHUB_TOKEN", "ghs_abc def", "GITHUB_TOKEN"},
		{"gate needs the log key", ActionGate, "HEIMDALL_LOG_KEY", "", "HEIMDALL_LOG_KEY"},
		{"a short log key", ActionGate, "HEIMDALL_LOG_KEY", "00112233", "HEIMDALL_LOG_KEY"},
		{"a log key that is not hex", ActionGate, "HEIMDALL_LOG_KEY", strings.Repeat("zz", 32), "HEIMDALL_LOG_KEY"},
		{"gate needs the run id", ActionGate, "GITHUB_RUN_ID", "", "GITHUB_RUN_ID"},
		{"a run id that is not a number", ActionGate, "GITHUB_RUN_ID", "12x", "GITHUB_RUN_ID"},
		{"a run id of zero", ActionGate, "GITHUB_RUN_ID", "0", "GITHUB_RUN_ID"},
		{"a malformed repository", ActionGate, "GITHUB_REPOSITORY", "pay", "GITHUB_REPOSITORY"},
		{"a repository with a path", ActionGate, "GITHUB_REPOSITORY", "a/b/c", "GITHUB_REPOSITORY"},
		{"an API URL over plain http", ActionGate, "GITHUB_API_URL", "http://api.github.com", "GITHUB_API_URL"},
		{"a relative event path", ActionGate, "GITHUB_EVENT_PATH", "event.json", "GITHUB_EVENT_PATH"},
		{"trigger needs the event path", ActionTrigger, "GITHUB_EVENT_PATH", "", "GITHUB_EVENT_PATH"},
		{"trigger needs the event name", ActionTrigger, "GITHUB_EVENT_NAME", "", "GITHUB_EVENT_NAME"},
		{"proof needs the token", ActionProof, "GITHUB_TOKEN", "", "GITHUB_TOKEN"},
		{"proof needs the workspace", ActionProof, "GITHUB_WORKSPACE", "", "GITHUB_WORKSPACE"},
		{"a relative step summary", ActionProof, "GITHUB_STEP_SUMMARY", "summary.md", "GITHUB_STEP_SUMMARY"},
		{"a model URL over plain http", ActionProof, "HEIMDALL_MODEL_BASE_URL", "http://llm.example.test/v1", "HEIMDALL_MODEL_BASE_URL"},
		{"a model URL with credentials", ActionProof, "HEIMDALL_MODEL_BASE_URL", "https://u:p@llm.example.test/v1", "HEIMDALL_MODEL_BASE_URL"},
		{"a key with no model", ActionProof, "HEIMDALL_MODEL", "", "HEIMDALL_MODEL"},
		{"a key with no input price", ActionProof, "HEIMDALL_MODEL_PRICE_INPUT", "", "HEIMDALL_MODEL_PRICE_INPUT"},
		{"a zero output price", ActionProof, "HEIMDALL_MODEL_PRICE_OUTPUT", "0", "HEIMDALL_MODEL_PRICE_OUTPUT"},
		{"a price that is not a number", ActionProof, "HEIMDALL_MODEL_PRICE_OUTPUT", "NaN", "HEIMDALL_MODEL_PRICE_OUTPUT"},
		{"a negative cached price", ActionProof, "HEIMDALL_MODEL_PRICE_CACHED_INPUT", "-1", "HEIMDALL_MODEL_PRICE_CACHED_INPUT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := actionEnv()
			if tc.val == "" {
				delete(env, tc.key)
			} else {
				env[tc.key] = tc.val
			}
			_, err := LoadAction(getenvOf(env), tc.role)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
			for _, secret := range []string{testGitHubToken, testLogKey, testModelKey, "ghs_abc def"} {
				require.NotContains(t, err.Error(), secret, "an error must never carry a secret value")
			}
		})
	}
}

// A model key is optional: a fork PR gets none by GitHub's rule, and the proof run then reports fork_no_key. Without a
// key the model, URL and price are not required.
func TestProofWithoutAModelKeyNeedsNoModelSettings(t *testing.T) {
	env := actionEnv()
	for _, k := range []string{"HEIMDALL_MODEL_KEY", "HEIMDALL_MODEL", "HEIMDALL_MODEL_PRICE_INPUT", "HEIMDALL_MODEL_PRICE_OUTPUT"} {
		delete(env, k)
	}
	got, err := LoadAction(getenvOf(env), ActionProof)
	require.NoError(t, err)
	require.False(t, got.HasModelKey())
}

func TestProofReadsTheModelEndpoint(t *testing.T) {
	env := actionEnv()
	env["HEIMDALL_MODEL_PRICE_CACHED_INPUT"] = "0.03"
	got, err := LoadAction(getenvOf(env), ActionProof)
	require.NoError(t, err)
	require.True(t, got.HasModelKey())
	require.Equal(t, "https://openrouter.ai/api/v1", got.ModelBaseURL)
	require.Equal(t, ActionPrice{InputPerM: 0.30, CachedInputPerM: 0.03, OutputPerM: 1.20}, got.Price)
}

// The proof step holds no write key: the log key is never read there, even when it is set by mistake.
func TestProofNeverReadsTheLogKey(t *testing.T) {
	got, err := LoadAction(getenvOf(actionEnv()), ActionProof)
	require.NoError(t, err)
	require.Empty(t, got.LogKey)
}

// The gate never reads the model key: the privileged job holds no key the PR could want.
func TestGateNeverReadsTheModelKey(t *testing.T) {
	got, err := LoadAction(getenvOf(actionEnv()), ActionGate)
	require.NoError(t, err)
	require.False(t, got.HasModelKey())
}

func TestActionEnvPrintsRedacted(t *testing.T) {
	got, err := LoadAction(getenvOf(actionEnv()), ActionGate)
	require.NoError(t, err)
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		out := fmt.Sprintf(verb, got)
		require.NotContains(t, out, testGitHubToken)
		require.NotContains(t, out, testLogKey)
	}
}

func TestAnUnknownRoleIsRefused(t *testing.T) {
	_, err := LoadAction(getenvOf(actionEnv()), ActionRole("deploy"))
	require.Error(t, err)
}

// The sandbox PATH defaults to the system directories and refuses a relative directory, which a job would resolve
// against its own work tree.
func TestTheSandboxPathIsAbsoluteDirectories(t *testing.T) {
	got, err := LoadAction(getenvOf(actionEnv()), ActionProof)
	require.NoError(t, err)
	require.Equal(t, "/usr/bin:/bin", got.SandboxPath)

	env := actionEnv()
	env["HEIMDALL_SANDBOX_PATH"] = "/opt/hostedtoolcache/go/1.27.4/x64/bin:/usr/bin:/bin"
	got, err = LoadAction(getenvOf(env), ActionProof)
	require.NoError(t, err)
	require.Equal(t, env["HEIMDALL_SANDBOX_PATH"], got.SandboxPath)

	env["HEIMDALL_SANDBOX_PATH"] = "bin:/usr/bin"
	_, err = LoadAction(getenvOf(env), ActionProof)
	require.ErrorContains(t, err, "HEIMDALL_SANDBOX_PATH")
}
