package policy_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// FuzzPolicyDecode feeds arbitrary JSON to the policy decoder and the validator. Neither may panic, and a policy that
// validates must still validate, and compare equal to itself, after it is encoded and decoded again.
func FuzzPolicyDecode(f *testing.F) {
	golden, err := os.ReadFile("testdata/policy.golden.json")
	require.NoError(f, err)
	f.Add(golden)
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"version":1,"slop":{"max_changed_lines":-1},"timeouts":{"question":"5x"}}`))
	f.Add([]byte(`{"domains":{"[":"auth"},"people":{"a":{"github":""}}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var p policy.Policy
		if json.Unmarshal(data, &p) != nil {
			return
		}
		if p.Validate() != nil {
			return
		}
		out, err := json.Marshal(p)
		require.NoError(t, err)
		var again policy.Policy
		require.NoError(t, json.Unmarshal(out, &again))
		require.NoError(t, again.Validate(), "an accepted policy must re-validate after re-encoding")
		require.True(t, policy.Equal(p, again))
	})
}
