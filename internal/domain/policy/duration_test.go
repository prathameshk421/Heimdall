package policy_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

func TestDurationDecodesFromAStringAndEncodesBackToTheSameString(t *testing.T) {
	cases := map[string]time.Duration{
		"15m": 15 * time.Minute, "4h": 4 * time.Hour, "10s": 10 * time.Second, "90m": 90 * time.Minute,
		"2h30m": 150 * time.Minute, "500ms": 500 * time.Millisecond,
	}
	for in, want := range cases {
		var d policy.Duration
		require.NoError(t, json.Unmarshal([]byte(`"`+in+`"`), &d), in)
		require.Equal(t, policy.Duration(want), d)
		out, err := json.Marshal(d)
		require.NoError(t, err)
		var again policy.Duration
		require.NoError(t, json.Unmarshal(out, &again))
		require.Equal(t, d, again, "round trip of %s", in)
	}
	out, err := json.Marshal(policy.Duration(4 * time.Hour))
	require.NoError(t, err)
	require.JSONEq(t, `"4h"`, string(out), "the string the author wrote, not 4h0m0s")
	out, err = json.Marshal(policy.Duration(15 * time.Minute))
	require.NoError(t, err)
	require.JSONEq(t, `"15m"`, string(out))
}

func TestDurationRefusesWhatIsNotADuration(t *testing.T) {
	for _, in := range []string{`"soon"`, `""`, `"-5m"`, `15`, `"1d"`} {
		var d policy.Duration
		require.Error(t, json.Unmarshal([]byte(in), &d), in)
	}
}

func TestDurationInYAML(t *testing.T) {
	var v struct {
		D policy.Duration `yaml:"d"`
	}
	require.NoError(t, yaml.Unmarshal([]byte("d: 8h\n"), &v))
	require.Equal(t, policy.Duration(8*time.Hour), v.D)
	require.Error(t, yaml.Unmarshal([]byte("d: eventually\n"), &v))
}
