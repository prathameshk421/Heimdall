package policyyaml_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

const fixturePath = "../../../testdata/repos/pay-demo/base/heimdall.yaml"

func TestDecodeAcceptsTheProjectFixture(t *testing.T) {
	raw, err := os.ReadFile(fixturePath)
	require.NoError(t, err)
	p, err := policyyaml.Decode(raw)
	require.NoError(t, err)
	require.Equal(t, "tests/heimdall", p.Tests.Dir)
	require.True(t, p.Slop.RequireTicket)
}

func TestDecodeNamesEachWayItCanBeWrong(t *testing.T) {
	fixture, err := os.ReadFile(fixturePath)
	require.NoError(t, err)
	cases := map[string]struct {
		raw  string
		want string
	}{
		"empty":        {"", "is empty"},
		"only comment": {"# nothing here\n", "is empty"},
		"unknown key":  {"nope: 1\n" + string(fixture), "does not decode"},
		"bad yaml":     {"slop: [unclosed", "does not decode"},
		"invalid":      {strings.Replace(string(fixture), "dir: tests/heimdall", "dir: .", 1), "is invalid"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := policyyaml.Decode([]byte(c.raw))
			require.Error(t, err)
			require.Contains(t, err.Error(), c.want)
		})
	}
}

// v2Fixture is the project fixture at version 2, the seats shape.
func v2Fixture(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(fixturePath)
	require.NoError(t, err)
	return string(raw)
}

func asVersion(body string, v string) string {
	return strings.Replace(body, "version: 2\n", "version: "+v+"\n", 1)
}

func TestDecodeReadsVersionTwoAsItIs(t *testing.T) {
	p, err := policyyaml.Decode([]byte(v2Fixture(t)))
	require.NoError(t, err)
	require.Equal(t, policy.CurrentVersion, p.Version)
	require.Equal(t, 2, p.Version)
	require.Equal(t, "bob-reviewer", p.People["bob"].Seat)
}

func TestDecodeConvertsVersionOneWithAgentToSeats(t *testing.T) {
	v1 := strings.ReplaceAll(asVersion(v2Fixture(t), "1"), "seat: ", "agent: ")
	p, err := policyyaml.Decode([]byte(v1))
	require.NoError(t, err)
	require.Equal(t, 2, p.Version, "a converted policy is the current version")
	require.Equal(t, "bob-reviewer", p.People["bob"].Seat)
	require.Equal(t, "ana-reviewer", p.People["ana"].Seat)
	want, err := policyyaml.Decode([]byte(v2Fixture(t)))
	require.NoError(t, err)
	require.True(t, policy.Equal(want, p), "version 1 with agent says the same as version 2 with seat")
}

func TestDecodeReadsVersionOneThatAlreadySaysSeat(t *testing.T) {
	// Between the seats rename (M0 step 7) and the version bump, files said version 1 with seat. Bases with that
	// shape exist, so they stay readable.
	p, err := policyyaml.Decode([]byte(asVersion(v2Fixture(t), "1")))
	require.NoError(t, err)
	require.Equal(t, 2, p.Version)
	require.Equal(t, "eli-reviewer", p.People["eli"].Seat)
}

func TestDecodeRefusesWhatNoVersionSays(t *testing.T) {
	body := v2Fixture(t)
	cases := map[string]struct {
		raw  string
		want string
	}{
		"unknown version":            {asVersion(body, "3"), "version 3 is not known"},
		"no version":                 {strings.Replace(body, "version: 2\n", "", 1), "version is required"},
		"version is not a number":    {asVersion(body, "two"), "version"},
		"agent in version 2":         {strings.Replace(body, "seat: bob-reviewer", "agent: bob-reviewer", 1), "field agent not found"},
		"agent and seat disagree v1": {strings.Replace(asVersion(body, "1"), "seat: bob-reviewer", "agent: bob-x, seat: bob-reviewer", 1), "people.bob"},
		"not a mapping":              {"- version: 2\n", "does not decode"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := policyyaml.Decode([]byte(c.raw))
			require.ErrorContains(t, err, c.want)
			require.ErrorIs(t, err, policy.ErrInvalid, "every policy that cannot be used is ErrInvalid, so callers can tell it from I/O")
		})
	}
}

func TestEveryDecodeFailureIsErrInvalid(t *testing.T) {
	for _, raw := range []string{"", "nope: 1\n", "slop: [unclosed"} {
		_, err := policyyaml.Decode([]byte(raw))
		require.ErrorIs(t, err, policy.ErrInvalid, raw)
	}
}
