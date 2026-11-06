package workflow_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

func golden(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/task_envelope.golden.json")
	require.NoError(t, err)
	return b
}

func TestTaskEnvelopeDecodesTheSpecExampleAndEncodesItBack(t *testing.T) {
	var env workflow.TaskEnvelope
	require.NoError(t, json.Unmarshal(golden(t), &env))
	require.NoError(t, env.Validate())
	require.Equal(t, workflow.RoleReviewer, env.Role)
	require.Equal(t, "PAY-142", env.Change.ID.String())
	require.Equal(t, 150000, env.ContextBudgetTokens)
	require.Equal(t, time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC), env.Deadline.UTC())
	out, err := json.Marshal(env)
	require.NoError(t, err)
	require.JSONEq(t, string(golden(t)), string(out), "golden JSON: the wire format is frozen")
}

func decode(t *testing.T) workflow.TaskEnvelope {
	t.Helper()
	var env workflow.TaskEnvelope
	require.NoError(t, json.Unmarshal(golden(t), &env))
	return env
}

func TestTaskEnvelopeValidateRefusesWhatAnAgentCouldNotActOn(t *testing.T) {
	mutations := map[string]func(*workflow.TaskEnvelope){
		"a skill that belongs to another role": func(e *workflow.TaskEnvelope) { e.Skill = "resolver@1.0.0" },
		"a skill with no version":              func(e *workflow.TaskEnvelope) { e.Skill = "reviewer" },
		"no goal":                              func(e *workflow.TaskEnvelope) { e.Goal = "  " },
		"a goal that is far too long":          func(e *workflow.TaskEnvelope) { e.Goal = strings.Repeat("x", 4001) },
		"no repository":                        func(e *workflow.TaskEnvelope) { e.Change.Repo = "acme" },
		"no diff":                              func(e *workflow.TaskEnvelope) { e.Inputs.Diff = "" },
		"a diff that is not a blob reference":  func(e *workflow.TaskEnvelope) { e.Inputs.Diff = "https://example.com/diff" },
		"an acceptance criterion with no ref":  func(e *workflow.TaskEnvelope) { e.Inputs.AcceptanceCriteria[0].Ref = "" },
		"an acceptance criterion id of AC0":    func(e *workflow.TaskEnvelope) { e.Inputs.AcceptanceCriteria[0].ID = "AC0" },
		"untrusted text with no source":        func(e *workflow.TaskEnvelope) { e.Inputs.Untrusted[0].Source = "" },
		"untrusted text that is not a blob":    func(e *workflow.TaskEnvelope) { e.Inputs.Untrusted[0].Ref = "inline text" },
		"a zero budget":                        func(e *workflow.TaskEnvelope) { e.ContextBudgetTokens = 0 },
		"an absurd budget":                     func(e *workflow.TaskEnvelope) { e.ContextBudgetTokens = 50_000_000 },
		"a malformed schema name":              func(e *workflow.TaskEnvelope) { e.OutputSchema = "review" },
		"no deadline":                          func(e *workflow.TaskEnvelope) { e.Deadline = time.Time{} },
		"an unknown role":                      func(e *workflow.TaskEnvelope) { e.Role = "wizard" },
		"a file path that escapes the repo":    func(e *workflow.TaskEnvelope) { e.Inputs.FilesChanged = []string{"../../etc/passwd"} },
		"an absolute file path":                func(e *workflow.TaskEnvelope) { e.Inputs.FilesChanged = []string{"/etc/passwd"} },
		"a file path with a backslash":         func(e *workflow.TaskEnvelope) { e.Inputs.FilesChanged = []string{`pay\refund.go`} },
		"a file path with a control character": func(e *workflow.TaskEnvelope) { e.Inputs.FilesChanged = []string{"pay/refund\x07.go"} },
		"a CI entry with a made-up conclusion": func(e *workflow.TaskEnvelope) { e.Inputs.CI[0].Conclusion = "maybe" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			env := decode(t)
			mutate(&env)
			require.Error(t, env.Validate())
		})
	}
}

func TestTaskEnvelopeRefusesBadValuesWhenDecoded(t *testing.T) {
	for name, field := range map[string]string{
		"a task id":  `"task_id": "tsk_x"`,
		"a role":     `"role": "intern"`,
		"a blob ref": `"diff": "blob:md5:abc"`,
		"a head sha": `"head_sha": "xyz"`,
	} {
		t.Run(name, func(t *testing.T) {
			key := strings.SplitN(field, ":", 2)[0]
			text := string(golden(t))
			i := strings.Index(text, key)
			require.GreaterOrEqual(t, i, 0)
			end := i + strings.Index(text[i:], "\n")
			bad := text[:i] + field + "," + text[end:]
			bad = strings.Replace(bad, ",,", ",", 1)
			var env workflow.TaskEnvelope
			require.Error(t, json.Unmarshal([]byte(bad), &env))
		})
	}
}

func TestRolesAndTheirSkills(t *testing.T) {
	for _, r := range []workflow.Role{workflow.RoleTestAuthor, workflow.RoleReviewer, workflow.RoleResolver, workflow.RoleProfiler} {
		parsed, err := workflow.ParseRole(string(r))
		require.NoError(t, err)
		require.Equal(t, r, parsed)
	}
	_, err := workflow.ParseRole("coordinator")
	require.Error(t, err, "the coordinator is not an LLM role and gets no task envelope")
}
