package people

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

func TestApplyMappingAddsNewPersonWithSenderID(t *testing.T) {
	p := specPolicy(t)
	got, err := ApplyMapping(p, Mapping{Name: "rev-backend", Chat: "rev-backend-id"})
	require.NoError(t, err)
	require.Equal(t, policy.Person{Chat: "rev-backend-id"}, got.People["rev-backend"])
	require.NoError(t, got.Validate())

	d, err := New(got)
	require.NoError(t, err)
	name, ok := d.HumanName(ChannelChat, "rev-backend-id")
	require.True(t, ok)
	require.Equal(t, "rev-backend", name)
}

func TestApplyMappingFillsEmptySenderIDAndKeepsGitHub(t *testing.T) {
	p := specPolicy(t)
	p.People["zoe"] = policy.Person{GitHub: "zoe"}
	got, err := ApplyMapping(p, Mapping{Name: "zoe", Chat: "zoe-switch"})
	require.NoError(t, err)
	require.Equal(t, policy.Person{GitHub: "zoe", Chat: "zoe-switch"}, got.People["zoe"])
}

func TestApplyMappingSameSenderIDIsNoChange(t *testing.T) {
	p := specPolicy(t)
	got, err := ApplyMapping(p, Mapping{Name: "alice", Chat: "alice"})
	require.NoError(t, err)
	require.True(t, policy.Equal(p, got))
}

func TestApplyMappingRefusesSenderIDOfAnotherPerson(t *testing.T) {
	p := specPolicy(t)
	_, err := ApplyMapping(p, Mapping{Name: "rev-backend", Chat: "alice"})
	require.ErrorIs(t, err, ErrDuplicate)
}

func TestApplyMappingRefusesSecondSenderIDForSamePerson(t *testing.T) {
	p := specPolicy(t)
	_, err := ApplyMapping(p, Mapping{Name: "alice", Chat: "alice-2"})
	require.ErrorIs(t, err, ErrAlreadySet, "change a mapping by editing heimdall.yaml, not by a second entry")
}

func TestApplyMappingRefusesGitHubLoginOfAnotherPersonAnyCase(t *testing.T) {
	p := specPolicy(t)
	_, err := ApplyMapping(p, Mapping{Name: "rev-backend", GitHub: "ALICE"})
	require.ErrorIs(t, err, ErrDuplicate)
}

func TestApplyMappingRefusesSecondGitHubLoginForSamePerson(t *testing.T) {
	p := specPolicy(t)
	_, err := ApplyMapping(p, Mapping{Name: "alice", GitHub: "alice-2"})
	require.ErrorIs(t, err, ErrAlreadySet)
}

func TestApplyMappingRefusesAgentNameOfAnotherPerson(t *testing.T) {
	p := specPolicy(t)
	_, err := ApplyMapping(p, Mapping{Name: "dana", Agent: "bob-reviewer"})
	require.ErrorIs(t, err, ErrDuplicate)
}

func TestApplyMappingRefusesSenderIDThatIsAnAgentName(t *testing.T) {
	p := specPolicy(t)
	_, err := ApplyMapping(p, Mapping{Name: "rev-backend", Chat: "bob-reviewer"})
	require.ErrorIs(t, err, ErrAmbiguous)
}

func TestApplyMappingRefusesUnknownRoleInMapping(t *testing.T) {
	p := specPolicy(t)
	_, err := ApplyMapping(p, Mapping{Name: "dana", Roles: []Role{Role("admin")}})
	require.ErrorIs(t, err, ErrUnknownRole)
}

func TestParseRoleAcceptsOnlyTheSettableRoles(t *testing.T) {
	for _, s := range []string{"member", "author", "approver"} {
		r, err := ParseRole(s)
		require.NoError(t, err, s)
		require.Equal(t, Role(s), r)
	}
	for _, s := range []string{"admin", "", "Author", "policy_owner"} {
		_, err := ParseRole(s)
		require.ErrorIs(t, err, ErrUnknownRole, "role %q", s)
	}
}

func TestApplyMappingAuthorNeedsGitHub(t *testing.T) {
	p := specPolicy(t)
	_, err := ApplyMapping(p, Mapping{Name: "rev-backend", Chat: "rev-backend-id", Roles: []Role{RoleAuthor}})
	require.ErrorIs(t, err, ErrGitHubRequired)
}

func TestApplyMappingAuthorWithGitHubIsAccepted(t *testing.T) {
	p := specPolicy(t)
	got, err := ApplyMapping(p, Mapping{Name: "rev-backend", Chat: "rev-backend-id", GitHub: "rev-backend", Roles: []Role{RoleAuthor}})
	require.NoError(t, err)
	require.Equal(t, "rev-backend", got.People["rev-backend"].GitHub)
}

func TestApplyMappingApproverNeedsAnApproverGroup(t *testing.T) {
	p := specPolicy(t)
	_, err := ApplyMapping(p, Mapping{Name: "dana", Roles: []Role{RoleApprover}})
	require.ErrorIs(t, err, ErrNotApprover, "the approver role comes from the approvers list, not from this command")
}

func TestApplyMappingApproverWithGroupIsAccepted(t *testing.T) {
	p := specPolicy(t)
	got, err := ApplyMapping(p, Mapping{Name: "alice", Roles: []Role{RoleApprover, RoleMember}})
	require.NoError(t, err)
	require.True(t, policy.Equal(p, got))
}

func TestApplyMappingRefusesBadInputsWithoutChangingPolicy(t *testing.T) {
	cases := map[string]Mapping{
		"name with a space":        {Name: "Dana Smith", Chat: "x-1"},
		"sender id with space":     {Name: "dana", Chat: "da na"},
		"github with a dash first": {Name: "dana", GitHub: "-dana"},
		"github too long":          {Name: "dana", GitHub: "abcdefghijklmnopqrstuvwxyzabcdefghijklmn"},
		"agent not valid":          {Name: "dana", Agent: "Bob Reviewer"},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			p := specPolicy(t)
			got, err := ApplyMapping(p, m)
			require.Error(t, err)
			require.Empty(t, got.People, "a refused mapping returns no policy")
			require.True(t, policy.Equal(specPolicy(t), p), "the caller's policy is unchanged")
		})
	}
}

// TestApplyMappingDoesNotChangeTheCallersPolicy: the people map is shared through the caller's value, so the
// result must be a copy.
func TestApplyMappingDoesNotChangeTheCallersPolicy(t *testing.T) {
	p := specPolicy(t)
	before := p.People["dana"]
	_, err := ApplyMapping(p, Mapping{Name: "rev-backend", Chat: "rev-backend-id"})
	require.NoError(t, err)
	require.Equal(t, before, p.People["dana"])
	_, ok := p.People["rev-backend"]
	require.False(t, ok, "the caller's map must not gain the new person")
}
