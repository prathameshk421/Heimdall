package id_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

const ulid = "01HZX3K9QW7M2P5R8T0V4YB6CD"

func TestChangeIDForAPullRequestWithNoTicket(t *testing.T) {
	got, err := id.PRChangeID("acme/pay-demo", 7)
	require.NoError(t, err)
	require.Equal(t, "pr-acme-pay-demo-7", got.String(), "D146: a PR with no ticket gets its own stream")
	_, err = id.PRChangeID("acme/pay-demo", 0)
	require.Error(t, err)
	_, err = id.PRChangeID("no-slash", 3)
	require.Error(t, err)
	require.True(t, got.IsPR())
	k, _ := id.ParseChangeID("PAY-142")
	require.False(t, k.IsPR())
}

func TestParseSHA(t *testing.T) {
	for _, ok := range []string{"9f1c2ab", "9f1c2ab0c3d4e5f60718293a4b5c6d7e8f901234", strings.Repeat("a", 64)} {
		_, err := id.ParseSHA(ok)
		require.NoError(t, err, ok)
	}
	for _, bad := range []string{"", "9f1c2a", "9F1C2AB", "9f1c2abg", strings.Repeat("a", 41), strings.Repeat("a", 65), " 9f1c2ab"} {
		_, err := id.ParseSHA(bad)
		require.Error(t, err, "should reject %q", bad)
	}
	s, err := id.ParseSHA("9f1c2ab0c3d4e5f60718293a4b5c6d7e8f901234")
	require.NoError(t, err)
	require.Equal(t, "9f1c2ab", s.Short(), "people see the first 7 characters")
}

func TestIDsRefuseBadValuesWhenDecodedFromJSON(t *testing.T) {
	var in struct {
		Change id.ChangeID   `json:"change"`
		SHA    id.SHA        `json:"sha"`
		Agent  id.AgentName  `json:"agent"`
		Find   id.FindingID  `json:"finding"`
		Ref    id.FindingRef `json:"ref"`
	}
	good := `{"change":"PAY-142","sha":"9f1c2ab","agent":"bob-reviewer","finding":"fnd_` + ulid + `","ref":"F3"}`
	require.NoError(t, json.Unmarshal([]byte(good), &in))
	out, err := json.Marshal(in)
	require.NoError(t, err)
	require.JSONEq(t, good, string(out), "round trip")

	for name, bad := range map[string]string{
		"change":  `{"change":"not-a-key"}`,
		"sha":     `{"sha":"zzz"}`,
		"agent":   `{"agent":"Bad Name"}`,
		"finding": `{"finding":"fnd_x"}`,
		"ref":     `{"ref":"F0"}`,
	} {
		t.Run(name, func(t *testing.T) {
			var v struct {
				Change id.ChangeID   `json:"change"`
				SHA    id.SHA        `json:"sha"`
				Agent  id.AgentName  `json:"agent"`
				Find   id.FindingID  `json:"finding"`
				Ref    id.FindingRef `json:"ref"`
			}
			require.Error(t, json.Unmarshal([]byte(bad), &v))
		})
	}
}
