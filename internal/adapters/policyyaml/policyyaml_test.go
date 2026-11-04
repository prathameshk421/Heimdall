package policyyaml_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
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
