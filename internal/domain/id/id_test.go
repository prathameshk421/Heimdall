package id_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

const ulid = "01HZX3K9QW7M2P5R8T0V4YB6CD"

func TestParseChangeID(t *testing.T) {
	for _, ok := range []string{"PAY-142", "KAN-1", "ABC-99999", "pr-acme-pay-demo-7", "pr-aarush323-heimdall-fixture-pay-12"} {
		got, err := id.ParseChangeID(ok)
		require.NoError(t, err, ok)
		require.Equal(t, ok, got.String())
	}
	for _, bad := range []string{"", "pay-142", "PAY142", "PAY-", "PAY-0x1", "PAY-1 2", "pr-", "pr-repo", "pr-repo-x", "PAY-142\n", strings.Repeat("A", 40) + "-1"} {
		_, err := id.ParseChangeID(bad)
		require.Error(t, err, "should reject %q", bad)
	}
}

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

func TestParseAgentName(t *testing.T) {
	for _, ok := range []string{"bob-reviewer", "test-author", "a", "ana.reviewer_2"} {
		_, err := id.ParseAgentName(ok)
		require.NoError(t, err, ok)
	}
	for _, bad := range []string{"", "-bob", "Bob", "bob reviewer", ".x", strings.Repeat("a", 65)} {
		_, err := id.ParseAgentName(bad)
		require.Error(t, err, "should reject %q", bad)
	}
}

func TestPrefixedIDsAreTypedAndValidated(t *testing.T) {
	f, err := id.ParseFindingID("fnd_" + ulid)
	require.NoError(t, err)
	require.Equal(t, "fnd_"+ulid, f.String())
	_, err = id.ParseRunID("run_" + ulid)
	require.NoError(t, err)
	_, err = id.ParseCaseID("case_" + ulid)
	require.NoError(t, err)
	_, err = id.ParseTaskID("tsk_" + ulid)
	require.NoError(t, err)

	wrong := map[string]func(string) error{
		"a finding id with a run prefix": func(s string) error { _, e := id.ParseFindingID(s); return e },
	}
	for name, parse := range wrong {
		require.Error(t, parse("run_"+ulid), name)
	}
	for _, bad := range []string{"", "fnd_", "fnd_" + ulid[:25], "fnd_" + ulid + "A", "fnd_" + strings.ToLower(ulid), "fnd_" + strings.Replace(ulid, "K", "U", 1), ulid} {
		_, err := id.ParseFindingID(bad)
		require.Error(t, err, "should reject %q (Crockford base32 has no I, L, O or U)", bad)
	}
}

func TestFindingRefIsWhatPeopleSee(t *testing.T) {
	r, err := id.ParseFindingRef("F12")
	require.NoError(t, err)
	require.Equal(t, 12, r.Number())
	require.Equal(t, "F12", r.String())
	for _, bad := range []string{"", "F", "F0", "f1", "F-1", "F1x", "G1", "F01", "F99999999999"} {
		_, err := id.ParseFindingRef(bad)
		require.Error(t, err, "should reject %q", bad)
	}
	require.Equal(t, "F3", id.NewFindingRef(3).String())
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
