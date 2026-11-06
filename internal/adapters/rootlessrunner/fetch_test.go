package rootlessrunner_test

import (
	"context"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/rootlessrunner"
	"github.com/stretchr/testify/require"
)

func TestFetcherRejectsDisallowedHostsBeforeAnyNetworkRequest(t *testing.T) {
	fetcher := rootlessrunner.Fetcher{}
	profile := rootlessrunner.RepoProfile{FetchHosts: []string{"proxy.golang.org"}}
	for _, raw := range []string{"https://evil.example/pkg.zip", "http://proxy.golang.org/pkg.zip", "https://proxy.golang.org:8443/pkg.zip", "https://user@proxy.golang.org/pkg.zip"} {
		_, err := fetcher.Fetch(context.Background(), profile.FetchHosts, raw)
		require.Error(t, err, "URL %s must be refused before DNS or network access", raw)
	}
}
