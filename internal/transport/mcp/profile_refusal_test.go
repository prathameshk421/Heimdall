package mcp

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// profilerRig is the fixed world with the profile lifecycle wired to a fake that answers Propose with err.
func profilerRig(t *testing.T, proposeErr error) (*rig, *fakeProfiles) {
	t.Helper()
	profiles := &fakeProfiles{proposeErr: proposeErr}
	r := newRig(t, workflow.RoleProfiler)
	r.server.deps.Profiles = profiles
	tools, err := r.server.buildTools()
	require.NoError(t, err)
	r.server.tools = tools
	return r, profiles
}

// Found live on Nov 21: the profiler sent an artifact with an unknown key and saw only "the server could not
// complete this call", so it retried the same artifact. An artifact problem is the agent's own input: name it.
func TestAProfileArtifactThatDoesNotParseIsRefusedWithTheParserReason(t *testing.T) {
	t.Parallel()
	parserErr := &ports.InvalidProfileError{Reason: errors.New("decode repo profile: " +
		"yaml: unmarshal errors:\n  line 10: field evidence not found in type profileyaml.RepoProfile")}
	r, profiles := profilerRig(t, parserErr)
	res := r.call("propose_profile", `{"artifact":"languages: [go]\nevidence: []"}`)
	require.True(t, res.IsError, res.text())
	text := res.text()
	require.Contains(t, text, "line 10: field evidence not found in type profileyaml.RepoProfile")
	require.Contains(t, text, "propose_profile again", "the agent is told what to do next")
	require.NotContains(t, text, "\n", "a refusal is one line")
	require.NotContains(t, text, genericFailure)
	require.Equal(t, 1, profiles.proposals, "the attempt reached the app once")
	require.Empty(t, r.reviews.proposals, "a refused artifact records no proposal on the change")
}

// A store or infrastructure failure is not the agent's input: it stays generic and its cause stays in the log.
func TestAProfileStoreFailureStaysGeneric(t *testing.T) {
	t.Parallel()
	r, profiles := profilerRig(t, errors.New("put profile: postgres: connection refused"))
	res := r.call("propose_profile", `{"artifact":"languages: [go]"}`)
	require.True(t, res.IsError)
	require.Equal(t, genericFailure, res.text())
	require.Equal(t, 1, profiles.proposals)
	require.Empty(t, r.reviews.proposals)
	require.Contains(t, r.logs.String(), "postgres: connection refused", "the cause is logged")
}

// The parser can name a long YAML problem; the refusal stays small so the agent's context is not spent on it.
func TestAProfileRefusalIsBoundedTo500Bytes(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("line 10: field evidence not found in type profileyaml.RepoProfile; ", 40)
	r, _ := profilerRig(t, &ports.InvalidProfileError{Reason: errors.New(long)})
	res := r.call("propose_profile", `{"artifact":"languages: [go]"}`)
	require.True(t, res.IsError)
	require.Contains(t, res.text(), "The profile artifact is not valid: ")
	require.Contains(t, res.text(), "... Fix it and call propose_profile again.", "the reason is cut, the sentence is whole")
	require.LessOrEqual(t, len(res.text()), 580, "500 bytes of reason plus the fixed sentence")
	require.NotContains(t, res.text(), "\n")
}
