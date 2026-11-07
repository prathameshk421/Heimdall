package rootlessrunner_test

import (
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/rootlessrunner"
	"github.com/stretchr/testify/require"
)

func TestProfileApprovalRejectsUnpinnedImageAndUnsafeFetchHost(t *testing.T) {
	image := "registry.example/go@sha256:" + strings.Repeat("a", 64)
	valid := "languages: [go]\nimage: " + image + "\nacceptance: 'go test {dir}'\nseam: api.Handler\nac_tag: '// heimdall:{ac}'\npaths:\n  tests: tests/heimdall\n"
	_, err := rootlessrunner.ParseApprovedProfile([]byte(valid), []string{image})
	require.NoError(t, err)

	_, err = rootlessrunner.ParseApprovedProfile([]byte(strings.Replace(valid, image, "registry.example/go:latest", 1)), []string{image})
	require.ErrorIs(t, err, rootlessrunner.ErrImageNotAllowed)

	unsafe := strings.Replace(valid, "paths:\n", "fetch_hosts: ['*.example']\npaths:\n", 1)
	_, err = rootlessrunner.ParseApprovedProfile([]byte(unsafe), []string{image})
	require.Error(t, err)
}
