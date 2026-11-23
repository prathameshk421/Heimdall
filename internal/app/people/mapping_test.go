package people

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

func TestApplyMappingAccepts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		m    Mapping
		want policy.Person
	}{
		{"new person with a sender id", Mapping{Name: "rev-backend", Chat: "rev-backend-id"}, policy.Person{Chat: "rev-backend-id"}},
		{"author with a github login", Mapping{Name: "rev-backend", Chat: "rev-backend-id", GitHub: "rev-backend", Roles: []Role{RoleAuthor}},
			policy.Person{Chat: "rev-backend-id", GitHub: "rev-backend"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := specPolicy(t)
			got, err := ApplyMapping(p, tc.m)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.People[tc.m.Name])
			require.NoError(t, got.Validate())
		})
	}

	t.Run("fills an empty sender id and keeps github", func(t *testing.T) {
		t.Parallel()
		p := specPolicy(t)
		p.People["zoe"] = policy.Person{GitHub: "zoe"}
		got, err := ApplyMapping(p, Mapping{Name: "zoe", Chat: "zoe-switch"})
		require.NoError(t, err)
		require.Equal(t, policy.Person{GitHub: "zoe", Chat: "zoe-switch"}, got.People["zoe"])
	})

	t.Run("the same sender id is no change", func(t *testing.T) {
		t.Parallel()
		p := specPolicy(t)
		got, err := ApplyMapping(p, Mapping{Name: "alice", Chat: "alice"})
		require.NoError(t, err)
		require.True(t, policy.Equal(p, got))
	})

	t.Run("an approver of a group is accepted", func(t *testing.T) {
		t.Parallel()
		p := specPolicy(t)
		got, err := ApplyMapping(p, Mapping{Name: "alice", Roles: []Role{RoleApprover, RoleMember}})
		require.NoError(t, err)
		require.True(t, policy.Equal(p, got))
	})

	t.Run("a new person resolves through the directory", func(t *testing.T) {
		t.Parallel()
		got, err := ApplyMapping(specPolicy(t), Mapping{Name: "rev-backend", Chat: "rev-backend-id"})
		require.NoError(t, err)
		d, err := New(got)
		require.NoError(t, err)
		name, ok := d.HumanName(ChannelChat, "rev-backend-id")
		require.True(t, ok)
		require.Equal(t, "rev-backend", name)
	})
}

// Every refused mapping names its reason and leaves the caller's policy alone.
func TestApplyMappingRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		m    Mapping
		want error
	}{
		{"sender id of another person", Mapping{Name: "rev-backend", Chat: "alice"}, ErrDuplicate},
		{"second sender id for the same person", Mapping{Name: "alice", Chat: "alice-2"}, ErrAlreadySet},
		{"github login of another person, any case", Mapping{Name: "rev-backend", GitHub: "ALICE"}, ErrDuplicate},
		{"second github login for the same person", Mapping{Name: "alice", GitHub: "alice-2"}, ErrAlreadySet},
		{"agent name of another person", Mapping{Name: "dana", Agent: "bob-reviewer"}, ErrDuplicate},
		{"sender id that is an agent name", Mapping{Name: "rev-backend", Chat: "bob-reviewer"}, ErrAmbiguous},
		{"unknown role", Mapping{Name: "dana", Roles: []Role{Role("admin")}}, ErrUnknownRole},
		{"author without github", Mapping{Name: "rev-backend", Chat: "rev-backend-id", Roles: []Role{RoleAuthor}}, ErrGitHubRequired},
		{"approver without an approver group", Mapping{Name: "dana", Roles: []Role{RoleApprover}}, ErrNotApprover},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ApplyMapping(specPolicy(t), tc.m)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestParseRoleAcceptsOnlyTheSettableRoles(t *testing.T) {
	t.Parallel()
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

func TestApplyMappingRefusesBadInputsWithoutChangingPolicy(t *testing.T) {
	t.Parallel()
	cases := map[string]Mapping{
		"name with a space":        {Name: "Dana Smith", Chat: "x-1"},
		"sender id with space":     {Name: "dana", Chat: "da na"},
		"github with a dash first": {Name: "dana", GitHub: "-dana"},
		"github too long":          {Name: "dana", GitHub: "abcdefghijklmnopqrstuvwxyzabcdefghijklmn"},
		"agent not valid":          {Name: "dana", Agent: "Bob Reviewer"},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
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
	t.Parallel()
	p := specPolicy(t)
	before := p.People["dana"]
	_, err := ApplyMapping(p, Mapping{Name: "rev-backend", Chat: "rev-backend-id"})
	require.NoError(t, err)
	require.Equal(t, before, p.People["dana"])
	_, ok := p.People["rev-backend"]
	require.False(t, ok, "the caller's map must not gain the new person")
}
