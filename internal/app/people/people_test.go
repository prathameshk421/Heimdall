package people

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// specPolicy reads the spec example that validates (TECH_SPEC 16), so the fixture is the one the policy package tests.
func specPolicy(t *testing.T) policy.Policy {
	t.Helper()
	raw, err := os.ReadFile("../../domain/policy/testdata/policy.golden.yaml")
	require.NoError(t, err)
	var p policy.Policy
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	require.NoError(t, dec.Decode(&p))
	require.NoError(t, p.Validate())
	return p
}

func newDir(t *testing.T) *Directory {
	t.Helper()
	d, err := New(specPolicy(t))
	require.NoError(t, err)
	return d
}

func TestResolveMapsChatSenderIDWholeAndExactly(t *testing.T) {
	d := newDir(t)
	ident, err := d.Resolve(ChannelChat, "dana")
	require.NoError(t, err)
	require.Equal(t, KindHuman, ident.Kind)
	require.Equal(t, "dana", ident.Name)
	require.Equal(t, "dana", ident.GitHub)
	require.Empty(t, ident.Groups)
	require.Equal(t, []Role{RoleMember}, ident.Roles)

	for _, sender := range []string{"Dana", "dana ", " dana", "dan", ""} {
		_, err := d.Resolve(ChannelChat, sender)
		require.ErrorIs(t, err, ErrUnknownSender, "sender %q must not match dana", sender)
	}
}

func TestResolveGitHubLoginIgnoresCase(t *testing.T) {
	d := newDir(t)
	ident, err := d.Resolve(ChannelGitHub, "ALICE")
	require.NoError(t, err)
	require.Equal(t, "alice", ident.Name)
	require.Equal(t, KindHuman, ident.Kind)
}

func TestResolveReportsApproverGroupsAndPolicyOwner(t *testing.T) {
	d := newDir(t)

	alice, err := d.Resolve(ChannelChat, "alice")
	require.NoError(t, err)
	require.Equal(t, []string{"default", "money_movement"}, alice.Groups, "groups are sorted")
	require.Equal(t, []Role{RoleMember, RoleApprover}, alice.Roles)

	ana, err := d.Resolve(ChannelChat, "ana")
	require.NoError(t, err)
	require.Equal(t, []string{"auth"}, ana.Groups)
	require.Equal(t, []Role{RoleMember, RoleApprover, RolePolicyOwner}, ana.Roles)
	require.Equal(t, "ana-reviewer", ana.Agent)
}

func TestResolveSaysAgentWhenSenderIsAnAgentName(t *testing.T) {
	d := newDir(t)
	ident, err := d.Resolve(ChannelChat, "bob-reviewer")
	require.NoError(t, err)
	require.Equal(t, KindAgent, ident.Kind)
	require.Equal(t, "bob-reviewer", ident.Name)
	require.Empty(t, ident.Roles, "an agent holds no person role")
}

func TestHumanForAgentMapsReviewerAgentToItsPerson(t *testing.T) {
	d := newDir(t)
	owner, ok := any(d).(interface{ HumanForAgent(string) (string, bool) })
	require.True(t, ok, "the directory must resolve the human responsible for a reviewer agent")
	name, found := owner.HumanForAgent("ana-reviewer")
	require.True(t, found)
	require.Equal(t, "ana", name)
	_, found = owner.HumanForAgent("not-mapped")
	require.False(t, found)
}

func TestResolveRefusesUnknownChannelAndGitHubLogin(t *testing.T) {
	d := newDir(t)
	_, err := d.Resolve(Channel("sms"), "dana")
	require.ErrorIs(t, err, ErrUnknownSender)
	_, err = d.Resolve(ChannelGitHub, "ghost")
	require.ErrorIs(t, err, ErrUnknownSender)
}

func TestHumanNameIsTrueOnlyForHumans(t *testing.T) {
	d := newDir(t)
	name, ok := d.HumanName(ChannelChat, "dana")
	require.True(t, ok)
	require.Equal(t, "dana", name)

	_, ok = d.HumanName(ChannelChat, "bob-reviewer")
	require.False(t, ok, "an agent is never a person to the parser")

	_, ok = d.HumanName(ChannelChat, "nobody")
	require.False(t, ok)

	_, ok = d.HumanName(Channel("sms"), "dana")
	require.False(t, ok)
}

func TestPersonWithoutGitHubIsMappedAndHasNoLogin(t *testing.T) {
	p := specPolicy(t)
	p.People["rev-backend"] = policy.Person{Chat: "rev-backend-id"}
	d, err := New(p)
	require.NoError(t, err)

	ident, err := d.Resolve(ChannelChat, "rev-backend-id")
	require.NoError(t, err)
	require.Equal(t, "rev-backend", ident.Name)
	require.Empty(t, ident.GitHub, "github is optional for a reviewer (D158)")
}

func TestNewRefusesSenderIDThatTwoPeopleShare(t *testing.T) {
	p := specPolicy(t)
	p.People["dana"] = policy.Person{GitHub: "dana", Chat: "alice"}
	_, err := New(p)
	require.ErrorIs(t, err, ErrDuplicate)
}

func TestNewRefusesSenderIDThatIsAnAgentName(t *testing.T) {
	p := specPolicy(t)
	p.People["dana"] = policy.Person{GitHub: "dana", Chat: "bob-reviewer"}
	_, err := New(p)
	require.ErrorIs(t, err, ErrAmbiguous, "an agent name must never also be a person's sender id")
}

func TestNewRefusesSenderIDOutsidePrintableASCII(t *testing.T) {
	p := specPolicy(t)
	p.People["dana"] = policy.Person{GitHub: "dana", Chat: "da na"}
	_, err := New(p)
	require.Error(t, err)
}

func TestNewRefusesPolicyThatDoesNotValidate(t *testing.T) {
	p := specPolicy(t)
	p.Version = 2
	_, err := New(p)
	require.Error(t, err)
}
