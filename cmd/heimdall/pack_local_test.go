package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunPackDispatchesRefreshAndReviewWithLocalProfile(t *testing.T) {
	for _, command := range []string{"refresh", "review"} {
		t.Run(command, func(t *testing.T) {
			runner := &packRuntimeSpy{}
			var out strings.Builder
			code := runPackWith([]string{command, "--profile", "private-pack.toml"}, strings.NewReader("approve\n"), &out, runner)
			require.Equal(t, 0, code, out.String())
			require.Equal(t, command, runner.command)
			require.Equal(t, "private-pack.toml", runner.profile)
		})
	}
}

func TestRunPackNamesMissingProfileSetup(t *testing.T) {
	var out strings.Builder
	code := runPack([]string{"refresh", "--profile", "missing-pack-profile.toml"}, strings.NewReader(""), &out)
	require.Equal(t, 1, code)
	require.Contains(t, out.String(), "missing-pack-profile.toml")
}

func TestPackProfileRefusesUnknownFieldsAndNamesMissingSetup(t *testing.T) {
	profile, err := loadPackProfile("pack_profile_unknown.toml")
	require.ErrorContains(t, err, "pack_profile_unknown.toml")
	require.Empty(t, profile.Person)
	require.ErrorContains(t, validatePackProfile(localPackProfile{}), "secrets_dir")
}

type packRuntimeSpy struct {
	command string
	profile string
}

func (s *packRuntimeSpy) Run(_ context.Context, command, profile string, _ io.Reader, _ io.Writer) error {
	s.command, s.profile = command, profile
	return nil
}
