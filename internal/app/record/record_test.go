package record

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Planted credentials never reach the record (H8).
func TestSummarizeRedactsPlantedCredentials(t *testing.T) {
	planted := map[string]string{
		"github": "ghs_" + strings.Repeat("A", 36),
		"pat":    "github_pat_" + strings.Repeat("b", 30),
		"slack":  "xoxb-" + strings.Repeat("1", 20),
		"model":  "sk-or-v1-" + strings.Repeat("c", 40),
		"key":    "-----BEGIN OPENSSH PRIVATE KEY-----abc",
		"base64": "aB3dE5fG7hJ9kL1mN3pQ5rS7tU9vW1xY3zA5",
	}
	args, err := json.Marshal(map[string]any{"path": "internal/api/refunds.go", "planted": planted})
	require.NoError(t, err)
	got := Summarize(args)
	for name, value := range planted {
		require.NotContains(t, got, value[:12], name)
	}
	require.Contains(t, got, "internal/api/refunds.go", "ordinary text stays readable")
	keep, err := json.Marshal(map[string]string{"sha": "5ce99f5cf92fa4cae2ab7f3c2f2d2fcd6f5bc3a3",
		"test": "TestGetPaymentByID_OtherMerchantSeesSameAnswerAsUnknownID"})
	require.NoError(t, err)
	require.Equal(t, string(keep), Summarize(keep), "a commit SHA and a test name are not credentials")
	long := Summarize(json.RawMessage(`{"x":"` + strings.Repeat("word ", 200) + `"}`))
	require.LessOrEqual(t, len([]rune(long)), MaxSummary+1)
	require.Empty(t, Summarize(json.RawMessage(`not json`)))
}

func TestRepeatedRefusals(t *testing.T) {
	same := ports.ToolCall{Tool: "submit_review", Outcome: "refused", Refusal: "rule_checks: missing card x"}
	other := ports.ToolCall{Tool: "submit_review", Outcome: "ok"}
	require.Equal(t, 2, RepeatedRefusals([]ports.ToolCall{same, same, other, same}, same.Tool, same.Refusal))
	require.Equal(t, 0, RepeatedRefusals([]ports.ToolCall{other, same}, same.Tool, same.Refusal))
	require.Equal(t, 0, RepeatedRefusals([]ports.ToolCall{same}, same.Tool, "another reason"))
}

// A long run with a slash or a dot is a file path or a qualified name, never a key (B9): a reviewer's read_code path
// must stay readable in the record even when it mixes digits and both letter cases.
func TestSummarizeKeepsFilePaths(t *testing.T) {
	for _, path := range []string{
		"internal/api/v2/RefundHandlerV2PartialRefunds.go",
		"cmd/heimdall-server/Flow2ComponentTestHelpers.go",
		"refund.RefundServiceV2CreatePartialRefundOnce",
		"web/review-report/src/Report2Page.Component.tsx",
	} {
		args, err := json.Marshal(map[string]string{"path": path})
		require.NoError(t, err)
		require.Equal(t, string(args), Summarize(args), path)
	}
	jwt, err := json.Marshal(map[string]string{"auth": "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJyZXZpZXdlci0xIn0.Qm9ndXNTaWduYXR1cmVWYWx1ZTEyMw"})
	require.NoError(t, err)
	require.NotContains(t, Summarize(jwt), "eyJhbGci", "a dot does not make a JWT a path: it is a known credential shape")
	key, err := json.Marshal(map[string]string{"v": "aB3dE5fG7hJ9kL1mN3pQ5rS7tU9vW1xY3zA5"})
	require.NoError(t, err)
	require.Contains(t, Summarize(key), "[redacted]", "a random run with no slash or dot is still redacted")
}
