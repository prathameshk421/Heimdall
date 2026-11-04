package fixtures_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/fixtures"
)

// The fixture's heimdall.yaml is read by the real product, so it must pass the real policy validation on every branch.
func TestEveryBranchPolicyDecodesStrictlyAndValidates(t *testing.T) {
	repo := fixtures.Materialize(t, "pay-demo")
	for _, branch := range fixtures.Branches(t, repo) {
		t.Run(branch, func(t *testing.T) {
			fixtures.Checkout(t, repo, branch)
			b, err := os.ReadFile(filepath.Join(repo, "heimdall.yaml"))
			require.NoError(t, err)
			var p policy.Policy
			dec := yaml.NewDecoder(bytes.NewReader(b))
			dec.KnownFields(true)
			require.NoError(t, dec.Decode(&p), "an unknown key in the policy file is a typo")
			require.NoError(t, p.Validate())
		})
	}
}
