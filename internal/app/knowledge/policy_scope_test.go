package knowledge

import (
	"context"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/stretchr/testify/require"
)

func TestPolicyScopesReadsBasePolicyAndReturnsMappedTeams(t *testing.T) {
	reader := &codeSpy{body: []byte("policy")}
	scopes := PolicyScopes{Code: reader, Decode: func([]byte) (policy.Policy, error) {
		return policy.Policy{People: map[string]policy.Person{"ana": {Agent: "reviewer-ana", Teams: []string{"payments", "shared"}}}}, nil
	}}
	groups, err := scopes.GroupsForAgent(context.Background(), "reviewer-ana", "acme/pay", id.SHA("base123"))
	require.NoError(t, err)
	require.Equal(t, []string{"payments", "shared"}, groups)
	require.Equal(t, []string{"acme/pay", "base123", "heimdall.yaml"}, reader.args)
}

// An agent that no person in the policy owns is not an error: it has no team memory and sees only the shared group.
func TestPolicyScopesGivesAnUnmappedAgentNoTeamGroups(t *testing.T) {
	scopes := PolicyScopes{Code: &codeSpy{body: []byte("policy")}, Decode: func([]byte) (policy.Policy, error) { return policy.Policy{People: map[string]policy.Person{}}, nil }}
	groups, err := scopes.GroupsForAgent(context.Background(), "unmapped", "acme/pay", "base123")
	require.NoError(t, err)
	require.Empty(t, groups)
}

func TestPolicyFileApprovalRequiresCapturedPolicyOwnerOrGroupApprover(t *testing.T) {
	reader := &codeSpy{body: []byte("policy")}
	authorize := PolicyFileApproval{Code: reader, Decode: func([]byte) (policy.Policy, error) {
		return policy.Policy{PolicyOwners: []string{"owner"}, Approvers: map[string][]string{"payments": {"payment-approver"}}}, nil
	}}
	doc := FileDoc{Repository: "acme/pay", SHA: "base123", Group: "payments"}
	require.NoError(t, authorize.ApproveKnowledgeFile(context.Background(), "payment-approver", doc))
	require.Equal(t, []string{"acme/pay", "base123", "heimdall.yaml"}, reader.args)
	require.ErrorContains(t, authorize.ApproveKnowledgeFile(context.Background(), "other", doc), "not a policy owner")
}

type codeSpy struct {
	body []byte
	args []string
}

func (c *codeSpy) ReadFile(_ context.Context, repo change.Repo, sha id.SHA, path string) ([]byte, error) {
	c.args = []string{string(repo), string(sha), path}
	return c.body, nil
}
